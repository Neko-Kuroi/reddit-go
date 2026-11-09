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
