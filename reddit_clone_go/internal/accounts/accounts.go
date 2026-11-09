// Package accounts is the user service layer, mirroring the Python
// version's app/accounts (services.py + schemas.py).
//
// Case handling follows method B: the display spelling is stored as
// typed, and all lookups and duplicate checks compare with lower(),
// backed by the functional unique indexes of migration 0009.
package accounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

// User is one row of the users table.
type User struct {
	ID           string
	Username     string
	Email        string
	PasswordHash string
	PostKarma    int
	CommentKarma int
	InsertedAt   time.Time
}

// ErrDuplicate reports a username/email that is already taken.
var ErrDuplicate = errors.New("このユーザー名またはメールアドレスはすでに使われています")

const bcryptCost = 12 // passlib's bcrypt default

// bcryptMaxBytes is the bcrypt algorithm's own input limit: only the
// first 72 bytes of a password take part. The Python stack
// (passlib + bcrypt) truncates silently; golang.org/x/crypto/bcrypt
// returns an error for longer inputs instead. The truncation is done
// explicitly here so both implementations treat long passwords the
// same way — without it, registration with a long password would
// fail on the Go side, and an account created by the Python version
// with a long password could never sign in here.
const bcryptMaxBytes = 72

func passwordBytes(password string) []byte {
	b := []byte(password)
	if len(b) > bcryptMaxBytes {
		b = b[:bcryptMaxBytes]
	}
	return b
}

// dummyHash is compared against when the account does not exist, so
// login timing does not reveal whether an email is registered (the
// Python version uses passlib's dummy_verify for the same purpose).
var dummyHash, _ = bcrypt.GenerateFromPassword(
	[]byte("dummy-password-for-timing"), bcryptCost)

const userColumns = `id, username, email, password_hash, post_karma, comment_karma, inserted_at`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash,
		&u.PostKarma, &u.CommentKarma, &u.InsertedAt)
	return u, err
}

// normalizeEmailForStorage lowercases the domain part of an email
// address, keeping the local part as typed. This matches what the
// Python version stores: pydantic's EmailStr normalizes only the
// domain. (All comparisons use lower() anyway; this only aligns the
// stored form.)
func normalizeEmailForStorage(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return email
	}
	return email[:at+1] + strings.ToLower(email[at+1:])
}

// CreateUser validates the duplicate rule, hashes the password and
// inserts the user. A registration race (the unique index firing after
// the pre-check) is reported as ErrDuplicate, never as a server error:
// the insert runs in its own transaction, which plays the role of the
// SAVEPOINT the Python version uses.
func CreateUser(ctx context.Context, db *sql.DB,
	username, email, password string) (User, error) {

	email = normalizeEmailForStorage(email)
	var exists bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM users
		WHERE lower(username) = lower($1) OR lower(email) = lower($2)
	)`, username, email).Scan(&exists)
	if err != nil {
		return User{}, err
	}
	if exists {
		return User{}, ErrDuplicate
	}

	hash, err := bcrypt.GenerateFromPassword(passwordBytes(password), bcryptCost)
	if err != nil {
		return User{}, fmt.Errorf("hash password: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()

	user, err := scanUser(tx.QueryRowContext(ctx,
		`INSERT INTO users (username, email, password_hash)
		 VALUES ($1, $2, $3) RETURNING `+userColumns,
		username, email, string(hash)))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, ErrDuplicate
		}
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return user, nil
}

// AuthenticateUser looks the user up by email (case-insensitive) and
// checks the password. Failure is deliberately ambiguous: it returns
// (nil, nil) for both "no such email" and "wrong password".
func AuthenticateUser(ctx context.Context, db *sql.DB,
	email, password string) (*User, error) {

	user, err := scanUser(db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE lower(email) = lower($1)`,
		email))
	if errors.Is(err, sql.ErrNoRows) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, passwordBytes(password))
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword(
		[]byte(user.PasswordHash), passwordBytes(password)) != nil {
		return nil, nil
	}
	return &user, nil
}

// GetUserByID returns the user, or (nil, nil) when absent.
func GetUserByID(ctx context.Context, db *sql.DB, id string) (*User, error) {
	user, err := scanUser(db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserByUsername returns the user by name, compared
// case-insensitively, or (nil, nil) when absent.
func GetUserByUsername(ctx context.Context, db *sql.DB,
	username string) (*User, error) {

	user, err := scanUser(db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE lower(username) = lower($1)`,
		username))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// Executor is satisfied by both *sql.DB and *sql.Tx, so the karma
// update can join a caller's transaction.
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// UpdateKarma adjusts post or comment karma with a SQL-side increment,
// so concurrent votes cannot lose each other's updates.
func UpdateKarma(ctx context.Context, exec Executor,
	userID, karmaType string, amount int) error {

	var query string
	switch karmaType {
	case "post":
		query = `UPDATE users SET post_karma = post_karma + $1 WHERE id = $2`
	case "comment":
		query = `UPDATE users SET comment_karma = comment_karma + $1 WHERE id = $2`
	default:
		return fmt.Errorf("unknown karma type %q", karmaType)
	}
	_, err := exec.ExecContext(ctx, query, amount, userID)
	return err
}
