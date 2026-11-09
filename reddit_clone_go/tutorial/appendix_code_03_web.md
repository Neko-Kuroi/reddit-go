# Reddit クローン（Go 版）— 全コード付録 3 Web 層

正本のコードファイルの全文を機械生成で収録したものです（手写しではありません）。

## internal/web/server.go

```go
// Package web is the HTTP layer: routing, handlers and templates.
package web

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"redditclone/internal/accounts"
	"redditclone/internal/auth"
	"redditclone/internal/communities"
	"redditclone/internal/config"
	"redditclone/internal/posts"
)

//go:embed templates all:static
var assets embed.FS

// UserView is the signed-in user as the layout needs it.
type UserView struct {
	ID       string
	Username string
}

// PageData is the shared data every full page receives.
type PageData struct {
	CurrentUser *UserView
	Query       string
	CSRFToken   string
}

// postListData is the argument of the post_list fragment (also embedded
// in the subreddit page).
type postListData struct {
	CurrentUser   *UserView
	Posts         []posts.Post
	Sort          string
	Page          int
	HasMore       bool
	Subreddit     communities.Subreddit
	ShowSubreddit bool
}

// joinButtonData is the argument of the join_button fragment.
type joinButtonData struct {
	Subreddit communities.Subreddit
	Joined    bool
}

// Server holds the HTTP layer's dependencies.
type Server struct {
	cfg       config.Config
	db        *sql.DB
	sessions  *auth.Sessions
	pages     map[string]*template.Template
	fragments map[string]*template.Template
}

// New parses the templates and returns the server.
func New(cfg config.Config, db *sql.DB) (*Server, error) {
	pages, err := parsePages()
	if err != nil {
		return nil, err
	}
	fragments, err := parseFragments()
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:       cfg,
		db:        db,
		sessions:  auth.NewSessions(cfg.SecretKey, cfg.SessionCookieName, !cfg.Debug),
		pages:     pages,
		fragments: fragments,
	}, nil
}

// componentFiles are parsed into every full page.
var componentFiles = []string{
	"templates/components/post_card.html",
	"templates/components/report_modal.html",
	"templates/components/post_list.html",
	"templates/components/join_button.html",
	"templates/components/vote_buttons.html",
	"templates/components/comment_tree.html",
}

// parsePages builds one template set per page: the base layout, the
// shared components, and the page itself. Page names are paths under
// templates/ without ".html" (for example "posts/home").
func parsePages() (map[string]*template.Template, error) {
	pages := map[string]*template.Template{}
	err := fs.WalkDir(assets, "templates", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".html") ||
			p == "templates/base.html" || strings.HasPrefix(p, "templates/components/") {
			return nil
		}
		files := append([]string{"templates/base.html"}, componentFiles...)
		files = append(files, p)
		t, err := template.New("").Funcs(templateFuncs()).ParseFS(assets, files...)
		if err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
		page := strings.TrimSuffix(strings.TrimPrefix(p, "templates/"), ".html")
		pages[page] = t
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pages, nil
}

// parseFragments builds the standalone HTMX fragments.
func parseFragments() (map[string]*template.Template, error) {
	defs := map[string][]string{
		"post_list": {
			"templates/components/post_card.html",
			"templates/components/post_list.html",
		},
		"join_button": {
			"templates/components/join_button.html",
		},
		"vote_buttons": {
			"templates/components/vote_buttons.html",
		},
		"report_reasons": {
			"templates/components/report_reasons.html",
		},
	}
	fragments := map[string]*template.Template{}
	for name, files := range defs {
		t, err := template.New("").Funcs(templateFuncs()).ParseFS(assets, files...)
		if err != nil {
			return nil, fmt.Errorf("parse fragment %s: %w", name, err)
		}
		fragments[name] = t
	}
	return fragments, nil
}

// Handler returns the root handler with all routes, wrapped in the
// CSRF middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("GET /register", s.handleRegisterPage)
	mux.HandleFunc("POST /register", s.handleRegister)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /subreddits/new", s.handleNewSubredditPage)
	mux.HandleFunc("POST /subreddits/new", s.handleCreateSubreddit)
	mux.HandleFunc("GET /r/{name}", s.handleSubredditPage)
	mux.HandleFunc("GET /r/{name}/posts", s.handleLoadMorePosts)
	mux.HandleFunc("POST /r/{name}/join", s.handleJoin)
	mux.HandleFunc("POST /r/{name}/leave", s.handleLeave)
	mux.HandleFunc("GET /r/{name}/submit", s.handleNewPostPage)
	mux.HandleFunc("POST /r/{name}/submit", s.handleCreatePost)
	mux.HandleFunc("GET /r/{name}/{post_id}", s.handlePostDetail)
	mux.HandleFunc("POST /posts/{post_id}/vote", s.handleVotePost)
	mux.HandleFunc("POST /posts/{post_id}/delete", s.handleDeletePost)
	mux.HandleFunc("POST /posts/{post_id}/comments", s.handleCreateComment)
	mux.HandleFunc("POST /comments/{comment_id}/vote", s.handleVoteComment)
	mux.HandleFunc("POST /comments/{comment_id}/delete", s.handleDeleteComment)
	mux.HandleFunc("POST /reports", s.handleCreateReport)
	mux.HandleFunc("GET /components/report-reasons", s.handleReportReasons)
	mux.HandleFunc("GET /u/{username}", s.handleProfile)
	mux.HandleFunc("GET /search", s.handleSearch)
	staticFS, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/",
		http.StripPrefix("/static/", http.FileServerFS(staticFS)))
	return s.csrfMiddleware(mux)
}

// currentUser resolves the session cookie to a user, mirroring the
// Python version's get_current_user dependency. It returns nil when
// logged out (or when anything about the session is off).
func (s *Server) currentUser(r *http.Request) *accounts.User {
	userID := s.sessions.UserID(r)
	if userID == "" {
		return nil
	}
	user, err := accounts.GetUserByID(r.Context(), s.db, userID)
	if err != nil {
		log.Printf("current user lookup: %v", err)
		return nil
	}
	return user
}

// requireUser returns the signed-in user, or redirects to the login
// page (303) and returns nil — the Python version's require_auth.
func (s *Server) requireUser(w http.ResponseWriter, r *http.Request) *accounts.User {
	user := s.currentUser(r)
	if user == nil {
		redirect303(w, r, "/login")
		return nil
	}
	return user
}

func userView(u *accounts.User) *UserView {
	if u == nil {
		return nil
	}
	return &UserView{ID: u.ID, Username: u.Username}
}

// baseData builds the shared page data for a request.
func (s *Server) baseData(r *http.Request) PageData {
	return PageData{
		CurrentUser: userView(s.currentUser(r)),
		Query:       r.URL.Query().Get("q"),
		CSRFToken:   CSRFToken(r),
	}
}

// baseDataFor builds the shared page data when the user is already
// resolved (avoids a second lookup).
func (s *Server) baseDataFor(r *http.Request, u *accounts.User) PageData {
	return PageData{
		CurrentUser: userView(u),
		Query:       r.URL.Query().Get("q"),
		CSRFToken:   CSRFToken(r),
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	w.Header().Set("Content-Type", "application/json")
	if err := s.db.PingContext(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error", "database": "down",
		})
		return
	}
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok", "database": "ok",
	})
}

// render executes the named page into a buffer first, so a template
// error does not produce a half-written response.
func (s *Server) render(w http.ResponseWriter, page string, status int, data any) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "page not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "base", data); err != nil {
		log.Printf("render %s: %v", page, err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

// renderFragment executes a standalone HTMX fragment.
func (s *Server) renderFragment(w http.ResponseWriter, name string, data any) {
	t, ok := s.fragments[name]
	if !ok {
		http.Error(w, "fragment not found", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("render fragment %s: %v", name, err)
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// redirect303 sends a 303 See Other, the redirect the Python version
// uses after form posts.
func redirect303(w http.ResponseWriter, r *http.Request, url string) {
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// detailError writes a JSON error in FastAPI's shape: {"detail": ...}.
// A nil detail renders as {"detail": null}, matching a bare
// HTTPException(status_code=...) in the Python version.
func detailError(w http.ResponseWriter, status int, detail any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"detail": detail})
}

// feedParams validates the sort/page query parameters the way the
// Python version's Literal/Query(ge=1) declarations do. On failure it
// writes a 422 and returns ok=false.
func feedParams(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	return feedParamsDefault(w, r, "hot")
}

// feedParamsDefault is feedParams with a different default sort (the
// profile page defaults to "new").
func feedParamsDefault(w http.ResponseWriter, r *http.Request,
	defaultSort string) (string, int, bool) {

	sort := r.URL.Query().Get("sort")
	if sort == "" {
		sort = defaultSort
	}
	if sort != "hot" && sort != "new" {
		detailError(w, http.StatusUnprocessableEntity,
			"sort は hot または new を指定してください")
		return "", 0, false
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		p, err := strconv.Atoi(raw)
		if err != nil || p < 1 {
			detailError(w, http.StatusUnprocessableEntity,
				"page は1以上の整数を指定してください")
			return "", 0, false
		}
		page = p
	}
	return sort, page, true
}
```

