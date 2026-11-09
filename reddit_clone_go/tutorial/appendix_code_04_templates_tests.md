# Reddit クローン（Go 版）— 全コード付録 4 テンプレートとテスト

正本のコードファイルの全文を機械生成で収録したものです（手写しではありません）。

## internal/web/templates/404.html

```html
{{define "title"}}ページが見つかりません · reddit_clone{{end}}
{{define "content"}}
<div class="max-w-3xl mx-auto px-4 py-16 text-center">
  <p class="text-6xl mb-4">🔍</p>
  <h1 class="text-2xl font-bold text-gray-800 mb-2">ページが見つかりません</h1>
  <p class="text-sm text-gray-500 mb-6">
    お探しのページは存在しないか、削除された可能性があります。
  </p>
  <a href="/"
    class="inline-block px-4 py-2 bg-blue-500 text-white text-sm rounded hover:bg-blue-600">
    ホームへ戻る
  </a>
</div>
{{end}}
```

## internal/web/templates/accounts/login.html

```html
{{define "title"}}ログイン · reddit_clone{{end}}
{{define "content"}}
<div class="max-w-md mx-auto mt-16 p-6">
  <h1 class="text-2xl font-bold mb-6">ログイン</h1>

  {{if .Error}}
    <div class="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded mb-4 text-sm">
      {{.Error}}
    </div>
  {{end}}

  <form action="/login" method="post" class="space-y-4">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}" />

    <!-- メールアドレス -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">
        メールアドレス
      </label>
      <input
        type="email"
        name="email"
        value="{{.Email}}"
        class="w-full border border-gray-300 rounded px-3 py-2 text-sm
               focus:outline-none focus:border-blue-400"
      />
    </div>

    <!-- パスワード -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">
        パスワード
      </label>
      <input
        type="password"
        name="password"
        class="w-full border border-gray-300 rounded px-3 py-2 text-sm
               focus:outline-none focus:border-blue-400"
      />
    </div>

    <button type="submit"
      class="w-full bg-blue-500 text-white rounded py-2 text-sm font-medium hover:bg-blue-600">
      ログイン
    </button>

  </form>

  <p class="mt-4 text-center text-sm text-gray-500">
    アカウントをお持ちでないですか？
    <a href="/register" class="text-blue-500 hover:underline">新規登録</a>
  </p>
</div>
{{end}}
```

## internal/web/templates/accounts/profile.html

```html
{{define "title"}}u/{{.ProfileUser.Username}} · reddit_clone{{end}}
{{define "content"}}
<div class="max-w-3xl mx-auto px-4 py-6">

  <!-- ユーザー情報 -->
  <div class="bg-white border border-gray-200 rounded p-6 mb-4">
    <h1 class="text-2xl font-bold text-gray-900">
      u/{{.ProfileUser.Username}}
    </h1>
    <p class="text-sm text-gray-400 mt-1">
      {{.ProfileYear}}年{{.ProfileMonth}}月に登録
    </p>

    <!-- Karma -->
    <div class="flex gap-6 mt-4 pt-4 border-t border-gray-100">
      <div class="text-center">
        <p class="text-xl font-bold text-gray-800">
          {{formatKarma .ProfileUser.PostKarma}}
        </p>
        <p class="text-xs text-gray-400">Post Karma</p>
      </div>
      <div class="text-center">
        <p class="text-xl font-bold text-gray-800">
          {{formatKarma .ProfileUser.CommentKarma}}
        </p>
        <p class="text-xs text-gray-400">Comment Karma</p>
      </div>
      <div class="text-center">
        <p class="text-xl font-bold text-gray-800">
          {{formatKarma (add .ProfileUser.PostKarma .ProfileUser.CommentKarma)}}
        </p>
        <p class="text-xs text-gray-400">合計 Karma</p>
      </div>
    </div>
  </div>

  <!-- 投稿一覧 -->
  <h2 class="text-sm font-bold text-gray-500 uppercase mb-3">投稿</h2>

  {{if .Posts}}
    {{range .Posts}}
      {{template "post_card" (card . $.CurrentUser $.ShowSubreddit)}}
    {{end}}
  {{else}}
    <div class="bg-white border border-gray-200 rounded p-8 text-center">
      <p class="text-gray-400 text-sm">まだ投稿がありません</p>
    </div>
  {{end}}

</div>

<!-- 報告モーダル -->
{{template "report_modal" .}}

{{end}}
```

## internal/web/templates/accounts/register.html

```html
{{define "title"}}ユーザー登録 · reddit_clone{{end}}
{{define "content"}}
<div class="max-w-md mx-auto mt-16 p-6">
  <h1 class="text-2xl font-bold mb-6">アカウントを作成</h1>

  {{if .Errors.General}}
    <div class="bg-red-50 border border-red-200 text-red-700 px-4 py-3 rounded mb-4 text-sm">
      {{.Errors.General}}
    </div>
  {{end}}

  <form action="/register" method="post" class="space-y-4">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}" />

    <!-- ユーザー名 -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">
        ユーザー名
      </label>
      <input
        type="text"
        name="username"
        value="{{.Username}}"
        placeholder="例: alice"
        class="w-full border {{if .Errors.Username}}border-red-400{{else}}border-gray-300{{end}}
               rounded px-3 py-2 text-sm focus:outline-none focus:border-blue-400"
      />
      {{if .Errors.Username}}
        <p class="text-red-500 text-xs mt-1">{{.Errors.Username}}</p>
      {{end}}
    </div>

    <!-- メールアドレス -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">
        メールアドレス
      </label>
      <input
        type="email"
        name="email"
        value="{{.Email}}"
        class="w-full border {{if .Errors.Email}}border-red-400{{else}}border-gray-300{{end}}
               rounded px-3 py-2 text-sm focus:outline-none focus:border-blue-400"
      />
      {{if .Errors.Email}}
        <p class="text-red-500 text-xs mt-1">{{.Errors.Email}}</p>
      {{end}}
    </div>

    <!-- パスワード -->
    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">
        パスワード（8文字以上）
      </label>
      <input
        type="password"
        name="password"
        class="w-full border {{if .Errors.Password}}border-red-400{{else}}border-gray-300{{end}}
               rounded px-3 py-2 text-sm focus:outline-none focus:border-blue-400"
      />
      {{if .Errors.Password}}
        <p class="text-red-500 text-xs mt-1">{{.Errors.Password}}</p>
      {{end}}
    </div>

    <button type="submit"
      class="w-full bg-blue-500 text-white rounded py-2 text-sm font-medium hover:bg-blue-600">
      登録する
    </button>

  </form>

  <p class="mt-4 text-center text-sm text-gray-500">
    すでにアカウントをお持ちですか？
    <a href="/login" class="text-blue-500 hover:underline">ログイン</a>
  </p>
</div>
{{end}}
```

