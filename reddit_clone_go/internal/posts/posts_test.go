package posts

import (
	"math"
	"strings"
	"testing"
	"time"
)

// Expected values computed with the Python version's
// Post.compute_hot_score (see app/posts/models.py).
func TestComputeHotScore(t *testing.T) {
	cases := []struct {
		score int
		at    time.Time
		want  float64
	}{
		{0, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 14071.05771111111},
		{10, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 14072.05771111111},
		{-3, time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC), 13687.540589856391},
		{1, time.Date(2005, 12, 8, 7, 46, 43, 0, time.UTC), 0.0},
	}
	for _, c := range cases {
		got := ComputeHotScore(c.score, c.at)
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("ComputeHotScore(%d, %v) = %v, want %v",
				c.score, c.at, got, c.want)
		}
	}
	// The sign is kept: a heavily downvoted post ranks below a fresh
	// zero-score post of the same age.
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !(ComputeHotScore(-50, at) < ComputeHotScore(0, at)) {
		t.Error("negative score should rank below zero score")
	}
}

func TestValidatePostCreate(t *testing.T) {
	// valid text post
	in, errs := ValidatePostCreate("タイトル", "text", "本文です", "")
	if !errs.Empty() || in.Body == nil || *in.Body != "本文です" || in.URL != nil {
		t.Errorf("valid text: %+v %+v", in, errs)
	}
	// valid link post
	in, errs = ValidatePostCreate("タイトル", "link", "", "https://example.com/x")
	if !errs.Empty() || in.URL == nil || in.Body != nil {
		t.Errorf("valid link: %+v %+v", in, errs)
	}
	// text without body -> general
	_, errs = ValidatePostCreate("タイトル", "text", "", "")
	if errs.General != "テキスト投稿には本文が必要です" {
		t.Errorf("empty body: %+v", errs)
	}
	// link without url -> general
	_, errs = ValidatePostCreate("タイトル", "link", "", "")
	if errs.General != "リンク投稿には URL が必要です" {
		t.Errorf("empty url: %+v", errs)
	}
	// link with bad scheme -> general
	_, errs = ValidatePostCreate("タイトル", "link", "", "ftp://example.com")
	if errs.General != "URL は http:// または https:// で始まる必要があります" {
		t.Errorf("bad scheme: %+v", errs)
	}
	// title too long (301 runes) -> title error
	long := make([]rune, 301)
	for i := range long {
		long[i] = 'あ'
	}
	_, errs = ValidatePostCreate(string(long), "text", "x", "")
	if errs.Title == "" {
		t.Errorf("long title: %+v", errs)
	}
	// url too long (2049 runes) -> url field error, not general
	longURL := "https://" + strings.Repeat("a", 2041)
	_, errs = ValidatePostCreate("t", "link", "", longURL)
	if errs.URL == "" {
		t.Errorf("long url: %+v", errs)
	}
}
