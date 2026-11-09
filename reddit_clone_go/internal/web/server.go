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
