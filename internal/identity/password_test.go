package identity

import (
	"strings"
	"testing"
)

func TestHashVerify(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Fatalf("bad phc %q", h)
	}
	if !VerifyPassword("correct horse battery", h) {
		t.Fatal("verify failed")
	}
	if VerifyPassword("correct horse batterx", h) {
		t.Fatal("wrong password verified")
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Fatal("salt not random")
	}
	for _, bad := range []string{"", "x", "$argon2id$v=19$m=65536,t=3,p=4$!!$!!", "$bcrypt$...", "$argon2id$v=19$m=9999999,t=3,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"} {
		if VerifyPassword("a", bad) {
			t.Fatalf("garbage hash %q verified", bad)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		pw, email string
		ok        bool
	}{
		{"short", "a@b.example", false},
		{strings.Repeat("a", 11), "a@b.example", false},
		{strings.Repeat("a", 12), "a@b.example", true},
		{strings.Repeat("a", 257), "a@b.example", false},
		{"Long.User@B.example", "long.user@b.example", false},
	}
	for _, c := range cases {
		if err := ValidatePassword(c.pw, c.email); (err == nil) != c.ok {
			t.Errorf("%q: err=%v", c.pw, err)
		}
	}
}
