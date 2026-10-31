package sitesconfig_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/platform/db"
	"github.com/hneyra/crawler-go/internal/platform/dbtest"
	"github.com/hneyra/crawler-go/internal/postgres"
	"github.com/hneyra/crawler-go/internal/sitesconfig"
)

func TestParseExampleFile(t *testing.T) {
	fh, err := os.Open("../../sites-config.example.yml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fh.Close() }()
	f, err := sitesconfig.Parse(fh)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Sites) != 1 || len(f.Sites[0].Categories) != 1 {
		t.Fatalf("got %+v", f)
	}
	s := f.Sites[0].Site()
	if s.Name != "Adondevivir.com" || s.ExtractionConfig.DetailStrategy != crawl.StrategyScriptJSON || len(s.ExtractionConfig.ScriptPatterns) != 1 || s.ExtractionConfig.JSONPaths["price"] == "" {
		t.Fatalf("site %+v", s)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("example site invalid: %v", err)
	}
	c := f.Sites[0].Categories[0].Category(1)
	if c.LinkExtractionType != crawl.LinkDataAttribute || c.LinkAttribute != "data-to-posting" || c.NextPageSelector == "" {
		t.Fatalf("category %+v", c)
	}
	if err := c.Validate(); err != nil {
		t.Fatalf("example category invalid: %v", err)
	}
}

func TestParseDefaultsAndStrict(t *testing.T) {
	f, err := sitesconfig.Parse(strings.NewReader(`
sites:
  - name: a
    baseUrl: https://a.pe
    categories:
      - name: c
        url: https://a.pe/c
        itemLinkSelector: a.p
`))
	if err != nil {
		t.Fatal(err)
	}
	s, c := f.Sites[0].Site(), f.Sites[0].Categories[0].Category(1)
	if !s.Enabled || s.PolitenessDelayMs != 1000 || s.ExtractionConfig.DetailStrategy != crawl.StrategyHTML {
		t.Fatalf("site defaults %+v", s)
	}
	if !c.Enabled || c.PaginationType != crawl.PaginationPageParam || c.LinkExtractionType != crawl.LinkHref {
		t.Fatalf("category defaults %+v", c)
	}
	if _, err := sitesconfig.Parse(strings.NewReader("sites:\n  - name: a\n    baseURL: https://a.pe\n")); err == nil {
		t.Fatal("typo key accepted")
	}
	if _, err := sitesconfig.Parse(strings.NewReader("sites:\n  - name: a\n    categories:\n      - nombre: x\n")); err == nil {
		t.Fatal("category typo key accepted")
	}
	for name, y := range map[string]string{
		"dup site":     "sites:\n  - {name: a, baseUrl: https://a.pe}\n  - {name: \"a \", baseUrl: https://b.pe}\n",
		"dup category": "sites:\n  - name: a\n    categories:\n      - {name: c}\n      - {name: c}\n",
		"multi doc":    "sites: []\n---\nsites: []\n",
		"too big":      "sites: []\n" + strings.Repeat("#", 4<<20),
	} {
		if _, err := sitesconfig.Parse(strings.NewReader(y)); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if f, err := sitesconfig.Parse(strings.NewReader("")); err != nil || len(f.Sites) != 0 {
		t.Fatalf("empty: %v %+v", err, f)
	}
}

func TestSyncIsIdempotentUpsert(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	cat := crawl.Catalog{Tx: db.Runner{Pool: pool}, Sites: postgres.Sites{Pool: pool}, Categories: postgres.Categories{Pool: pool}}
	parse := func(y string) sitesconfig.File {
		f, err := sitesconfig.Parse(strings.NewReader(y))
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	v1 := parse(`
sites:
  - name: "a "
    baseUrl: https://a.pe
    categories:
      - {name: c1, url: "https://a.pe/1", itemLinkSelector: a}
      - {name: c2, url: "https://a.pe/2", itemLinkSelector: a}
`)
	res, err := sitesconfig.Sync(ctx, cat, v1)
	if err != nil || res != (sitesconfig.Result{SitesCreated: 1, CategoriesCreated: 2}) {
		t.Fatalf("first: %v %+v", err, res)
	}
	res, err = sitesconfig.Sync(ctx, cat, v1)
	if err != nil || res != (sitesconfig.Result{SitesUpdated: 1, CategoriesUpdated: 2}) {
		t.Fatalf("second: %v %+v", err, res)
	}
	v2 := parse(`
sites:
  - name: a
    baseUrl: https://a2.pe
    enabled: false
    categories:
      - {name: c1, url: "https://a.pe/1b", itemLinkSelector: div, paginationType: SCROLL_AJAX}
`)
	if _, err := sitesconfig.Sync(ctx, cat, v2); err != nil {
		t.Fatal(err)
	}
	s, _ := cat.Sites.GetSiteByName(ctx, "a")
	cats, _ := cat.Categories.ListCategories(ctx, s.ID)
	if s.BaseURL != "https://a2.pe" || s.Enabled || len(cats) != 2 || cats[0].URL != "https://a.pe/1b" || cats[0].PaginationType != crawl.PaginationScrollAJAX {
		t.Fatalf("after v2: %+v %+v", s, cats)
	}
	// invalid entry -> whole sync rolled back.
	bad := parse(`
sites:
  - name: new
    baseUrl: https://n.pe
  - name: broken
    baseUrl: not-a-url
`)
	if _, err := sitesconfig.Sync(ctx, cat, bad); err == nil || !strings.Contains(err.Error(), `site "broken"`) {
		t.Fatalf("bad sync: %v", err)
	}
	all, _ := cat.Sites.ListSites(ctx)
	if len(all) != 1 {
		t.Fatalf("rollback failed: %d sites", len(all))
	}
}
