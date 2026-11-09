// Package posts is the post service layer, mirroring the Python
// version's app/posts/services.py (creation and feeds; voting and
// deletion arrive in Phase 4).
package posts

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

// PageSize is the feed page size (PAGE_SIZE in the Python version).
const PageSize = 25

// Post is one row of the posts table plus the fields the templates
// display (author name, subreddit name, comment count).
type Post struct {
	ID             string
	Title          string
	Type           string // "text" | "link"
	Body           string // "" when NULL
	URL            string // "" when NULL
	Score          int
	HotScore       float64
	AuthorID       string
	SubredditID    string
	InsertedAt     time.Time
	AuthorUsername string
	SubredditName  string
	CommentCount   int
}

// CreateInput is a validated post submission.
type CreateInput struct {
	Title string
	Type  string
	Body  *string // text posts
	URL   *string // link posts
}

var hotEpoch = time.Date(2005, 12, 8, 7, 46, 43, 0, time.UTC)

// ComputeHotScore is Reddit's hot formula with the sign kept, exactly
// as in the Python version's Post.compute_hot_score (and the Elixir
// version's signed formula).
func ComputeHotScore(score int, insertedAt time.Time) float64 {
	sign := 0
	if score > 0 {
		sign = 1
	} else if score < 0 {
		sign = -1
	}
	order := float64(sign) * math.Log10(math.Max(math.Abs(float64(score)), 1))
	seconds := insertedAt.Sub(hotEpoch).Seconds()
	return order + seconds/45000
}

// CreatePost inserts the post with its initial hot score.
func CreatePost(ctx context.Context, db *sql.DB,
	in CreateInput, authorID, subredditID string) (Post, error) {

	hot := ComputeHotScore(0, time.Now().UTC())
	var body, url any
	if in.Body != nil {
		body = *in.Body
	}
	if in.URL != nil {
		url = *in.URL
	}
	var post Post
	err := db.QueryRowContext(ctx,
		`INSERT INTO posts (title, type, body, url, hot_score, author_id, subreddit_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, title, type, COALESCE(body, ''), COALESCE(url, ''),
		           score, hot_score, author_id, subreddit_id, inserted_at`,
		in.Title, in.Type, body, url, hot, authorID, subredditID).
		Scan(&post.ID, &post.Title, &post.Type, &post.Body, &post.URL,
			&post.Score, &post.HotScore, &post.AuthorID, &post.SubredditID,
			&post.InsertedAt)
	return post, err
}

const postSelect = `
	SELECT p.id, p.title, p.type, COALESCE(p.body, ''), COALESCE(p.url, ''),
	       p.score, p.hot_score, p.author_id, p.subreddit_id, p.inserted_at,
	       a.username, s.name, COUNT(c.id)
	FROM posts p
	JOIN users a ON a.id = p.author_id
	JOIN subreddits s ON s.id = p.subreddit_id
	LEFT JOIN comments c ON c.post_id = p.id`

func scanPost(row interface{ Scan(...any) error }) (Post, error) {
	var p Post
	err := row.Scan(&p.ID, &p.Title, &p.Type, &p.Body, &p.URL,
		&p.Score, &p.HotScore, &p.AuthorID, &p.SubredditID, &p.InsertedAt,
		&p.AuthorUsername, &p.SubredditName, &p.CommentCount)
	return p, err
}

// GetPost returns one post with display fields, or (nil, nil).
func GetPost(ctx context.Context, db *sql.DB, postID string) (*Post, error) {
	post, err := scanPost(db.QueryRowContext(ctx,
		postSelect+` WHERE p.id = $1 GROUP BY p.id, a.username, s.name`, postID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func sortClause(sort string) string {
	if sort == "new" {
		return ` ORDER BY p.inserted_at DESC, p.id DESC`
	}
	return ` ORDER BY p.hot_score DESC, p.id DESC`
}

// listFeed runs the shared feed query. It fetches one row past the
// page to decide hasMore, exactly like the Python version.
func listFeed(ctx context.Context, db *sql.DB,
	where string, arg any, sort string, page int) ([]Post, bool, error) {

	query := postSelect + where +
		` GROUP BY p.id, a.username, s.name` + sortClause(sort) +
		` LIMIT $2 OFFSET $3`
	rows, err := db.QueryContext(ctx, query, arg, PageSize+1, (page-1)*PageSize)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, false, err
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(posts) > PageSize
	if hasMore {
		posts = posts[:PageSize]
	}
	return posts, hasMore, nil
}

// ListPosts is the subreddit feed.
func ListPosts(ctx context.Context, db *sql.DB,
	subredditID, sort string, page int) ([]Post, bool, error) {
	return listFeed(ctx, db, ` WHERE p.subreddit_id = $1`, subredditID, sort, page)
}

// ListHomePosts is the home feed: posts of the subreddits the user
// has joined.
func ListHomePosts(ctx context.Context, db *sql.DB,
	userID, sort string, page int) ([]Post, bool, error) {
	return listFeed(ctx, db,
		` JOIN subreddit_members m ON m.subreddit_id = p.subreddit_id
		  WHERE m.user_id = $1`, userID, sort, page)
}

// ListUserPosts is the profile feed (used in Phase 5).
func ListUserPosts(ctx context.Context, db *sql.DB,
	userID, sort string, page int) ([]Post, bool, error) {
	return listFeed(ctx, db, ` WHERE p.author_id = $1`, userID, sort, page)
}

// SearchPosts finds posts whose title contains the query
// (case-insensitive), hottest first, at most 25 — the Python version's
// search_posts. The ILIKE pattern keeps the query's % and _ as
// wildcards, exactly as in the Python version.
func SearchPosts(ctx context.Context, db *sql.DB, query string) ([]Post, error) {
	if query == "" {
		return nil, nil
	}
	rows, err := db.QueryContext(ctx, postSelect+`
		WHERE p.title ILIKE $1
		GROUP BY p.id, a.username, s.name
		ORDER BY p.hot_score DESC, p.id DESC
		LIMIT 25`, "%"+query+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		post, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, post)
	}
	return out, rows.Err()
}
