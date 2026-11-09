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
