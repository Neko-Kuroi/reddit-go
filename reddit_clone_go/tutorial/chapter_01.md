# 第1章 土台

この章では、アプリの土台になる四つを順に作ります。

1. 入口（`main.go`）— どんな命令で何が動くか
2. 設定（`internal/config`）— 環境変数の読み方
3. 接続（`internal/db/db.go`）— データベースへのつなぎ方
4. マイグレーション（`internal/db/migrate.go` と `migrations/`）— テーブルの用意の仕方

## 1.1 入口はサブコマンド方式

Go 版のバイナリは、引数で役割を切り替える小さなプログラムです。

- `redditclone` または `redditclone serve` — HTTP サーバーを起動する
- `redditclone migrate` — まだ当てていないマイグレーションを当てる
- `redditclone migrate down` — 直近のマイグレーションを1つ戻す
- `redditclone migrate status` — 当たっているマイグレーションを一覧する

Python 版では Alembic（マイグレーション）と uvicorn（サーバー）が別々の道具でした。Go 版では単一バイナリにまとめる方針なので、入口が両方の顔を持つ形になりました。ただし中の作りは分けていて、マイグレーションだけのときは `SECRET_KEY` を要求しません（Alembic がデータベースの URL だけで動くのと同じです）。

`runServe` は、起動に失敗したら理由を返し、Ctrl+C や終了シグナルを受けたら10秒の猶予つきで穏やかに閉じます。

```go
// Command redditclone is the Go port of the Reddit clone tutorial app.
//
// Usage:
//
//	redditclone          serve HTTP (default)
//	redditclone serve    same as above
//	redditclone migrate        apply pending migrations
//	redditclone migrate down   roll back the latest migration
//	redditclone migrate status list applied migrations
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"redditclone/internal/config"
	"redditclone/internal/db"
	"redditclone/internal/web"
)

func main() {
	log.SetFlags(0)
	ctx := context.Background()

	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	var err error
	switch cmd {
	case "serve":
		err = runServe(ctx)
	case "migrate":
		sub := ""
		if len(os.Args) > 2 {
			sub = os.Args[2]
		}
		err = runMigrate(ctx, sub)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		fmt.Fprintln(os.Stderr, "usage: redditclone [serve|migrate [down|status]]")
		os.Exit(2)
	}
	if err != nil {
		log.Fatalf("%s: %v", cmd, err)
	}
}

func runMigrate(ctx context.Context, sub string) error {
	cfg, err := config.LoadForMigrate()
	if err != nil {
		return err
	}
	conn, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	m := db.NewMigrator(conn)
	switch sub {
	case "", "up":
		newly, err := m.Up(ctx)
		if err != nil {
			return err
		}
		if len(newly) == 0 {
			fmt.Println("migrations: already up to date")
		}
		for _, v := range newly {
			fmt.Printf("migrations: applied %s\n", v)
		}
		return nil
	case "down":
		v, err := m.Down(ctx)
		if err != nil {
			return err
		}
		if v == "" {
			fmt.Println("migrations: nothing to roll back")
		} else {
			fmt.Printf("migrations: rolled back %s\n", v)
		}
		return nil
	case "status":
		applied, err := m.Applied(ctx)
		if err != nil {
			return err
		}
		for _, v := range applied {
			fmt.Printf("applied: %s\n", v)
		}
		if len(applied) == 0 {
			fmt.Println("applied: (none)")
		}
		return nil
	}
	return fmt.Errorf("unknown migrate subcommand %q", sub)
}

func runServe(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	conn, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer conn.Close()

	srv, err := web.New(cfg, conn)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on :%d", cfg.Port)
		errCh <- httpSrv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-sigCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}
```

## 1.2 設定は環境変数から

設定の置き場は環境変数です。作業フォルダに `.env` ファイルがあれば先に読みますが、すでに環境にある変数は上書きしません（本物の環境が優先、という Python 版と同じ約束です）。

設定項目は次のとおりです。

