# 第5章 コメント・投票・報告

この章は、この教材でいちばん神経を使う部分です。コメントの木、投票の競合、そして報告を扱います。

## 5.1 コメントと木の組み立て

コメントは、投稿への直接の返信と、コメントへの返信（親コメントを指定）で作ります。規則は次のとおりです。

- 本文は必須で、空白だけは不可。長さは 10,000 文字まで。
- 返信先がある場合は、**挿入する前に**確かめます: 返信先が存在すること、返信先が同じ投稿に属すること。Python 版では外部キー制約に頼っていた部分を、Go 版では明示的な確認にしました（制約違反の 500 を、理由の分かる 422 に変えるためです）。

一覧は、投稿のコメントを投稿順に全部取り、メモリ上で木に組み立てます（`BuildTree`）。親が見つからないコメントは木に現れません（Python 版と同じ扱い）。表示は深さ 8 までで、それより深い返信はテンプレート側で描かれません。これは仕様上の割り切りとして残っています。

削除は投稿者本人かモデレーターだけが行え、配下の返信は DB の連鎖削除で一緒に消えます。削除後の戻り先は、リクエストの Referer ヘッダーではなく、コメント→投稿→コミュニティを引いて組み立てます。これは Python 版のレビュー対応（「戻り先はデータから作る」）を最初から含んだものです。

```go
// Package comments is the comment service layer, mirroring the Python
// version's app/comments/services.py: creation with reply-target
// checks, the in-memory tree build, voting (the same four patterns as
// posts), and deletion.
package comments

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"

	"redditclone/internal/accounts"
	"redditclone/internal/communities"
)

// Comment is one row of the comments table plus the author's name and,
// after the tree build, its children.
type Comment struct {
	ID             string
	Body           string
	Score          int
	AuthorID       string
	PostID         string
	ParentID       string // "" for a top-level comment
	InsertedAt     time.Time
	AuthorUsername string
	Children       []*Comment
}

// User-facing failures, mirroring the Python version's messages.
var (
	ErrBodyRequired    = errors.New("コメントの本文は必須です")
	ErrBodyTooLong     = errors.New("コメントは10,000文字以内にしてください")
	ErrParentNotFound  = errors.New("返信先のコメントが見つかりません")
	ErrParentWrongPost = errors.New("返信先のコメントがこの投稿に属していません")
	ErrCommentNotFound = errors.New("コメントが見つかりません")
	ErrOwnCommentVote  = errors.New("自分のコメントには投票できません")
	ErrInvalidValue    = errors.New("投票値は 1 または -1 のみ有効です")
	ErrForbidden       = errors.New("削除する権限がありません")
)

const commentColumns = `c.id, c.body, c.score, c.author_id, c.post_id,
	COALESCE(c.parent_id::text, ''), c.inserted_at, a.username`

func scanComment(row interface{ Scan(...any) error }) (Comment, error) {
	var c Comment
	err := row.Scan(&c.ID, &c.Body, &c.Score, &c.AuthorID, &c.PostID,
		&c.ParentID, &c.InsertedAt, &c.AuthorUsername)
	return c, err
}

// CreateComment validates the body and the reply target, then inserts.
// The reply target is checked here (existence and same-post) so a bad
// parent_id never surfaces as a database foreign-key error.
func CreateComment(ctx context.Context, db *sql.DB,
	body, authorID, postID, parentID string) (Comment, error) {

	if strings.TrimSpace(body) == "" {
		return Comment{}, ErrBodyRequired
	}
	if utf8.RuneCountInString(body) > 10000 {
		return Comment{}, ErrBodyTooLong
	}

	var parent any
	if parentID != "" {
		var parentPostID string
		err := db.QueryRowContext(ctx,
			`SELECT post_id FROM comments WHERE id = $1`, parentID).
			Scan(&parentPostID)
		if errors.Is(err, sql.ErrNoRows) {
			return Comment{}, ErrParentNotFound
		}
		if err != nil {
			return Comment{}, err
		}
		if parentPostID != postID {
			return Comment{}, ErrParentWrongPost
		}
		parent = parentID
	}

	comment, err := scanComment(db.QueryRowContext(ctx, `
		INSERT INTO comments (body, author_id, post_id, parent_id)
		VALUES ($1, $2, $3, $4)
		RETURNING id, body, score, author_id, post_id,
		          COALESCE(parent_id::text, ''), inserted_at,
		          (SELECT username FROM users WHERE id = $2)`,
		body, authorID, postID, parent))
	return comment, err
}

// GetComment returns one comment, or (nil, nil).
func GetComment(ctx context.Context, db *sql.DB, id string) (*Comment, error) {
	comment, err := scanComment(db.QueryRowContext(ctx, `
		SELECT `+commentColumns+`
		FROM comments c JOIN users a ON a.id = c.author_id
		WHERE c.id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

