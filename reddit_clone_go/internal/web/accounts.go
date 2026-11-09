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