## internal/web/templates/base.html

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

## internal/web/templates/communities/new_subreddit.html

```html
{{define "title"}}コミュニティを作成 · reddit_clone{{end}}
{{define "content"}}
<div class="max-w-lg mx-auto mt-12 px-4">
  <h1 class="text-2xl font-bold mb-6">コミュニティを作成</h1>

  <form action="/subreddits/new" method="post"
    class="bg-white border border-gray-200 rounded p-6 space-y-4">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}" />

    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">
        コミュニティ名
      </label>
      <input type="text" name="name"
        value="{{.Name}}"
        placeholder="例: elixir"
        class="w-full border {{if .Errors.Name}}border-red-400{{else}}border-gray-300{{end}}
               rounded px-3 py-2 text-sm focus:outline-none focus:border-blue-400"
      />
      <p class="text-xs text-gray-400 mt-1">英数字とアンダースコアのみ・3〜21文字</p>
      {{if .Errors.Name}}
        <p class="text-red-500 text-xs mt-1">{{.Errors.Name}}</p>
      {{end}}
    </div>

    <div>
      <label class="block text-sm font-medium text-gray-700 mb-1">
        説明（任意）
      </label>
      <textarea name="description" rows="3"
        placeholder="このコミュニティは何について話す場所ですか？"
        class="w-full border {{if .Errors.Description}}border-red-400{{else}}border-gray-300{{end}} rounded px-3 py-2 text-sm
               focus:outline-none focus:border-blue-400 resize-none"
      >{{.Description}}</textarea>
      {{if .Errors.Description}}
        <p class="text-red-500 text-xs mt-1">{{.Errors.Description}}</p>
      {{end}}
    </div>

    <button type="submit"
      class="w-full bg-blue-500 text-white rounded py-2 text-sm font-medium hover:bg-blue-600">
      コミュニティを作成する
    </button>

  </form>
</div>
{{end}}
```

## internal/web/templates/communities/subreddit.html

```html
{{define "title"}}r/{{.Subreddit.Name}} · reddit_clone{{end}}
{{define "content"}}
<div class="max-w-4xl mx-auto px-4 py-4 flex gap-4">

  <!-- メインカラム -->
  <div class="flex-1">

    <!-- ソートタブ -->
    <div class="bg-white border border-gray-200 rounded p-2 mb-4 flex gap-2">
      <a href="/r/{{.Subreddit.Name}}?sort=hot"
        class="{{if eq .Sort "hot"}}px-4 py-1 rounded bg-gray-200 font-bold text-sm{{else}}px-4 py-1 rounded hover:bg-gray-100 text-sm text-gray-600{{end}}">
        🔥 Hot
      </a>
      <a href="/r/{{.Subreddit.Name}}?sort=new"
        class="{{if eq .Sort "new"}}px-4 py-1 rounded bg-gray-200 font-bold text-sm{{else}}px-4 py-1 rounded hover:bg-gray-100 text-sm text-gray-600{{end}}">
        ✨ New
      </a>
    </div>

    <!-- 投稿一覧 -->
    <div id="post-list">
      {{template "post_list" .PostList}}
    </div>

  </div>

  <!-- サイドバー -->
  <div class="w-72 shrink-0">
    <div class="bg-white border border-gray-200 rounded p-4">
      <h2 class="font-bold text-lg mb-2">r/{{.Subreddit.Name}}</h2>

      {{if .Subreddit.Description}}
        <p class="text-sm text-gray-600 mb-4">{{.Subreddit.Description}}</p>
      {{end}}

      <!-- Join / Leave ボタン -->
      {{if .CurrentUser}}
        <div id="join-button">
          {{template "join_button" .JoinButton}}
        </div>

        <a href="/r/{{.Subreddit.Name}}/submit"
          class="block w-full mt-2 border border-gray-300 text-center
                 rounded-full py-1 text-sm hover:bg-gray-50">
          投稿する
        </a>
      {{end}}
    </div>
  </div>

</div>

<!-- 報告モーダル -->
{{template "report_modal" .}}

{{end}}
```

## internal/web/templates/components/comment_tree.html

