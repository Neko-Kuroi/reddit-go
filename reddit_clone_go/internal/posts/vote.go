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
