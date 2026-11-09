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
