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
