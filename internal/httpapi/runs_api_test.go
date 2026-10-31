package httpapi_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/httpapi"
	"github.com/hneyra/crawler-go/internal/postgres"
	"github.com/hneyra/crawler-go/internal/urlx"
)

func TestRunsAndItemsOverHTTP(t *testing.T) {
	env := newEnv(t)
	c := env.login(t, "admin@crawler.test", true)
	var site httpapi.Site
	c.do("POST", "/api/v1/sites", map[string]any{"name": "s", "base_url": "https://s.pe"}, &site)
	var cat httpapi.Category
	c.do("POST", fmt.Sprintf("/api/v1/sites/%d/categories", site.Id), map[string]any{"name": "c", "url": "https://s.pe/c", "pagination_type": "PAGE_PARAM", "item_link_selector": "a"}, &cat)

	var run httpapi.Run
	if st := c.do("POST", fmt.Sprintf("/api/v1/sites/%d/crawl", site.Id), nil, &run); st != 202 || run.Status != httpapi.QUEUED || run.Kind != httpapi.CRAWLSITE {
		t.Fatalf("crawl: %d %+v", st, run)
	}
	var again httpapi.Run
	if st := c.do("POST", fmt.Sprintf("/api/v1/sites/%d/crawl", site.Id), nil, &again); st != 202 || again.Id != run.Id {
		t.Fatalf("dedupe: %d %+v", st, again)
	}
	var cr httpapi.Run
	if st := c.do("POST", fmt.Sprintf("/api/v1/categories/%d/discover", cat.Id), nil, &cr); st != 202 || cr.CategoryId == nil || *cr.CategoryId != cat.Id {
		t.Fatalf("discover category: %d %+v", st, cr)
	}
	for _, p := range []string{"discover", "extract"} {
		if st := c.do("POST", fmt.Sprintf("/api/v1/sites/%d/%s", site.Id, p), nil, nil); st != 202 {
			t.Fatalf("%s: %d", p, st)
		}
	}
	if st := c.do("POST", "/api/v1/sites/999/crawl", nil, nil); st != 404 {
		t.Fatalf("missing site: %d", st)
	}
	var list httpapi.RunList
	if st := c.do("GET", fmt.Sprintf("/api/v1/runs?site_id=%d&per_page=2", site.Id), nil, &list); st != 200 || list.Total != 4 || len(list.Items) != 2 || list.PerPage != 2 {
		t.Fatalf("runs: %d %+v", st, list)
	}
	if st := c.do("GET", fmt.Sprintf("/api/v1/runs/%d", run.Id), nil, nil); st != 200 {
		t.Fatalf("get run: %d", st)
	}
	if st := c.do("GET", "/api/v1/runs?per_page=500", nil, nil); st != 400 {
		t.Fatalf("per_page cap: %d", st)
	}

	// seed one url + item directly, then read via api.
	ctx := context.Background()
	urls := postgres.URLs{Pool: env.pool}
	u := "https://s.pe/p/1"
	_, _ = urls.InsertURL(ctx, crawl.DiscoveredURL{SiteID: site.Id, CategoryID: cat.Id, URL: u, URLHash: urlx.SHA256(u)})
	du, _ := urls.GetURLByHash(ctx, urlx.SHA256(u))
	_, _ = postgres.Items{Pool: env.pool}.UpsertItem(ctx, crawl.Item{DiscoveredURLID: du.ID, SiteID: site.Id, Title: "P1", Properties: map[string]any{"price": "9.90", "nested": map[string]any{"a": 1}}})

	var stats httpapi.SiteStats
	if st := c.do("GET", fmt.Sprintf("/api/v1/sites/%d/stats", site.Id), nil, &stats); st != 200 || stats.TotalUrls != 1 || stats.Pending != 1 || stats.ExtractedItems != 1 || stats.SiteName != "s" {
		t.Fatalf("stats: %d %+v", st, stats)
	}
	var items httpapi.ItemList
	if st := c.do("GET", fmt.Sprintf("/api/v1/items?site_id=%d", site.Id), nil, &items); st != 200 || items.Total != 1 || items.Items[0].Url != u || items.Items[0].Properties["price"] != "9.90" {
		t.Fatalf("items: %d %+v", st, items)
	}
	// properties = json object, not a string (java returned a string).
	if _, ok := items.Items[0].Properties["nested"].(map[string]any); !ok {
		t.Fatalf("nested: %#v", items.Items[0].Properties)
	}
	if st := c.do("GET", fmt.Sprintf("/api/v1/items/%d", items.Items[0].Id), nil, nil); st != 200 {
		t.Fatalf("get item: %d", st)
	}
	if st := c.do("GET", "/api/v1/items/999", nil, nil); st != 404 {
		t.Fatalf("missing item: %d", st)
	}
	// non-admin locked out of runs.
	user := env.login(t, "user@crawler.test", false)
	if st := user.do("GET", "/api/v1/runs", nil, nil); st != 403 {
		t.Fatalf("user runs: %d", st)
	}
}
