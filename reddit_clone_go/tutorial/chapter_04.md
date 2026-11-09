# 第4章 コミュニティと投稿

この章では、掲示板の中心である二つを作ります。

1. **コミュニティ**（`internal/communities/`）— 作成・参加・脱退と、モデレーターの扱い
2. **投稿**（`internal/posts/`）— 作成・フィード（並び替えとページ送り）・削除

画面側は `internal/web/communities.go`（コミュニティの画面とホーム）と `internal/web/posts.go`（投稿の作成・詳細・検索）です。投票とコメントのハンドラも `web/posts.go` と `web/comments.go` にありますが、その中身は第5章で扱います。

## 4.1 コミュニティの規則

- 名前は 3〜21 文字、英数字とアンダースコアだけ。重なりは `lower()` で判定します（方式B、第1章の 0009）。
- 説明は 500 文字まで。空の説明は、Go 版では NULL として保存されます（Python 版は空文字列。保存の形だけの違いで、表示は同じです）。
- 作成者は自動でモデレーターとして参加します。
- 作成者は脱退できません。参加していない人の脱退は、エラーではなく素通りします。

作成（`CreateSubreddit`）は、コミュニティ本体と作成者の参加記録を**1つのトランザクション**で入れます。先に `lower()` で存在を確かめておき、それでも競合で一意制約に触れたら「すでに存在します」に翻訳します。この「専用トランザクションで囲む」形が、Python 版でいう SAVEPOINT（`begin_nested`）と同じ役割を果たします。Go の `database/sql` では、制約違反が起きるとそのトランザクション全体が使えなくなるため、部分的に囲むのではなく、作成処理ごと囲むのが素直な対応になります。

参加（`JoinSubreddit`）も同じ考え方で、二度押しや同時参加でも参加記録は1件だけ、応答は成功にそろえます。

```go
package communities

import (
	"regexp"
	"unicode/utf8"
)

// FormErrors carries one message per field of the create form.
type FormErrors struct {
	Name        string
	Description string
}

// Empty reports whether no error was recorded.
func (e FormErrors) Empty() bool { return e == FormErrors{} }

var nameRE = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ValidateSubredditCreate applies the rules of the Python version's
// SubredditCreate schema. Character counts are rune counts.
func ValidateSubredditCreate(name, description string) FormErrors {
	var errs FormErrors
	if n := utf8.RuneCountInString(name); n < 3 || n > 21 {
		errs.Name = "コミュニティ名は3〜21文字にしてください"
	} else if !nameRE.MatchString(name) {
		errs.Name = "英数字とアンダースコアのみ使用できます"
	}
	if utf8.RuneCountInString(description) > 500 {
		errs.Description = "説明は500文字以内にしてください"
	}
	return errs
}
```

```go
// Package communities is the subreddit service layer, mirroring the
// Python version's app/communities/services.py.
//
// Case handling follows method B: the display spelling is stored as
// typed, lookups and duplicate checks compare with lower(), backed by
// the functional unique indexes of migration 0009.
package communities

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// Subreddit is one row of the subreddits table.
type Subreddit struct {
	ID          string
	Name        string
	Description string // "" when NULL
	OwnerID     string
}

// ExistsError reports a subreddit name that is already taken.
type ExistsError struct{ Name string }

func (e ExistsError) Error() string { return fmt.Sprintf("r/%s はすでに存在します", e.Name) }

// ErrAlreadyMember reports a join attempt by an existing member.
var ErrAlreadyMember = errors.New("すでに参加しています")

// ErrOwnerCannotLeave reports a leave attempt by the owner.
var ErrOwnerCannotLeave = errors.New("作成者はコミュニティを脱退できません")

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

const subredditColumns = `id, name, COALESCE(description, ''), owner_id`

func scanSubreddit(row interface{ Scan(...any) error }) (Subreddit, error) {
	var s Subreddit
	err := row.Scan(&s.ID, &s.Name, &s.Description, &s.OwnerID)
	return s, err
}

// CreateSubreddit inserts the subreddit and registers the owner as a
// moderator member, in one transaction. A name race (the unique index
// firing after the pre-check) is reported as ExistsError: the
// transaction plays the role of the SAVEPOINT the Python version uses,
// so a failed create never poisons the caller's state.
func CreateSubreddit(ctx context.Context, db *sql.DB,
	name, description string, ownerID string) (Subreddit, error) {

	existing, err := getByName(ctx, db, name)
	if err != nil {
		return Subreddit{}, err
	}
	if existing != nil {
		return Subreddit{}, ExistsError{Name: name}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Subreddit{}, err
	}
	defer tx.Rollback()

	var desc any
	if description != "" {
		desc = description
	}
	sub, err := scanSubreddit(tx.QueryRowContext(ctx,
		`INSERT INTO subreddits (name, description, owner_id)
		 VALUES ($1, $2, $3) RETURNING `+subredditColumns,
		name, desc, ownerID))
	if err != nil {
		if isUniqueViolation(err) {
			return Subreddit{}, ExistsError{Name: name}
		}
		return Subreddit{}, err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO subreddit_members (user_id, subreddit_id, role)
		 VALUES ($1, $2, 'moderator')`, ownerID, sub.ID); err != nil {
		return Subreddit{}, err
	}
	if err := tx.Commit(); err != nil {
		return Subreddit{}, err
	}
	return sub, nil
}

