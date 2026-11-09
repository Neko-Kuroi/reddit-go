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
