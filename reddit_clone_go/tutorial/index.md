# Reddit クローン（Go 版）— ファイルインデックス

この教材は、Elixir / Phoenix LiveView 版を原典とし、Python / FastAPI 版で教科書化した Reddit クローンを、Go（標準ライブラリ中心）へ移植したものの解説です。仕様と振る舞いの正本は Python 版で、Go 版はその E2E 99 項目に 1 対 1 で合格しています。

---

## チュートリアル

| ファイル | 内容 |
|---------|------|
| [第0章 はじめに](chapter_00.md) | 作るもの・仕様・Python 版との関係・Go 版の方針 |
| [第1章 土台](chapter_01.md) | 入口とサブコマンド・設定・DB 接続・マイグレーション |
| [第2章 認証の部品](chapter_02.md) | 署名つき Cookie セッション・CSRF 二重送信方式 |
| [第3章 アカウント](chapter_03.md) | 入力検証（メール実測の話を含む）・登録とログイン |
| [第4章 コミュニティと投稿](chapter_04.md) | コミュニティの作成と参加・投稿とフィード |
| [第5章 コメント・投票・報告](chapter_05.md) | 木構造のコメント・投票のトランザクション・報告 |
| [第6章 画面の仕上げ](chapter_06.md) | ルーティングとテンプレート・補助関数・プロフィールと検索 |
| [第7章 テストと検証](chapter_07.md) | 単体テスト・E2E 99 項目の写し替え・競合の出し方 |
| [第8章 仕上げ](chapter_08.md) | ビルドと配布・運用・Python 版との違い・残っている判断事項 |
| [付録](appendix.md) | ファイル構成一覧・URL 対応表・関数一覧 |

---

## 全コード付録（自動生成）

正本のコードファイルの全文を機械生成で収録したものです（手写しではありません）。章の中に出てくるコードも、同じ正本から差し込んでいるため、常に一致します。

| ファイル | 内容 |
|---------|------|
| [全コード付録 1 基盤とマイグレーション](appendix_code_01_foundation.md) | 設定ファイル・入口・土台の3ファイル・マイグレーション SQL 全18件 |
| [全コード付録 2 部品とサービス層](appendix_code_02_services.md) | セッション・アカウント・コミュニティ・投稿・コメント・報告 |
| [全コード付録 3 Web 層](appendix_code_03_web.md) | ルーティング・CSRF・補助関数・各ハンドラ |
| [全コード付録 4 テンプレートとテスト](appendix_code_04_templates_tests.md) | テンプレート全18件・単体テスト全7件 |

---

## コード — Go（入口と土台）

| ファイル | 役割 |
|---------|------|
| [main.go](../main.go) | 入口（serve / migrate のサブコマンド） |
| [internal/config/config.go](../internal/config/config.go) | 設定（環境変数と .env） |
| [internal/db/db.go](../internal/db/db.go) | 接続プール |
| [internal/db/migrate.go](../internal/db/migrate.go) | マイグレーション実行係 |
| [migrations/migrations.go](../migrations/migrations.go) | SQL ファイルの埋め込み |
| [migrations/](../migrations/) | マイグレーション SQL（0001〜0009、up/down） |

## コード — Go（部品とサービス層）

| ファイル | 役割 |
|---------|------|
| [internal/auth/session.go](../internal/auth/session.go) | 署名つき Cookie セッション |
| [internal/accounts/](../internal/accounts/) | アカウント（検証・登録・認証・Karma） |
| [internal/communities/](../internal/communities/) | コミュニティ（作成・参加・脱退） |
| [internal/posts/](../internal/posts/) | 投稿とフィード・投票・検証 |
| [internal/comments/comments.go](../internal/comments/comments.go) | コメントと木の組み立て |
| [internal/reports/reports.go](../internal/reports/reports.go) | 報告 |

## コード — Go（Web 層）

| ファイル | 役割 |
|---------|------|
| [internal/web/server.go](../internal/web/server.go) | ルーティング・テンプレート基盤・共通部品 |
| [internal/web/csrf.go](../internal/web/csrf.go) | CSRF ミドルウェア |
| [internal/web/funcs.go](../internal/web/funcs.go) | テンプレート補助関数 |
| [internal/web/uuid.go](../internal/web/uuid.go) | UUID の最小実装 |
| [internal/web/accounts.go](../internal/web/accounts.go) | 登録・ログイン・プロフィールのハンドラ |
| [internal/web/communities.go](../internal/web/communities.go) | コミュニティ画面のハンドラ |
| [internal/web/posts.go](../internal/web/posts.go) | 投稿・投票・検索のハンドラ |
| [internal/web/comments.go](../internal/web/comments.go) | コメント・報告のハンドラ |
| [internal/web/templates/](../internal/web/templates/) | テンプレート（自動エスケープつき） |