```html
{{define "comment_tree"}}
<div class="mb-2 {{if gt .Depth 0}}border-l border-gray-200 pl-3{{end}}">

  <div class="flex gap-2">

    <!-- 投票ボタン -->
    <div id="comment-vote-{{.Comment.ID}}"
      class="flex flex-col items-center gap-0.5 pt-1 shrink-0">
      <button
        hx-post="/comments/{{.Comment.ID}}/vote"
        hx-vals='{"value": 1}'
        hx-target="#comment-vote-{{.Comment.ID}}"
        hx-swap="innerHTML"
        class="text-gray-300 hover:text-orange-500 text-xs"
      >▲</button>
      <span class="text-xs text-gray-500">{{.Comment.Score}}</span>
      <button
        hx-post="/comments/{{.Comment.ID}}/vote"
        hx-vals='{"value": -1}'
        hx-target="#comment-vote-{{.Comment.ID}}"
        hx-swap="innerHTML"
        class="text-gray-300 hover:text-blue-500 text-xs"
      >▼</button>
    </div>

    <!-- コメント内容 -->
    <div class="flex-1 border-l-2 border-gray-100 pl-2">
      <div class="text-xs text-gray-500 mb-1">
        <span class="font-medium text-gray-700">
          u/{{.Comment.AuthorUsername}}
        </span>
        • {{timeAgo .Comment.InsertedAt}}
        • {{.Comment.Score}} ポイント
      </div>

      <p class="text-sm text-gray-800 whitespace-pre-wrap">{{.Comment.Body}}</p>

      <div class="flex gap-3 mt-1">

        {{if .CurrentUser}}
          <!-- 返信フォーム（クリックでトグル） -->
          <button
            onclick="toggleReplyForm('{{.Comment.ID}}')"
            class="text-xs text-gray-400 hover:text-gray-600"
          >返信</button>
        {{end}}

        {{if and .CurrentUser (eq .CurrentUser.ID .Comment.AuthorID)}}
          <form action="/comments/{{.Comment.ID}}/delete" method="post" class="inline">
            <input type="hidden" name="csrf_token" value="{{.CSRFToken}}" />
            <button type="submit"
              onclick="return confirm('このコメントを削除しますか？')"
              class="text-xs text-red-400 hover:text-red-600">
              削除
            </button>
          </form>
        {{end}}

        {{if and .CurrentUser (ne .CurrentUser.ID .Comment.AuthorID)}}
          <button
            onclick="openReportModal('comment', '{{.Comment.ID}}')"
            class="text-xs text-gray-400 hover:text-gray-600">
            報告
          </button>
        {{end}}
      </div>

      <!-- 返信フォーム（デフォルト非表示） -->
      {{if .CurrentUser}}
        <div id="reply-form-{{.Comment.ID}}" class="hidden mt-2">
          <form action="/posts/{{.Comment.PostID}}/comments" method="post">
            <input type="hidden" name="csrf_token" value="{{.CSRFToken}}" />
            <input type="hidden" name="parent_id" value="{{.Comment.ID}}" />
            <textarea name="body" rows="3"
              placeholder="返信を入力..."
              class="w-full border border-gray-300 rounded p-2 text-xs
                     resize-none focus:outline-none focus:border-blue-400"
            ></textarea>
            <div class="flex gap-2 mt-1 justify-end">
              <button type="button"
                onclick="toggleReplyForm('{{.Comment.ID}}')"
                class="text-xs text-gray-500 px-2 py-1">
                キャンセル
              </button>
              <button type="submit"
                class="text-xs bg-blue-500 text-white px-3 py-1 rounded hover:bg-blue-600">
                返信する
              </button>
            </div>
          </form>
        </div>
      {{end}}

    </div>
  </div>

  <!-- 子コメント（再帰）-->
  {{if and .Comment.Children (lt .Depth 8)}}
    <div class="ml-4 mt-2">
      {{$node := .}}
      {{range .Comment.Children}}
        {{template "comment_tree" (kid $node .)}}
      {{end}}
    </div>
  {{end}}

</div>
{{end}}
```

## internal/web/templates/components/join_button.html

```html
{{define "join_button"}}
{{if .Joined}}
  <button
    hx-post="/r/{{.Subreddit.Name}}/leave"
    hx-target="#join-button"
    hx-swap="innerHTML"
    class="w-full border border-blue-500 text-blue-500 rounded-full
           py-1 text-sm hover:bg-blue-50"
  >
    参加中
  </button>
{{else}}
  <button
    hx-post="/r/{{.Subreddit.Name}}/join"
    hx-target="#join-button"
    hx-swap="innerHTML"
    class="w-full bg-blue-500 text-white rounded-full
           py-1 text-sm hover:bg-blue-600"
  >
    参加する
  </button>
{{end}}
{{end}}
```

## internal/web/templates/components/post_card.html

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

## internal/web/templates/components/post_list.html

```html
{{define "post_list"}}
{{if .Posts}}
  {{range .Posts}}
    {{template "post_card" (card . $.CurrentUser $.ShowSubreddit)}}
  {{end}}

  {{if .HasMore}}
    <div class="text-center py-4"
      hx-get="/r/{{.Subreddit.Name}}/posts?sort={{.Sort}}&page={{add .Page 1}}"
      hx-trigger="revealed"
      hx-target="this"
      hx-swap="outerHTML"
    >
      <button class="px-6 py-2 border border-gray-300 rounded text-sm text-gray-600 hover:bg-gray-50">
        もっと見る
      </button>
    </div>
  {{end}}
{{else}}
  <p class="text-gray-500 text-center py-8">まだ投稿がありません</p>
{{end}}
{{end}}
```

## internal/web/templates/components/report_modal.html

```html
{{define "report_modal"}}
<!-- ページの末尾に1つだけ配置する -->

<!-- 背景オーバーレイ（クリックで閉じる） -->
<div id="report-overlay"
  class="hidden fixed inset-0 bg-black bg-opacity-50 z-40"
  onclick="closeReportModal()"
></div>

<!-- モーダル本体 -->
<div id="report-modal"
  class="hidden fixed inset-0 z-50 flex items-center justify-center p-4"
>
  <div class="bg-white rounded-lg shadow-xl w-full max-w-md">

    <!-- ヘッダー -->
    <div class="flex justify-between items-center p-4 border-b border-gray-200">
      <h2 class="text-lg font-bold">報告する</h2>
      <button onclick="closeReportModal()"
        class="text-gray-400 hover:text-gray-600 text-xl">✕</button>
    </div>

    <!-- 報告フォーム -->
    <div class="p-4">
      <p class="text-sm text-gray-600 mb-3">報告する理由を選択してください</p>

      <!-- hidden フィールド（JavaScript でセットする） -->
      <!-- hx-include は name 属性のある要素の値を集めるため、name が必須 -->
      <input type="hidden" id="report-target-type" name="target_type" value="" />
      <input type="hidden" id="report-target-id" name="target_id" value="" />

      <div class="space-y-2" id="report-result">
        {{range reportReasons}}
          <button
            hx-post="/reports"
            hx-include="#report-target-type, #report-target-id"
            hx-vals='{"reason": "{{.Value}}"}'
            hx-target="#report-result"
            hx-swap="innerHTML"
            class="w-full text-left px-4 py-3 rounded border border-gray-200
                   text-sm hover:bg-gray-50 hover:border-gray-300 transition-colors"
          >
            {{.Label}}
          </button>
        {{end}}
      </div>
    </div>

    <!-- フッター -->
    <div class="p-4 border-t border-gray-100">
      <button onclick="closeReportModal()"
        class="w-full py-2 text-sm text-gray-500 hover:text-gray-700">
        キャンセル
      </button>
    </div>

  </div>
</div>

<script>
  function openReportModal(targetType, targetId) {
    // hidden フィールドに値をセットする
    document.getElementById("report-target-type").value = targetType;
    document.getElementById("report-target-id").value = targetId;

    // モーダルを表示する
    document.getElementById("report-overlay").classList.remove("hidden");
    document.getElementById("report-modal").classList.remove("hidden");
  }

  function closeReportModal() {
    document.getElementById("report-overlay").classList.add("hidden");
    document.getElementById("report-modal").classList.add("hidden");

    // 報告結果をリセットする（次回開いたとき理由ボタンが表示される）
    // ページをリロードせずにリセットするため HTMX で再取得する
    htmx.ajax("GET", "/components/report-reasons", "#report-result");
  }
</script>
{{end}}
```