// ListComments fetches all comments of a post in one query (oldest
// first) and builds the tree in memory, as the Python version does.
func ListComments(ctx context.Context, db *sql.DB, postID string) ([]*Comment, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT `+commentColumns+`
		FROM comments c JOIN users a ON a.id = c.author_id
		WHERE c.post_id = $1
		ORDER BY c.inserted_at ASC`, postID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var flat []*Comment
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		cc := c
		flat = append(flat, &cc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return BuildTree(flat), nil
}

// BuildTree links a flat list (oldest first) into roots with children.
func BuildTree(flat []*Comment) []*Comment {
	byParent := map[string][]*Comment{}
	for _, c := range flat {
		byParent[c.ParentID] = append(byParent[c.ParentID], c)
	}
	var link func(parentID string) []*Comment
	link = func(parentID string) []*Comment {
		children := byParent[parentID]
		for _, child := range children {
			child.Children = link(child.ID)
		}
		return children
	}
	return link("")
}

// VoteComment applies the same four patterns as posts.VotePost, in one
// transaction (see its comment for the race handling).
func VoteComment(ctx context.Context, db *sql.DB,
	userID, commentID string, value int) (Comment, error) {

	if value != 1 && value != -1 {
		return Comment{}, ErrInvalidValue
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Comment{}, err
	}
	defer tx.Rollback()

	var authorID string
	err = tx.QueryRowContext(ctx,
		`SELECT author_id FROM comments WHERE id = $1`, commentID).Scan(&authorID)
	if errors.Is(err, sql.ErrNoRows) {
		return Comment{}, ErrCommentNotFound
	}
	if err != nil {
		return Comment{}, err
	}
	if authorID == userID {
		return Comment{}, ErrOwnCommentVote
	}

	var existing sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT value FROM votes
		WHERE user_id = $1 AND target_type = 'comment' AND target_id = $2
		FOR UPDATE`, userID, commentID).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Comment{}, err
	}

	applyScore := func(diff int) (Comment, error) {
		var c Comment
		c.ID = commentID
		err := tx.QueryRowContext(ctx, `
			UPDATE comments SET score = score + $1 WHERE id = $2
			RETURNING score`, diff, commentID).Scan(&c.Score)
		return c, err
	}

	var result Comment
	switch {
	case !existing.Valid:
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO votes (user_id, target_type, target_id, value)
			VALUES ($1, 'comment', $2, $3)`, userID, commentID, value); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" &&
				pgErr.ConstraintName == "ix_votes_unique" {
				tx.Rollback()
				current, err := GetComment(ctx, db, commentID)
				if err != nil {
					return Comment{}, err
				}
				if current == nil {
					return Comment{}, ErrCommentNotFound
				}
				return *current, nil
			}
			return Comment{}, err
		}
		if result, err = applyScore(value); err != nil {
			return Comment{}, err
		}
		if err := accounts.UpdateKarma(ctx, tx, authorID, "comment", value); err != nil {
			return Comment{}, err
		}

	case int(existing.Int64) == value:
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM votes
			WHERE user_id = $1 AND target_type = 'comment' AND target_id = $2`,
			userID, commentID); err != nil {
			return Comment{}, err
		}
		if result, err = applyScore(-value); err != nil {
			return Comment{}, err
		}
		if err := accounts.UpdateKarma(ctx, tx, authorID, "comment", -value); err != nil {
			return Comment{}, err
		}

	default:
		diff := value - int(existing.Int64)
		if _, err := tx.ExecContext(ctx, `
			UPDATE votes SET value = $3
			WHERE user_id = $1 AND target_type = 'comment' AND target_id = $2`,
			userID, commentID, value); err != nil {
			return Comment{}, err
		}
		if result, err = applyScore(diff); err != nil {
			return Comment{}, err
		}
		if err := accounts.UpdateKarma(ctx, tx, authorID, "comment", diff); err != nil {
			return Comment{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Comment{}, err
	}
	return result, nil
}

