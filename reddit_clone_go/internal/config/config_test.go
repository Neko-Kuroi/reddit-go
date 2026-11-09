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
