# 第3章 アカウント

この章では、アカウントまわりを三つの層で作ります。

1. **検証**（`internal/accounts/validate.go`）— 入力の形を確かめる
2. **サービス**（`internal/accounts/accounts.go`）— 登録・認証・Karma の実処理
3. **画面**（`internal/web/accounts.go`）— 登録・ログイン・ログアウト、そしてプロフィール（プロフィールの画面そのものは第6章と対で読みます）

## 3.1 検証の規則

ユーザー名・メールアドレス・パスワードの規則は Python 版と同一です。

- ユーザー名: 3〜20文字、英数字とアンダースコアだけ
- パスワード: 8文字以上（文字数で数えます）
- メールアドレス: 形式が正しいこと

エラーの文面も Python 版の日本語をそのまま写しました。ただし、メールアドレスの「正しさ」だけは単純な写しでは済みませんでした。Python 版の判定は pydantic の `EmailStr`（実体は email-validator というライブラリ）が担っていて、規則が文章で手に入らないため、**49件のアドレスを両方にかけて判定を実測し、一致するよう Go 側の規則を組み立て直した**のです。実測で分かった代表例を挙げます。

- `user@example.c`（TLD が1文字）— 通ります。Go 版の最初の実装には「TLD は2文字以上の英字」という、EmailStr には無い規則が混ざっていて、ここで食い違いました。
- `user@example.123`（TLD が数字だけ）— 落ちます（IPv4 の形も同じ理由で落ちます）。
- `ユー@example.com` や `user@例え.jp`（非 ASCII）— 通ります。
- 全体の長さは 254 文字まで（SMTP の上限）。255 文字は落ちます。
- ローカル部の64文字上限は、実測では効いていませんでした（100文字でも通る）。上限として残っているのは全体の254文字だけです。

この対応表は単体テスト（`internal/accounts/email_test.go`、第7章）として残してあるので、将来どちらかの側の規則が変わっても、食い違いにすぐ気づけます。

もう一つ、登録の入口では、メールアドレスの前後の空白を取り除いてから検証と保存に回します。これも EmailStr の実挙動（前後の空白は取り除かれる）に合わせたもので、Python 版ではスキーマの層で起きていたことを、Go 版ではハンドラの入口で行っています。保存時の形も Python 版に合わせ、ドメイン部分だけを小文字化します（`Carol@Example.COM` は `Carol@example.com` として残る）。大文字小文字を無視した照合は、DB 側の `lower()` と第1章の関数索引が担います。

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

## 3.2 サービス: 登録と認証

`CreateUser` の流れはこうです。

1. まず `lower()` で照合して、同じユーザー名・メールアドレスが無いか確かめます（表示用の綴り違いもここで拾います）。
2. bcrypt でパスワードのハッシュを作ります（コスト 12、Python 版と同じ）。
3. 登録は専用のトランザクションで入れます。万が一、確認と挿入のあいだに競合して一意制約に触れたら、制約違反（SQLSTATE 23505）を「すでに使われています」に翻訳して返します。利用者には 500 ではなく、理由の分かる 422 が届きます。

`AuthenticateUser` は、メールアドレスを `lower()` で引いて照合します。ユーザーが居ない場合もダミーのハッシュで照合を1回行い、応答時間から「居る・居ない」が漏れないようにしています。

### パスワードの72バイト

ここには、査読で見つかった大事な境界があります。bcrypt というアルゴリズムは、**先頭の72バイトまでしかパスワードとして使いません**。

- Python 版（passlib + bcrypt）は、72バイトを超える入力を黙って切り捨てます。100文字のパスワードでも登録でき、72バイトより後ろが違うパスワードでもログインできてしまいます（それが bcrypt の仕様です）。
- Go の `golang.org/x/crypto/bcrypt` は、超えたらエラーを返します。何も考えずに移植すると、長いパスワードの登録が 500 になり、さらに深刻なことに、**Python 版で長いパスワードで作ったアカウントは Go 版では一生ログインできません**。

そこで Go 版は、ハッシュ化と照合の両方の入口で、先に72バイトへ切り詰める `passwordBytes` を通すことにしました。これで両版の意味が揃います。確認も実物で行っていて、passlib が作ったハッシュ（104バイトのパスワード）を Go 側で照合できることを単体テストに残し、実サーバーでも登録・ログインを確認しました。

`password:` の保存形式は bcrypt のハッシュ文字列で、`$2a$`（Go）と `$2b$`（Python）の違いは照合時に吸収されます。

Karma の増減（`UpdateKarma`）は、読み出して足して書き戻すのではなく、SQL の側で「現在の値に差分を足す」更新を1文で当てる形にしています。投票のときに同時に走っても数がずれないためです（第5章）。

なお、サービス層の関数は第一引数に `Executor` という小さなインターフェースを取ります。`*sql.DB`（プール）でも `*sql.Tx`（トランザクション）でも同じ顔で渡せるようにするためで、投票のように「全部を1つのトランザクションで」という処理を書くときの土台になります。

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

## 3.3 画面: 登録・ログイン・ログアウト・プロフィール

画面側の約束事は Python 版と同じです。