## internal/web/templates/components/report_reasons.html

```html
{{define "report_reasons"}}
{{range reportReasons}}
  <button
    hx-post="/reports"
    hx-include="#report-target-type, #report-target-id"
    hx-vals='{"reason": "{{.Value}}"}'
    hx-target="#report-result"
    hx-swap="innerHTML"
    class="w-full text-left px-4 py-3 rounded border border-gray-200
           text-sm hover:bg-gray-50 hover:border-gray-300 transition-colors"
  >
    {{.Label}}
  </button>
{{end}}
{{end}}
```

## internal/web/templates/components/vote_buttons.html

```html
{{define "vote_buttons"}}
<div class="flex flex-col items-center gap-0.5">
  <button
    hx-post="/{{.TargetType}}s/{{.TargetID}}/vote"
    hx-vals='{"value": 1}'
    hx-target="#{{.TargetType}}-vote-{{.TargetID}}"
    hx-swap="innerHTML"
    class="text-gray-400 hover:text-orange-500 text-sm"
  >▲</button>

  <span class="text-xs font-bold text-gray-700">{{.Score}}</span>

  <button
    hx-post="/{{.TargetType}}s/{{.TargetID}}/vote"
    hx-vals='{"value": -1}'
    hx-target="#{{.TargetType}}-vote-{{.TargetID}}"
    hx-swap="innerHTML"
    class="text-gray-400 hover:text-blue-500 text-sm"
  >▼</button>
</div>
{{end}}
```

## internal/web/templates/posts/detail.html

```html
{{define "title"}}{{.Post.Title}} · reddit_clone{{end}}
{{define "content"}}
<div class="max-w-3xl mx-auto px-4 py-4">

  <!-- 投稿本体 -->
  <div class="bg-white border border-gray-200 rounded p-4 mb-4">
    <div class="flex gap-3">

      <!-- 投票 -->
      <div id="post-vote-{{.Post.ID}}" class="flex flex-col items-center gap-1">
        {{template "vote_buttons" (voteBtn "post" .Post.ID .Post.Score)}}
      </div>

      <div class="flex-1">
        <h1 class="text-xl font-bold text-gray-900 mb-1">{{.Post.Title}}</h1>

        <div class="text-xs text-gray-500 mb-3 flex gap-2">
          <a href="/r/{{.Post.SubredditName}}" class="hover:underline font-medium">
            r/{{.Post.SubredditName}}
          </a>
          <span>•</span>
          <a href="/u/{{.Post.AuthorUsername}}" class="hover:underline">
            u/{{.Post.AuthorUsername}}
          </a>
        </div>

        {{if and (eq .Post.Type "text") .Post.Body}}
          <p class="text-sm text-gray-800 whitespace-pre-wrap mb-3">{{.Post.Body}}</p>
        {{end}}

        {{if eq .Post.Type "link"}}
          <a href="{{.Post.URL}}" target="_blank" rel="noopener noreferrer"
            class="text-sm text-blue-500 hover:underline break-all">
            {{.Post.URL}} ↗
          </a>
        {{end}}

        {{if and .CurrentUser (eq .CurrentUser.ID .Post.AuthorID)}}
          <form action="/posts/{{.Post.ID}}/delete" method="post" class="mt-2">
            <input type="hidden" name="csrf_token" value="{{.CSRFToken}}" />
            <button type="submit"
              onclick="return confirm('この投稿を削除しますか？')"
              class="text-xs text-red-400 hover:text-red-600">
              削除
            </button>
          </form>
        {{end}}
      </div>
    </div>
  </div>

  <!-- コメント投稿フォーム -->
  {{if .CurrentUser}}
    <div class="bg-white border border-gray-200 rounded p-4 mb-4">
      <p class="text-sm font-medium mb-2">コメントを追加</p>
      <form action="/posts/{{.Post.ID}}/comments" method="post">
      <input type="hidden" name="csrf_token" value="{{.CSRFToken}}" />
        <input type="hidden" name="parent_id" value="" />
        <textarea name="body" rows="4"
          placeholder="コメントを入力してください"
          class="w-full border border-gray-300 rounded p-2 text-sm resize-none
                 focus:outline-none focus:border-blue-400"
        ></textarea>
        <div class="flex justify-end mt-2">
          <button type="submit"
            class="px-4 py-1 bg-blue-500 text-white text-sm rounded hover:bg-blue-600">
            投稿する
          </button>
        </div>
      </form>
    </div>
  {{end}}

  <!-- コメント一覧 -->
  <div class="bg-white border border-gray-200 rounded p-4">
    {{if .Comments}}
      {{range .Comments}}
        {{template "comment_tree" (node . $.CurrentUser $.CSRFToken)}}
      {{end}}
    {{else}}
      <p class="text-gray-400 text-sm text-center py-4">
        まだコメントはありません。最初のコメントを投稿しましょう！
      </p>
    {{end}}
  </div>

</div>

<!-- 報告モーダル -->
{{template "report_modal" .}}

{{end}}
```

## internal/web/templates/posts/home.html

