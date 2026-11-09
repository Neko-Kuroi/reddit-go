package web

import (
	"regexp"
	"strings"
)

// Minimal UUID handling (the port avoids an external uuid module).
// Accepts the canonical hyphenated form and the bare 32-hex-digit
// form, as Python's uuid.UUID does, and normalizes to canonical
// lowercase.

var (
	uuidCanonicalRE = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	uuidBareRE = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
)

// parseUUID validates and normalizes a UUID string.
func parseUUID(s string) (string, bool) {
	if uuidCanonicalRE.MatchString(s) {
		return strings.ToLower(s), true
	}
	if uuidBareRE.MatchString(s) {
		s = strings.ToLower(s)
		return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], true
	}
	return "", false
}
