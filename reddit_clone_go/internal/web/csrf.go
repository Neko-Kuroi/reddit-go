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
