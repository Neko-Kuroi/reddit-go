# Reddit クローン（Go 版）— 全コード付録 2 部品とサービス層

正本のコードファイルの全文を機械生成で収録したものです（手写しではありません）。

## internal/auth/session.go

```go
// Package auth implements the signed session cookie, mirroring the
// Python version's app/auth.py (itsdangerous URLSafeTimedSerializer
// with salt "session").
//
// Token format (this port's own, not byte-compatible with itsdangerous):
//
//	base64url(JSON {"uid": user id, "iat": issued-at unix seconds})
//	+ "." + base64url(HMAC-SHA256(body, key))
//	key = HMAC-SHA256(secret, "session")
//
// A token is accepted for 7 days from issuance, like COOKIE_MAX_AGE.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// MaxAge is the session lifetime: 7 days, in seconds.
const MaxAge = 60 * 60 * 24 * 7

// Sessions issues and verifies session tokens.
type Sessions struct {
	key     []byte
	cookie  string
	secure  bool
	nowFunc func() time.Time // for tests
}

// NewSessions derives the signing key from the secret with the
// "session" salt, as itsdangerous does.
func NewSessions(secret, cookieName string, secure bool) *Sessions {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("session"))
	return &Sessions{
		key:     mac.Sum(nil),
		cookie:  cookieName,
		secure:  secure,
		nowFunc: time.Now,
	}
}

type payload struct {
	UserID   string `json:"uid"`
	IssuedAt int64  `json:"iat"`
}

func (s *Sessions) sign(body string) string {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(body))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// CreateToken signs a token for the user id.
func (s *Sessions) CreateToken(userID string) string {
	raw, _ := json.Marshal(payload{
		UserID:   userID,
		IssuedAt: s.nowFunc().Unix(),
	})
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + s.sign(body)
}

// VerifyToken returns the user id, or "" when the token is invalid,
// tampered with, or older than MaxAge.
func (s *Sessions) VerifyToken(token string) string {
	body, sig, ok := strings.Cut(token, ".")
	if !ok || body == "" || sig == "" {
		return ""
	}
	if !hmac.Equal([]byte(s.sign(body)), []byte(sig)) {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return ""
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil || p.UserID == "" {
		return ""
	}
	age := s.nowFunc().Unix() - p.IssuedAt
	if age < 0 || age > MaxAge {
		return ""
	}
	return p.UserID
}

// SetCookie writes the session cookie on the response.
func (s *Sessions) SetCookie(w http.ResponseWriter, userID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookie,
		Value:    s.CreateToken(userID),
		Path:     "/",
		MaxAge:   MaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secure,
	})
}

// ClearCookie expires the session cookie (logout).
func (s *Sessions) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secure,
	})
}

// UserID reads and verifies the session cookie of the request. It
// returns "" when there is no valid session.
func (s *Sessions) UserID(r *http.Request) string {
	c, err := r.Cookie(s.cookie)
	if err != nil || c.Value == "" {
		return ""
	}
	return s.VerifyToken(c.Value)
}
```

## internal/accounts/validate.go

```go
package accounts

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// FormErrors carries one message per field, plus a general error. It
// mirrors the errors dict the Python version passes to the register
// template.
type FormErrors struct {
	General  string
	Username string
	Email    string
	Password string
}

// Empty reports whether no error was recorded.
func (e FormErrors) Empty() bool {
	return e == FormErrors{}
}

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ValidateUserCreate applies the rules of the Python version's
// UserCreate schema (schemas.py). Character counts are rune counts,
// matching Python's len() on str.
func ValidateUserCreate(username, email, password string) FormErrors {
	var errs FormErrors

	if n := utf8.RuneCountInString(username); n < 3 || n > 20 {
		errs.Username = "ユーザー名は3〜20文字にしてください"
	} else if !usernameRE.MatchString(username) {
		errs.Username = "ユーザー名は英数字とアンダースコアのみ使用できます"
	}

	if !validEmail(email) {
		errs.Email = "メールアドレスの形式が正しくありません"
	}

	if utf8.RuneCountInString(password) < 8 {
		errs.Password = "パスワードは8文字以上にしてください"
	}

	return errs
}

// validEmail mirrors the acceptance of pydantic's EmailStr as
// measured against the pinned Python stack (email-validator 2.3.0).
// The correspondence battery lives in validate_test.go; the rules
// below are the ones the measurements support:
//
//   - the whole address is at most 254 characters (the SMTP limit);
//   - exactly one @; the local part is a dot-atom (no leading,
//     trailing or doubled dot) over ASCII letters/digits and the
//     usual specials, with non-ASCII characters also allowed;
//   - the domain has at least two dot-separated labels of at most 63
//     characters, letters/digits (Unicode) and hyphens, never
//     starting or ending with a hyphen;
//   - the last label (TLD) is not made of digits only.
//
// EmailStr also strips surrounding whitespace from the address; that
// happens at the form boundary in the handler, so the input here is
// expected to be trimmed already.
func validEmail(email string) bool {
	if n := utf8.RuneCountInString(email); n == 0 || n > 254 {
		return false
	}
	if strings.Count(email, "@") != 1 {
		return false
	}
	local, domain, _ := strings.Cut(email, "@")
	return validEmailLocal(local) && validEmailDomain(domain)
}

func validEmailLocal(local string) bool {
	if local == "" {
		return false
	}
	if strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") ||
		strings.Contains(local, "..") {
		return false
	}
	for _, r := range local {
		if r < 0x80 {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
				r >= '0' && r <= '9' ||
				strings.ContainsRune(".!#$%&'*+-/=?^_`{|}~", r)) {
				return false
			}
		} else if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func validEmailDomain(domain string) bool {
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || utf8.RuneCountInString(label) > 63 ||
			strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if r != '-' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return false
			}
		}
	}
	// The TLD must not be all digits ("example.123" and bare IPv4
	// forms are rejected by EmailStr for this reason).
	tld := labels[len(labels)-1]
	for _, r := range tld {
		if !unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
```

## internal/accounts/accounts.go

```go
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
```

## internal/communities/validate.go

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

## internal/communities/communities.go

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

## internal/posts/posts.go

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

## internal/posts/vote.go

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

## internal/posts/validate.go

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

## internal/comments/comments.go

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

## internal/reports/reports.go

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

