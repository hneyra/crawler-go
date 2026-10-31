package urlx_test

import (
	"testing"

	"github.com/hneyra/crawler-go/internal/urlx"
)

// port of java UrlUtilsTest / HashUtilsTest.
func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"https://example.com/product/1#reviews": "https://example.com/product/1",
		"https://example.com/p?id=42&ref=home":  "https://example.com/p?id=42&ref=home",
		"https://example.com/p?id=1#section":    "https://example.com/p?id=1",
		"  https://example.com/p/1  ":           "https://example.com/p/1",
		"https://example.com/":                  "https://example.com/",
		"https://example.com/B?z=1&a=2":         "https://example.com/B?z=1&a=2",
		"https://example.com/a%20b?q=%C3%B1#x":  "https://example.com/a%20b?q=%C3%B1",
	}
	for in, want := range cases {
		got, err := urlx.Normalize(in)
		if err != nil || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"http://[invalid", "", "/relative/path", "not a url"} {
		if _, err := urlx.Normalize(bad); err == nil {
			t.Errorf("Normalize(%q) accepted", bad)
		}
	}
}

func TestResolve(t *testing.T) {
	base := "https://shop.pe/list/page?x=1"
	cases := map[string]string{
		"https://other.pe/p/1": "https://other.pe/p/1",
		"/p/2":                 "https://shop.pe/p/2",
		"p/3":                  "https://shop.pe/list/p/3",
		"?page=2":              "https://shop.pe/list/page?page=2",
	}
	for in, want := range cases {
		if got := urlx.Resolve(in, base); got != want {
			t.Errorf("Resolve(%q) = %q want %q", in, got, want)
		}
	}
	if got := urlx.Resolve("/x", "http://[bad"); got != "" {
		t.Errorf("bad base: %q", got)
	}
}

func TestSHA256(t *testing.T) {
	if got := urlx.SHA256(""); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("empty: %s", got)
	}
	a, b := urlx.SHA256("https://x.pe/1"), urlx.SHA256("https://x.pe/1")
	if a != b || len(a) != 64 || a == urlx.SHA256("https://x.pe/2") {
		t.Fatalf("hash: %s %s", a, b)
	}
}