```html
{{define "title"}}ホーム · reddit_clone{{end}}
{{define "content"}}
<div class="max-w-4xl mx-auto px-4 py-4 flex gap-4">

  <!-- 左サイドバー -->
  <div class="w-48 shrink-0">
    <div class="bg-white border border-gray-200 rounded p-3">
      <h2 class="text-xs font-bold text-gray-500 uppercase mb-2">参加中</h2>

      {{if .JoinedSubreddits}}
        {{range .JoinedSubreddits}}
          <a href="/r/{{.Name}}"
            class="block text-sm py-1 hover:text-blue-500 truncate">
            r/{{.Name}}
          </a>
        {{end}}
      {{else}}
        <p class="text-xs text-gray-400">まだ参加していません</p>
      {{end}}

      <div class="border-t border-gray-100 mt-3 pt-3">
        <a href="/subreddits/new"
          class="text-sm text-blue-500 hover:underline">
          ＋ コミュニティを作成
        </a>
      </div>
    </div>
  </div>

  <!-- メインカラム -->
  <div class="flex-1">

    <!-- ソートタブ -->
    <div class="bg-white border border-gray-200 rounded p-2 mb-4 flex gap-2">
      <a href="/?sort=hot"
        class="{{if eq .Sort "hot"}}px-4 py-1 rounded bg-gray-200 font-bold text-sm{{else}}px-4 py-1 rounded hover:bg-gray-100 text-sm text-gray-600{{end}}">
        🔥 Hot
      </a>
      <a href="/?sort=new"
        class="{{if eq .Sort "new"}}px-4 py-1 rounded bg-gray-200 font-bold text-sm{{else}}px-4 py-1 rounded hover:bg-gray-100 text-sm text-gray-600{{end}}">
        ✨ New
      </a>
    </div>

    <!-- 投稿一覧 -->
    {{if .Posts}}
      {{range .Posts}}
        {{template "post_card" (card . $.CurrentUser $.ShowSubreddit)}}
      {{end}}

      {{if .HasMore}}
        <div class="text-center py-4">
          <a href="/?sort={{.Sort}}&page={{add .Page 1}}"
            class="px-6 py-2 border border-gray-300 rounded text-sm text-gray-600 hover:bg-gray-50">
            もっと見る
          </a>
        </div>
      {{end}}

    {{else}}
      <div class="bg-white border border-gray-200 rounded p-8 text-center">
        <p class="text-gray-500 mb-2">まだ投稿がありません</p>
        <p class="text-sm text-gray-400">コミュニティに参加して投稿を見てみましょう</p>
        <a href="/subreddits/new"
          class="inline-block mt-4 px-4 py-2 bg-blue-500 text-white text-sm
                 rounded hover:bg-blue-600">
          コミュニティを作成
        </a>
      </div>
    {{end}}

  </div>
</div>

<!-- 報告モーダル -->
{{template "report_modal" .}}

{{end}}
```

## internal/web/templates/posts/new_post.html

```html
{{define "title"}}r/{{.Subreddit.Name}} に投稿する · reddit_clone{{end}}
{{define "content"}}
<div class="max-w-2xl mx-auto mt-8 px-4">
  <h1 class="text-xl font-bold mb-4">r/{{.Subreddit.Name}} に投稿する</h1>

  <!-- タブ切り替え -->
  <div class="flex border-b border-gray-200 mb-4" id="type-tabs">
    <button
      onclick="switchType('text')"
      id="tab-text"
      class="px-6 py-2 text-sm font-bold border-b-2 border-blue-500 text-blue-500"
    >
      📝 テキスト
    </button>
    <button
      onclick="switchType('link')"
      id="tab-link"
      class="px-6 py-2 text-sm text-gray-500 hover:text-gray-700"
    >
      🔗 リンク
    </button>
  </div>

  <form action="/r/{{.Subreddit.Name}}/submit" method="post"
    class="bg-white border border-gray-200 rounded p-4 space-y-4">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}" />

    <input type="hidden" name="post_type" id="post_type" value="{{.PostType}}" />

    <div>
      <input type="text" name="title"
        value="{{.Title}}"
        placeholder="タイトルを入力してください"
        class="w-full border {{if .Errors.Title}}border-red-400{{else}}border-gray-300{{end}}
               rounded px-3 py-2 text-sm focus:outline-none focus:border-blue-400"
      />
      {{if .Errors.Title}}
        <p class="text-red-500 text-xs mt-1">{{.Errors.Title}}</p>
      {{end}}
    </div>

    <!-- テキスト入力欄 -->
    <div id="body-section">
      <textarea name="body" rows="6"
        placeholder="本文を入力してください"
        class="w-full border {{if .Errors.Body}}border-red-400{{else}}border-gray-300{{end}} rounded px-3 py-2 text-sm
               focus:outline-none focus:border-blue-400 resize-none"
      >{{.Body}}</textarea>
      {{if .Errors.Body}}
        <p class="text-red-500 text-xs mt-1">{{.Errors.Body}}</p>
      {{end}}
    </div>

    <!-- URL 入力欄 -->
    <div id="url-section" class="hidden">
      <input type="url" name="url"
        value="{{.URL}}"
        placeholder="https://example.com"
        class="w-full border {{if .Errors.URL}}border-red-400{{else}}border-gray-300{{end}}
               rounded px-3 py-2 text-sm focus:outline-none focus:border-blue-400"
      />
      {{if .Errors.URL}}
        <p class="text-red-500 text-xs mt-1">{{.Errors.URL}}</p>
      {{end}}
    </div>

    {{if .Errors.General}}
      <p class="text-red-500 text-xs">{{.Errors.General}}</p>
    {{end}}

    <div class="flex justify-end gap-2">
      <a href="/r/{{.Subreddit.Name}}"
        class="px-4 py-2 text-sm border border-gray-300 rounded hover:bg-gray-50">
        キャンセル
      </a>
      <button type="submit"
        class="px-4 py-2 text-sm bg-blue-500 text-white rounded hover:bg-blue-600">
        投稿する
      </button>
    </div>

  </form>
</div>

<script>
  function switchType(type) {
    document.getElementById("post_type").value = type;

    const tabText = document.getElementById("tab-text");
    const tabLink = document.getElementById("tab-link");
    const bodySection = document.getElementById("body-section");
    const urlSection = document.getElementById("url-section");

    const activeClass = "px-6 py-2 text-sm font-bold border-b-2 border-blue-500 text-blue-500";
    const inactiveClass = "px-6 py-2 text-sm text-gray-500 hover:text-gray-700";

    if (type === "text") {
      tabText.className = activeClass;
      tabLink.className = inactiveClass;
      bodySection.classList.remove("hidden");
      urlSection.classList.add("hidden");
    } else {
      tabText.className = inactiveClass;
      tabLink.className = activeClass;
      bodySection.classList.add("hidden");
      urlSection.classList.remove("hidden");
    }
  }

  // 再表示時（バリデーションエラーなど）も現在の post_type に合わせる
  switchType("{{.PostType}}");
</script>
{{end}}
```