func getByName(ctx context.Context, db *sql.DB, name string) (*Subreddit, error) {
	sub, err := scanSubreddit(db.QueryRowContext(ctx,
		`SELECT `+subredditColumns+` FROM subreddits WHERE lower(name) = lower($1)`,
		name))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// GetSubredditByName returns the subreddit by name, compared
// case-insensitively, or (nil, nil) when absent.
func GetSubredditByName(ctx context.Context, db *sql.DB,
	name string) (*Subreddit, error) {
	return getByName(ctx, db, name)
}

// GetSubredditByID returns the subreddit, or (nil, nil) when absent.
func GetSubredditByID(ctx context.Context, db *sql.DB,
	id string) (*Subreddit, error) {

	sub, err := scanSubreddit(db.QueryRowContext(ctx,
		`SELECT `+subredditColumns+` FROM subreddits WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// JoinSubreddit adds the user as a member. A double-click race is
// reported as ErrAlreadyMember, never as a server error.
func JoinSubreddit(ctx context.Context, db *sql.DB,
	userID, subredditID string) error {

	member, err := IsMember(ctx, db, userID, subredditID)
	if err != nil {
		return err
	}
	if member {
		return ErrAlreadyMember
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO subreddit_members (user_id, subreddit_id, role)
		 VALUES ($1, $2, 'member')`, userID, subredditID); err != nil {
		if isUniqueViolation(err) {
			return ErrAlreadyMember
		}
		return err
	}
	return tx.Commit()
}

// LeaveSubreddit removes the membership. The owner cannot leave.
// Leaving without a membership is not an error.
func LeaveSubreddit(ctx context.Context, db *sql.DB,
	userID, subredditID string) error {

	sub, err := GetSubredditByID(ctx, db, subredditID)
	if err != nil {
		return err
	}
	if sub != nil && sub.OwnerID == userID {
		return ErrOwnerCannotLeave
	}
	_, err = db.ExecContext(ctx,
		`DELETE FROM subreddit_members
		 WHERE user_id = $1 AND subreddit_id = $2`, userID, subredditID)
	return err
}

// IsMember reports whether the user belongs to the subreddit.
func IsMember(ctx context.Context, db *sql.DB,
	userID, subredditID string) (bool, error) {

	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM subreddit_members
		WHERE user_id = $1 AND subreddit_id = $2
	)`, userID, subredditID).Scan(&exists)
	return exists, err
}

// IsModerator reports whether the user is a moderator member.
func IsModerator(ctx context.Context, db *sql.DB,
	userID, subredditID string) (bool, error) {

	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM subreddit_members
		WHERE user_id = $1 AND subreddit_id = $2 AND role = 'moderator'
	)`, userID, subredditID).Scan(&exists)
	return exists, err
}

