package posts

import "unicode/utf8"

// FormErrors carries one message per field of the submit form, plus a
// general error (the Python version maps model-level validation
// failures to "general").
type FormErrors struct {
	General string
	Title   string
	URL     string
	Body    string
}

// Empty reports whether no error was recorded.
func (e FormErrors) Empty() bool { return e == FormErrors{} }

// ValidatePostCreate applies the rules of the Python version's
// PostCreate schema. As in the router there, the body only applies to
// text posts and the URL only to link posts. Field errors are reported
// first; the type-dependent requirements are checked only when the
// fields themselves are valid (pydantic's order).
func ValidatePostCreate(title, postType, body, url string) (CreateInput, FormErrors) {
	var errs FormErrors
	in := CreateInput{Title: title, Type: postType}

	if postType != "text" && postType != "link" {
		errs.General = "投稿の種別が正しくありません"
		return in, errs
	}

	if n := utf8.RuneCountInString(title); n < 1 || n > 300 {
		errs.Title = "タイトルは1〜300文字にしてください"
	}
	if postType == "link" && utf8.RuneCountInString(url) > 2048 {
		errs.URL = "URLは2048文字以内にしてください"
	}
	if postType == "text" && utf8.RuneCountInString(body) > 40000 {
		errs.Body = "本文は40,000文字以内にしてください"
	}
	if !errs.Empty() {
		return in, errs
	}

	switch postType {
	case "text":
		if body == "" {
			errs.General = "テキスト投稿には本文が必要です"
			return in, errs
		}
		in.Body = &body
	case "link":
		if url == "" {
			errs.General = "リンク投稿には URL が必要です"
			return in, errs
		}
		if !hasHTTPPrefix(url) {
			errs.General = "URL は http:// または https:// で始まる必要があります"
			return in, errs
		}
		in.URL = &url
	}
	return in, errs
}

func hasHTTPPrefix(url string) bool {
	return len(url) >= 7 && url[:7] == "http://" ||
		len(url) >= 8 && url[:8] == "https://"
}
