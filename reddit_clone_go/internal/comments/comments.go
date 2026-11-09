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
