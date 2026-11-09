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
