package discovery_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/discovery"
	"github.com/hneyra/crawler-go/internal/fetch"
	"github.com/hneyra/crawler-go/internal/urlx"
)

// memSink = InsertURL with hash dedupe, like db unique index.
type memSink struct {
	mu   sync.Mutex
	urls map[string]crawl.DiscoveredURL
	err  error
}

func (m *memSink) InsertURL(_ context.Context, u crawl.DiscoveredURL) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	if m.urls == nil {
		m.urls = map[string]crawl.DiscoveredURL{}
	}
	if _, ok := m.urls[u.URLHash]; ok {
		return false, nil
	}
	m.urls[u.URLHash] = u
	return true, nil
}

func (m *memSink) has(u string) bool { _, ok := m.urls[urlx.SHA256(u)]; return ok }

type fakeRenderer struct {
	html    string
	err     error
	clickTo string
	renders []fetch.RenderRequest
	clicks  []fetch.ClickTarget
}

func (f *fakeRenderer) Render(_ context.Context, r fetch.RenderRequest) (fetch.RenderResult, error) {
	f.renders = append(f.renders, r)
	return fetch.RenderResult{HTML: f.html}, f.err
}

func (f *fakeRenderer) ClickNavigate(_ context.Context, _ string, t fetch.ClickTarget, _ time.Duration) (string, error) {
	f.clicks = append(f.clicks, t)
	if f.clickTo == "" {
		return "", errors.New("no nav")
	}
	return f.clickTo, nil
}

func pages(t *testing.T, m map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := m[r.URL.RequestURI()]
		if !ok {
			http.Error(w, "boom", 500)
			return
		}
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func svc(r fetch.Renderer, sink *memSink) (*discovery.Service, *[]time.Duration) {
	var slept []time.Duration
	return &discovery.Service{
		Fetcher:  &fetch.HTTPFetcher{UserAgent: "Mozilla/5.0 test", Renderer: r},
		Renderer: r,
		URLs:     sink,
		Sleep:    func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil },
	}, &slept
}

var site = crawl.Site{ID: 1, PolitenessDelayMs: 700}

func cat(url string) crawl.Category {
	return crawl.Category{ID: 2, SiteID: 1, URL: url, PaginationType: crawl.PaginationPageParam, ItemLinkSelector: "a.item", LinkExtractionType: crawl.LinkHref}
}

func TestSinglePage(t *testing.T) {
	srv := pages(t, map[string]string{"/list": `<a class="item" href="/p/1">1</a><a class="item" href="/p/2#top">2</a><a class="other" href="/x">x</a>`})
	sink := &memSink{}
	s, slept := svc(nil, sink)
	res, err := s.DiscoverCategory(context.Background(), site, cat(srv.URL+"/list"))
	if err != nil || res != (discovery.Result{Pages: 1, New: 2}) || len(*slept) != 0 {
		t.Fatalf("%v %+v slept=%v", err, res, *slept)
	}
	// fragment stripped, hash of absolute normalized url.
	if !sink.has(srv.URL+"/p/2") || !sink.has(srv.URL+"/p/1") {
		t.Fatalf("stored: %+v", sink.urls)
	}
	for _, u := range sink.urls {
		if u.SiteID != 1 || u.CategoryID != 2 || u.URLHash != urlx.SHA256(u.URL) {
			t.Fatalf("row: %+v", u)
		}
	}
}

func TestFollowsRelNextAndDedupes(t *testing.T) {
	srv := pages(t, map[string]string{
		"/list":        `<a class="item" href="/p/1">1</a><a class="item" href="/p/2">2</a><a rel="next" href="/list?page=2">»</a>`,
		"/list?page=2": `<a class="item" href="/p/2">2</a><a class="item" href="/p/3">3</a>`,
	})
	sink := &memSink{}
	s, slept := svc(nil, sink)
	res, err := s.DiscoverCategory(context.Background(), site, cat(srv.URL+"/list"))
	if err != nil || res != (discovery.Result{Pages: 2, New: 3, Duplicates: 1}) {
		t.Fatalf("%v %+v", err, res)
	}
	if len(*slept) != 1 || (*slept)[0] != 700*time.Millisecond {
		t.Fatalf("politeness: %v", *slept)
	}
}

func TestNextByTextAndCustomSelector(t *testing.T) {
	srv := pages(t, map[string]string{
		"/a": `<a class="item" href="/p/1">1</a><a href="/b"> Siguiente </a>`,
		"/b": `<a class="item" href="/p/2">2</a><a class="custom-next" href="/c">más</a><a href="/zzz">Next</a>`,
		"/c": `<a class="item" href="/p/3">3</a>`,
	})
	sink := &memSink{}
	s, _ := svc(nil, sink)
	res, _ := s.DiscoverCategory(context.Background(), site, cat(srv.URL+"/a"))
	// text "Siguiente" -> /b; then "Next" -> /zzz (500) -> stop with error.
	if res.Pages != 3 || res.New != 2 || res.Errors != 1 || res.LastError == "" {
		t.Fatalf("auto next: %+v", res)
	}
	c := cat(srv.URL + "/b")
	c.NextPageSelector = "a.custom-next"
	res, _ = s.DiscoverCategory(context.Background(), site, c)
	if res.Pages != 2 || res.Errors != 0 || !sink.has(srv.URL+"/p/3") {
		t.Fatalf("custom next: %+v", res)
	}
}

