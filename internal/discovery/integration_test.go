package discovery_test

import (
	"context"
	"testing"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/discovery"
	"github.com/hneyra/crawler-go/internal/fetch"
	"github.com/hneyra/crawler-go/internal/platform/dbtest"
	"github.com/hneyra/crawler-go/internal/postgres"
	"github.com/hneyra/crawler-go/internal/urlx"
)

// port of java ListDiscoveryIntegrationTest: real db, rerun = all duplicates.
func TestDiscoveryIntoPostgres(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	srv := pages(t, map[string]string{
		"/list":     `<div data-to-posting="/prop/1.html"></div><div data-to-posting="/prop/2.html"></div><a data-qa="PAGING_NEXT" href="/list?p=2">›</a>`,
		"/list?p=2": `<div data-to-posting="/prop/3.html"></div>`,
	})
	site, err := postgres.Sites{Pool: pool}.CreateSite(ctx, crawl.Site{Name: "adv", BaseURL: srv.URL, Enabled: true, ExtractionConfig: crawl.DefaultExtractionConfig()})
	if err != nil {
		t.Fatal(err)
	}
	cat, err := postgres.Categories{Pool: pool}.CreateCategory(ctx, crawl.Category{
		SiteID: site.ID, Name: "alquiler", URL: srv.URL + "/list", PaginationType: crawl.PaginationPageParam,
		ItemLinkSelector: "div[data-to-posting]", NextPageSelector: "a[data-qa=PAGING_NEXT]",
		LinkExtractionType: crawl.LinkDataAttribute, LinkAttribute: "data-to-posting", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	urls := postgres.URLs{Pool: pool}
	s := &discovery.Service{Fetcher: &fetch.HTTPFetcher{UserAgent: "Mozilla/5.0"}, URLs: urls}
	res, err := s.DiscoverCategory(ctx, site, cat)
	if err != nil || res.New != 3 || res.Pages != 2 {
		t.Fatalf("first: %v %+v", err, res)
	}
	u, err := urls.GetURLByHash(ctx, urlx.SHA256(srv.URL+"/prop/3.html"))
	if err != nil || u.Status != crawl.URLPending || u.CategoryID != cat.ID || u.SiteID != site.ID {
		t.Fatalf("row: %v %+v", err, u)
	}
	res, err = s.DiscoverCategory(ctx, site, cat)
	if err != nil || res.New != 0 || res.Duplicates != 3 {
		t.Fatalf("rerun: %v %+v", err, res)
	}
	st, _ := urls.SiteStats(ctx, site.ID)
	if st.Total != 3 || st.Pending != 3 {
		t.Fatalf("stats %+v", st)
	}
}
