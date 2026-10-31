package extraction_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/extraction"
	"github.com/hneyra/crawler-go/internal/fetch"
	"github.com/hneyra/crawler-go/internal/platform/dbtest"
	"github.com/hneyra/crawler-go/internal/postgres"
	"github.com/hneyra/crawler-go/internal/storage"
	"github.com/hneyra/crawler-go/internal/urlx"
)

type fakeRenderer struct {
	res   fetch.RenderResult
	err   error
	calls []fetch.RenderRequest
}

func (f *fakeRenderer) Render(_ context.Context, r fetch.RenderRequest) (fetch.RenderResult, error) {
	f.calls = append(f.calls, r)
	return f.res, f.err
}

func (f *fakeRenderer) ClickNavigate(context.Context, string, fetch.ClickTarget, time.Duration) (string, error) {
	return "", errors.New("unused")
}

type env struct {
	svc   *extraction.Service
	urls  postgres.URLs
	items postgres.Items
	site  crawl.Site
	cat   crawl.Category
	dir   string
	sleep []time.Duration
}

func newEnv(t *testing.T, cfg crawl.ExtractionConfig, r fetch.Renderer) *env {
	t.Helper()
	pool := dbtest.New(t)
	ctx := context.Background()
	site, err := postgres.Sites{Pool: pool}.CreateSite(ctx, crawl.Site{Name: "s", BaseURL: "https://s.pe", Enabled: true, PolitenessDelayMs: 250, ExtractionConfig: cfg})
	if err != nil {
		t.Fatal(err)
	}
	cat, err := postgres.Categories{Pool: pool}.CreateCategory(ctx, crawl.Category{SiteID: site.ID, Name: "c", URL: "https://s.pe/c", PaginationType: crawl.PaginationPageParam, ItemLinkSelector: "a", LinkExtractionType: crawl.LinkHref, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{urls: postgres.URLs{Pool: pool}, items: postgres.Items{Pool: pool}, site: site, cat: cat, dir: t.TempDir()}
	e.svc = &extraction.Service{
		Sites: postgres.Sites{Pool: pool}, Queue: e.urls, Items: e.items,
		Fetcher: &fetch.HTTPFetcher{UserAgent: "Mozilla/5.0"}, Renderer: r,
		Snapshots: storage.Raw{Dir: e.dir}, BatchSize: 10,
		Sleep: func(_ context.Context, d time.Duration) error { e.sleep = append(e.sleep, d); return nil },
	}
	return e
}

func (e *env) add(t *testing.T, u string) {
	t.Helper()
	if _, err := e.urls.InsertURL(context.Background(), crawl.DiscoveredURL{SiteID: e.site.ID, CategoryID: e.cat.ID, URL: u, URLHash: urlx.SHA256(u)}); err != nil {
		t.Fatal(err)
	}
}

func TestBatchExtractsAndMarks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			http.Error(w, "gone", 404)
			return
		}
		_, _ = fmt.Fprintf(w, `<html><head><title>T %s</title></head><body><h1>My Product</h1><span class="price">$9.99</span>%s</body></html>`, r.URL.Path, "\x00")
	}))
	defer srv.Close()
	e := newEnv(t, crawl.ExtractionConfig{DetailStrategy: crawl.StrategyHTML, FieldSelectors: map[string]string{"title": "h1", "price": "span.price"}}, nil)
	e.add(t, srv.URL+"/p/1")
	e.add(t, srv.URL+"/bad")
	e.add(t, srv.URL+"/p/2")
	ctx := context.Background()
	b, err := e.svc.ProcessBatch(ctx, e.site.ID)
	if err != nil || b.Claimed != 3 || b.Succeeded != 2 || b.Failed != 1 || b.LastError == "" {
		t.Fatalf("%v %+v", err, b)
	}
	if len(e.sleep) != 2 || e.sleep[0] != 250*time.Millisecond {
		t.Fatalf("politeness %v", e.sleep)
	}
	items, total, _ := e.items.ListItems(ctx, e.site.ID, 1, 10)
	if total != 2 || items[0].Title != "My Product" || items[0].Properties["price"] != "$9.99" {
		t.Fatalf("items %+v", items)
	}
	if _, err := os.Stat(filepath.Join(e.dir, filepath.FromSlash(items[0].RawSnapshotPath))); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	st, _ := e.urls.SiteStats(ctx, e.site.ID)
	if st.Completed != 2 || st.Failed != 1 || st.Pending != 0 {
		t.Fatalf("stats %+v", st)
	}
	failed, _ := e.urls.ListURLs(ctx, e.site.ID, crawl.URLFailed, 1)
	if failed[0].RetryCount != 1 || failed[0].LastError == "" {
		t.Fatalf("failed %+v", failed[0])
	}
	// nothing pending -> empty batch.
	if b, err := e.svc.ProcessBatch(ctx, e.site.ID); err != nil || b.Claimed != 0 {
		t.Fatalf("empty: %v %+v", err, b)
	}
}

func TestTitleFallsBackToDocumentTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `<html><head><title> Doc   Title </title></head></html>`)
	}))
	defer srv.Close()
	e := newEnv(t, crawl.DefaultExtractionConfig(), nil)
	e.add(t, srv.URL+"/x")
	if _, err := e.svc.ProcessBatch(context.Background(), e.site.ID); err != nil {
		t.Fatal(err)
	}
	items, _, _ := e.items.ListItems(context.Background(), e.site.ID, 1, 1)
	if items[0].Title != "Doc Title" {
		t.Fatalf("title %q", items[0].Title)
	}
}

func TestMissingSite(t *testing.T) {
	e := newEnv(t, crawl.DefaultExtractionConfig(), nil)
	if _, err := e.svc.ProcessBatch(context.Background(), 9999); !apperr.IsKind(err, apperr.KindNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestAJAXUsesRendererAndInterceptions(t *testing.T) {
	r := &fakeRenderer{res: fetch.RenderResult{
		HTML:        `<html><head><title>T</title></head><body><h1>Rendered</h1></body></html>`,
		Intercepted: []fetch.Intercepted{{URL: "https://api.s.pe/item/1", Status: 200, Body: `{"price":42}`}},
	}}
	e := newEnv(t, crawl.ExtractionConfig{DetailStrategy: crawl.StrategyAJAX, FieldSelectors: map[string]string{"title": "h1"}, JSONPaths: map[string]string{"price": "$.price"}, InterceptPatterns: []string{"*/item/*"}}, r)
	e.add(t, "https://s.pe/p/1")
	b, err := e.svc.ProcessBatch(context.Background(), e.site.ID)
	if err != nil || b.Succeeded != 1 || len(r.calls) != 1 || r.calls[0].Intercept[0] != "*/item/*" {
		t.Fatalf("%v %+v %+v", err, b, r.calls)
	}
	items, _, _ := e.items.ListItems(context.Background(), e.site.ID, 1, 1)
	p := items[0].Properties
	if items[0].Title != "Rendered" || p["price"] != float64(42) || p["ajax_200_https___api_s_pe_item_1"] == nil {
		t.Fatalf("%+v", items[0])
	}
	// html + json snapshots on disk.
	n := 0
	_ = filepath.Walk(e.dir, func(_ string, fi os.FileInfo, _ error) error {
		if fi != nil && !fi.IsDir() {
			n++
		}
		return nil
	})
	if n != 2 {
		t.Fatalf("snapshots %d", n)
	}
	// render failure -> url FAILED.
	r.err = errors.New("chrome died")
	e.add(t, "https://s.pe/p/2")
	if b, err := e.svc.ProcessBatch(context.Background(), e.site.ID); err != nil || b.Failed != 1 {
		t.Fatalf("render fail: %v %+v", err, b)
	}
}

// hostile content must not wedge the queue: +Inf numbers, latin-1 title, NUL.
func TestHostileContentIsStoredOrFailsURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/inf":
			_, _ = fmt.Fprint(w, `<title>x</title><script type="application/ld+json">{"big":1e400,"ok":"y"}</script>`)
		case "/latin1":
			w.Header().Set("Content-Type", "text/html; charset=iso-8859-1")
			_, _ = w.Write([]byte("<title>Caf\xe9</title>"))
		case "/rawbytes":
			_, _ = w.Write([]byte("<html><head><meta charset=\"utf-8\"><title>bad \xff\xfe</title></head></html>"))
		}
	}))
	defer srv.Close()
	e := newEnv(t, crawl.DefaultExtractionConfig(), nil)
	for _, p := range []string{"/inf", "/latin1", "/rawbytes"} {
		e.add(t, srv.URL+p)
	}
	b, err := e.svc.ProcessBatch(context.Background(), e.site.ID)
	if err != nil || b.Succeeded != 3 {
		t.Fatalf("%v %+v", err, b)
	}
	items, _, _ := e.items.ListItems(context.Background(), e.site.ID, 1, 10)
	titles := map[string]bool{}
	for _, it := range items {
		titles[it.Title] = true
		if ld, ok := it.Properties["jsonLd"].(map[string]any); ok && (ld["big"] != nil || ld["ok"] != "y") {
			t.Fatalf("inf not nulled: %v", ld)
		}
	}
	if !titles["Café"] {
		t.Fatalf("latin-1 not decoded: %v", titles)
	}
}

