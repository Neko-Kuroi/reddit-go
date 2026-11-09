# 第6章 画面の仕上げ

この章では、Web 層の共通部品をまとめます。ルーティング、テンプレートの組み立て方、補助関数、そして残りの画面（プロフィールと検索）の読み方です。

## 6.1 ルーティング

Go 1.22 以降の標準ライブラリ `http.ServeMux` は、`"GET /r/{name}"` のようにメソッドとパスパターンを書けます。Go 版はこれを使い、Python 版の25経路＋静的ファイル＋`/health` をすべて登録しています。パターンに合わないメソッドは 405 が自動で返ります。

ハンドラの登録とあわせて、全体を第2章の CSRF ミドルウェアで包むのが `Handler()` です。つまり、どの経路も必ず CSRF の配布と検証を通ります。Python 版で「全体に掛かる依存」が先に走るのと同じ順序です。

共通の小物もこのファイルにあります。

- `currentUser`: セッションから現在のユーザーを引く（Python 版の `get_current_user` 相当）。セッションが壊れていれば未ログイン扱いです。
- `requireUser`: ログイン必須の入口（`require_auth` 相当）。未ログインなら 303 でログイン画面へ。
- `baseData`: 全ページ共通のデータ（現在のユーザー、検索語、CSRF トークン）。
- `detailError`: FastAPI と同じ形の `{"detail": ...}` の JSON を書く。
- `feedParams`: `sort` と `page` の検証（422 の条件）。プロフィール用に既定値が違う版（`feedParamsDefault`）もあり、第3章のファイルに置いてあります。

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

## 6.2 テンプレートの組み立て

テンプレートはバイナリに埋め込み、起動時に解析します。組み立て方は二通りです。

- **ページ**: 共通レイアウト（base）＋共通部品（投稿カード、報告モーダル、投稿一覧、参加ボタン、投票ボタン、コメントの木）＋そのページ自身、を1組にして解析します。
- **断片**: HTMX が差し替える部分だけ（投稿一覧、参加ボタン、投票ボタン、報告の理由）を、単独で描けるよう別に解析します。

Go の `html/template` は**自動でエスケープします**。Python 版でレビュー指摘になった「報告の理由をそのまま HTML に出す」種類の事故は、この層では起きません。逆に注意が要るのは、エスケープの細部の違い（第5章の `pyEscape`）のような、文字列そのものを比べる場面だけです。

なお、静的ファイルの配信では `all:static` という埋め込みの指定が要りました。`static/` の中身が `.gitkeep` だけの間は、プレフィックス無しでは埋め込み対象から漏れるためです。

## 6.3 補助関数

テンプレートから使う小さな関数群（`templateFuncs`）です。Python 版のカスタムフィルターに対応します。

- `timeAgo`: 「たった今 / N分前 / N時間前 / N日前 / Nヶ月前」
- `format_karma`: 1000 以上を `1.5k` のように省略する（プロフィールの Karma 表示）
- `urlHost`: リンク投稿の行き先ドメイン表示
- `add`、`card`（投稿カードへの詰め替え）、`reportReasons`（報告理由の一覧）

`format_karma` の境界（999 はそのまま、1000 は `1.0k`、12345 は `12.3k`、負数はそのまま）は単体テストで固定してあります。

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

## 6.4 UUID の最小実装

経路の中の ID は UUID です。外部モジュールを使わない方針のため、必要な分だけの判定・正規化を自前で用意しました。正規のハイフンつき形式と、ハイフン無しの32桁を受け付け、小文字の正規形に直します。Python の `uuid.UUID` が受け付ける波括弧つきや `urn:uuid:` の形は受け付けませんが、ブラウザから届く通常の経路では現れません（査読で記録した軽微な差の一つです）。

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

## 6.5 プロフィールと検索の読み方

残る二画面は、第3章・第4章のファイルに実装があります。読みどころだけ挙げます。

**プロフィール**（`handleProfile`、`internal/web/accounts.go`）

- 公開画面です（ログインしていなくても見られます）。ユーザーの引き方は `lower()` です。
- 居ないユーザーは、JSON ではなく通常の 404 ページ（`templates/404.html`）を 404 で返します。
- 登録年月（「2026年11月に登録」）と、投稿・コメント・合計の Karma（`format_karma` 適用）が並びます。
- `sort` の既定値は `new` で、フィードの `hot` とは違います。並び替えタブやページ送りの UI は、パラメータはあっても画面にはありません。Python 版の癖をそのまま写したものです。

**検索**（`handleSearch`、`internal/web/posts.go`）

- 投稿は題名の部分一致（ILIKE）で Hot 順に25件、コミュニティは名前と説明の部分一致で名前順に10件です。
- 検索語の中の `%` や `_` はエスケープされません。ILIKE の模様文字として効きますが、これも Python 版と同じ扱いです。
- キーワードが空なら、両方とも空の結果で「キーワードを入力して検索してください」を出します。

テンプレートの全文は付録4にあります。代表として、土台レイアウトと投稿カードを見てください。