// DeleteComment removes a comment (its subtree follows by CASCADE).
// Only the author or a moderator of the subreddit may delete.
func DeleteComment(ctx context.Context, db *sql.DB,
	commentID, userID, subredditID string) error {

	comment, err := GetComment(ctx, db, commentID)
	if err != nil {
		return err
	}
	if comment == nil {
		return ErrCommentNotFound
	}
	moderator, err := communities.IsModerator(ctx, db, userID, subredditID)
	if err != nil {
		return err
	}
	if comment.AuthorID != userID && !moderator {
		return ErrForbidden
	}
	_, err = db.ExecContext(ctx, `DELETE FROM comments WHERE id = $1`, commentID)
	return err
}

// Context is where a comment lives: the post and subreddit to return
// to after deletion (built from the data, never from a Referer header).
type Context struct {
	PostID        string
	SubredditID   string
	SubredditName string
}

// GetContext resolves a comment's post and subreddit, or (nil, nil).
func GetContext(ctx context.Context, db *sql.DB, commentID string) (*Context, error) {
	var out Context
	err := db.QueryRowContext(ctx, `
		SELECT p.id, p.subreddit_id, s.name
		FROM comments c
		JOIN posts p ON p.id = c.post_id
		JOIN subreddits s ON s.id = p.subreddit_id
		WHERE c.id = $1`, commentID).
		Scan(&out.PostID, &out.SubredditID, &out.SubredditName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}
```

## 5.2 投票: 1つのトランザクションで全部やる

投票は、見た目こそ小さい機能ですが、この教材全体で最も競合に弱い場所でした。外部レビューでは、Python 版で「同時に投票するとスコアがずれる」「二度押しで 500 になる」が実際に再現されています。Go 版の設計は、その教訓から決めました。

処理の流れは、**最初から最後まで1つのトランザクション**です。

1. 対象（投稿またはコメント）を取る。
2. 自分の既存の票を `SELECT ... FOR UPDATE` で取る（行に鍵を掛け、同時の投票同士が重ならないようにする）。
3. 状況で分岐します。
   - 新しい票: 入れて、差分は +票の値。
   - 同じ票がもうある: 票を取り消し（削除）、差分は −票の値（トグルの動き）。
   - 反対の票がある: 票を差し替え、差分は 新−旧（±2）。
   - 自分のものへの投票: ここで弾きます。
4. スコアの更新は、読み出して計算して書き戻すのではなく、**SQL の側で差分を当てる1文**で行います。Hot スコアも同じ1文の中で、符号つきの式で再計算します。
5. 投稿者（コメントの場合はコメント主）の Karma も、同じトランザクションの中で差分更新します。

二重クリックへの備えも、この形の中にあります。新規の票の挿入が一意制約に触れた場合（ほぼ同時に同じ票が入った場合）は、トランザクションごと巻き戻し、**現在の状態を読み直して成功として返します**。票は1件だけ、スコアも1回分だけ、という結果にそろいます。Go の `database/sql` では制約違反でトランザクションが使えなくなるため、Python 版の「SAVEPOINT で部分的に巻き戻す」手が使えません。その代わりが「全部を巻き戻して読み直す」です。

検証も実物で重ねています。同時に20人が投票すればスコアはちょうど20、Karma も+20、500 はゼロ。票の付け替えを同時に行っても、最後は「スコア = 票の合計」に一致します（第7章）。

画面側の細部も Python 版に揃えました。投票のエラーの断片は、Python の `html.escape` と同じ出力になるよう専用の置換（`pyEscape`）を使っています。Go 標準のエスケープは引用符の扱いが1文字違い（`&#39;` と `&#x27;`）で、E2E の文字列比較で実際に引っかかった場所です。

```go
package posts

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"

	"redditclone/internal/accounts"
	"redditclone/internal/communities"
)

// User-facing failures of the vote/delete services, mirroring the
// Python version's ValueError / PermissionError messages.
var (
	ErrInvalidVoteValue = errors.New("投票値は 1 または -1 のみ有効です")
	ErrPostNotFound     = errors.New("投稿が見つかりません")
	ErrOwnPostVote      = errors.New("自分の投稿には投票できません")
	ErrForbidden        = errors.New("削除する権限がありません")
)

func isVoteUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" &&
		pgErr.ConstraintName == "ix_votes_unique"
}

// updatePostScoreTx applies the score diff on the SQL side, with the
// hot score recomputed by the same formula (sign kept), and returns
// the values the database settled on.
func updatePostScoreTx(ctx context.Context, tx *sql.Tx,
	postID string, diff int) (int, float64, error) {

	var score int
	var hot float64
	err := tx.QueryRowContext(ctx, `
		UPDATE posts SET
		  score = score + $1,
		  hot_score =
		    sign(score + $1)
		    * log(10, greatest(abs(score + $1), 1))
		    + extract(epoch FROM inserted_at
		        - TIMESTAMPTZ '2005-12-08 07:46:43+00') / 45000.0
		WHERE id = $2
		RETURNING score, hot_score`, diff, postID).Scan(&score, &hot)
	return score, hot, err
}

// VotePost casts, toggles or flips the user's vote on a post, in one
// transaction, mirroring vote_post in the Python version:
//
//   - no existing vote: insert it (pattern A)
//   - same value again: remove it (pattern B, toggle off)
//   - opposite value: change it (pattern C)
//   - own post: an error (pattern D)
//
// The existing vote row is read FOR UPDATE so a concurrent change or
// removal cannot interleave with the decision. A brand-new vote that
// loses an insert race against the unique index reports the post as
// it now stands, without touching the score twice — the Python
// version reaches the same outcome through a SAVEPOINT; here the
// whole (small) transaction is rolled back instead.
func VotePost(ctx context.Context, db *sql.DB,
	userID, postID string, value int) (Post, error) {

	if value != 1 && value != -1 {
		return Post{}, ErrInvalidVoteValue
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Post{}, err
	}
	defer tx.Rollback()

	var authorID string
	var post Post
	err = tx.QueryRowContext(ctx,
		`SELECT author_id FROM posts WHERE id = $1`, postID).Scan(&authorID)
	if errors.Is(err, sql.ErrNoRows) {
		return Post{}, ErrPostNotFound
	}
	if err != nil {
		return Post{}, err
	}
	if authorID == userID {
		return Post{}, ErrOwnPostVote
	}

	var existing sql.NullInt64
	err = tx.QueryRowContext(ctx, `
		SELECT value FROM votes
		WHERE user_id = $1 AND target_type = 'post' AND target_id = $2
		FOR UPDATE`, userID, postID).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Post{}, err
	}

	switch {
	case !existing.Valid:
		// Pattern A: new vote.
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO votes (user_id, target_type, target_id, value)
			VALUES ($1, 'post', $2, $3)`, userID, postID, value); err != nil {
			if isVoteUniqueViolation(err) {
				// A concurrent identical vote won. Report the post as
				// it stands now, in a fresh read after rollback.
				tx.Rollback()
				current, err := GetPost(ctx, db, postID)
				if err != nil {
					return Post{}, err
				}
				if current == nil {
					return Post{}, ErrPostNotFound
				}
				return *current, nil
			}
			return Post{}, err
		}
		if err := applyPostScore(ctx, tx, &post, postID, value); err != nil {
			return Post{}, err
		}
		if err := accounts.UpdateKarma(ctx, tx, authorID, "post", value); err != nil {
			return Post{}, err
		}

	case int(existing.Int64) == value:
		// Pattern B: same direction again cancels the vote.
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM votes
			WHERE user_id = $1 AND target_type = 'post' AND target_id = $2`,
			userID, postID); err != nil {
			return Post{}, err
		}
		if err := applyPostScore(ctx, tx, &post, postID, -value); err != nil {
			return Post{}, err
		}
		if err := accounts.UpdateKarma(ctx, tx, authorID, "post", -value); err != nil {
			return Post{}, err
		}

	default:
		// Pattern C: opposite direction flips the vote.
		diff := value - int(existing.Int64)
		if _, err := tx.ExecContext(ctx, `
			UPDATE votes SET value = $3
			WHERE user_id = $1 AND target_type = 'post' AND target_id = $2`,
			userID, postID, value); err != nil {
			return Post{}, err
		}
		if err := applyPostScore(ctx, tx, &post, postID, diff); err != nil {
			return Post{}, err
		}
		if err := accounts.UpdateKarma(ctx, tx, authorID, "post", diff); err != nil {
			return Post{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return Post{}, err
	}
	return post, nil
}

func applyPostScore(ctx context.Context, tx *sql.Tx,
	post *Post, postID string, diff int) error {

	score, hot, err := updatePostScoreTx(ctx, tx, postID, diff)
	if err != nil {
		return err
	}
	post.ID = postID
	post.Score = score
	post.HotScore = hot
	return nil
}

// DeletePost removes a post. Only the author or a moderator of its
// subreddit may delete. Karma already given is not returned (the same
// rule as Reddit, and as the Python version). Comments are removed by
// the database's CASCADE.
func DeletePost(ctx context.Context, db *sql.DB,
	postID, userID string) error {

	post, err := GetPost(ctx, db, postID)
	if err != nil {
		return err
	}
	if post == nil {
		return ErrPostNotFound
	}
	moderator, err := communities.IsModerator(ctx, db, userID, post.SubredditID)
	if err != nil {
		return err
	}
	if post.AuthorID != userID && !moderator {
		return ErrForbidden
	}
	_, err = db.ExecContext(ctx, `DELETE FROM posts WHERE id = $1`, postID)
	return err
}
```