type deadRenderer struct{}

func (deadRenderer) Render(context.Context, fetch.RenderRequest) (fetch.RenderResult, error) {
	return fetch.RenderResult{}, fmt.Errorf("%w: no chrome", fetch.ErrRendererUnavailable)
}

func (deadRenderer) ClickNavigate(context.Context, string, fetch.ClickTarget, time.Duration) (string, error) {
	return "", fetch.ErrRendererUnavailable
}

// browser launch failure = ours: urls back to PENDING, retries untouched.
func TestRendererUnavailableKeepsRetries(t *testing.T) {
	e := newEnv(t, crawl.ExtractionConfig{DetailStrategy: crawl.StrategyAJAX}, deadRenderer{})
	e.add(t, "https://s.pe/p/1")
	e.add(t, "https://s.pe/p/2")
	if _, err := e.svc.ProcessBatch(context.Background(), e.site.ID); !errors.Is(err, fetch.ErrRendererUnavailable) {
		t.Fatalf("got %v", err)
	}
	st, _ := e.urls.SiteStats(context.Background(), e.site.ID)
	if st.Pending != 2 || st.Failed != 0 {
		t.Fatalf("stats %+v", st)
	}
}

// ctx cancelled right after a url succeeded: still marked COMPLETED, not left IN_PROGRESS.
func TestCompletedMarkedDespiteShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, "<title>x</title>")
	}))
	defer srv.Close()
	e := newEnv(t, crawl.DefaultExtractionConfig(), nil)
	e.add(t, srv.URL+"/p/1")
	e.svc.Items = cancelAfter{ItemSink: e.items, cancel: cancel}
	if _, err := e.svc.ProcessBatch(ctx, e.site.ID); err != nil {
		t.Fatal(err)
	}
	st, _ := e.urls.SiteStats(context.Background(), e.site.ID)
	if st.Completed != 1 || st.InProgress != 0 {
		t.Fatalf("stats %+v", st)
	}
}

type cancelAfter struct {
	extraction.ItemSink
	cancel func()
}

func (c cancelAfter) UpsertItem(ctx context.Context, it crawl.Item) (crawl.Item, error) {
	out, err := c.ItemSink.UpsertItem(ctx, it)
	c.cancel()
	return out, err
}

func TestCancelReleasesClaimed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "<title>x</title>") }))
	defer srv.Close()
	e := newEnv(t, crawl.DefaultExtractionConfig(), nil)
	for i := range 3 {
		e.add(t, fmt.Sprintf("%s/p/%d", srv.URL, i))
	}
	ctx, cancel := context.WithCancel(context.Background())
	e.svc.Sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	b, err := e.svc.ProcessBatch(ctx, e.site.ID)
	if !errors.Is(err, context.Canceled) || b.Succeeded != 1 {
		t.Fatalf("%v %+v", err, b)
	}
	st, _ := e.urls.SiteStats(context.Background(), e.site.ID)
	if st.Completed != 1 || st.Pending != 2 || st.InProgress != 0 || st.Failed != 0 {
		t.Fatalf("stats %+v", st)
	}
}

func TestStorageFailureAbortsWithoutBlamingURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "<title>x</title>") }))
	defer srv.Close()
	e := newEnv(t, crawl.DefaultExtractionConfig(), nil)
	f := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(f, []byte("x"), 0o600)
	e.svc.Snapshots = storage.Raw{Dir: f}
	e.add(t, srv.URL+"/p/1")
	if _, err := e.svc.ProcessBatch(context.Background(), e.site.ID); err == nil {
		t.Fatal("want error")
	}
	st, _ := e.urls.SiteStats(context.Background(), e.site.ID)
	if st.Pending != 1 || st.Failed != 0 {
		t.Fatalf("stats %+v", st)
	}
}
