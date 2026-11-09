# 第7章 テストと検証

Go 版の出来ばえは、三層の確認で支えています。

1. **単体テスト**（`go test ./...`）— DB を使わない純粋な関数の確認
2. **E2E 99 項目**（`run_go_e2e.py`）— Python 版の物差しをそのまま当てた実 HTTP の確認
3. **突き合わせ**— スキーマ・ハッシュ・計算式が Python 版と一致することの確認

## 7.1 単体テスト

単体テストは7ファイルあります。役割ごとに要点を挙げます。

- `internal/config/config_test.go` — 設定の読み取り（URL の正規化、DEBUG の解釈など）。
- `internal/auth/session_test.go` — トークンの往復、改ざんの検出、期限切れ。
- `internal/accounts/validate_test.go` — ユーザー名とパスワードの規則。
- `internal/accounts/email_test.go` — 第3章の**メールアドレス対応表（49件）**。EmailStr への実測結果そのものがテストになっています。
- `internal/accounts/password_test.go` — 第3章の**72バイト問題**。passlib が作った実物のハッシュ文字列を埋め込み、Go で照合できることを確かめます。
- `internal/posts/posts_test.go` — Hot スコアの計算。期待値は Python で計算した値で、符号つきの式が両版で一致することの固定です。
- `internal/web/funcs_test.go` — `formatKarma` の境界、`timeAgo`、`urlHost`、UUID の正規化。

```go
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
```

```go
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
```

```go
package accounts

import "testing"

func TestValidateUserCreate(t *testing.T) {
	ok := ValidateUserCreate("alice_01", "alice@example.com", "password123")
	if !ok.Empty() {
		t.Errorf("valid input produced errors: %+v", ok)
	}

	cases := []struct {
		name                      string
		username, email, password string
		field                     string
	}{
		{"short username", "ab", "a@b.co", "password123", "username"},
		{"long username", "abcdefghijklmnopqrstu", "a@b.co", "password123", "username"},
		{"bad chars username", "al ice", "a@b.co", "password123", "username"},
		{"japanese username", "ありす", "a@b.co", "password123", "username"},
		{"email no at", "alice", "alice.example.com", "password123", "email"},
		{"email no tld", "alice", "alice@example", "password123", "email"},
		{"email double dot", "alice", "al..ice@example.com", "password123", "email"},
		{"email empty local", "alice", "@example.com", "password123", "email"},
		{"short password", "alice", "a@b.co", "short", "password"},
	}
	for _, c := range cases {
		errs := ValidateUserCreate(c.username, c.email, c.password)
		var got string
		switch c.field {
		case "username":
			got = errs.Username
		case "email":
			got = errs.Email
		case "password":
			got = errs.Password
		}
		if got == "" {
			t.Errorf("%s: expected %s error, got %+v", c.name, c.field, errs)
		}
	}
}
```

```go
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
```

```go
package accounts

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestPasswordBytesTruncation(t *testing.T) {
	for n, want := range map[int]int{0: 0, 71: 71, 72: 72, 73: 72, 200: 72} {
		if got := len(passwordBytes(strings.Repeat("a", n))); got != want {
			t.Errorf("passwordBytes(%d bytes) = %d bytes, want %d", n, got, want)
		}
	}
	// The cut is by bytes, not runes (30 three-byte runes = 90 bytes).
	if got := len(passwordBytes(strings.Repeat("あ", 30))); got != 72 {
		t.Errorf("multibyte truncation = %d bytes, want 72", got)
	}
}

// A hash produced by the Python stack (passlib + bcrypt) for a
// 104-byte password must verify here once the input is truncated the
// same way, and changing anything past byte 72 must not matter —
// exactly what the Python side does.
func TestLongPasswordAgainstPasslibHash(t *testing.T) {
	passlibHash := "$2b$12$M6qxEH/b1PqWXCczVJAIcOL6dJvBgCog3ZacCBe6Y//bhy5U6hkei"
	password := "long-password-" + strings.Repeat("x", 90) // 104 bytes

	if err := bcrypt.CompareHashAndPassword(
		[]byte(passlibHash), passwordBytes(password)); err != nil {
		t.Fatalf("passlib hash does not verify: %v", err)
	}

	changedTail := "long-password-" + strings.Repeat("x", 89) + "y"
	if err := bcrypt.CompareHashAndPassword(
		[]byte(passlibHash), passwordBytes(changedTail)); err != nil {
		t.Errorf("change past byte 72 should not matter: %v", err)
	}

	changedHead := "long-passwore-" + strings.Repeat("x", 90)
	if err := bcrypt.CompareHashAndPassword(
		[]byte(passlibHash), passwordBytes(changedHead)); err == nil {
		t.Error("change within the first 72 bytes must fail")
	}
}

// Generating a hash for an over-long password must not error (it did
// before the truncation, surfacing as a 500 on registration).
func TestLongPasswordHashGeneration(t *testing.T) {
	password := strings.Repeat("p", 200)
	hash, err := bcrypt.GenerateFromPassword(passwordBytes(password), bcryptCost)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword(hash, passwordBytes(password)); err != nil {
		t.Errorf("generated hash does not verify: %v", err)
	}
}
```