## internal/web/templates/posts/search.html

```html
{{define "title"}}{{if .Query}}「{{.Query}}」の検索結果{{else}}検索{{end}} · reddit_clone{{end}}
{{define "content"}}
<div class="max-w-4xl mx-auto px-4 py-6">

  <form action="/search" method="get" class="mb-6">
    <div class="flex gap-2">
      <input type="text" name="q" value="{{.Query}}"
        placeholder="キーワードを入力..."
        autofocus
        class="flex-1 border border-gray-300 rounded px-4 py-2 text-sm
               focus:outline-none focus:border-blue-400"
      />
      <button type="submit"
        class="px-4 py-2 bg-blue-500 text-white text-sm rounded hover:bg-blue-600">
        検索
      </button>
    </div>
  </form>

  {{if not .Query}}
    <p class="text-gray-400 text-center py-12">キーワードを入力して検索してください</p>
  {{else}}
    <h1 class="text-lg font-bold mb-4">「{{.Query}}」の検索結果</h1>

    <div class="flex gap-4">

      <!-- 投稿 -->
      <div class="flex-1">
        <h2 class="text-sm font-bold text-gray-500 uppercase mb-3">
          投稿 {{if .Posts}}({{len .Posts}}件){{end}}
        </h2>

        {{if .Posts}}
          {{range .Posts}}
            {{template "post_card" (card . $.CurrentUser $.ShowSubreddit)}}
          {{end}}
        {{else}}
          <div class="bg-white border border-gray-200 rounded p-6 text-center">
            <p class="text-gray-400 text-sm">投稿が見つかりませんでした</p>
          </div>
        {{end}}
      </div>

      <!-- コミュニティ -->
      <div class="w-64 shrink-0">
        <h2 class="text-sm font-bold text-gray-500 uppercase mb-3">
          コミュニティ {{if .Subreddits}}({{len .Subreddits}}件){{end}}
        </h2>

        {{if .Subreddits}}
          {{range .Subreddits}}
            <div class="bg-white border border-gray-200 rounded p-3 mb-2 hover:border-gray-400">
              <a href="/r/{{.Name}}" class="block">
                <p class="font-medium text-sm">r/{{.Name}}</p>
                {{if .Description}}
                  <p class="text-xs text-gray-400 mt-1 truncate">
                    {{.Description}}
                  </p>
                {{end}}
              </a>
            </div>
          {{end}}
        {{else}}
          <div class="bg-white border border-gray-200 rounded p-4 text-center">
            <p class="text-gray-400 text-sm">見つかりませんでした</p>
          </div>
        {{end}}
      </div>

    </div>
  {{end}}
</div>

<!-- 報告モーダル -->
{{template "report_modal" .}}

{{end}}
```

## internal/config/config_test.go

```go
package config

import "testing"

func TestNormalizeDatabaseURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"postgresql+asyncpg://u:p@h:5432/db", "postgres://u:p@h:5432/db"},
		{"postgres+asyncpg://u:p@h/db", "postgres://u:p@h/db"},
		{"postgres://u:p@h/db", "postgres://u:p@h/db"},
		{"postgresql://u:p@h/db", "postgresql://u:p@h/db"},
	}
	for _, c := range cases {
		if got := NormalizeDatabaseURL(c.in); got != c.want {
			t.Errorf("NormalizeDatabaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseBool(t *testing.T) {
	for _, v := range []string{"true", "TRUE", "1", "yes", "on"} {
		b, err := parseBool(v)
		if err != nil || !b {
			t.Errorf("parseBool(%q) = %v, %v; want true, nil", v, b, err)
		}
	}
	for _, v := range []string{"false", "0", "no", "off", ""} {
		if v == "" {
			continue // empty is handled by the caller, never passed here
		}
		b, err := parseBool(v)
		if err != nil || b {
			t.Errorf("parseBool(%q) = %v, %v; want false, nil", v, b, err)
		}
	}
	if _, err := parseBool("maybe"); err == nil {
		t.Error("parseBool(\"maybe\") should fail")
	}
}

func TestLoadDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@h/db")
	t.Setenv("SECRET_KEY", "test-secret")
	t.Setenv("SESSION_COOKIE_NAME", "")
	t.Setenv("DEBUG", "")
	t.Setenv("PORT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SessionCookieName != "session" {
		t.Errorf("SessionCookieName = %q, want \"session\"", cfg.SessionCookieName)
	}
	if cfg.Debug {
		t.Error("Debug should default to false")
	}
	if cfg.Port != 8000 {
		t.Errorf("Port = %d, want 8000", cfg.Port)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("SECRET_KEY", "x")
	if _, err := Load(); err == nil {
		t.Error("Load without DATABASE_URL should fail")
	}
	t.Setenv("DATABASE_URL", "postgres://u:p@h/db")
	t.Setenv("SECRET_KEY", "")
	if _, err := Load(); err == nil {
		t.Error("Load without SECRET_KEY should fail")
	}
	if _, err := LoadForMigrate(); err != nil {
		t.Errorf("LoadForMigrate without SECRET_KEY should succeed: %v", err)
	}
}
```

## internal/auth/session_test.go

