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
