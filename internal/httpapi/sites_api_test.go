package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/httpapi"
	"github.com/hneyra/crawler-go/internal/platform/db"
	"github.com/hneyra/crawler-go/internal/platform/dbtest"
	"github.com/hneyra/crawler-go/internal/postgres"
)

type client struct {
	t   *testing.T
	url string
}

func newSitesServer(t *testing.T) client {
	t.Helper()
	pool := dbtest.New(t)
	cat := crawl.Catalog{Tx: db.Runner{Pool: pool}, Sites: postgres.Sites{Pool: pool}, Categories: postgres.Categories{Pool: pool}}
	h, err := httpapi.NewHandler(&httpapi.Server{
		Ops:      &httpapi.Ops{DB: pool, Version: "t"},
		SitesAPI: &httpapi.SitesAPI{Catalog: cat, Sites: cat.Sites, Categories: cat.Categories},
	}, httpapi.Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return client{t, srv.URL}
}

// do sends json, decodes json response into out (if non-nil). returns status.
func (c client) do(method, path string, body any, out any) int {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, c.url+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if out != nil {
		raw, _ := io.ReadAll(res.Body)
		if err := json.Unmarshal(raw, out); err != nil && len(raw) > 0 {
			c.t.Fatalf("%s %s: decode %q: %v", method, path, raw, err)
		}
	}
	return res.StatusCode
}

type problem struct {
	Code   string `json:"code"`
	Errors []struct {
		Field string `json:"field"`
	} `json:"errors"`
}

func TestSitesCRUDOverHTTP(t *testing.T) {
	c := newSitesServer(t)
	var site httpapi.Site
	st := c.do("POST", "/api/v1/sites", map[string]any{
		"name": "Adondevivir", "base_url": "https://www.adondevivir.com",
		"extraction_config": map[string]any{
			"detail_strategy": "SCRIPT_JSON",
			"script_patterns": []string{`const\s+avisoInfo\s*=\s*(\{.*?\});\s*$`},
			"json_paths":      map[string]string{"title": "$.units[0].title"},
		},
	}, &site)
	if st != 201 || site.Id == 0 || !site.Enabled || site.PolitenessDelayMs != 1000 ||
		site.ExtractionConfig.DetailStrategy != httpapi.SCRIPTJSON || site.ExtractionConfig.FieldSelectors == nil {
		t.Fatalf("create: %d %+v", st, site)
	}

	var p problem
	if st := c.do("POST", "/api/v1/sites", map[string]any{"name": "Adondevivir", "base_url": "https://x.pe"}, &p); st != 409 || p.Code != "site_name_taken" {
		t.Fatalf("dup: %d %+v", st, p)
	}
	p = problem{}
	if st := c.do("POST", "/api/v1/sites", map[string]any{"name": "x", "base_url": "nota-url", "extraction_config": map[string]any{"script_patterns": []string{"(bad"}}}, &p); st != 400 || len(p.Errors) != 2 {
		t.Fatalf("invalid: %d %+v", st, p)
	}
	p = problem{}
	if st := c.do("POST", "/api/v1/sites", map[string]any{"name": "x", "base_url": "https://x.pe", "extraction_config": map[string]any{"detail_strategy": "XML"}}, &p); st != 400 || p.Code != "validation_failed" {
		t.Fatalf("bad enum (spec): %d %+v", st, p)
	}
	if st := c.do("POST", "/api/v1/sites", map[string]any{"name": "x", "base_url": "https://x.pe", "extra": 1}, nil); st != 400 {
		t.Fatalf("unknown field: %d", st)
	}

	var up httpapi.Site
	if st := c.do("PATCH", fmt.Sprintf("/api/v1/sites/%d", site.Id), map[string]any{"enabled": false}, &up); st != 200 || up.Enabled || up.Name != "Adondevivir" || len(up.ExtractionConfig.ScriptPatterns) != 1 {
		t.Fatalf("patch: %d %+v", st, up)
	}
	var list httpapi.SiteList
	if st := c.do("GET", "/api/v1/sites", nil, &list); st != 200 || len(list.Items) != 1 {
		t.Fatalf("list: %d %+v", st, list)
	}
	if st := c.do("GET", "/api/v1/sites/999", nil, &p); st != 404 || p.Code != "site_not_found" {
		t.Fatalf("missing: %d %+v", st, p)
	}
	if st := c.do("GET", "/api/v1/sites/abc", nil, nil); st != 400 {
		t.Fatalf("bad id: %d", st)
	}
	if st := c.do("DELETE", fmt.Sprintf("/api/v1/sites/%d", site.Id), nil, nil); st != 204 {
		t.Fatalf("delete: %d", st)
	}
	if st := c.do("DELETE", fmt.Sprintf("/api/v1/sites/%d", site.Id), nil, nil); st != 404 {
		t.Fatalf("delete again: %d", st)
	}
}

func TestCategoriesCRUDOverHTTP(t *testing.T) {
	c := newSitesServer(t)
	var site httpapi.Site
	c.do("POST", "/api/v1/sites", map[string]any{"name": "s", "base_url": "https://s.pe"}, &site)
	base := fmt.Sprintf("/api/v1/sites/%d/categories", site.Id)

	var cat httpapi.Category
	st := c.do("POST", base, map[string]any{"name": "celulares", "url": "https://s.pe/celulares", "pagination_type": "PAGE_PARAM", "item_link_selector": "a.product"}, &cat)
	if st != 201 || cat.LinkExtractionType != httpapi.HREF || !cat.Enabled || cat.SiteId != site.Id || cat.NextPageSelector != nil {
		t.Fatalf("create: %d %+v", st, cat)
	}
	var p problem
	if st := c.do("POST", base, map[string]any{"name": "d", "url": "https://s.pe/d", "pagination_type": "PAGE_PARAM", "item_link_selector": "div", "link_extraction_type": "DATA_ATTRIBUTE"}, &p); st != 400 || len(p.Errors) != 1 || p.Errors[0].Field != "link_attribute" {
		t.Fatalf("data attr: %d %+v", st, p)
	}
	if st := c.do("POST", "/api/v1/sites/999/categories", map[string]any{"name": "d", "url": "https://s.pe/d", "pagination_type": "PAGE_PARAM", "item_link_selector": "div"}, nil); st != 404 {
		t.Fatalf("missing site: %d", st)
	}
	var up httpapi.Category
	path := fmt.Sprintf("/api/v1/categories/%d", cat.Id)
	if st := c.do("PATCH", path, map[string]any{"pagination_type": "SCROLL_AJAX", "next_page_selector": "a.next"}, &up); st != 200 || up.PaginationType != httpapi.SCROLLAJAX || up.NextPageSelector == nil || *up.NextPageSelector != "a.next" || up.Name != "celulares" {
		t.Fatalf("patch: %d %+v", st, up)
	}
	var list httpapi.CategoryList
	if st := c.do("GET", base, nil, &list); st != 200 || len(list.Items) != 1 {
		t.Fatalf("list: %d %+v", st, list)
	}
	if st := c.do("GET", "/api/v1/sites/999/categories", nil, nil); st != 404 {
		t.Fatalf("list missing site: %d", st)
	}
	if st := c.do("DELETE", path, nil, nil); st != 204 {
		t.Fatalf("delete: %d", st)
	}
	if st := c.do("GET", path, nil, nil); st != 404 {
		t.Fatalf("get deleted: %d", st)
	}
}
