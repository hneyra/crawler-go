package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/httpapi"
	"github.com/hneyra/crawler-go/internal/identity"
	"github.com/hneyra/crawler-go/internal/pipeline"
	"github.com/hneyra/crawler-go/internal/platform/db"
	"github.com/hneyra/crawler-go/internal/platform/dbtest"
	"github.com/hneyra/crawler-go/internal/postgres"
)

// client = cookie jar + csrf header. zero csrf = anonymous-ish.
type client struct {
	t      *testing.T
	url    string
	http   *http.Client
	csrf   string
	header map[string]string
}

type testEnv struct {
	url  string
	ids  *identity.Service
	pool *pgxpool.Pool
}

const testOrigin = "http://ui.test"

func newEnv(t *testing.T) testEnv {
	t.Helper()
	pool := dbtest.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	ids, err := identity.New(postgres.Identity{Pool: pool}, db.Runner{Pool: pool}, log, identity.Settings{SessionTTL: time.Hour}, nil)
	if err != nil {
		t.Fatal(err)
	}
	cat := crawl.Catalog{Tx: db.Runner{Pool: pool}, Sites: postgres.Sites{Pool: pool}, Categories: postgres.Categories{Pool: pool}}
	pipe := &pipeline.Service{Tx: db.Runner{Pool: pool}, Runs: postgres.Runs{Pool: pool}, Sites: cat.Sites, Categories: cat.Categories, Jobs: postgres.JobQueue{Pool: pool}}
	h, err := httpapi.NewHandler(&httpapi.Server{
		Ops:      &httpapi.Ops{DB: pool, Version: "t"},
		AuthAPI:  &httpapi.AuthAPI{Identity: ids, SessionTTL: time.Hour},
		SitesAPI: &httpapi.SitesAPI{Catalog: cat, Sites: cat.Sites, Categories: cat.Categories},
		RunsAPI:  &httpapi.RunsAPI{Pipeline: pipe, Runs: postgres.Runs{Pool: pool}, URLs: postgres.URLs{Pool: pool}, Items: postgres.Items{Pool: pool}},
	}, httpapi.Options{Log: log, PublicURL: testOrigin, Sessions: ids, Limits: httpapi.NoLimits()})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return testEnv{url: srv.URL, ids: ids, pool: pool}
}

func (e testEnv) anon(t *testing.T) client {
	jar, _ := cookiejar.New(nil)
	return client{t: t, url: e.url, http: &http.Client{Jar: jar}}
}

// login creates user (admin or not) and returns logged-in client with csrf.
func (e testEnv) login(t *testing.T, email string, admin bool) client {
	t.Helper()
	if _, err := e.ids.CreateUser(context.Background(), email, "correct horse battery", admin); err != nil {
		t.Fatal(err)
	}
	c := e.anon(t)
	var s httpapi.Session
	if st := c.do("POST", "/api/v1/auth/login", map[string]any{"email": email, "password": "correct horse battery"}, &s); st != 200 || s.CsrfToken == "" {
		t.Fatalf("login: %d %+v", st, s)
	}
	c.csrf = s.CsrfToken
	return c
}

func newSitesServer(t *testing.T) client {
	t.Helper()
	return newEnv(t).login(t, "admin@crawler.test", true)
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
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	res, err := c.http.Do(req)
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
	// camelCase (yaml) keys inside extraction_config must not silently reset rules.
	if st := c.do("PATCH", fmt.Sprintf("/api/v1/sites/%d", site.Id), map[string]any{"extraction_config": map[string]any{"detailStrategy": "HTML"}}, nil); st != 400 {
		t.Fatalf("unknown config key: %d", st)
	}
	p = problem{}
	if st := c.do("POST", "/api/v1/sites", map[string]any{"name": "nul\u0000", "base_url": "https://x.pe"}, &p); st != 400 {
		t.Fatalf("nul byte: %d %+v", st, p)
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
	// "" clears optional selector.
	var cleared httpapi.Category
	if st := c.do("PATCH", path, map[string]any{"next_page_selector": ""}, &cleared); st != 200 || cleared.NextPageSelector != nil {
		t.Fatalf("clear selector: %d %+v", st, cleared)
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