func TestServerErrorStoresNothing(t *testing.T) {
	srv := pages(t, map[string]string{})
	sink := &memSink{}
	s, _ := svc(nil, sink)
	res, err := s.DiscoverCategory(context.Background(), site, cat(srv.URL+"/list"))
	if err != nil || res.Errors != 1 || res.New != 0 || len(sink.urls) != 0 {
		t.Fatalf("%v %+v", err, res)
	}
}

func TestMaxPagesCap(t *testing.T) {
	m := map[string]string{}
	for i := range 10 {
		m[fmt.Sprintf("/l?p=%d", i)] = fmt.Sprintf(`<a class="item" href="/p/%d">x</a><a rel="next" href="/l?p=%d">n</a>`, i, i+1)
	}
	srv := pages(t, m)
	s, _ := svc(nil, &memSink{})
	s.MaxPages = 3
	res, _ := s.DiscoverCategory(context.Background(), site, cat(srv.URL+"/l?p=0"))
	if res.Pages != 3 || res.New != 3 {
		t.Fatalf("%+v", res)
	}
}

func TestBlockedPageUsesRenderer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer srv.Close()
	r := &fakeRenderer{html: `<a class="item" href="/p/9">9</a>`}
	sink := &memSink{}
	s, _ := svc(r, sink)
	res, err := s.DiscoverCategory(context.Background(), site, cat(srv.URL+"/list"))
	if err != nil || res.New != 1 || len(r.renders) != 1 || r.renders[0].WaitForSelector != "a.item" || !sink.has(srv.URL+"/p/9") {
		t.Fatalf("%v %+v %+v", err, res, r.renders)
	}
}

func TestJSPaginationClicks(t *testing.T) {
	srv := pages(t, map[string]string{
		"/list":        `<a class="item" href="/p/1">1</a><a href="#">Siguiente</a>`,
		"/list?page=2": `<a class="item" href="/p/2">2</a>`,
	})
	r := &fakeRenderer{}
	s, _ := svc(r, &memSink{})
	r.clickTo = srv.URL + "/list?page=2"
	res, _ := s.DiscoverCategory(context.Background(), site, cat(srv.URL+"/list"))
	if res.Pages != 2 || res.New != 2 || len(r.clicks) != 1 || r.clicks[0].Text != "Siguiente" {
		t.Fatalf("%+v %+v", res, r.clicks)
	}
}

func TestScrollAJAX(t *testing.T) {
	r := &fakeRenderer{html: `<a class="item" href="/p/1#x">1</a><a class="item" href="https://shop.pe/p/2">2</a><a class="item" href="/p/1">dup</a>`}
	sink := &memSink{}
	s, _ := svc(r, sink)
	s.MaxPages = 7
	c := cat("https://shop.pe/list")
	c.PaginationType = crawl.PaginationScrollAJAX
	res, err := s.DiscoverCategory(context.Background(), site, c)
	if err != nil || res.New != 2 || res.Duplicates != 1 || !r.renders[0].ScrollToBottom || r.renders[0].MaxScrolls != 7 {
		t.Fatalf("%v %+v %+v", err, res, r.renders)
	}
	if !sink.has("https://shop.pe/p/1") || !sink.has("https://shop.pe/p/2") {
		t.Fatalf("stored %+v", sink.urls)
	}
	r.err = errors.New("chrome gone")
	res, err = s.DiscoverCategory(context.Background(), site, c)
	if err != nil || res.Errors != 1 {
		t.Fatalf("render fail: %v %+v", err, res)
	}
}

func TestStorageErrorIsReturned(t *testing.T) {
	srv := pages(t, map[string]string{"/list": `<a class="item" href="/p/1">1</a>`})
	s, _ := svc(nil, &memSink{err: errors.New("db down")})
	if _, err := s.DiscoverCategory(context.Background(), site, cat(srv.URL+"/list")); err == nil || !strings.Contains(err.Error(), "db down") {
		t.Fatalf("got %v", err)
	}
}

func TestRelativeLinksUseRedirectedBase(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/alquiler", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/alquiler/", http.StatusFound) })
	mux.HandleFunc("/alquiler/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<a class="item" href="p/1">x</a>`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	sink := &memSink{}
	s, _ := svc(nil, sink)
	if _, err := s.DiscoverCategory(context.Background(), site, cat(srv.URL+"/alquiler")); err != nil {
		t.Fatal(err)
	}
	if !sink.has(srv.URL + "/alquiler/p/1") {
		t.Fatalf("stored %+v", sink.urls)
	}
}

func TestFragmentOnlyClickStops(t *testing.T) {
	srv := pages(t, map[string]string{"/list": `<a class="item" href="/p/1">1</a><a href="#">Siguiente</a>`})
	r := &fakeRenderer{}
	s, _ := svc(r, &memSink{})
	r.clickTo = srv.URL + "/list#page2"
	res, _ := s.DiscoverCategory(context.Background(), site, cat(srv.URL+"/list"))
	if res.Pages != 1 || len(r.clicks) != 1 {
		t.Fatalf("looped: %+v clicks=%d", res, len(r.clicks))
	}
}