| 名前 | 必須 | 既定値 | 意味 |
|------|------|--------|------|
| `DATABASE_URL` | ○ | — | PostgreSQL への接続 URL |
| `SECRET_KEY` | serve のとき ○ | — | セッション Cookie の署名鍵 |
| `SESSION_COOKIE_NAME` | — | `session` | セッション Cookie の名前 |
| `DEBUG` | — | false | true のとき Cookie の Secure 属性を外す（地元の HTTP 確認用） |
| `PORT` | — | 8000 | 待ち受けポート（Go 版だけの追加） |

細かい工夫が二つあります。一つ目は `NormalizeDatabaseURL` で、Python 版の URL（`postgresql+asyncpg://…`）をそのまま渡されても、pgx が読める形（`postgres://…`）に直します。環境変数を三つの版で使い回せるようにするための合わせ込みです。二つ目は `DEBUG` の解釈で、`1/true/yes/on` のような表記ゆれを受けます。

```text
DATABASE_URL=postgres://user:password@localhost:5432/reddit_clone
SECRET_KEY=change-me-to-a-long-random-string
SESSION_COOKIE_NAME=session
DEBUG=false
PORT=8000
```

```text
module redditclone

go 1.27

require (
	github.com/jackc/pgx/v5 v5.12.0
	golang.org/x/crypto v0.58.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)
```

ご覧のとおり、外部モジュールは pgx（と、その依存である pgpassfile / pgservicefile / puddle）と `golang.org/x/crypto`（bcrypt）だけです。

```go
// Package config loads application settings from the environment,
// mirroring the Python version's app/config.py (pydantic-settings).
//
// A .env file in the working directory is read first; variables already
// present in the real environment take precedence over the file.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the application settings.
type Config struct {
	// DatabaseURL is normalized for pgx (the "+asyncpg" marker used by
	// the Python version is stripped).
	DatabaseURL string
	// SecretKey signs the session cookie. Required to serve.
	SecretKey string
	// SessionCookieName defaults to "session".
	SessionCookieName string
	// Debug defaults to false.
	Debug bool
	// Port is the HTTP listen port. Defaults to 8000, the same as the
	// Python version's uvicorn default.
	Port int
}

// Load reads settings for serving: DATABASE_URL and SECRET_KEY are
// required.
func Load() (Config, error) {
	return load(true)
}

// LoadForMigrate reads settings for the migrate subcommand: SECRET_KEY is
// not required (Alembic likewise only needs the database URL).
func LoadForMigrate() (Config, error) {
	return load(false)
}

func load(requireSecret bool) (Config, error) {
	if err := loadEnvFile(".env"); err != nil {
		return Config{}, err
	}

	cfg := Config{
		SessionCookieName: "session",
		Port:              8000,
	}

	rawURL := os.Getenv("DATABASE_URL")
	if rawURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is not set")
	}
	cfg.DatabaseURL = NormalizeDatabaseURL(rawURL)

	cfg.SecretKey = os.Getenv("SECRET_KEY")
	if requireSecret && cfg.SecretKey == "" {
		return Config{}, fmt.Errorf("SECRET_KEY is not set")
	}

	if v := os.Getenv("SESSION_COOKIE_NAME"); v != "" {
		cfg.SessionCookieName = v
	}

	if v := os.Getenv("DEBUG"); v != "" {
		b, err := parseBool(v)
		if err != nil {
			return Config{}, fmt.Errorf("DEBUG: %w", err)
		}
		cfg.Debug = b
	}

	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return Config{}, fmt.Errorf("PORT: invalid port %q", v)
		}
		cfg.Port = p
	}

	return cfg, nil
}

// NormalizeDatabaseURL converts a SQLAlchemy-style URL into one pgx
// accepts: "postgresql+asyncpg://..." becomes "postgres://...".
func NormalizeDatabaseURL(raw string) string {
	for _, prefix := range []string{"postgresql+asyncpg://", "postgres+asyncpg://"} {
		if strings.HasPrefix(raw, prefix) {
			return "postgres://" + strings.TrimPrefix(raw, prefix)
		}
	}
	return raw
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean %q", v)
}

// loadEnvFile reads simple KEY=VALUE lines from path into the process
// environment, without overriding variables that are already set. A
// missing file is not an error.
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		value = strings.Trim(value, `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, value)
		}
	}
	return sc.Err()
}
```

## 1.3 接続プール

`internal/db/db.go` は短いファイルです。`database/sql` に pgx のドライバ（`stdlib` 経由）を登録し、プールを1つ開いて、応答があるかを最初に確かめてから返します。プールの大きさ（最大10本など）は、この規模のアプリには十分な固定値にしています。

```go
// Package db owns the PostgreSQL connection pool and the migration
// runner.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver

	"redditclone/internal/config"
)