```go
package auth

import (
	"strings"
	"testing"
	"time"
)

func TestSessionRoundtrip(t *testing.T) {
	s := NewSessions("test-secret", "session", false)
	token := s.CreateToken("user-123")
	if got := s.VerifyToken(token); got != "user-123" {
		t.Errorf("VerifyToken = %q, want user-123", got)
	}
}

func TestSessionTamperRejected(t *testing.T) {
	s := NewSessions("test-secret", "session", false)
	token := s.CreateToken("user-123")
	if got := s.VerifyToken(token[:len(token)-2] + "xx"); got != "" {
		t.Errorf("tampered token accepted: %q", got)
	}
	if got := s.VerifyToken("not-a-token"); got != "" {
		t.Errorf("garbage token accepted: %q", got)
	}
	if got := s.VerifyToken(""); got != "" {
		t.Errorf("empty token accepted: %q", got)
	}
}

func TestSessionWrongSecretRejected(t *testing.T) {
	a := NewSessions("secret-a", "session", false)
	b := NewSessions("secret-b", "session", false)
	if got := b.VerifyToken(a.CreateToken("user-123")); got != "" {
		t.Errorf("token from another secret accepted: %q", got)
	}
}

func TestSessionExpiry(t *testing.T) {
	s := NewSessions("test-secret", "session", false)
	base := time.Unix(1_700_000_000, 0)
	s.nowFunc = func() time.Time { return base }
	token := s.CreateToken("user-123")

	s.nowFunc = func() time.Time { return base.Add(MaxAge*time.Second - time.Second) }
	if got := s.VerifyToken(token); got != "user-123" {
		t.Errorf("token just inside max age rejected: %q", got)
	}
	s.nowFunc = func() time.Time { return base.Add(MaxAge*time.Second + time.Second) }
	if got := s.VerifyToken(token); got != "" {
		t.Errorf("expired token accepted: %q", got)
	}
	s.nowFunc = func() time.Time { return base.Add(-time.Hour) }
	if got := s.VerifyToken(token); got != "" {
		t.Errorf("future-dated token accepted: %q", got)
	}
	if !strings.Contains(token, ".") {
		t.Error("token should have body.signature form")
	}
}
```

## internal/accounts/validate_test.go

```go
package accounts

import "testing"

func TestValidateUserCreate(t *testing.T) {
	ok := ValidateUserCreate("alice_01", "alice@example.com", "password123")
	if !ok.Empty() {
		t.Errorf("valid input produced errors: %+v", ok)
	}

	cases := []struct {
		name                      string
		username, email, password string
		field                     string
	}{
		{"short username", "ab", "a@b.co", "password123", "username"},
		{"long username", "abcdefghijklmnopqrstu", "a@b.co", "password123", "username"},
		{"bad chars username", "al ice", "a@b.co", "password123", "username"},
		{"japanese username", "ありす", "a@b.co", "password123", "username"},
		{"email no at", "alice", "alice.example.com", "password123", "email"},
		{"email no tld", "alice", "alice@example", "password123", "email"},
		{"email double dot", "alice", "al..ice@example.com", "password123", "email"},
		{"email empty local", "alice", "@example.com", "password123", "email"},
		{"short password", "alice", "a@b.co", "short", "password"},
	}
	for _, c := range cases {
		errs := ValidateUserCreate(c.username, c.email, c.password)
		var got string
		switch c.field {
		case "username":
			got = errs.Username
		case "email":
			got = errs.Email
		case "password":
			got = errs.Password
		}
		if got == "" {
			t.Errorf("%s: expected %s error, got %+v", c.name, c.field, errs)
		}
	}
}
```

## internal/accounts/email_test.go

```go
package accounts

import (
	"strings"
	"testing"
)

// TestValidEmailBattery is the correspondence table between Go's
// validEmail and pydantic's EmailStr, measured address by address
// against the pinned Python stack (email-validator 2.3.0) during the
// 2026-11-09 full review (batteries 1–3). Every verdict below is the
// Python verdict; validEmail must agree with all of them.
func TestValidEmailBattery(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"user@example.com", true},
		{"user@localhost", false}, // single-label domain
		{"user@example.c", true},  // 1-char TLD is fine
		{"ユー@example.com", true},  // non-ASCII local part
		{"user@例え.jp", true},      // non-ASCII domain
		{"user..dots@example.com", false},
		{".user@example.com", false},
		{"user@-example.com", false},
		{"user@example-.com", false},
		{"user@exam_ple.com", false}, // underscore in a domain label
		{"user+tag@example.com", true},
		{"user@sub.example.co.jp", true},
		{"a@b.co", true},
		{"user@[127.0.0.1]", false},
		{"user%name@example.com", true},
		{"user name@example.com", false},
		{"user@example..com", false},
		{"user@.example.com", false},
		{"USER@EXAMPLE.COM", true},
		{"user@exam ple.com", false},
		{"user@domain", false},
		{"user@a.b", true},
		{"user@example.123", false}, // all-digit TLD
		{`"quoted"@example.com`, false},
		{"user@127.0.0.1", false},
		{"user@exam-ple.com", true},
		{"user.@example.com", false},
		{"user@sub_domain.example.com", false},
		{"ユーザー@例え.jp", true},
		{"user@bücher.de", true},
		{"😀@example.com", true}, // emoji local part
		{"user@😀.com", false},   // but not in the domain
		{"first.last+tag@sub.domain.co", true},
		{strings.Repeat("a", 64) + "@example.com", true},
		{strings.Repeat("a", 65) + "@example.com", true}, // no 64 cap in practice
		{strings.Repeat("a", 100) + "@example.com", true},
		{"u@" + strings.Repeat("a", 63) + ".com", true},
		{"u@" + strings.Repeat("a", 64) + ".com", false}, // label over 63
		{"user@localhost.localdomain", true},
		{"user@example.c0m", true},
		{"user@example.12a", true},
		{"@example.com", false},
		{"user@", false},
		{"user@１２３.jp", true}, // fullwidth digits count as digits
		{"us\ter@example.com", false},
		{"user@exam　ple.com", false}, // ideographic space in domain
		{"user@@example.com", false},
		{strings.Repeat("a", 242) + "@example.com", true},  // 254 chars total
		{strings.Repeat("a", 243) + "@example.com", false}, // 255 chars total
		{"", false},
	}
	for _, c := range cases {
		if got := validEmail(c.addr); got != c.want {
			t.Errorf("validEmail(%q) = %v, want %v (EmailStr)", c.addr, got, c.want)
		}
	}
}
```

## internal/accounts/password_test.go

