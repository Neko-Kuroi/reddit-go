package accounts

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// FormErrors carries one message per field, plus a general error. It
// mirrors the errors dict the Python version passes to the register
// template.
type FormErrors struct {
	General  string
	Username string
	Email    string
	Password string
}

// Empty reports whether no error was recorded.
func (e FormErrors) Empty() bool {
	return e == FormErrors{}
}

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ValidateUserCreate applies the rules of the Python version's
// UserCreate schema (schemas.py). Character counts are rune counts,
// matching Python's len() on str.
func ValidateUserCreate(username, email, password string) FormErrors {
	var errs FormErrors

	if n := utf8.RuneCountInString(username); n < 3 || n > 20 {
		errs.Username = "ユーザー名は3〜20文字にしてください"
	} else if !usernameRE.MatchString(username) {
		errs.Username = "ユーザー名は英数字とアンダースコアのみ使用できます"
	}

	if !validEmail(email) {
		errs.Email = "メールアドレスの形式が正しくありません"
	}

	if utf8.RuneCountInString(password) < 8 {
		errs.Password = "パスワードは8文字以上にしてください"
	}

	return errs
}

// validEmail mirrors the acceptance of pydantic's EmailStr as
// measured against the pinned Python stack (email-validator 2.3.0).
// The correspondence battery lives in validate_test.go; the rules
// below are the ones the measurements support:
//
//   - the whole address is at most 254 characters (the SMTP limit);
//   - exactly one @; the local part is a dot-atom (no leading,
//     trailing or doubled dot) over ASCII letters/digits and the
//     usual specials, with non-ASCII characters also allowed;
//   - the domain has at least two dot-separated labels of at most 63
//     characters, letters/digits (Unicode) and hyphens, never
//     starting or ending with a hyphen;
//   - the last label (TLD) is not made of digits only.
//
// EmailStr also strips surrounding whitespace from the address; that
// happens at the form boundary in the handler, so the input here is
// expected to be trimmed already.
func validEmail(email string) bool {
	if n := utf8.RuneCountInString(email); n == 0 || n > 254 {
		return false
	}
	if strings.Count(email, "@") != 1 {
		return false
	}
	local, domain, _ := strings.Cut(email, "@")
	return validEmailLocal(local) && validEmailDomain(domain)
}

func validEmailLocal(local string) bool {
	if local == "" {
		return false
	}
	if strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") ||
		strings.Contains(local, "..") {
		return false
	}
	for _, r := range local {
		if r < 0x80 {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
				r >= '0' && r <= '9' ||
				strings.ContainsRune(".!#$%&'*+-/=?^_`{|}~", r)) {
				return false
			}
		} else if unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func validEmailDomain(domain string) bool {
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || utf8.RuneCountInString(label) > 63 ||
			strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if r != '-' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return false
			}
		}
	}
	// The TLD must not be all digits ("example.123" and bare IPv4
	// forms are rejected by EmailStr for this reason).
	tld := labels[len(labels)-1]
	for _, r := range tld {
		if !unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