## internal/web/csrf.go

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

## internal/web/funcs.go

```go
package web

import (
	"fmt"
	"html/template"
	"net/url"
	"time"

	"redditclone/internal/comments"
	"redditclone/internal/posts"
	"redditclone/internal/reports"
)

// Template helpers, mirroring the Python version's app/templating.py
// filters and globals.

// postCardData is the argument of the post_card component.
type postCardData struct {
	Post          posts.Post
	CurrentUser   *UserView
	ShowSubreddit bool
}

// reportReason is one entry of the report modal's reason list.
type reportReason struct {
	Value string
	Label string
}

// reportReasons comes from the reports service (order and labels).
var reportReasons = func() []reportReason {
	out := make([]reportReason, 0, len(reports.ValidReasons))
	for _, v := range reports.ValidReasons {
		out = append(out, reportReason{Value: v, Label: reports.ReasonLabels[v]})
	}
	return out
}()

// voteButtonsData is the argument of the vote_buttons component.
type voteButtonsData struct {
	TargetType string // "post" | "comment"
	TargetID   string
	Score      int
}

// commentNode is the argument of the recursive comment_tree component.
type commentNode struct {
	Comment     *comments.Comment
	CurrentUser *UserView
	CSRFToken   string
	Depth       int
}

func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"timeAgo":     timeAgo,
		"urlHost":     urlHost,
		"formatKarma": formatKarma,
		"add":         func(a, b int) int { return a + b },
		"card": func(p posts.Post, u *UserView, show bool) postCardData {
			return postCardData{Post: p, CurrentUser: u, ShowSubreddit: show}
		},
		"reportReasons": func() []reportReason { return reportReasons },
		"voteBtn": func(targetType, targetID string, score int) voteButtonsData {
			return voteButtonsData{TargetType: targetType, TargetID: targetID, Score: score}
		},
		"node": func(c *comments.Comment, u *UserView, csrf string) commentNode {
			return commentNode{Comment: c, CurrentUser: u, CSRFToken: csrf, Depth: 0}
		},
		"kid": func(n commentNode, c *comments.Comment) commentNode {
			n.Comment = c
			n.Depth++
			return n
		},
	}
}

// timeAgo renders a relative time like the Python filter.
func timeAgo(dt time.Time) string {
	diff := int(time.Since(dt).Seconds())
	switch {
	case diff < 60:
		return "たった今"
	case diff < 3600:
		return fmt.Sprintf("%d分前", diff/60)
	case diff < 86400:
		return fmt.Sprintf("%d時間前", diff/3600)
	case diff < 86400*30:
		return fmt.Sprintf("%d日前", diff/86400)
	default:
		return fmt.Sprintf("%dヶ月前", diff/(86400*30))
	}
}

// formatKarma renders karma like the Python filter: 1000 and above
// become k notation with one decimal.
func formatKarma(karma int) string {
	if karma >= 1000 {
		return fmt.Sprintf("%.1fk", float64(karma)/1000)
	}
	return fmt.Sprintf("%d", karma)
}

// urlHost returns the host part of a URL, or the URL itself when it
// cannot be parsed (the Python urlparse_host filter).
func urlHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return raw
	}
	return u.Hostname()
}
```

