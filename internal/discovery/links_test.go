package discovery_test

import (
	"testing"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/discovery"
	"github.com/hneyra/crawler-go/internal/fetch"
)

func TestExtractLink(t *testing.T) {
	base := "https://shop.pe/list/"
	cases := []struct {
		html string
		kind crawl.LinkExtractionType
		attr string
		want string
	}{
		{`<a href="/p/1">x</a>`, crawl.LinkHref, "", "https://shop.pe/p/1"},
		{`<a href="p/2">x</a>`, crawl.LinkHref, "", "https://shop.pe/list/p/2"},
		{`<a>x</a>`, crawl.LinkHref, "", ""},
		{`<div onclick="location.href='/p/3'">x</div>`, crawl.LinkOnclick, "", "https://shop.pe/p/3"},
		{`<div onclick="window.open('https://o.pe/p/4', '_blank')">x</div>`, crawl.LinkOnclick, "", "https://o.pe/p/4"},
		{`<div onclick="window.location = &quot;/p/5&quot;">x</div>`, crawl.LinkOnclick, "", "https://shop.pe/p/5"},
		{`<div onclick="location.replace('/p/6')">x</div>`, crawl.LinkOnclick, "", "https://shop.pe/p/6"},
		{`<div onclick="go('/p/7', 1)">x</div>`, crawl.LinkOnclick, "", "https://shop.pe/p/7"},
		{`<div onclick="track(42)">x</div>`, crawl.LinkOnclick, "", ""},
		{`<div data-to-posting="/propiedades/8.html">x</div>`, crawl.LinkDataAttribute, "data-to-posting", "https://shop.pe/propiedades/8.html"},
		{`<div data-url="https://o.pe/9">x</div>`, crawl.LinkDataAttribute, "data-url", "https://o.pe/9"},
		{`<div data-url="">x</div>`, crawl.LinkDataAttribute, "data-url", ""},
	}
	for _, c := range cases {
		p, err := fetch.NewPage(base, "<html><body>"+c.html+"</body></html>", false)
		if err != nil {
			t.Fatal(err)
		}
		got := discovery.ExtractLink(p.Doc.Find("body > *").First(), c.kind, c.attr, base)
		if got != c.want {
			t.Errorf("%s %s: got %q want %q", c.kind, c.html, got, c.want)
		}
	}
}

func TestNextPage(t *testing.T) {
	page := func(h string) *fetch.Page {
		p, _ := fetch.NewPage("https://shop.pe/l?p=1", "<html><body>"+h+"</body></html>", false)
		return &p
	}
	if got := discovery.NextPageURL(page(`<a rel="next" href="?p=2">n</a>`).Doc, ""); got != "https://shop.pe/l?p=2" {
		t.Errorf("rel next: %q", got)
	}
	if got := discovery.NextPageURL(page(`<a href="/a">1</a><a href="/b">›</a>`).Doc, ""); got != "https://shop.pe/b" {
		t.Errorf("text: %q", got)
	}
	if got := discovery.NextPageURL(page(`<a rel="next" href="/x">n</a><a class="c" href="/y">y</a>`).Doc, "a.c"); got != "https://shop.pe/y" {
		t.Errorf("custom wins: %q", got)
	}
	if got := discovery.NextPageURL(page(`<a rel="next" href="/x">n</a>`).Doc, "a.missing"); got != "" {
		t.Errorf("custom missing: %q", got)
	}
	if got := discovery.NextPageURL(page(`<a href="/a">1</a>`).Doc, ""); got != "" {
		t.Errorf("none: %q", got)
	}
	sel, text, ok := discovery.NextClickTarget(page(`<a href="#">Next</a>`).Doc, "")
	if !ok || sel != "" || text != "Next" {
		t.Errorf("click target: %q %q %v", sel, text, ok)
	}
	if _, _, ok := discovery.NextClickTarget(page(`<a href="#">1</a>`).Doc, ""); ok {
		t.Error("no target expected")
	}
}
