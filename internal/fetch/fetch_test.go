package fetch_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hneyra/crawler-go/internal/fetch"
)

type fakeRenderer struct {
	calls []fetch.RenderRequest
	html  string
	err   error
}

func (f *fakeRenderer) Render(_ context.Context, r fetch.RenderRequest) (fetch.RenderResult, error) {
	f.calls = append(f.calls, r)
	return fetch.RenderResult{HTML: f.html}, f.err
}

func (f *fakeRenderer) ClickNavigate(context.Context, string, fetch.ClickTarget, time.Duration) (string, error) {
	return "", errors.New("not used")
}

type counter struct{ fetches, fallbacks int }

func (c *counter) FetchDone(time.Duration, error) { c.fetches++ }
func (c *counter) CloudflareFallback()            { c.fallbacks++ }

func site(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.UserAgent(), "Mozilla/") {
			t.Errorf("ua %q", r.UserAgent())
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchPlainPage(t *testing.T) {
	srv := site(t, 200, `<html><head><title>Tienda</title></head><body><a class="item" href="/p/1">x</a></body></html>`)
	r := &fakeRenderer{}
	obs := &counter{}
	f := &fetch.HTTPFetcher{UserAgent: "Mozilla/5.0 test", Renderer: r, Observer: obs}
	p, err := f.Fetch(context.Background(), srv.URL+"/list", "a.item")
	if err != nil || p.Rendered || len(r.calls) != 0 {
		t.Fatalf("plain: %v rendered=%v calls=%d", err, p.Rendered, len(r.calls))
	}
	href, _ := p.Doc.Find("a.item").Attr("href")
	if href != "/p/1" || p.Doc.Url.String() != srv.URL+"/list" || obs.fetches != 1 || obs.fallbacks != 0 {
		t.Fatalf("doc: %q %v %+v", href, p.Doc.Url, obs)
	}
}

func TestFetchFallsBackOnBlockedStatus(t *testing.T) {
	for _, st := range []int{403, 429, 503} {
		srv := site(t, st, "blocked")
		r := &fakeRenderer{html: `<html><body><a class="item" href="/p/9">ok</a></body></html>`}
		obs := &counter{}
		f := &fetch.HTTPFetcher{UserAgent: "Mozilla/5.0", Renderer: r, Observer: obs}
		p, err := f.Fetch(context.Background(), srv.URL, "a.item")
		if err != nil || !p.Rendered || len(r.calls) != 1 || r.calls[0].WaitForSelector != "a.item" || obs.fallbacks != 1 {
			t.Fatalf("status %d: %v %+v %+v", st, err, r.calls, obs)
		}
		if p.Doc.Find("a.item").Length() != 1 {
			t.Fatalf("status %d: rendered dom lost", st)
		}
	}
}

func TestFetchFallsBackOnChallenge(t *testing.T) {
	srv := site(t, 200, `<html><head><title>Just a moment...</title></head><body><div id="cf-browser-verification"></div></body></html>`)
	r := &fakeRenderer{html: `<html><body>real</body></html>`}
	p, err := (&fetch.HTTPFetcher{UserAgent: "Mozilla/5.0", Renderer: r}).Fetch(context.Background(), srv.URL, "")
	if err != nil || !p.Rendered || !strings.Contains(p.HTML, "real") {
		t.Fatalf("challenge: %v %+v", err, p)
	}
}

func TestFetchErrors(t *testing.T) {
	srv := site(t, 500, "boom")
	_, err := (&fetch.HTTPFetcher{UserAgent: "Mozilla/5.0", Renderer: &fakeRenderer{}}).Fetch(context.Background(), srv.URL, "")
	var se *fetch.StatusError
	if !errors.As(err, &se) || se.Status != 500 {
		t.Fatalf("500: %v", err)
	}
	blocked := site(t, 403, "no")
	if _, err := (&fetch.HTTPFetcher{UserAgent: "Mozilla/5.0"}).Fetch(context.Background(), blocked.URL, ""); !errors.Is(err, fetch.ErrNoRenderer) {
		t.Fatalf("no renderer: %v", err)
	}
	if _, err := (&fetch.HTTPFetcher{UserAgent: "Mozilla/5.0", Renderer: &fakeRenderer{err: errors.New("chrome died")}}).Fetch(context.Background(), blocked.URL, ""); err == nil {
		t.Fatal("render error swallowed")
	}
	big := site(t, 200, strings.Repeat("x", 2048))
	if _, err := (&fetch.HTTPFetcher{UserAgent: "Mozilla/5.0", MaxBytes: 1024}).Fetch(context.Background(), big.URL, ""); err == nil {
		t.Fatal("oversize accepted")
	}
}

// port of java CloudflareDetectorTest.
func TestIsChallenge(t *testing.T) {
	cases := map[string]bool{
		`<title>Just a moment...</title>`:                     true,
		`<title>Attention Required! | Cloudflare</title>`:     true,
		`<title>Sorry, you have been blocked</title>`:         true,
		`<body><div class="cf_chl_prog"></div></body>`:        true,
		`<body><input name="jschl-answer"></body>`:            true,
		`<body><span class="cf-spinner"></span></body>`:       true,
		`<body><div id="cf-im-under-attack"></div></body>`:    true,
		`<body><form id="__cf_chl_jschl_tk__"></form></body>`: true,
		`<title>Celulares | Tienda</title><body>ok</body>`:    false,
	}
	for html, want := range cases {
		p, err := fetch.NewPage("https://x.pe", "<html>"+html+"</html>", false)
		if err != nil {
			t.Fatal(err)
		}
		if got := fetch.IsChallenge(p.Doc); got != want {
			t.Errorf("%q: got %v", html, got)
		}
	}
	for st, want := range map[int]bool{403: true, 429: true, 503: true, 200: false, 404: false} {
		if fetch.IsBlockedStatus(st) != want {
			t.Errorf("status %d", st)
		}
	}
}