## internal/web/uuid.go

```go
package web

import (
	"regexp"
	"strings"
)

// Minimal UUID handling (the port avoids an external uuid module).
// Accepts the canonical hyphenated form and the bare 32-hex-digit
// form, as Python's uuid.UUID does, and normalizes to canonical
// lowercase.

var (
	uuidCanonicalRE = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	uuidBareRE = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
)

// parseUUID validates and normalizes a UUID string.
func parseUUID(s string) (string, bool) {
	if uuidCanonicalRE.MatchString(s) {
		return strings.ToLower(s), true
	}
	if uuidBareRE.MatchString(s) {
		s = strings.ToLower(s)
		return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], true
	}
	return "", false
}
```

## internal/web/accounts.go

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

## internal/web/communities.go

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

## internal/web/posts.go

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

## internal/web/comments.go

```go
package web

import (
	"errors"
	"fmt"
	"log"
	"net/http"

	"redditclone/internal/comments"
	"redditclone/internal/posts"
	"redditclone/internal/reports"
)

// Handlers for comments and reports, mirroring the Python version's
// app/comments/router.py.

func (s *Server) handleCreateComment(w http.ResponseWriter, r *http.Request) {
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

	parentID := ""
	if raw := r.FormValue("parent_id"); raw != "" {
		parsed, ok := parseUUID(raw)
		if !ok {
			detailError(w, http.StatusUnprocessableEntity,
				"parent_id の形式が正しくありません")
			return
		}
		parentID = parsed
	}

	_, err = comments.CreateComment(r.Context(), s.db,
		r.FormValue("body"), user.ID, postID, parentID)
	if err != nil {
		if errors.Is(err, comments.ErrBodyRequired) ||
			errors.Is(err, comments.ErrBodyTooLong) ||
			errors.Is(err, comments.ErrParentNotFound) ||
			errors.Is(err, comments.ErrParentWrongPost) {
			detailError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		log.Printf("create comment: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirect303(w, r, "/r/"+post.SubredditName+"/"+postID)
}

func (s *Server) handleVoteComment(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	commentID, ok := parseUUID(r.PathValue("comment_id"))
	if !ok {
		detailError(w, http.StatusUnprocessableEntity, "comment_id の形式が正しくありません")
		return
	}
	value, ok := voteValue(w, r)
	if !ok {
		return
	}
	comment, err := comments.VoteComment(r.Context(), s.db, user.ID, commentID, value)
	if err != nil {
		if errors.Is(err, comments.ErrInvalidValue) ||
			errors.Is(err, comments.ErrCommentNotFound) ||
			errors.Is(err, comments.ErrOwnCommentVote) {
			voteError(w, err)
			return
		}
		log.Printf("vote comment: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	s.renderFragment(w, "vote_buttons", voteButtonsData{
		TargetType: "comment", TargetID: commentID, Score: comment.Score,
	})
}

func (s *Server) handleDeleteComment(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	commentID, ok := parseUUID(r.PathValue("comment_id"))
	if !ok {
		detailError(w, http.StatusUnprocessableEntity, "comment_id の形式が正しくありません")
		return
	}
	// The return address is built from the comment's own data, never
	// from a Referer header (as fixed in the Python version).
	ctx, err := comments.GetContext(r.Context(), s.db, commentID)
	if err != nil {
		log.Printf("comment context: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if ctx == nil {
		detailError(w, http.StatusNotFound, nil)
		return
	}
	if err := comments.DeleteComment(r.Context(), s.db, commentID, user.ID, ctx.SubredditID); err != nil {
		if errors.Is(err, comments.ErrForbidden) {
			detailError(w, http.StatusForbidden, "削除する権限がありません")
			return
		}
		if errors.Is(err, comments.ErrCommentNotFound) {
			detailError(w, http.StatusNotFound, nil)
			return
		}
		log.Printf("delete comment: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	redirect303(w, r, "/r/"+ctx.SubredditName+"/"+ctx.PostID)
}

// reportError writes the small red error paragraph the Python version
// returns for a rejected report (status 400, message escaped).
func reportError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<p class="text-red-500 text-sm">%s</p>`,
		pyEscape(message))
}

func (s *Server) handleCreateReport(w http.ResponseWriter, r *http.Request) {
	user := s.requireUser(w, r)
	if user == nil {
		return
	}
	targetID, ok := parseUUID(r.FormValue("target_id"))
	if !ok {
		// The Python version lets uuid.UUID() raise inside its error
		// handling, so a malformed id comes back as this message.
		reportError(w, "badly formed hexadecimal UUID string")
		return
	}
	err := reports.CreateReport(r.Context(), s.db, user.ID,
		r.FormValue("target_type"), targetID, r.FormValue("reason"))
	if err != nil {
		reportError(w, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `<p class="text-green-600 text-sm text-center py-2">報告しました。ご協力ありがとうございます。</p>`)
}

func (s *Server) handleReportReasons(w http.ResponseWriter, r *http.Request) {
	s.renderFragment(w, "report_reasons", nil)
}
```

