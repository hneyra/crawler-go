package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hneyra/crawler-go/internal/platform/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	c, err := config.Load(env(map[string]string{"CRAWLER_DATABASE_URL": "postgres://x"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != "dev" || c.HTTPAddr != ":8080" || !c.RateLimit || c.CookieSecure {
		t.Fatalf("defaults: %+v", c)
	}
	cr := c.Crawl
	if cr.DiscoveryMaxPages != 50 || cr.ExtractionBatchSize != 10 || cr.RetryMax != 3 || cr.RetryInterval != 10*time.Minute {
		t.Fatalf("crawl defaults: %+v", cr)
	}
}

func TestLoadCollectsAllErrors(t *testing.T) {
	_, err := config.Load(env(map[string]string{
		"CRAWLER_ENV":                 "production",
		"CRAWLER_DB_MAX_CONNS":        "abc",
		"CRAWLER_HTTP_ADDR":           "nope",
		"CRAWLER_RETRY_INTERVAL":      "-1s",
		"CRAWLER_PUBLIC_URL":          "ftp://x/y",
		"CRAWLER_TRUSTED_PROXIES":     "10.0.0.0/8, bad",
		"CRAWLER_DISCOVERY_MAX_PAGES": "0",
	}))
	if err == nil {
		t.Fatal("want error")
	}
	for _, want := range []string{
		"CRAWLER_DATABASE_URL required", "CRAWLER_ENV", "CRAWLER_DB_MAX_CONNS", "CRAWLER_HTTP_ADDR",
		"CRAWLER_RETRY_INTERVAL", "CRAWLER_PUBLIC_URL", "bad cidr", "CRAWLER_DISCOVERY_MAX_PAGES",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %v", want, err)
		}
	}
}

func TestProdGuards(t *testing.T) {
	c, err := config.Load(env(map[string]string{"CRAWLER_DATABASE_URL": "postgres://x", "CRAWLER_ENV": "prod", "CRAWLER_PUBLIC_URL": "https://crawler.example"}))
	if err != nil {
		t.Fatal(err)
	}
	if !c.CookieSecure {
		t.Fatal("prod cookie must be secure by default")
	}
	_, err = config.Load(env(map[string]string{"CRAWLER_DATABASE_URL": "postgres://x", "CRAWLER_ENV": "prod", "CRAWLER_RATE_LIMIT": "off"}))
	if err == nil || !strings.Contains(err.Error(), "refused in prod") {
		t.Fatalf("rate limit off in prod: %v", err)
	}
}