## 5.3 報告

報告の規則は単純ですが、確認の順序まで Python 版と揃えてあります。

1. 理由（spam / harassment / hate_speech / misinformation / other のいずれか）
2. 対象の種別（post / comment）
3. 対象が存在すること
4. 自分のものではないこと

成功すると緑色の「報告しました」の断片が返ります。理由の選択肢を返す部品（`/components/report-reasons`）は、ログインしていなくても使えます（Python 版と同じ）。

```go
// Package reports is the report service layer, mirroring the Python
// version's app/reports/services.py.
package reports

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ValidReasons are the accepted report reasons, in display order.
var ValidReasons = []string{
	"spam", "harassment", "hate_speech", "misinformation", "other",
}

// ReasonLabels maps each reason to its Japanese label.
var ReasonLabels = map[string]string{
	"spam":           "スパム",
	"harassment":     "嫌がらせ・ハラスメント",
	"hate_speech":    "ヘイトスピーチ",
	"misinformation": "誤情報",
	"other":          "その他",
}

func validReason(reason string) bool {
	for _, r := range ValidReasons {
		if r == reason {
			return true
		}
	}
	return false
}

// CreateReport files a report on a post or comment. A nonexistent
// target and a report on one's own content are errors.
func CreateReport(ctx context.Context, db *sql.DB,
	reporterID, targetType, targetID, reason string) error {

	if !validReason(reason) {
		return fmt.Errorf("無効な報告理由です: %s", reason)
	}
	if targetType != "post" && targetType != "comment" {
		return errors.New("target_type は 'post' または 'comment' のみ有効です")
	}

	table := "posts"
	if targetType == "comment" {
		table = "comments"
	}
	var authorID string
	err := db.QueryRowContext(ctx,
		`SELECT author_id FROM `+table+` WHERE id = $1`, targetID).Scan(&authorID)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("報告対象が見つかりません")
	}
	if err != nil {
		return err
	}
	if authorID == reporterID {
		return errors.New("自分のコンテンツは報告できません")
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO reports (reporter_id, target_type, target_id, reason)
		VALUES ($1, $2, $3, $4)`, reporterID, targetType, targetID, reason)
	return err
}
```

コメントの投稿・削除と報告の受け口は、次のファイルです。報告の対象 ID が UUID の形でない場合は、Python 版のエラー文面（`badly formed hexadecimal UUID string`）まで写してあります。

```go
package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"redditclone/internal/comments"
	"redditclone/internal/posts"
	"redditclone/internal/reports"
)

