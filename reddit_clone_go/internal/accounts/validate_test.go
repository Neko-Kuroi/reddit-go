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
