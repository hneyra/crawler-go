package metrics_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/hneyra/crawler-go/internal/discovery"
	"github.com/hneyra/crawler-go/internal/metrics"
)

type counts struct{ err error }

func (c counts) GlobalCounts(context.Context) (metrics.Counts, error) {
	return metrics.Counts{Sites: 2, Items: 7, URLs: map[string]int64{"pending": 3, "failed": 1}}, c.err
}

func TestTotals(t *testing.T) {
	reg := prometheus.NewRegistry()
	metrics.NewTotals(reg, counts{})
	want := `
# HELP crawler_items_total Extracted items.
# TYPE crawler_items_total gauge
crawler_items_total 7
# HELP crawler_sites_total Configured sites.
# TYPE crawler_sites_total gauge
crawler_sites_total 2
# HELP crawler_urls Discovered urls by status.
# TYPE crawler_urls gauge
crawler_urls{status="completed"} 0
crawler_urls{status="failed"} 1
crawler_urls{status="in_progress"} 0
crawler_urls{status="pending"} 3
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want)); err != nil {
		t.Fatal(err)
	}
	bad := prometheus.NewRegistry()
	metrics.NewTotals(bad, counts{err: errors.New("db down")})
	if n, err := testutil.GatherAndCount(bad); err != nil || n != 0 {
		t.Fatalf("db down: %d %v", n, err)
	}
}

func TestCrawlCounters(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := metrics.NewCrawl(reg)
	m.DiscoveryDone(time.Second, discovery.Result{Pages: 2, New: 5, Duplicates: 1, Errors: 1})
	m.URLDone(time.Second, nil)
	m.URLDone(time.Second, errors.New("x"))
	m.CloudflareFallback()
	m.FetchDone(time.Second, errors.New("x"))
	want := `
# HELP crawler_discovery_urls_total Urls found by discovery.
# TYPE crawler_discovery_urls_total counter
crawler_discovery_urls_total{result="duplicate"} 1
crawler_discovery_urls_total{result="new"} 5
# HELP crawler_extraction_urls_total Extracted urls by result.
# TYPE crawler_extraction_urls_total counter
crawler_extraction_urls_total{result="failure"} 1
crawler_extraction_urls_total{result="success"} 1
# HELP crawler_fetch_cloudflare_fallbacks_total Fetches retried with headless browser (blocked status or challenge).
# TYPE crawler_fetch_cloudflare_fallbacks_total counter
crawler_fetch_cloudflare_fallbacks_total 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "crawler_discovery_urls_total", "crawler_extraction_urls_total", "crawler_fetch_cloudflare_fallbacks_total"); err != nil {
		t.Fatal(err)
	}
}
