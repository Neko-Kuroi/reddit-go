# Reddit クローン（Go 版）— 全コード付録 1 基盤とマイグレーション

正本のコードファイルの全文を機械生成で収録したものです（手写しではありません）。

## .env.example

```text
DATABASE_URL=postgres://user:password@localhost:5432/reddit_clone
SECRET_KEY=change-me-to-a-long-random-string
SESSION_COOKIE_NAME=session
DEBUG=false
PORT=8000
```

## go.mod

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

## main.go

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

## internal/config/config.go

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

## internal/db/db.go

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

## internal/db/migrate.go

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

## migrations/migrations.go

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

## migrations/0001_enable_uuid_extension.down.sql

```sql
DROP EXTENSION IF EXISTS "uuid-ossp";
```

## migrations/0001_enable_uuid_extension.up.sql

```sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
```

## migrations/0002_create_users.down.sql

```sql
DROP INDEX ix_users_email;
DROP INDEX ix_users_username;
DROP TABLE users;
```

## migrations/0002_create_users.up.sql

```sql
CREATE TABLE users (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    username character varying(20) NOT NULL,
    email character varying(255) NOT NULL,
    password_hash character varying(255) NOT NULL,
    post_karma integer DEFAULT 0 NOT NULL,
    comment_karma integer DEFAULT 0 NOT NULL,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX ix_users_username ON users (username);
CREATE UNIQUE INDEX ix_users_email ON users (email);
```

## migrations/0003_create_subreddits.down.sql

```sql
DROP INDEX ix_subreddits_owner_id;
DROP INDEX ix_subreddits_name;
DROP TABLE subreddits;
```

## migrations/0003_create_subreddits.up.sql

```sql
CREATE TABLE subreddits (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    name character varying(21) NOT NULL,
    description text,
    owner_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX ix_subreddits_name ON subreddits (name);
CREATE INDEX ix_subreddits_owner_id ON subreddits (owner_id);
```

## migrations/0004_create_subreddit_members.down.sql

```sql
DROP INDEX ix_subreddit_members_subreddit_id;
DROP INDEX ix_subreddit_members_user_id;
DROP INDEX ix_subreddit_members_unique;
DROP TABLE subreddit_members;
```

## migrations/0004_create_subreddit_members.up.sql

```sql
CREATE TABLE subreddit_members (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    subreddit_id uuid NOT NULL REFERENCES subreddits (id) ON DELETE CASCADE,
    role character varying(20) DEFAULT 'member' NOT NULL,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX ix_subreddit_members_unique ON subreddit_members (user_id, subreddit_id);
CREATE INDEX ix_subreddit_members_user_id ON subreddit_members (user_id);
CREATE INDEX ix_subreddit_members_subreddit_id ON subreddit_members (subreddit_id);
```

## migrations/0005_create_posts.down.sql

```sql
DROP INDEX ix_posts_hot_score;
DROP INDEX ix_posts_subreddit_new;
DROP INDEX ix_posts_subreddit_hot;
DROP INDEX ix_posts_subreddit_id;
DROP INDEX ix_posts_author_id;
DROP TABLE posts;
```

## migrations/0005_create_posts.up.sql

```sql
CREATE TABLE posts (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    title character varying(300) NOT NULL,
    type character varying(10) NOT NULL,
    body text,
    url character varying(2048),
    score integer DEFAULT 0 NOT NULL,
    hot_score double precision DEFAULT '0'::double precision NOT NULL,
    author_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    subreddit_id uuid NOT NULL REFERENCES subreddits (id) ON DELETE CASCADE,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE INDEX ix_posts_author_id ON posts (author_id);
CREATE INDEX ix_posts_subreddit_id ON posts (subreddit_id);
CREATE INDEX ix_posts_subreddit_hot ON posts (subreddit_id, hot_score);
CREATE INDEX ix_posts_subreddit_new ON posts (subreddit_id, inserted_at);
CREATE INDEX ix_posts_hot_score ON posts (hot_score);
-- 注: hot_score の既定値は Python 版と同じ 0.0。PG 上の既定値の表記まで
-- Python 版（SQLAlchemy が文字列として渡す形）に一致させるため、この書き方にしている。
```

## migrations/0006_create_comments.down.sql

```sql
DROP INDEX ix_comments_author_id;
DROP INDEX ix_comments_parent_id;
DROP INDEX ix_comments_post_id;
DROP TABLE comments;
```

## migrations/0006_create_comments.up.sql

```sql
CREATE TABLE comments (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    body text NOT NULL,
    score integer DEFAULT 0 NOT NULL,
    author_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    post_id uuid NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    parent_id uuid REFERENCES comments (id) ON DELETE CASCADE,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE INDEX ix_comments_post_id ON comments (post_id);
CREATE INDEX ix_comments_parent_id ON comments (parent_id);
CREATE INDEX ix_comments_author_id ON comments (author_id);
```

## migrations/0007_create_votes.down.sql

```sql
DROP INDEX ix_votes_target;
DROP INDEX ix_votes_unique;
DROP TABLE votes;
```

## migrations/0007_create_votes.up.sql

```sql
CREATE TABLE votes (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    value integer NOT NULL,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    target_type character varying(10) NOT NULL,
    target_id uuid NOT NULL,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE UNIQUE INDEX ix_votes_unique ON votes (user_id, target_type, target_id);
CREATE INDEX ix_votes_target ON votes (target_type, target_id);
```

## migrations/0008_create_reports.down.sql

```sql
DROP INDEX ix_reports_reporter_id;
DROP INDEX ix_reports_target;
DROP TABLE reports;
```

## migrations/0008_create_reports.up.sql

```sql
CREATE TABLE reports (
    id uuid DEFAULT uuid_generate_v4() NOT NULL,
    reason character varying(50) NOT NULL,
    reporter_id uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    target_type character varying(10) NOT NULL,
    target_id uuid NOT NULL,
    inserted_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (id)
);
CREATE INDEX ix_reports_target ON reports (target_type, target_id);
CREATE INDEX ix_reports_reporter_id ON reports (reporter_id);
```

## migrations/0009_add_case_insensitive_unique_indexes.down.sql

```sql
DROP INDEX ix_subreddits_name_lower;
CREATE UNIQUE INDEX ix_subreddits_name ON subreddits (name);
DROP INDEX ix_users_email_lower;
DROP INDEX ix_users_username_lower;
CREATE UNIQUE INDEX ix_users_email ON users (email);
CREATE UNIQUE INDEX ix_users_username ON users (username);
```

## migrations/0009_add_case_insensitive_unique_indexes.up.sql

```sql
DROP INDEX ix_users_username;
DROP INDEX ix_users_email;
CREATE UNIQUE INDEX ix_users_username_lower ON users (lower(username));
CREATE UNIQUE INDEX ix_users_email_lower ON users (lower(email));
DROP INDEX ix_subreddits_name;
CREATE UNIQUE INDEX ix_subreddits_name_lower ON subreddits (lower(name));
```