// SearchSubreddits finds subreddits whose name or description
// contains the query (case-insensitive), ordered by name, at most 10 —
// the Python version's search_subreddits.
func SearchSubreddits(ctx context.Context, db *sql.DB,
	query string) ([]Subreddit, error) {

	if query == "" {
		return nil, nil
	}
	term := "%" + query + "%"
	rows, err := db.QueryContext(ctx,
		`SELECT `+subredditColumns+` FROM subreddits
		 WHERE name ILIKE $1 OR description ILIKE $1
		 ORDER BY name LIMIT 10`, term)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subreddit
	for rows.Next() {
		sub, err := scanSubreddit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// ListJoinedSubreddits returns the subreddits the user belongs to,
// ordered by name (sidebar).
func ListJoinedSubreddits(ctx context.Context, db *sql.DB,
	userID string) ([]Subreddit, error) {

	rows, err := db.QueryContext(ctx,
		`SELECT s.id, s.name, COALESCE(s.description, ''), s.owner_id
		 FROM subreddits s
		 JOIN subreddit_members m ON m.subreddit_id = s.id
		 WHERE m.user_id = $1
		 ORDER BY s.name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subreddit
	for rows.Next() {
		sub, err := scanSubreddit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}
```

## 4.2 投稿と Hot スコア

投稿にはテキスト投稿とリンク投稿があり、検証の順序が Python 版と揃えてあります。まず項目ごとの形（タイトルの長さ、URL の長さ、本文の長さ）を確かめ、次に種別ごとの必須（テキストには本文、リンクには URL と `http://` / `https://` の始まり）を確かめます。関係ない側の項目は検証に回りません（リンク投稿の本文欄に何が入っていても、長さでは落としません）。URL の上限 2048 文字は、レビュー対応で Python 版に入ったものを最初から含んでいます。

並び替えの心臓部が Hot スコアです。式は Reddit の古典的なもので、符号つきです。

```
hot = sign(score) × log10(max(|score|, 1)) + (投稿時刻 − 2005-12-08 07:46:43 UTC) ÷ 45000
```

レビューで「符号が無い」という指摘を受けて Python 版と Elixir 版で直した経緯があり、Go 版は最初から符号つきで実装しました。Go 側の計算（`ComputeHotScore`）と、投票時に SQL の側で計算する式（第5章）は、同じ結果になることをテストで確かめています（単体テストの期待値は Python で計算した値です）。

```go
package posts

import "unicode/utf8"

// FormErrors carries one message per field of the submit form, plus a
// general error (the Python version maps model-level validation
// failures to "general").
type FormErrors struct {
	General string
	Title   string
	URL     string
	Body    string
}

// Empty reports whether no error was recorded.
func (e FormErrors) Empty() bool { return e == FormErrors{} }

// ValidatePostCreate applies the rules of the Python version's
// PostCreate schema. As in the router there, the body only applies to
// text posts and the URL only to link posts. Field errors are reported
// first; the type-dependent requirements are checked only when the
// fields themselves are valid (pydantic's order).
func ValidatePostCreate(title, postType, body, url string) (CreateInput, FormErrors) {
	var errs FormErrors
	in := CreateInput{Title: title, Type: postType}

	if postType != "text" && postType != "link" {
		errs.General = "投稿の種別が正しくありません"
		return in, errs
	}

	if n := utf8.RuneCountInString(title); n < 1 || n > 300 {
		errs.Title = "タイトルは1〜300文字にしてください"
	}
	if postType == "link" && utf8.RuneCountInString(url) > 2048 {
		errs.URL = "URLは2048文字以内にしてください"
	}
	if postType == "text" && utf8.RuneCountInString(body) > 40000 {
		errs.Body = "本文は40,000文字以内にしてください"
	}
	if !errs.Empty() {
		return in, errs
	}

	switch postType {
	case "text":
		if body == "" {
			errs.General = "テキスト投稿には本文が必要です"
			return in, errs
		}
		in.Body = &body
	case "link":
		if url == "" {
			errs.General = "リンク投稿には URL が必要です"
			return in, errs
		}
		if !hasHTTPPrefix(url) {
			errs.General = "URL は http:// または https:// で始まる必要があります"
			return in, errs
		}
		in.URL = &url
	}
	return in, errs
}

func hasHTTPPrefix(url string) bool {
	return len(url) >= 7 && url[:7] == "http://" ||
		len(url) >= 8 && url[:8] == "https://"
}
```

## 4.3 フィードの組み立て

一覧の取得は3種類（コミュニティの投稿一覧、ホーム、プロフィール用）ありますが、中身は1つのクエリ組み立て（`listFeed`）に集約されています。要点は次のとおりです。

- 並び順は Hot なら `hot_score DESC, id DESC`、New なら投稿日時の降順に加えて `id DESC`。**第2の並びキー（id）を必ず入れる**のが、レビューで学んだ境界の直しです。これが無いと、同点の投稿の順序がページをまたいで揺れます。
- ページ送りは「必要数より1件多く取る」方式です。25 件のページなら 26 件取り、26 件目があれば「次のページあり」とします。数え直しのクエリは要りません。
- コメント数は、投稿ごとに数えるのではなく `LEFT JOIN` と `GROUP BY` で一括で取ります。
- ホームは、ログイン中のユーザーが参加しているコミュニティの投稿だけです。そしてホーム自体がログイン必須です（未ログインではログイン画面へ送られます）。これは Python 版の仕様の発見でした。

投稿の削除（`DeletePost`）は、投稿者本人か、そのコミュニティのモデレーターだけができます。Karma は戻しません。投票の記録は対象を指す外部キーを持たない設計（Python 版の割り切り）なので、消えた投稿の投票は残りますが、コメントは外部キーの連鎖削除で一緒に消えます。

```go
// Package posts is the post service layer, mirroring the Python
// version's app/posts/services.py (creation and feeds; voting and
// deletion arrive in Phase 4).
package posts

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

// PageSize is the feed page size (PAGE_SIZE in the Python version).
const PageSize = 25

// Post is one row of the posts table plus the fields the templates
// display (author name, subreddit name, comment count).
type Post struct {
	ID             string
	Title          string
	Type           string // "text" | "link"
	Body           string // "" when NULL
	URL            string // "" when NULL
	Score          int
	HotScore       float64
	AuthorID       string
	SubredditID    string
	InsertedAt     time.Time
	AuthorUsername string
	SubredditName  string
	CommentCount   int
}

// CreateInput is a validated post submission.
type CreateInput struct {
	Title string
	Type  string
	Body  *string // text posts
	URL   *string // link posts
}

var hotEpoch = time.Date(2005, 12, 8, 7, 46, 43, 0, time.UTC)

// ComputeHotScore is Reddit's hot formula with the sign kept, exactly
// as in the Python version's Post.compute_hot_score (and the Elixir
// version's signed formula).
func ComputeHotScore(score int, insertedAt time.Time) float64 {
	sign := 0
	if score > 0 {
		sign = 1
	} else if score < 0 {
		sign = -1
	}
	order := float64(sign) * math.Log10(math.Max(math.Abs(float64(score)), 1))
	seconds := insertedAt.Sub(hotEpoch).Seconds()
	return order + seconds/45000
}

// CreatePost inserts the post with its initial hot score.
func CreatePost(ctx context.Context, db *sql.DB,
	in CreateInput, authorID, subredditID string) (Post, error) {

	hot := ComputeHotScore(0, time.Now().UTC())
	var body, url any
	if in.Body != nil {
		body = *in.Body
	}
	if in.URL != nil {
		url = *in.URL
	}
	var post Post
	err := db.QueryRowContext(ctx,
		`INSERT INTO posts (title, type, body, url, hot_score, author_id, subreddit_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, title, type, COALESCE(body, ''), COALESCE(url, ''),
		           score, hot_score, author_id, subreddit_id, inserted_at`,
		in.Title, in.Type, body, url, hot, authorID, subredditID).
		Scan(&post.ID, &post.Title, &post.Type, &post.Body, &post.URL,
			&post.Score, &post.HotScore, &post.AuthorID, &post.SubredditID,
			&post.InsertedAt)
	return post, err
}

const postSelect = `
	SELECT p.id, p.title, p.type, COALESCE(p.body, ''), COALESCE(p.url, ''),
	       p.score, p.hot_score, p.author_id, p.subreddit_id, p.inserted_at,
	       a.username, s.name, COUNT(c.id)
	FROM posts p
	JOIN users a ON a.id = p.author_id
	JOIN subreddits s ON s.id = p.subreddit_id
	LEFT JOIN comments c ON c.post_id = p.id`

func scanPost(row interface{ Scan(...any) error }) (Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.Title, &p.Type, &p.Body, &p.URL,
		&p.Score, &p.HotScore, &p.AuthorID, &p.SubredditID, &p.InsertedAt,
		&p.AuthorUsername, &p.SubredditName, &p.CommentCount)
	return p, err
}

// GetPost returns one post with display fields, or (nil, nil).
func GetPost(ctx context.Context, db *sql.DB, postID string) (*Post, error) {
	post, err := scanPost(db.QueryRowContext(ctx,
		postSelect+` WHERE p.id = $1 GROUP BY p.id, a.username, s.name`, postID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func sortClause(sort string) string {
	if sort == "new" {
		return ` ORDER BY p.inserted_at DESC, p.id DESC`
	}
	return ` ORDER BY p.hot_score DESC, p.id DESC`
}

// listFeed runs the shared feed query. It fetches one row past the
// page to decide hasMore, exactly like the Python version.
func listFeed(ctx context.Context, db *sql.DB,
	where string, arg any, sort string, page int) ([]Post, bool, error) {

	query := postSelect + where +
		` GROUP BY p.id, a.username, s.name` + sortClause(sort) +
		` LIMIT $2 OFFSET $3`
	rows, err := db.QueryContext(ctx, query, arg, PageSize+1, (page-1)*PageSize)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, false, err
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(posts) > PageSize
	if hasMore {
		posts = posts[:PageSize]
	}
	return posts, hasMore, nil
}

// ListPosts is the subreddit feed.
func ListPosts(ctx context.Context, db *sql.DB,
	subredditID, sort string, page int) ([]Post, bool, error) {
	return listFeed(ctx, db, ` WHERE p.subreddit_id = $1`, subredditID, sort, page)
}

// ListHomePosts is the home feed: posts of the subreddits the user
// has joined.
func ListHomePosts(ctx context.Context, db *sql.DB,
	userID, sort string, page int) ([]Post, bool, error) {
	return listFeed(ctx, db,
		` JOIN subreddit_members m ON m.subreddit_id = p.subreddit_id
		  WHERE m.user_id = $1`, userID, sort, page)
}

// ListUserPosts is the profile feed (used in Phase 5).
func ListUserPosts(ctx context.Context, db *sql.DB,
	userID, sort string, page int) ([]Post, bool, error) {
	return listFeed(ctx, db, ` WHERE p.author_id = $1`, userID, sort, page)
}

// SearchPosts finds posts whose title contains the query
// (case-insensitive), hottest first, at most 25 — the Python version's
// search_posts. The ILIKE pattern keeps the query's % and _ as
// wildcards, exactly as in the Python version.
func SearchPosts(ctx context.Context, db *sql.DB, query string) ([]Post, error) {
	if query == "" {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, postSelect+`
		WHERE p.title ILIKE $1
		GROUP BY p.id, a.username, s.name
		ORDER BY p.hot_score DESC, p.id DESC
		LIMIT 25`, "%"+query+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, post)
	}
	return out, rows.Err()
}
```

## 4.4 画面: コミュニティとホーム

画面側の要点です。

- コミュニティが無い場合は 404 で、文面は `r/{name} は存在しません` です。
- `sort` が hot/new 以外、または `page` が 1 未満や数値でない場合は 422 です。この検証は対象の照会より**先**に行います（第3章で触れた FastAPI の順序に合わせたもの。第5章のプロフィールも同じ）。
- 参加・脱退は HTMX の断片（参加ボタン部分だけ）を返します。二重参加はエラーにせず「参加済み」のボタンを返し、作成者の脱退だけ理由を表示します。
- 投稿の作成に成功すると、その投稿の詳細へ 303 で送ります。

```go
package web

import (
	"errors"
	"log"
	"net/http"

	"redditclone/internal/communities"
	"redditclone/internal/posts"
)

// Handlers for subreddits, mirroring the Python version's
// app/communities/router.py.

type subredditPage struct {
	PageData
	Subreddit  communities.Subreddit
	Sort       string
	Page       int
	HasMore    bool
	PostList   postListData
	JoinButton joinButtonData
}

type newSubredditPage struct {
	PageData
	Errors      communities.FormErrors
	Name        string
	Description string
}

// lookupSubreddit resolves the {name} path value. On a miss it writes
// the 404 and returns nil; withDetail selects the Python version's
// detail message variant.
func (s *Server) lookupSubreddit(w http.ResponseWriter, r *http.Request,
	withDetail bool) *communities.Subreddit {

	name := r.PathValue("name")
	sub, err := communities.GetSubredditByName(r.Context(), s.db, name)
	if err != nil {
		log.Printf("subreddit lookup: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil
	}
	if sub == nil {
		if withDetail {
			detailError(w, http.StatusNotFound, "r/"+name+" は存在しません")
		} else {
			detailError(w, http.StatusNotFound, nil)
		}
		return nil
	}
	return sub
}

func (s *Server) handleSubredditPage(w http.ResponseWriter, r *http.Request) {
	// Parameters are validated before the lookup, as FastAPI does.
	sort, page, ok := feedParams(w, r)
	if !ok {
		return
	}
	sub := s.lookupSubreddit(w, r, true)
	if sub == nil {
		return
	}
	user := s.currentUser(r)

	list, hasMore, err := posts.ListPosts(r.Context(), s.db, sub.ID, sort, page)
	if err != nil {
		log.Printf("list posts: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	joined := false
	if user != nil {
		joined, err = communities.IsMember(r.Context(), s.db, user.ID, sub.ID)
		if err != nil {
			log.Printf("is member: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	data := s.baseDataFor(r, user)
	s.render(w, "communities/subreddit", http.StatusOK, subredditPage{
		PageData:  data,
		Subreddit: *sub,
		Sort:      sort,
		Page:      page,
		HasMore:   hasMore,
		PostList: postListData{
			CurrentUser: data.CurrentUser,
			Posts:       list,
			Sort:        sort,
			Page:        page,
			HasMore:     hasMore,
			Subreddit:   *sub,
		},
		JoinButton: joinButtonData{Subreddit: *sub, Joined: joined},
	})
}

func (s *Server) handleLoadMorePosts(w http.ResponseWriter, r *http.Request) {
	// Parameters are validated before the lookup, as FastAPI does.
	sort, page, ok := feedParams(w, r)
	if !ok {
		return
	}
	sub := s.lookupSubreddit(w, r, false)
	if sub == nil {
		return
	}
	list, hasMore, err := posts.ListPosts(r.Context(), s.db, sub.ID, sort, page)
	if err != nil {
		log.Printf("list posts: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, "post_list", postListData{
		CurrentUser: userView(s.currentUser(r)),
		Posts:       list,
		Sort:        sort,
		Page:        page,
		HasMore:     hasMore,
		Subreddit:   *sub,
	})
}

func (s *Server) handleJoin(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	sub := s.lookupSubreddit(w, r, false)
	if sub == nil {
		return
	}
	// Already-a-member is ignored, as in the Python version.
	if err := communities.JoinSubreddit(r.Context(), s.db, user.ID, sub.ID); err != nil &&
		!errors.Is(err, communities.ErrAlreadyMember) {
		log.Printf("join: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, "join_button", joinButtonData{Subreddit: *sub, Joined: true})
}

func (s *Server) handleLeave(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	sub := s.lookupSubreddit(w, r, false)
	if sub == nil {
		return
	}
	joined := false
	if err := communities.LeaveSubreddit(r.Context(), s.db, user.ID, sub.ID); err != nil {
		if errors.Is(err, communities.ErrOwnerCannotLeave) {
			joined = true // the owner stays a member
		} else {
			log.Printf("leave: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	s.renderFragment(w, "join_button", joinButtonData{Subreddit: *sub, Joined: joined})
}

func (s *Server) handleNewSubredditPage(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	s.render(w, "communities/new_subreddit", http.StatusOK,
		newSubredditPage{PageData: s.baseDataFor(r, user)})
}

func (s *Server) handleCreateSubreddit(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	name := r.FormValue("name")
	description := r.FormValue("description")

	page := newSubredditPage{
		PageData:    s.baseDataFor(r, user),
		Name:        name,
		Description: description,
	}
	if errs := communities.ValidateSubredditCreate(name, description); !errs.Empty() {
		page.Errors = errs
		s.render(w, "communities/new_subreddit", http.StatusUnprocessableEntity, page)
		return
	}

	sub, err := communities.CreateSubreddit(r.Context(), s.db, name, description, user.ID)
	if err != nil {
		var exists communities.ExistsError
		if errors.As(err, &exists) {
			page.Errors.Name = exists.Error()
			s.render(w, "communities/new_subreddit", http.StatusUnprocessableEntity, page)
			return
		}
		log.Printf("create subreddit: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirect303(w, r, "/r/"+sub.Name)
}
```

投稿の作成画面・詳細画面・検索のハンドラは次のファイルです。詳細画面は、URL のコミュニティ名と投稿の属するコミュニティが食い違っていても投稿の側で引く（Python 版と同じ）こと、削除ボタンは投稿者本人にだけ出すこと（モデレーターには出しません。これもテンプレートまで含めて Python 版と一致させています）が要点です。投票とコメントの送信先もこのファイルにありますが、中身は第5章です。

```go
package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"redditclone/internal/comments"
	"redditclone/internal/communities"
	"redditclone/internal/posts"
)

// Handlers for the home feed and post creation, mirroring the Python
// version's app/posts/router.py (detail, voting and deletion arrive in
// Phase 4).

type homePage struct {
	PageData
	Posts            []posts.Post
	Sort             string
	Page             int
	HasMore          bool
	JoinedSubreddits []communities.Subreddit
	ShowSubreddit    bool
}

type newPostPage struct {
	PageData
	Subreddit communities.Subreddit
	Errors    posts.FormErrors
	PostType  string
	Title     string
	Body      string
	URL       string
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	sort, page, ok := feedParams(w, r)
	if !ok {
		return
	}
	list, hasMore, err := posts.ListHomePosts(r.Context(), s.db, user.ID, sort, page)
	if err != nil {
		log.Printf("home feed: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	joined, err := communities.ListJoinedSubreddits(r.Context(), s.db, user.ID)
	if err != nil {
		log.Printf("joined subreddits: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, "posts/home", http.StatusOK, homePage{
		PageData:         s.baseDataFor(r, user),
		Posts:            list,
		Sort:             sort,
		Page:             page,
		HasMore:          hasMore,
		JoinedSubreddits: joined,
		ShowSubreddit:    true,
	})
}

func (s *Server) handleNewPostPage(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	sub := s.lookupSubreddit(w, r, false)
	if sub == nil {
		return
	}
	s.render(w, "posts/new_post", http.StatusOK, newPostPage{
		PageData:  s.baseDataFor(r, user),
		Subreddit: *sub,
		PostType:  "text",
	})
}

func (s *Server) handleCreatePost(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	sub := s.lookupSubreddit(w, r, false)
	if sub == nil {
		return
	}

	title := r.FormValue("title")
	postType := r.FormValue("post_type")
	body := r.FormValue("body")
	url := r.FormValue("url")

	page := newPostPage{
		PageData:  s.baseDataFor(r, user),
		Subreddit: *sub,
		PostType:  postType,
		Title:     title,
		Body:      body,
		URL:       url,
	}
	in, errs := posts.ValidatePostCreate(title, postType, body, url)
	if !errs.Empty() {
		page.Errors = errs
		s.render(w, "posts/new_post", http.StatusUnprocessableEntity, page)
		return
	}

	post, err := posts.CreatePost(r.Context(), s.db, in, user.ID, sub.ID)
	if err != nil {
		log.Printf("create post: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirect303(w, r, "/r/"+sub.Name+"/"+post.ID)
}

type postDetailPage struct {
	PageData
	Post     posts.Post
	Comments []*comments.Comment
}

func (s *Server) handlePostDetail(w http.ResponseWriter, r *http.Request) {
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
		detailError(w, http.StatusNotFound, "投稿が見つかりません")
		return
	}
	list, err := comments.ListComments(r.Context(), s.db, postID)
	if err != nil {
		log.Printf("list comments: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, "posts/detail", http.StatusOK, postDetailPage{
		PageData: s.baseData(r),
		Post:     *post,
		Comments: list,
	})
}

// pyEscape escapes exactly like Python's html.escape (which the
// Python version uses for these fragments): note ' becomes &#x27;.
var pyEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	`"`, "&quot;",
	"'", "&#x27;",
)

func pyEscape(s string) string { return pyEscaper.Replace(s) }

// voteError writes the small red error fragment the Python version
// returns for a rejected vote (status 400, message escaped).
func voteError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<span class="text-red-500 text-xs">%s</span>`,
		pyEscape(err.Error()))
}

// voteValue parses the value form field of a vote request.
func voteValue(w http.ResponseWriter, r *http.Request) (int, bool) {
	v, err := strconv.Atoi(r.FormValue("value"))
	if err != nil {
		detailError(w, http.StatusUnprocessableEntity, "value は整数で指定してください")
		return 0, false
	}
	return v, true
}

func (s *Server) handleVotePost(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	postID, ok := parseUUID(r.PathValue("post_id"))
	if !ok {
		detailError(w, http.StatusUnprocessableEntity, "post_id の形式が正しくありません")
		return
	}
	value, ok := voteValue(w, r)
	if !ok {
		return
	}
	post, err := posts.VotePost(r.Context(), s.db, user.ID, postID, value)
	if err != nil {
		if errors.Is(err, posts.ErrInvalidVoteValue) ||
			errors.Is(err, posts.ErrPostNotFound) ||
			errors.Is(err, posts.ErrOwnPostVote) {
			voteError(w, err)
			return
		}
		log.Printf("vote post: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, "vote_buttons", voteButtonsData{
		TargetType: "post", TargetID: postID, Score: post.Score,
	})
}

type searchPage struct {
	PageData
	Posts         []posts.Post
	Subreddits    []communities.Subreddit
	ShowSubreddit bool
}

// handleSearch renders the search results page. The query the template
// shows is PageData.Query, which baseData fills from ?q= (the same
// value the Python version passes as "query"). An empty query shows
// the prompt state.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	page := searchPage{
		PageData:      s.baseData(r),
		ShowSubreddit: true,
	}
	if q != "" {
		foundPosts, err := posts.SearchPosts(r.Context(), s.db, q)
		if err != nil {
			log.Printf("search posts: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		foundSubs, err := communities.SearchSubreddits(r.Context(), s.db, q)
		if err != nil {
			log.Printf("search subreddits: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		page.Posts = foundPosts
		page.Subreddits = foundSubs
	}
	s.render(w, "posts/search", http.StatusOK, page)
}

func (s *Server) handleDeletePost(w http.ResponseWriter, r *http.Request) {
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
	if err := posts.DeletePost(r.Context(), s.db, postID, user.ID); err != nil {
		if errors.Is(err, posts.ErrForbidden) {
			detailError(w, http.StatusForbidden, "削除する権限がありません")
			return
		}
		if errors.Is(err, posts.ErrPostNotFound) {
			detailError(w, http.StatusNotFound, nil)
			return
		}
		log.Printf("delete post: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirect303(w, r, "/r/"+post.SubredditName)
}
```

## 4.5 この章の確認

- コミュニティを作ると、作成者がモデレーターとして参加済みになること
- 26 件投稿すると、1ページ目が25件＋「次へ」が出て、2ページ目に1件出ること
- 同じ名前の大文字違いのコミュニティが作れないこと（422）
- 参加を連打しても参加記録が1件のままであること
