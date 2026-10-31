package postgres_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/platform/db"
	"github.com/hneyra/crawler-go/internal/platform/dbtest"
	"github.com/hneyra/crawler-go/internal/postgres"
)

type fixture struct {
	pool  *pgxpool.Pool
	sites postgres.Sites
	cats  postgres.Categories
	urls  postgres.URLs
	items postgres.Items
}

func newFixture(t *testing.T) fixture {
	pool := dbtest.New(t)
	return fixture{pool, postgres.Sites{Pool: pool}, postgres.Categories{Pool: pool}, postgres.URLs{Pool: pool}, postgres.Items{Pool: pool}}
}

func (f fixture) site(t *testing.T, name string) crawl.Site {
	t.Helper()
	cfg := crawl.DefaultExtractionConfig()
	cfg.FieldSelectors["title"] = "h1"
	s, err := f.sites.CreateSite(context.Background(), crawl.Site{Name: name, BaseURL: "https://" + name + ".test", Enabled: true, PolitenessDelayMs: 0, ExtractionConfig: cfg})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (f fixture) category(t *testing.T, siteID int64, name string, enabled bool) crawl.Category {
	t.Helper()
	c, err := f.cats.CreateCategory(context.Background(), crawl.Category{
		SiteID: siteID, Name: name, URL: "https://x.test/" + name, PaginationType: crawl.PaginationPageParam,
		ItemLinkSelector: "a.item", LinkExtractionType: crawl.LinkHref, Enabled: enabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f fixture) url(t *testing.T, c crawl.Category, n int) crawl.DiscoveredURL {
	t.Helper()
	ctx := context.Background()
	hash := fmt.Sprintf("%064d", n)
	ok, err := f.urls.InsertURL(ctx, crawl.DiscoveredURL{SiteID: c.SiteID, CategoryID: c.ID, URL: fmt.Sprintf("https://x.test/p/%d", n), URLHash: hash})
	if err != nil || !ok {
		t.Fatalf("insert url %d: %v %v", n, ok, err)
	}
	u, err := f.urls.GetURLByHash(ctx, hash)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSitesCRUD(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.site(t, "tienda")
	if s.ID == 0 || s.ExtractionConfig.FieldSelectors["title"] != "h1" || s.ExtractionConfig.DetailStrategy != crawl.StrategyHTML {
		t.Fatalf("created %+v", s)
	}
	got, err := f.sites.GetSiteByName(ctx, "tienda")
	if err != nil || got.ID != s.ID {
		t.Fatalf("by name: %v %+v", err, got)
	}
	if _, err := f.sites.CreateSite(ctx, crawl.Site{Name: "tienda", BaseURL: "https://y.test"}); !apperr.IsKind(err, apperr.KindConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	s.Enabled = false
	s.ExtractionConfig.DetailStrategy = crawl.StrategyScriptJSON
	up, err := f.sites.UpdateSite(ctx, s)
	if err != nil || up.Enabled || up.ExtractionConfig.DetailStrategy != crawl.StrategyScriptJSON || !up.UpdatedAt.After(s.CreatedAt.Add(-time.Second)) {
		t.Fatalf("update: %v %+v", err, up)
	}
	if err := f.sites.DeleteSite(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sites.GetSite(ctx, s.ID); !apperr.IsKind(err, apperr.KindNotFound) {
		t.Fatalf("after delete: %v", err)
	}
	if err := f.sites.DeleteSite(ctx, s.ID); !apperr.IsKind(err, apperr.KindNotFound) {
		t.Fatalf("delete missing: %v", err)
	}
}

func TestCategoriesEnabledAndCascade(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.site(t, "a")
	on := f.category(t, s.ID, "on", true)
	f.category(t, s.ID, "off", false)
	all, _ := f.cats.ListCategories(ctx, s.ID)
	enabled, _ := f.cats.EnabledCategories(ctx, s.ID)
	if len(all) != 2 || len(enabled) != 1 || enabled[0].ID != on.ID {
		t.Fatalf("all=%d enabled=%+v", len(all), enabled)
	}
	if _, err := f.cats.CreateCategory(ctx, crawl.Category{SiteID: s.ID, Name: "on", URL: "https://x.test", PaginationType: crawl.PaginationCursor, ItemLinkSelector: "a", LinkExtractionType: crawl.LinkHref}); !apperr.IsKind(err, apperr.KindConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := f.cats.CreateCategory(ctx, crawl.Category{SiteID: 9999, Name: "x", URL: "https://x.test", PaginationType: crawl.PaginationCursor, ItemLinkSelector: "a", LinkExtractionType: crawl.LinkHref}); !apperr.IsKind(err, apperr.KindNotFound) {
		t.Fatalf("missing site: %v", err)
	}
	f.url(t, on, 1)
	if err := f.sites.DeleteSite(ctx, s.ID); err != nil {
		t.Fatalf("cascade delete: %v", err)
	}
	var n int
	_ = f.pool.QueryRow(ctx, `SELECT count(*) FROM discovered_url`).Scan(&n)
	if n != 0 {
		t.Fatalf("urls left: %d", n)
	}
}

func TestURLsDedupeStatsAndRetry(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.site(t, "a")
	c := f.category(t, s.ID, "c", true)
	u1 := f.url(t, c, 1)
	f.url(t, c, 2)
	f.url(t, c, 3)
	if ok, err := f.urls.InsertURL(ctx, crawl.DiscoveredURL{SiteID: s.ID, CategoryID: c.ID, URL: "dup", URLHash: u1.URLHash}); err != nil || ok {
		t.Fatalf("dup insert: %v %v", ok, err)
	}
	if u1.Status != crawl.URLPending || u1.RetryCount != 0 {
		t.Fatalf("defaults %+v", u1)
	}
	// 2 failed: one below budget, one at budget. 3 completed.
	exec := func(q string, args ...any) {
		if _, err := f.pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE discovered_url SET status='FAILED', retry_count=1, last_attempt_at=now() - interval '1 hour' WHERE url_hash=$1`, fmt.Sprintf("%064d", 1))
	exec(`UPDATE discovered_url SET status='FAILED', retry_count=3, last_attempt_at=now() WHERE url_hash=$1`, fmt.Sprintf("%064d", 2))
	exec(`UPDATE discovered_url SET status='COMPLETED', last_attempt_at=now() - interval '2 hour' WHERE url_hash=$1`, fmt.Sprintf("%064d", 3))

	st, err := f.urls.SiteStats(ctx, s.ID)
	if err != nil || st.Total != 3 || st.Failed != 2 || st.Completed != 1 || st.Pending != 0 || st.LastCrawl == nil || st.SiteName != "a" {
		t.Fatalf("stats %v %+v", err, st)
	}
	n, err := f.urls.ResetFailed(ctx, 3)
	if err != nil || n != 1 {
		t.Fatalf("reset: %d %v", n, err)
	}
	pending, _ := f.urls.ListURLs(ctx, s.ID, crawl.URLPending, 10)
	if len(pending) != 1 || pending[0].ID != u1.ID || pending[0].RetryCount != 1 {
		t.Fatalf("pending after reset: %+v", pending)
	}
	limited, _ := f.urls.ListURLs(ctx, s.ID, crawl.URLFailed, 0)
	if len(limited) != 0 {
		t.Fatalf("limit 0: %d", len(limited))
	}
	// url site must match its category's site.
	other := f.site(t, "other")
	if _, err := f.urls.InsertURL(ctx, crawl.DiscoveredURL{SiteID: other.ID, CategoryID: c.ID, URL: "x", URLHash: fmt.Sprintf("%064d", 99)}); err == nil {
		t.Fatal("cross-site url accepted")
	}
	if items, total, err := f.items.ListItems(ctx, 1<<40, 1, 10); err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("big site id: %v %d", err, total)
	}
	if _, err := f.urls.SiteStats(ctx, 9999); !apperr.IsKind(err, apperr.KindNotFound) {
		t.Fatalf("stats missing site: %v", err)
	}
	empty := f.site(t, "empty")
	st, err = f.urls.SiteStats(ctx, empty.ID)
	if err != nil || st.Total != 0 || st.LastCrawl != nil {
		t.Fatalf("empty stats: %v %+v", err, st)
	}
}

func TestItemsUpsertAndOrdering(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, b := f.site(t, "a"), f.site(t, "b")
	ca, cb := f.category(t, a.ID, "c", true), f.category(t, b.ID, "c", true)
	ua1, ua2, ub := f.url(t, ca, 1), f.url(t, ca, 2), f.url(t, cb, 3)

	first, err := f.items.UpsertItem(ctx, crawl.Item{DiscoveredURLID: ua1.ID, SiteID: a.ID, Title: "old", Properties: map[string]any{"price": "1"}})
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.items.UpsertItem(ctx, crawl.Item{DiscoveredURLID: ua1.ID, SiteID: a.ID, Title: "new", Properties: map[string]any{"price": "2", "n": 3.0}, RawSnapshotPath: "1/2026-01-01/x.html"})
	if err != nil || again.ID != first.ID || again.Title != "new" || again.Properties["price"] != "2" || again.URL != ua1.URL || again.SiteName != "a" {
		t.Fatalf("upsert: %v %+v", err, again)
	}
	if _, err := f.items.UpsertItem(ctx, crawl.Item{DiscoveredURLID: ub.ID, SiteID: b.ID, Title: "b"}); err != nil {
		t.Fatal(err)
	}
	last, err := f.items.UpsertItem(ctx, crawl.Item{DiscoveredURLID: ua2.ID, SiteID: a.ID, Title: "a2"})
	if err != nil {
		t.Fatal(err)
	}
	items, total, err := f.items.ListItems(ctx, a.ID, 1, 10)
	if err != nil || total != 2 || len(items) != 2 || items[0].ID != last.ID {
		t.Fatalf("site a: %v total=%d %+v", err, total, items)
	}
	all, total, _ := f.items.ListItems(ctx, 0, 1, 2)
	if total != 3 || len(all) != 2 {
		t.Fatalf("all page1: total=%d len=%d", total, len(all))
	}
	page2, _, _ := f.items.ListItems(ctx, 0, 2, 2)
	if len(page2) != 1 {
		t.Fatalf("page2: %d", len(page2))
	}
	st, _ := f.urls.SiteStats(ctx, a.ID)
	if st.ExtractedItems != 2 {
		t.Fatalf("items count: %d", st.ExtractedItems)
	}
	if _, err := f.items.GetItem(ctx, 9999); !apperr.IsKind(err, apperr.KindNotFound) {
		t.Fatalf("missing item: %v", err)
	}
}

// concurrent patches of different fields both survive (row lock, no lost update).
func TestConcurrentSitePatchesNoLostUpdate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.site(t, "a")
	cat := crawl.Catalog{Tx: db.Runner{Pool: f.pool}, Sites: f.sites, Categories: f.cats}
	off, delay := false, 5000
	errs := make(chan error, 2)
	go func() { _, err := cat.UpdateSite(ctx, s.ID, crawl.SitePatch{Enabled: &off}); errs <- err }()
	go func() { _, err := cat.UpdateSite(ctx, s.ID, crawl.SitePatch{PolitenessDelayMs: &delay}); errs <- err }()
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	got, _ := f.sites.GetSite(ctx, s.ID)
	if got.Enabled || got.PolitenessDelayMs != 5000 {
		t.Fatalf("lost update: %+v", got)
	}
}

func TestURLQueueClaimFailRelease(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.site(t, "a")
	c := f.category(t, s.ID, "c", true)
	for i := 1; i <= 5; i++ {
		f.url(t, c, i)
	}
	// two concurrent claimers never share a url.
	type res struct {
		urls []crawl.DiscoveredURL
		err  error
	}
	ch := make(chan res, 2)
	for range 2 {
		go func() { u, err := f.urls.ClaimPending(ctx, s.ID, 3); ch <- res{u, err} }()
	}
	seen := map[int64]bool{}
	total := 0
	for range 2 {
		r := <-ch
		if r.err != nil {
			t.Fatal(r.err)
		}
		for _, u := range r.urls {
			if seen[u.ID] || u.Status != crawl.URLInProgress || u.LastAttemptAt == nil {
				t.Fatalf("bad claim %+v", u)
			}
			seen[u.ID] = true
			total++
		}
	}
	if total != 5 {
		t.Fatalf("claimed %d", total)
	}
	if n, _ := f.urls.CountPending(ctx, s.ID); n != 0 {
		t.Fatalf("pending %d", n)
	}
	ids := []int64{}
	for id := range seen {
		ids = append(ids, id)
	}
	if err := f.urls.MarkCompleted(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := f.urls.MarkFailed(ctx, ids[1], strings.Repeat("é", 2000)+"\x00"); err != nil {
		t.Fatal(err)
	}
	if err := f.urls.Release(ctx, ids[2:]); err != nil {
		t.Fatal(err)
	}
	st, _ := f.urls.SiteStats(ctx, s.ID)
	if st.Completed != 1 || st.Failed != 1 || st.Pending != 3 || st.InProgress != 0 {
		t.Fatalf("stats %+v", st)
	}
	failed, _ := f.urls.ListURLs(ctx, s.ID, crawl.URLFailed, 1)
	if failed[0].RetryCount != 1 || len(failed[0].LastError) > 1000 || failed[0].LastError == "" {
		t.Fatalf("failed row: retry=%d errlen=%d", failed[0].RetryCount, len(failed[0].LastError))
	}
	// released keep retry budget; completed not re-claimed.
	again, _ := f.urls.ClaimPending(ctx, s.ID, 10)
	if len(again) != 3 || again[0].ID > again[1].ID {
		t.Fatalf("reclaim %+v", again)
	}
}