// Open creates the connection pool and verifies the database answers.
func Open(ctx context.Context, cfg config.Config) (*sql.DB, error) {
	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}
```

ここで大事なのは、**このプール（`*sql.DB`）がそのまま「DB への入口」として全体に渡っていく**ことです。サービス層の関数は、プールでもトランザクションでも同じ顔で受けられるよう、小さなインターフェース（第3章の `Executor`）を使います。

## 1.4 マイグレーション

テーブルの用意は、SQL ファイルを順に当てる方式です。ファイルは `migrations/` フォルダに、`0001_名前.up.sql`（進める）と `0001_名前.down.sql`（戻す）の組で置きます。

| 版 | 中身 | Python 版の対応 |
|----|------|----------------|
| 0001 | UUID 拡張（uuid-ossp）を有効化 | revision 0001 |
| 0002 | users | 0002 |
| 0003 | subreddits | 0003 |
| 0004 | subreddit_members | 0004 |
| 0005 | posts | 0005 |
| 0006 | comments | 0006 |
| 0007 | votes | 0007 |
| 0008 | reports | 0008 |
| 0009 | 大文字小文字を区別しない一意索引（lower() 関数索引） | 0009 |

この対応は偶然ではなく、意図して 1 対 1 にしてあります。おかげで、Go 版が当てた DB と Python 版（Alembic）が当てた DB を比べることができ、実際にスキーマの記述 93 項目が完全一致することを確認しました（第7章）。

SQL ファイルはバイナリに埋め込みます。`migrations/migrations.go` の `//go:embed` がその仕掛けで、配るファイルはバイナリ1つで済みます。

```go
// Package migrations holds the SQL migration files, embedded into the
// binary so a single file is all that needs to be deployed.
//
// Files are named NNNN_description.up.sql / NNNN_description.down.sql and
// correspond one-to-one with the Python version's Alembic chain
// (revisions 0001 to 0009).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
```

実行係（`internal/db/migrate.go`）の動きは次のとおりです。

- 当たった版は `schema_migrations` テーブルに記録します。
- 進めるときは、未適用のものを番号順に、**1つずつトランザクションで**当てます。PostgreSQL は DDL もトランザクションで守れるので、途中で失敗しても半端な状態は残りません。
- 戻すときは、直近の1つだけを戻します（Alembic の `downgrade -1` 相当）。

代表として、最初のファイル（拡張の有効化）と、最後のファイル（方式B の関数索引）を見てください。全文は付録1にあります。

```sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
```

```sql
DROP INDEX ix_users_username;
DROP INDEX ix_users_email;
CREATE UNIQUE INDEX ix_users_username_lower ON users (lower(username));
CREATE UNIQUE INDEX ix_users_email_lower ON users (lower(email));
DROP INDEX ix_subreddits_name;
CREATE UNIQUE INDEX ix_subreddits_name_lower ON subreddits (lower(name));
```

0009 が要点です。ユーザー名やメールアドレスの一意性は、`lower()` を掛けた値に対する一意索引で守ります。表示用の綴り（`Alice` のような大文字混じり）はそのまま保存し、重なりだけを大文字小文字を無視して判定する——これがレビュー対応で決まった「方式B」で、第3章の登録処理と対になっています。

