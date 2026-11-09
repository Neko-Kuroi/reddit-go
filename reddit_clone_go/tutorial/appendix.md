# 付録（Go 版）

## ファイル構成

コードの正本は、この教材のフォルダ構成そのものです。

- `main.go` — 入口（serve / migrate）
- `internal/config/` — 設定
- `internal/db/` — 接続プールとマイグレーション実行係
- `migrations/` — SQL マイグレーション（0001〜0009、up/down の18ファイル）と埋め込み
- `internal/auth/` — 署名つき Cookie セッション
- `internal/accounts/` — 検証・登録・認証・Karma
- `internal/communities/` — コミュニティ
- `internal/posts/` — 投稿・フィード・投票・検証
- `internal/comments/` — コメントと木
- `internal/reports/` — 報告
- `internal/web/` — ルーティング・CSRF・テンプレート・ハンドラ
- `tutorial/` — この教材（章・目次・本付録・全コード付録）
- `run_go_e2e.py` — E2E 99 項目（検証用。アプリの動作には不要）

全文は全コード付録 1〜4 に機械生成で収録してあります。

## URL 対応表

Python 版の経路と 1 対 1 に対応します（＋ `/health`）。

| メソッドと経路 | 画面・処理 |
|---------------|-----------|
| GET /health | 死活確認（JSON） |
| GET / | ホーム（ログイン必須、参加中コミュニティのフィード） |
| GET /register / POST /register | 登録画面 / 登録 |
| GET /login / POST /login | ログイン画面 / ログイン |
| POST /logout | ログアウト |
| GET /subreddits/new / POST /subreddits/new | コミュニティ作成 |
| GET /r/{name} | コミュニティの投稿一覧 |
| GET /r/{name}/posts | 一覧の続き（HTMX 断片） |
| POST /r/{name}/join / POST /r/{name}/leave | 参加 / 脱退（断片を返す） |
| GET /r/{name}/submit / POST /r/{name}/submit | 投稿作成 |
| GET /r/{name}/{post_id} | 投稿詳細とコメント |
| POST /posts/{post_id}/vote | 投稿への投票（断片を返す） |
| POST /posts/{post_id}/delete | 投稿の削除 |
| POST /posts/{post_id}/comments | コメントの投稿 |
| POST /comments/{comment_id}/vote | コメントへの投票（断片を返す） |
| POST /comments/{comment_id}/delete | コメントの削除 |
| POST /reports | 報告（断片を返す） |
| GET /components/report-reasons | 報告理由の部品（断片） |
| GET /u/{username} | プロフィール |
| GET /search | 検索 |
| GET /static/ | 静的ファイル（埋め込み） |

## 関数一覧の見方

Go 版の関数（テストを除く137件）は、すべて担当の章に全文で登場します。対応は次のとおりです。

| 章 | 主なファイル |
|----|------------|
| 第1章 | main.go、config、db、migrations |
| 第2章 | auth/session.go、web/csrf.go |
| 第3章 | accounts（検証・サービス）、web/accounts.go |
| 第4章 | communities、posts（投稿とフィード）、web/communities.go、web/posts.go |
| 第5章 | posts/vote.go、comments、reports、web/comments.go |
| 第6章 | web/server.go、web/funcs.go、web/uuid.go、テンプレート |
| 第7章 | 単体テスト7ファイル |

網羅の確認は機械で行っています（全関数の名前が章の中に現れること、付録の全文が正本と一致すること）。方法は第7章の考え方と同じで、数え上げは手作業ではなく道具に任せます。
