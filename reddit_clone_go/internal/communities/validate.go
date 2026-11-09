package communities

import (
	"regexp"
	"unicode/utf8"
)

// FormErrors carries one message per field of the create form.
type FormErrors struct {
	Name        string
	Description string
}

// Empty reports whether no error was recorded.
func (e FormErrors) Empty() bool { return e == FormErrors{} }

var nameRE = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ValidateSubredditCreate applies the rules of the Python version's
// SubredditCreate schema. Character counts are rune counts.
func ValidateSubredditCreate(name, description string) FormErrors {
	var errs FormErrors
	if n := utf8.RuneCountInString(name); n < 3 || n > 21 {
		errs.Name = "コミュニティ名は3〜21文字にしてください"
	} else if !nameRE.MatchString(name) {
		errs.Name = "英数字とアンダースコアのみ使用できます"
	}
	if utf8.RuneCountInString(description) > 500 {
		errs.Description = "説明は500文字以内にしてください"
	}
	return errs
}