```html
{{define "base"}}<!DOCTYPE html>
<html lang="ja">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{template "title" .}}</title>

  <!-- Tailwind CSS（CDN） -->
  <script src="https://cdn.tailwindcss.com"></script>

  <!-- HTMX -->
  <script src="https://unpkg.com/htmx.org@1.9.12"></script>

  <!-- CSRF トークン（HTMX のリクエストに自動付与） -->
  <!-- 値はミドルウェアが発行した Cookie と同じもの -->
  <meta name="csrf-token" content="{{.CSRFToken}}" />
  <script>
    document.addEventListener("htmx:configRequest", (e) => {
      e.detail.headers["X-CSRFToken"] = document
        .querySelector('meta[name="csrf-token"]').content;
    });
  </script>
</head>

<body class="bg-gray-100 min-h-screen">

  <!-- ナビゲーションバー -->
  <nav class="bg-white border-b border-gray-200 sticky top-0 z-30">
    <div class="max-w-5xl mx-auto px-4 h-12 flex items-center gap-4">

      <!-- ロゴ -->
      <a href="/" class="flex items-center gap-1 shrink-0">
        <span class="text-orange-500 text-xl font-black">🔴</span>
        <span class="font-bold text-gray-800 hidden sm:block">reddit_clone</span>
      </a>

      <!-- 検索フォーム -->
      <form action="/search" method="get" class="flex-1 max-w-xl">
        <div class="relative">
          <input
            type="text"
            name="q"
            value="{{.Query}}"
            placeholder="検索..."
            class="w-full border border-gray-300 rounded-full px-4 py-1.5 text-sm
                   bg-gray-50 focus:outline-none focus:border-blue-400"
          />
          <button type="submit"
            class="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400">
            🔍
          </button>
        </div>
      </form>

      <!-- 右側: 認証ボタン -->
      <div class="flex items-center gap-2 shrink-0">
        {{if .CurrentUser}}
          <a href="/u/{{.CurrentUser.Username}}"
            class="text-sm text-gray-700 hover:text-blue-500 font-medium hidden sm:block">
            u/{{.CurrentUser.Username}}
          </a>
          <a href="/subreddits/new"
            class="text-sm text-gray-500 hover:text-gray-700 px-2 hidden sm:block">
            ＋ 作成
          </a>
          <form action="/logout" method="post" class="inline">
            <input type="hidden" name="csrf_token" value="{{.CSRFToken}}" />
            <button type="submit"
              class="text-sm border border-gray-300 rounded-full px-3 py-1 hover:bg-gray-50">
              ログアウト
            </button>
          </form>
        {{else}}
          <a href="/login"
            class="text-sm border border-blue-500 text-blue-500 rounded-full px-3 py-1 hover:bg-blue-50">
            ログイン
          </a>
          <a href="/register"
            class="text-sm bg-blue-500 text-white rounded-full px-3 py-1 hover:bg-blue-600">
            登録
          </a>
        {{end}}
      </div>

    </div>
  </nav>

  <!-- メインコンテンツ -->
  <main>
    {{template "content" .}}
  </main>

  <!-- 共通スクリプト -->
  <script>
    // コメントの返信フォームを開閉する
    function toggleReplyForm(commentId) {
      document.getElementById("reply-form-" + commentId).classList.toggle("hidden");
    }
  </script>

</body>
</html>
{{end}}
```

```html
{{define "post_card"}}
<div class="bg-white border border-gray-200 rounded hover:border-gray-400 mb-2 flex">

  <!-- 投票エリア -->
  <div class="bg-gray-50 w-10 flex flex-col items-center py-2 gap-1 rounded-l"
    id="post-vote-{{.Post.ID}}">
    <button
      hx-post="/posts/{{.Post.ID}}/vote"
      hx-vals='{"value": 1}'
      hx-target="#post-vote-{{.Post.ID}}"
      hx-swap="innerHTML"
      class="text-gray-400 hover:text-orange-500"
    >▲</button>
    <span class="text-xs font-bold text-gray-700">{{.Post.Score}}</span>
    <button
      hx-post="/posts/{{.Post.ID}}/vote"
      hx-vals='{"value": -1}'
      hx-target="#post-vote-{{.Post.ID}}"
      hx-swap="innerHTML"
      class="text-gray-400 hover:text-blue-500"
    >▼</button>
  </div>

  <!-- 投稿内容 -->
  <div class="p-2 flex-1">

    <div class="flex items-start gap-2">
      <a href="/r/{{.Post.SubredditName}}/{{.Post.ID}}"
        class="text-sm font-medium text-gray-900 hover:underline flex-1">
        {{.Post.Title}}
      </a>

      {{if eq .Post.Type "link"}}
        <a href="{{.Post.URL}}" target="_blank" rel="noopener noreferrer"
          class="text-xs text-blue-500 hover:underline whitespace-nowrap">
          {{urlHost .Post.URL}} ↗
        </a>
      {{else}}
        <span class="text-xs text-gray-400">text</span>
      {{end}}
    </div>

    <div class="flex items-center gap-2 mt-1 text-xs text-gray-500">
      {{if .ShowSubreddit}}
        <a href="/r/{{.Post.SubredditName}}" class="font-medium hover:underline">
          r/{{.Post.SubredditName}}
        </a>
        <span>•</span>
      {{end}}
      <span>
        投稿者
        <a href="/u/{{.Post.AuthorUsername}}" class="hover:underline">
          u/{{.Post.AuthorUsername}}
        </a>
      </span>
      <span>•</span>
      <span>{{timeAgo .Post.InsertedAt}}</span>
    </div>

    <div class="mt-1 flex items-center gap-2">
      <a href="/r/{{.Post.SubredditName}}/{{.Post.ID}}"
        class="text-xs text-gray-500 hover:bg-gray-100 px-1 py-0.5 rounded">
        💬 {{.Post.CommentCount}} コメント
      </a>

      {{if and .CurrentUser (ne .CurrentUser.ID .Post.AuthorID)}}
        <button
          onclick="openReportModal('post', '{{.Post.ID}}')"
          class="text-xs text-gray-400 hover:text-gray-600">
          報告
        </button>
      {{end}}
    </div>

  </div>
</div>
{{end}}
```

## 6.6 この章の確認

- 存在しない経路では 404、合っている経路に違うメソッドでは 405 になること
- プロフィールで Karma が 1000 を超えると `k` 表記になること
- 検索で `%` が模様文字として効くこと（Python 版と同じ結果になること）
