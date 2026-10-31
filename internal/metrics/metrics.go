// Package metrics = prometheus adapters for crawl observers (java CrawlerMetrics parity, crawler_* names).
package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/hneyra/crawler-go/internal/discovery"
	"github.com/hneyra/crawler-go/internal/extraction"
	"github.com/hneyra/crawler-go/internal/fetch"
	"github.com/hneyra/crawler-go/internal/pipeline"
)

// Crawl = worker-side counters/timers. implements fetch, extraction and pipeline observers.
type Crawl struct {
	fetchDur      prometheus.Histogram
	fetchErrors   prometheus.Counter
	cfFallbacks   prometheus.Counter
	discoveryDur  prometheus.Histogram
	discoveryURLs *prometheus.CounterVec
	discoveryPgs  prometheus.Counter
	discoveryErrs prometheus.Counter
	batchDur      prometheus.Histogram
	urlDur        prometheus.Histogram
	extractURLs   *prometheus.CounterVec
}

var (
	_ fetch.Observer      = (*Crawl)(nil)
	_ extraction.Observer = (*Crawl)(nil)
	_ pipeline.Observer   = (*Crawl)(nil)
)

// NewCrawl registers collectors on reg.
func NewCrawl(reg prometheus.Registerer) *Crawl {
	slow := prometheus.ExponentialBuckets(0.1, 2, 14) // 100ms .. ~27min
	m := &Crawl{
		fetchDur:      prometheus.NewHistogram(prometheus.HistogramOpts{Name: "crawler_fetch_duration_seconds", Help: "Page fetch time (incl. render fallback).", Buckets: slow}),
		fetchErrors:   prometheus.NewCounter(prometheus.CounterOpts{Name: "crawler_fetch_errors_total", Help: "Failed page fetches."}),
		cfFallbacks:   prometheus.NewCounter(prometheus.CounterOpts{Name: "crawler_fetch_cloudflare_fallbacks_total", Help: "Fetches retried with headless browser (blocked status or challenge)."}),
		discoveryDur:  prometheus.NewHistogram(prometheus.HistogramOpts{Name: "crawler_discovery_duration_seconds", Help: "Discovery time per category.", Buckets: slow}),
		discoveryURLs: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "crawler_discovery_urls_total", Help: "Urls found by discovery."}, []string{"result"}),
		discoveryPgs:  prometheus.NewCounter(prometheus.CounterOpts{Name: "crawler_discovery_pages_total", Help: "Listing pages visited."}),
		discoveryErrs: prometheus.NewCounter(prometheus.CounterOpts{Name: "crawler_discovery_errors_total", Help: "Discovery errors (category walk stopped)."}),
		batchDur:      prometheus.NewHistogram(prometheus.HistogramOpts{Name: "crawler_extraction_batch_duration_seconds", Help: "Extraction batch time.", Buckets: slow}),
		urlDur:        prometheus.NewHistogram(prometheus.HistogramOpts{Name: "crawler_extraction_url_duration_seconds", Help: "Extraction time per url.", Buckets: slow}),
		extractURLs:   prometheus.NewCounterVec(prometheus.CounterOpts{Name: "crawler_extraction_urls_total", Help: "Extracted urls by result."}, []string{"result"}),
	}
	reg.MustRegister(m.fetchDur, m.fetchErrors, m.cfFallbacks, m.discoveryDur, m.discoveryURLs, m.discoveryPgs, m.discoveryErrs, m.batchDur, m.urlDur, m.extractURLs)
	// zero series visible before first event.
	for _, r := range []string{"new", "duplicate"} {
		m.discoveryURLs.WithLabelValues(r)
	}
	for _, r := range []string{"success", "failure"} {
		m.extractURLs.WithLabelValues(r)
	}
	return m
}

// FetchDone = fetch.Observer.
func (m *Crawl) FetchDone(d time.Duration, err error) {
	m.fetchDur.Observe(d.Seconds())
	if err != nil {
		m.fetchErrors.Inc()
	}
}

// CloudflareFallback = fetch.Observer.
func (m *Crawl) CloudflareFallback() { m.cfFallbacks.Inc() }

// URLDone = extraction.Observer.
func (m *Crawl) URLDone(d time.Duration, err error) {
	m.urlDur.Observe(d.Seconds())
	if err != nil {
		m.extractURLs.WithLabelValues("failure").Inc()
		return
	}
	m.extractURLs.WithLabelValues("success").Inc()
}

// DiscoveryDone = pipeline.Observer.
func (m *Crawl) DiscoveryDone(d time.Duration, r discovery.Result) {
	m.discoveryDur.Observe(d.Seconds())
	m.discoveryURLs.WithLabelValues("new").Add(float64(r.New))
	m.discoveryURLs.WithLabelValues("duplicate").Add(float64(r.Duplicates))
	m.discoveryPgs.Add(float64(r.Pages))
	m.discoveryErrs.Add(float64(r.Errors))
}

// BatchDone = pipeline.Observer.
func (m *Crawl) BatchDone(d time.Duration, _ extraction.Batch) { m.batchDur.Observe(d.Seconds()) }

// Counts = global totals for gauges.
type Counts struct {
	Sites int64
	Items int64
	URLs  map[string]int64 // status -> count
}

// Counter = db totals. postgres.Stats fits.
type Counter interface {
	GlobalCounts(ctx context.Context) (Counts, error)
}

// Totals = scrape-time gauges (crawler_sites_total, crawler_urls{status}, crawler_items_total).
type Totals struct {
	src   Counter
	sites *prometheus.Desc
	urls  *prometheus.Desc
	items *prometheus.Desc
}

// NewTotals registers db gauge collector.
func NewTotals(reg prometheus.Registerer, src Counter) *Totals {
	t := &Totals{
		src:   src,
		sites: prometheus.NewDesc("crawler_sites_total", "Configured sites.", nil, nil),
		urls:  prometheus.NewDesc("crawler_urls", "Discovered urls by status.", []string{"status"}, nil),
		items: prometheus.NewDesc("crawler_items_total", "Extracted items.", nil, nil),
	}
	reg.MustRegister(t)
	return t
}

// Describe = prometheus.Collector.
func (t *Totals) Describe(ch chan<- *prometheus.Desc) {
	ch <- t.sites
	ch <- t.urls
	ch <- t.items
}

// Collect = prometheus.Collector. db error -> no samples (scrape still ok).
func (t *Totals) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := t.src.GlobalCounts(ctx)
	if err != nil {
		return
	}
	ch <- prometheus.MustNewConstMetric(t.sites, prometheus.GaugeValue, float64(c.Sites))
	ch <- prometheus.MustNewConstMetric(t.items, prometheus.GaugeValue, float64(c.Items))
	for _, st := range []string{"pending", "in_progress", "completed", "failed"} {
		ch <- prometheus.MustNewConstMetric(t.urls, prometheus.GaugeValue, float64(c.URLs[st]), st)
	}
}
