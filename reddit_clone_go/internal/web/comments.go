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