```go
package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"redditclone/migrations"
)

// Migrator applies and rolls back the embedded SQL migrations. Applied
// versions are recorded in the schema_migrations table. Each migration
// runs in its own transaction (PostgreSQL DDL is transactional), so a
// failed migration leaves no half-applied state.
type Migrator struct {
	db *sql.DB
}

func NewMigrator(db *sql.DB) *Migrator {
	return &Migrator{db: db}
}

type migration struct {
	version string // "0001"
	name    string // "0001_enable_uuid_extension"
	upSQL   string
	downSQL string
}

func loadMigrations() ([]migration, error) {
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		return nil, err
	}
	byVersion := map[string]*migration{}
	for _, e := range entries {
		name := e.Name()
		var version, base string
		var isUp bool
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			base = strings.TrimSuffix(name, ".up.sql")
			isUp = true
		case strings.HasSuffix(name, ".down.sql"):
			base = strings.TrimSuffix(name, ".down.sql")
		default:
			continue
		}
		version, _, _ = strings.Cut(base, "_")
		m := byVersion[version]
		if m == nil {
			m = &migration{version: version, name: base}
			byVersion[version] = m
		}
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			return nil, err
		}
		if isUp {
			m.upSQL = string(body)
		} else {
			m.downSQL = string(body)
		}
	}

	list := make([]migration, 0, len(byVersion))
	for _, m := range byVersion {
		if m.upSQL == "" || m.downSQL == "" {
			return nil, fmt.Errorf("migration %s is missing its up or down SQL", m.name)
		}
		list = append(list, *m)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].version < list[j].version })
	return list, nil
}

func (m *Migrator) ensureTable(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version text PRIMARY KEY,
		applied_at timestamp with time zone NOT NULL DEFAULT now()
	)`)
	return err
}

// Applied returns the applied versions in ascending order.
func (m *Migrator) Applied(ctx context.Context) ([]string, error) {
	if err := m.ensureTable(ctx); err != nil {
		return nil, err
	}
	rows, err := m.db.QueryContext(ctx,
		`SELECT version FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Up applies every pending migration in order and returns the versions
// applied by this call.
func (m *Migrator) Up(ctx context.Context) ([]string, error) {
	all, err := loadMigrations()
	if err != nil {
		return nil, err
	}
	done, err := m.Applied(ctx)
	if err != nil {
		return nil, err
	}
	applied := map[string]bool{}
	for _, v := range done {
		applied[v] = true
	}

	var newly []string
	for _, mig := range all {
		if applied[mig.version] {
			continue
		}
		if err := m.apply(ctx, mig); err != nil {
			return newly, fmt.Errorf("migration %s: %w", mig.name, err)
		}
		newly = append(newly, mig.version)
	}
	return newly, nil
}

func (m *Migrator) apply(ctx context.Context, mig migration) error {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, mig.upSQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)`, mig.version); err != nil {
		return err
	}
	return tx.Commit()
}

// Down rolls back the most recently applied migration and returns its
// version. It returns "" when nothing is applied.
func (m *Migrator) Down(ctx context.Context) (string, error) {
	all, err := loadMigrations()
	if err != nil {
		return "", err
	}
	done, err := m.Applied(ctx)
	if err != nil {
		return "", err
	}
	if len(done) == 0 {
		return "", nil
	}
	latest := done[len(done)-1]
	for _, mig := range all {
		if mig.version != latest {
			continue
		}
		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return "", err
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, mig.downSQL); err != nil {
			return "", fmt.Errorf("migration %s (down): %w", mig.name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM schema_migrations WHERE version = $1`, mig.version); err != nil {
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return mig.version, nil
	}
	return "", fmt.Errorf("applied version %s has no migration file", latest)
}
```

## 1.5 この章の確認

- `redditclone migrate` で 0001〜0009 が順に当たり、`migrate status` で9件が並ぶこと
- テーブルが7つ（users / subreddits / subreddit_members / posts / comments / votes / reports）と `schema_migrations` ができること
- `redditclone serve` で起動し、`GET /health` が `{"database":"ok","status":"ok"}` を返すこと（`/health` のハンドラ自体は第6章で出てきます）
