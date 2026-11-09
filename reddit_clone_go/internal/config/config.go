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