// Handlers for comments and reports, mirroring the Python version's
// app/comments/router.py.

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	postID, ok := parseUUID(r.PathValue("post_id"))
	if !ok {
		detailError(w, http.StatusUnprocessableEntity, "post_id の形式が正しくありません")
		return
	}
	post, err := posts.GetPost(r.Context(), s.db, postID)
	if err != nil {
		log.Printf("get post: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if post == nil {
		detailError(w, http.StatusNotFound, nil)
		return
	}

	parentID := ""
	if raw := r.FormValue("parent_id"); raw != "" {
		parsed, ok := parseUUID(raw)
		if !ok {
			detailError(w, http.StatusUnprocessableEntity,
				"parent_id の形式が正しくありません")
			return
		}
		parentID = parsed
	}

	_, err = comments.CreateComment(r.Context(), s.db,
		r.FormValue("body"), user.ID, postID, parentID)
	if err != nil {
		if errors.Is(err, comments.ErrBodyRequired) ||
			errors.Is(err, comments.ErrBodyTooLong) ||
			errors.Is(err, comments.ErrParentNotFound) ||
			errors.Is(err, comments.ErrParentWrongPost) {
			detailError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		log.Printf("create comment: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirect303(w, r, "/r/"+post.SubredditName+"/"+postID)
}

func (s *Server) handleVoteComment(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	commentID, ok := parseUUID(r.PathValue("comment_id"))
	if !ok {
		detailError(w, http.StatusUnprocessableEntity, "comment_id の形式が正しくありません")
		return
	}
	value, ok := voteValue(w, r)
	if !ok {
		return
	}
	comment, err := comments.VoteComment(r.Context(), s.db, user.ID, commentID, value)
	if err != nil {
		if errors.Is(err, comments.ErrInvalidValue) ||
			errors.Is(err, comments.ErrCommentNotFound) ||
			errors.Is(err, comments.ErrOwnCommentVote) {
			voteError(w, err)
			return
		}
		log.Printf("vote comment: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, "vote_buttons", voteButtonsData{
		TargetType: "comment", TargetID: commentID, Score: comment.Score,
	})
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	commentID, ok := parseUUID(r.PathValue("comment_id"))
	if !ok {
		detailError(w, http.StatusUnprocessableEntity, "comment_id の形式が正しくありません")
		return
	}
	// The return address is built from the comment's own data, never
	// from a Referer header (as fixed in the Python version).
	ctx, err := comments.GetContext(r.Context(), s.db, commentID)
	if err != nil {
		log.Printf("comment context: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if ctx == nil {
		detailError(w, http.StatusNotFound, nil)
		return
	}
	if err := comments.DeleteComment(r.Context(), s.db, commentID, user.ID, ctx.SubredditID); err != nil {
		if errors.Is(err, comments.ErrForbidden) {
			detailError(w, http.StatusForbidden, "削除する権限がありません")
			return
		}
		if errors.Is(err, comments.ErrCommentNotFound) {
			detailError(w, http.StatusNotFound, nil)
			return
		}
		log.Printf("delete comment: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirect303(w, r, "/r/"+ctx.SubredditName+"/"+ctx.PostID)
}

// reportError writes the small red error paragraph the Python version
// returns for a rejected report (status 400, message escaped).
func reportError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<p class="text-red-500 text-sm">%s</p>`,
		pyEscape(message))
}

func (s *Server) handleCreateReport(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	targetID, ok := parseUUID(r.FormValue("target_id"))
	if !ok {
		// The Python version lets uuid.UUID() raise inside its error
		// handling, so a malformed id comes back as this message.
		reportError(w, "badly formed hexadecimal UUID string")
		return
	}
	err := reports.CreateReport(r.Context(), s.db, user.ID,
		r.FormValue("target_type"), targetID, r.FormValue("reason"))
	if err != nil {
		reportError(w, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<p class="text-green-600 text-sm text-center py-2">報告しました。ご協力ありがとうございます。</p>`)
}

func (s *Server) handleReportReasons(w http.ResponseWriter, r *http.Request) {
	s.renderFragment(w, "report_reasons", nil)
}
```

## 5.4 この章の確認

- 投票 → もう一度同じ投票（取り消し）→ 反対の投票（差し替え）で、スコアが +1 → 0 → −1 と動くこと
- 自分の投稿に投票すると、その場に理由が表示されること（ページ遷移しないこと）
- 深い返信の連鎖が深さ 8 で止まること
- コメントを消すと、配下の返信も一緒に消え、投稿の画面に戻ること