- 登録に成功すると、そのままログイン状態にしてホームへ 303 で送ります（二度手間をさせません）。
- 検証に失敗したら 422 でフォームを描き直し、項目ごとのエラーを出し、**入力した値（パスワード以外）は残します**。
- ログインの失敗は 401 で、理由は「メールアドレスまたはパスワードが違います」と曖昧にします（どちらが違うか教えると、登録の有無を調べる手がかりになるため）。メールアドレスは残し、パスワードは残しません。
- すでにログインしている人が登録・ログイン画面を開いたら、ホームへ送り返します。
- ログアウトは、セッション Cookie を消してログイン画面へ送ります。ログインしていなくても同じ動きです（Python 版と同じ）。

プロフィールのハンドラもこのファイルにあります。並び替え（sort）の既定値がフィードと違って `new` であること、居ないユーザーは通常の 404 ページを出すこと、`sort` や `page` が不正なら対象の照会より先に 422 を返すこと（FastAPI が引数を先に検証する順序に合わせたもの）が要点です。表示の中身（Karma の `1.5k` のような省略表記など）は第6章で扱います。

```go
package web

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"redditclone/internal/accounts"
	"redditclone/internal/posts"
)

// Handlers for registration, login and logout, mirroring the Python
// version's app/accounts/router.py.

type registerPage struct {
	PageData
	Errors   accounts.FormErrors
	Username string
	Email    string
}

type loginPage struct {
	PageData
	Error string
	Email string
}

func (s *Server) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	if s.currentUser(r) != nil {
		redirect303(w, r, "/")
		return
	}
	s.render(w, "accounts/register", http.StatusOK,
		registerPage{PageData: s.baseData(r)})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	username := r.FormValue("username")
	email := r.FormValue("email")
	password := r.FormValue("password")

	page := registerPage{
		PageData: s.baseData(r),
		Username: username,
		Email:    email,
	}

	// EmailStr strips surrounding whitespace at the schema boundary
	// in the Python version; the value validated and stored is the
	// trimmed one (the form keeps showing what was typed).
	trimmedEmail := strings.TrimSpace(email)
	if errs := accounts.ValidateUserCreate(username, trimmedEmail, password); !errs.Empty() {
		page.Errors = errs
		s.render(w, "accounts/register", http.StatusUnprocessableEntity, page)
		return
	}

	user, err := accounts.CreateUser(r.Context(), s.db, username, trimmedEmail, password)
	if errors.Is(err, accounts.ErrDuplicate) {
		page.Errors.General = accounts.ErrDuplicate.Error()
		s.render(w, "accounts/register", http.StatusUnprocessableEntity, page)
		return
	}
	if err != nil {
		log.Printf("register: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Registration logs the user in immediately.
	s.sessions.SetCookie(w, user.ID)
	redirect303(w, r, "/")
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if s.currentUser(r) != nil {
		redirect303(w, r, "/")
		return
	}
	s.render(w, "accounts/login", http.StatusOK,
		loginPage{PageData: s.baseData(r)})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	email := r.FormValue("email")
	password := r.FormValue("password")

	user, err := accounts.AuthenticateUser(r.Context(), s.db, email, password)
	if err != nil {
		log.Printf("login: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		// The failure reason stays ambiguous, as in the Python version.
		s.render(w, "accounts/login", http.StatusUnauthorized, loginPage{
			PageData: s.baseData(r),
			Error:    "メールアドレスまたはパスワードが違います",
			Email:    email,
		})
		return
	}

	s.sessions.SetCookie(w, user.ID)
	redirect303(w, r, "/")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.sessions.ClearCookie(w)
	redirect303(w, r, "/login")
}

type profilePage struct {
	PageData
	ProfileUser   accounts.User
	ProfileYear   int
	ProfileMonth  int
	Posts         []posts.Post
	Sort          string
	Page          int
	HasMore       bool
	ShowSubreddit bool
}

// handleProfile renders the public profile page. A missing user gets
// the 404 page with a 404 status, as in the Python version. The
// sort/page parameters are validated before the lookup, matching
// FastAPI's order (declared parameters are validated before the
// handler runs), so a bad sort on a missing user is a 422, not a 404.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	sort, page, ok := feedParamsDefault(w, r, "new")
	if !ok {
		return
	}
	username := r.PathValue("username")
	profileUser, err := accounts.GetUserByUsername(r.Context(), s.db, username)
	if err != nil {
		log.Printf("profile lookup: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if profileUser == nil {
		s.render(w, "404", http.StatusNotFound, s.baseData(r))
		return
	}
	list, hasMore, err := posts.ListUserPosts(r.Context(), s.db,
		profileUser.ID, sort, page)
	if err != nil {
		log.Printf("profile posts: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.render(w, "accounts/profile", http.StatusOK, profilePage{
		PageData:      s.baseData(r),
		ProfileUser:   *profileUser,
		ProfileYear:   profileUser.InsertedAt.Year(),
		ProfileMonth:  int(profileUser.InsertedAt.Month()),
		Posts:         list,
		Sort:          sort,
		Page:          page,
		HasMore:       hasMore,
		ShowSubreddit: true,
	})
}
```

## 3.4 この章の確認

- 登録 → そのままログイン状態でホームが開くこと
- 同じ名前の大文字違い（Alice / alice）で二重登録が 422 になること
- 72バイトを超えるパスワードで登録・ログインできること
- 間違ったパスワードでは 401 になり、メールアドレスだけ入力が残ること
