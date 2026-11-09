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