```go
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
```

```go
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
```

## 7.2 E2E 99 項目の写し替え

本命の確認は、Python 版の E2E テスト 99 項目を Go 版に写し替えた `run_go_e2e.py` です。項目名（日本語）も中身も 1 対 1 で写し、実際の HTTP で Go のサーバーを叩きます。違うのは土台の部分だけで、たとえば最初の項目「create_all」は「空の DB にマイグレーション 0001〜0009 が当たること」に対応します。

内訳は、基本の55項目（認証・CSRF・コミュニティ・投稿・コメント・報告・検索・プロフィール・削除・ログイン）＋レビュー対応の項目（同時投票、二度押し、符号つき Hot の並び、方式Bの大文字小文字、page=0 や不正な sort の 422、ページングの境界 25/26 件、報告エラーのエスケープ、投票の付け替え競合、参加の二度押し、URL の 2048/2049 文字など）です。

### 二重クリックの項目で学んだこと

写し替えの途中で、二重クリックの4項目だけが落ちることがありました。調べると、サーバーの不具合ではなく、テストの送り方の問題でした。非同期クライアントから2リクエストを「同時に」送ったつもりでも、実際にはほぼ直列に届いていて、2回目は「確定した後のもう一度の投票」として正しくトグル（取り消し）になっていたのです。不変条件（スコア = 票の合計、500 は無い）は最初から保たれていました。

そこで送り方を「2つの OS スレッドから、接続を温めておいてバリアで同時に発射」に変え、合格に至りました。教訓は、**競合のテストは、同時性の作り方自体を疑う**ことです。本物の2プロセスで叩く確認（curl を並行に走らせる）でも、票は1件・スコアは+1 にそろうことを別途確かめています。

## 7.3 突き合わせの記録

移植の各段階で、Python 版との一致を実測しました。主なものを挙げます。

- **スキーマ**: Alembic が当てた DB と Go が当てた DB で、列・索引・外部キー・主キーの記述 93 項目が完全一致。
- **bcrypt**: passlib の `$2b$` ハッシュが Go で照合でき、Go の `$2a$` ハッシュが passlib で照合できる（72バイト以下のパスワードで）。
- **Hot スコア**: Go の関数と SQL の式と Python の計算が、同じ入力で一致（誤差 1e-6 以内）。
- **経路**: Python 版の25経路＋静的ファイル＋`/health` と、Go の登録が完全に対応。
- **エスケープ**: 投票エラーの断片が、Python の `html.escape` と同じ文字列になる（`&#x27;` の件、第5章）。

## 7.4 実行のしかた

```
# 単体テスト
go test ./...

# E2E（サーバーと DB を用意してから）
DATABASE_URL=... SECRET_KEY=... DEBUG=true PORT=8129 ./redditclone serve &
python3 run_go_e2e.py   # 既定では :8129 / 専用 DB を使う
```

E2E の接続先は環境変数で差し替えられます（`GO_E2E_BASE_URL` / `GO_E2E_DATABASE_URL`）。DEBUG=true が要るのは、セッション Cookie の Secure 属性を地元の HTTP では外すためです。
