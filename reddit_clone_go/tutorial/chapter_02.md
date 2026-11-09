# 第2章 認証の部品

機能を作り始める前に、土台の部品を二つ用意します。どちらも Python 版にあった仕組みの移植です。

1. **セッション**（`internal/auth/session.go`）— ログイン状態を署名つき Cookie で運ぶ
2. **CSRF 対策**（`internal/web/csrf.go`）— 二重送信（ダブルサブミット）方式のミドルウェア

## 2.1 セッションの考え方

このアプリのセッションは、サーバー側に記録を持たない方式です。Cookie の中身自体が「誰が・いつログインしたか」で、改ざんできないよう署名がついています。Python 版は itsdangerous という部品で作っていました。Go 版は標準の暗号部品で同じ意味のものを自作しています（バイト列まで同じ互換ではなく、**意味の互換**です。Cookie を作るのも読むのも同じ版のサーバーなので、それで足ります）。

トークンの形はこうです。

```
base64url(JSON) + "." + base64url(署名)
```

- JSON の中身は `{"uid": ユーザーID, "iat": 発行時刻（UNIX秒）}` です。
- 署名は HMAC-SHA256 です。ただし鍵は `SECRET_KEY` をそのまま使わず、itsdangerous と同じ流儀で、ソルト `"session"` と組み合わせて鍵を導出してから署名します。
- 有効期間は7日間です。発行時刻が未来のもの（時計のずれた別サーバーで作られたものなど）も受け付けません。

読み取りの入口は `Sessions.UserID(r)` で、「いまのリクエストは誰のものか」を返します。Web 層はこれを使って現在のユーザーを引く（第6章の `currentUser`）ので、ハンドラ側が Cookie を直接触ることはありません。

発行側の設定も大事なところです。Cookie には `HttpOnly`（JavaScript から読めない）と `SameSite=Lax` をつけ、`Secure`（HTTPS のときだけ送る）は DEBUG でないときだけ有効にします。地元の HTTP で確認するときに困らないよう、DEBUG=true なら Secure を外す、という Python 版と同じ扱いです。

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

## 2.2 CSRF の考え方

CSRF（クロスサイトリクエストフォージェリ）は、ログイン中の利用者のブラウザを騙して、別サイトから勝手にフォーム送信させる攻撃です。この教材では、Python 版で採用した**ダブルサブミットクッキー方式**をそのまま移植しました。

仕組みは三段です。

1. サーバーは、ランダムなトークンを `csrf_token` という Cookie で配ります。この Cookie は `HttpOnly` ですがセッションとは別物で、ページのフォームには同じトークンが隠しフィールドとして埋め込まれます。
2. 送信（POST など）のとき、ブラウザは Cookie とフォーム（または `X-CSRFToken` ヘッダー）の両方にトークンを載せます。
3. サーバーは両方が一致するかを見ます。一致しなければ 403 で、Python 版と同じ文面の JSON を返します。

別サイトからの偽の送信は、Cookie は自動で付いてきますが、フォーム側のトークンを知る方法がありません（他サイトのページは、このサイトの Cookie を読めないため）。そこが効きどころです。

実装の工夫は、トークンの配り方です。Go のミドルウェアは、応答を書き始める瞬間に割り込む `ResponseWriter` の包み（`csrfResponseWriter`）を使い、Cookie が無い・形式が変なときにだけ新しいトークンを配って、本文中のフォームが参照できるようリクエストの文脈にも同じ値を置きます。安全なメソッド（GET など）は検証を素通りします。

```go
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

// CSRF protection, mirroring the Python version's app/csrf.py
// (double-submit cookie):
//
//   - Issuance: a request without a valid csrf_token cookie receives a
//     fresh random token. The token is set as a cookie on the response
//     and stored in the request context, so templates rendered by the
//     same request can already use it (the Python version injects it
//     into the Cookie header for the same purpose).
//   - Verification: for non-safe methods, the X-CSRFToken / X-CSRF-Token
//     header (HTMX) or the csrf_token form field must match the cookie
//     value in constant time. Failure is a 403 with a JSON detail body,
//     as FastAPI renders HTTPException.
const csrfCookieName = "csrf_token"

var csrfTokenRE = regexp.MustCompile(`^[A-Za-z0-9_-]{40,}$`)

var csrfSafeMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true,
	http.MethodOptions: true, http.MethodTrace: true,
}

type csrfContextKey struct{}

// CSRFToken returns the effective CSRF token for the request (from the
// cookie, or newly issued by the middleware).
func CSRFToken(r *http.Request) string {
	if v, ok := r.Context().Value(csrfContextKey{}).(string); ok {
		return v
	}
	return ""
}

func generateCSRFToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func csrfTokensMatch(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// csrfResponseWriter sets the issued token's cookie just before the
// first write, whenever the response is produced.
type csrfResponseWriter struct {
	http.ResponseWriter
	token string
	wrote bool
}

func (w *csrfResponseWriter) WriteHeader(status int) {
	if !w.wrote {
		w.wrote = true
		http.SetCookie(w.ResponseWriter, &http.Cookie{
			Name:     csrfCookieName,
			Value:    w.token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *csrfResponseWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (s *Server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if c, err := r.Cookie(csrfCookieName); err == nil &&
			csrfTokenRE.MatchString(c.Value) {
			token = c.Value
		}
		issued := token == ""
		if issued {
			token = generateCSRFToken()
		}
		r = r.WithContext(context.WithValue(r.Context(), csrfContextKey{}, token))
		if issued {
			w = &csrfResponseWriter{ResponseWriter: w, token: token}
		}

		if !csrfSafeMethods[r.Method] && !verifyCSRF(r, token) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{
				"detail": "CSRFトークンの検証に失敗しました",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func verifyCSRF(r *http.Request, cookieToken string) bool {
	for _, header := range []string{"X-CSRFToken", "X-CSRF-Token"} {
		if v := r.Header.Get(header); v != "" && csrfTokensMatch(v, cookieToken) {
			return true
		}
	}
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		_ = r.ParseMultipartForm(32 << 20)
	} else {
		_ = r.ParseForm()
	}
	if v := r.Form.Get(csrfCookieName); v != "" && csrfTokensMatch(v, cookieToken) {
		return true
	}
	return false
}
```

トークンの形式は「URL セーフな Base64 で40文字以上」です。これは検証の入口の形チェックであり、本当の照合は Cookie との一致（定数時間比較）で決まります。

## 2.3 この章の確認

- ログインしていない状態でページを開くと、`csrf_token` Cookie が配られること
- フォーム送信でトークンを消すと 403 になること（第7章の E2E にも項目があります）
- セッション Cookie の中身を1文字変えると、ログインしていない扱いになること
