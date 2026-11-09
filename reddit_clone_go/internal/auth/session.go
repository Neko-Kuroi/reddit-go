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