```go
package accounts

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordBytesTruncation(t *testing.T) {
	for n, want := range map[int]int{0: 0, 71: 71, 72: 72, 73: 72, 200: 72} {
		if got := len(passwordBytes(strings.Repeat("a", n))); got != want {
			t.Errorf("passwordBytes(%d bytes) = %d bytes, want %d", n, got, want)
		}
	}
	// The cut is by bytes, not runes (30 three-byte runes = 90 bytes).
	if got := len(passwordBytes(strings.Repeat("あ", 30))); got != 72 {
		t.Errorf("multibyte truncation = %d bytes, want 72", got)
	}
}

// A hash produced by the Python stack (passlib + bcrypt) for a
// 104-byte password must verify here once the input is truncated the
// same way, and changing anything past byte 72 must not matter —
// exactly what the Python side does.
func TestLongPasswordAgainstPasslibHash(t *testing.T) {
	passlibHash := "$2b$12$M6qxEH/b1PqWXCczVJAIcOL6dJvBgCog3ZacCBe6Y//bhy5U6hkei"
	password := "long-password-" + strings.Repeat("x", 90) // 104 bytes

	if err := bcrypt.CompareHashAndPassword(
		[]byte(passlibHash), passwordBytes(password)); err != nil {
		t.Fatalf("passlib hash does not verify: %v", err)
	}

	changedTail := "long-password-" + strings.Repeat("x", 89) + "y"
	if err := bcrypt.CompareHashAndPassword(
		[]byte(passlibHash), passwordBytes(changedTail)); err != nil {
		t.Errorf("change past byte 72 should not matter: %v", err)
	}

	changedHead := "long-passwore-" + strings.Repeat("x", 90)
	if err := bcrypt.CompareHashAndPassword(
		[]byte(passlibHash), passwordBytes(changedHead)); err == nil {
		t.Error("change within the first 72 bytes must fail")
	}
}

// Generating a hash for an over-long password must not error (it did
// before the truncation, surfacing as a 500 on registration).
func TestLongPasswordHashGeneration(t *testing.T) {
	password := strings.Repeat("p", 200)
	hash, err := bcrypt.GenerateFromPassword(passwordBytes(password), bcryptCost)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword(hash, passwordBytes(password)); err != nil {
		t.Errorf("generated hash does not verify: %v", err)
	}
}
```

## internal/posts/posts_test.go

```go
package posts

import (
	"math"
	"strings"
	"testing"
	"time"
)

// Expected values computed with the Python version's
// Post.compute_hot_score (see app/posts/models.py).
func TestComputeHotScore(t *testing.T) {
	cases := []struct {
		score int
		at    time.Time
		want  float64
	}{
		{0, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 14071.05771111111},
		{10, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 14072.05771111111},
		{-3, time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC), 13687.540589856391},
		{1, time.Date(2005, 12, 8, 7, 46, 43, 0, time.UTC), 0.0},
	}
	for _, c := range cases {
		got := ComputeHotScore(c.score, c.at)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("ComputeHotScore(%d, %v) = %v, want %v",
				c.score, c.at, got, c.want)
		}
	}
	// The sign is kept: a heavily downvoted post ranks below a fresh
	// zero-score post of the same age.
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !(ComputeHotScore(-50, at) < ComputeHotScore(0, at)) {
		t.Error("negative score should rank below zero score")
	}
}

func TestValidatePostCreate(t *testing.T) {
	// valid text post
	in, errs := ValidatePostCreate("タイトル", "text", "本文です", "")
	if !errs.Empty() || in.Body == nil || *in.Body != "本文です" || in.URL != nil {
		t.Errorf("valid text: %+v %+v", in, errs)
	}
	// valid link post
	in, errs = ValidatePostCreate("タイトル", "link", "", "https://example.com/x")
	if !errs.Empty() || in.URL == nil || in.Body != nil {
		t.Errorf("valid link: %+v %+v", in, errs)
	}
	// text without body -> general
	_, errs = ValidatePostCreate("タイトル", "text", "", "")
	if errs.General != "テキスト投稿には本文が必要です" {
		t.Errorf("empty body: %+v", errs)
	}
	// link without url -> general
	_, errs = ValidatePostCreate("タイトル", "link", "", "")
	if errs.General != "リンク投稿には URL が必要です" {
		t.Errorf("empty url: %+v", errs)
	}
	// link with bad scheme -> general
	_, errs = ValidatePostCreate("タイトル", "link", "", "ftp://example.com")
	if errs.General != "URL は http:// または https:// で始まる必要があります" {
		t.Errorf("bad scheme: %+v", errs)
	}
	// title too long (301 runes) -> title error
	long := make([]rune, 301)
	for i := range long {
		long[i] = 'あ'
	}
	_, errs = ValidatePostCreate(string(long), "text", "x", "")
	if errs.Title == "" {
		t.Errorf("long title: %+v", errs)
	}
	// url too long (2049 runes) -> url field error, not general
	longURL := "https://" + strings.Repeat("a", 2041)
	_, errs = ValidatePostCreate("t", "link", "", longURL)
	if errs.URL == "" {
		t.Errorf("long url: %+v", errs)
	}
}
```

## internal/web/funcs_test.go

```go
package web

import (
	"testing"
	"time"
)

func TestFormatKarma(t *testing.T) {
	cases := map[int]string{
		0:     "0",
		42:    "42",
		999:   "999",
		1000:  "1.0k",
		1500:  "1.5k",
		12345: "12.3k",
		-5:    "-5",
	}
	for in, want := range cases {
		if got := formatKarma(in); got != want {
			t.Errorf("formatKarma(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestTimeAgo(t *testing.T) {
	now := time.Now()
	cases := []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Second, "たった今"},
		{5 * time.Minute, "5分前"},
		{3 * time.Hour, "3時間前"},
		{2 * 24 * time.Hour, "2日前"},
		{65 * 24 * time.Hour, "2ヶ月前"},
	}
	for _, c := range cases {
		if got := timeAgo(now.Add(-c.ago)); got != c.want {
			t.Errorf("timeAgo(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
}

func TestURLHost(t *testing.T) {
	if got := urlHost("https://example.com/path?q=1"); got != "example.com" {
		t.Errorf("urlHost = %q", got)
	}
	if got := urlHost("not a url"); got != "not a url" {
		t.Errorf("urlHost fallback = %q", got)
	}
}

func TestParseUUID(t *testing.T) {
	canonical, ok := parseUUID("550E8400-E29B-41D4-A716-446655440000")
	if !ok || canonical != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("canonical: %q %v", canonical, ok)
	}
	bare, ok := parseUUID("550e8400e29b41d4a716446655440000")
	if !ok || bare != canonical {
		t.Errorf("bare: %q %v", bare, ok)
	}
	if _, ok := parseUUID("zzz"); ok {
		t.Error("garbage accepted")
	}
	if _, ok := parseUUID(""); ok {
		t.Error("empty accepted")
	}
}
```

