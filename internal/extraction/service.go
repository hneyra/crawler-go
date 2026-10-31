package extraction

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/fetch"
)

// Fetcher = plain fetch w/ render fallback. fetch.HTTPFetcher fits.
type Fetcher interface {
	Fetch(ctx context.Context, url, waitSelector string) (fetch.Page, error)
}

// SiteGetter = site lookup. crawl.SiteStore fits.
type SiteGetter interface {
	GetSite(ctx context.Context, id int64) (crawl.Site, error)
}

// Snapshots = raw page storage. storage.Raw fits.
type Snapshots interface {
	SaveHTML(siteID int64, url, html string) (string, error)
	SaveJSON(siteID int64, key, json string) (string, error)
}

// ItemSink = item upsert. crawl.ItemStore fits.
type ItemSink interface {
	UpsertItem(ctx context.Context, it crawl.Item) (crawl.Item, error)
}

// Batch = one ProcessBatch outcome.
type Batch struct {
	Claimed   int    `json:"claimed"`
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
	LastError string `json:"last_error,omitempty"`
}

// Add sums batches.
func (b *Batch) Add(o Batch) {
	b.Claimed += o.Claimed
	b.Succeeded += o.Succeeded
	b.Failed += o.Failed
	if o.LastError != "" {
		b.LastError = o.LastError
	}
}

// Observer = metrics hook. nil-safe.
type Observer interface {
	URLDone(d time.Duration, err error)
}

// Service = detail extraction.
type Service struct {
	Sites         SiteGetter
	Queue         crawl.URLQueue
	Items         ItemSink
	Fetcher       Fetcher
	Renderer      fetch.Renderer // AJAX strategy. nil -> AJAX urls fail
	Snapshots     Snapshots
	BatchSize     int           // default 10
	RenderTimeout time.Duration // default 30s
	Log           *slog.Logger
	Observer      Observer
	Sleep         func(ctx context.Context, d time.Duration) error // nil -> timer
}

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Log
}

func (s *Service) sleep(ctx context.Context, d time.Duration) error {
	if s.Sleep != nil {
		return s.Sleep(ctx, d)
	}
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ProcessBatch claims up to BatchSize PENDING urls of site and extracts each.
// per-url failures -> FAILED + retry_count++ (counted, not returned). returns error only for
// missing site, bad rules, storage/db failure or ctx cancel; unprocessed claimed urls released.
func (s *Service) ProcessBatch(ctx context.Context, siteID int64) (Batch, error) {
	site, err := s.Sites.GetSite(ctx, siteID)
	if err != nil {
		return Batch{}, err
	}
	rules, err := Compile(site.ExtractionConfig)
	if err != nil {
		return Batch{}, fmt.Errorf("site %d extraction rules: %w", siteID, err)
	}
	size := s.BatchSize
	if size <= 0 {
		size = 10
	}
	urls, err := s.Queue.ClaimPending(ctx, siteID, size)
	if err != nil {
		return Batch{}, err
	}
	b := Batch{Claimed: len(urls)}
	log := s.log().With("site_id", siteID)
	for i, u := range urls {
		if i > 0 {
			if err := s.sleep(ctx, site.PolitenessDelay()); err != nil {
				return b, s.release(urls[i:], err)
			}
		}
		start := time.Now()
		perr := s.processURL(ctx, site, rules, u)
		if s.Observer != nil {
			s.Observer.URLDone(time.Since(start), perr)
		}
		if perr != nil && ctx.Err() != nil {
			// shutdown, not the site's fault: no retry consumed.
			return b, s.release(urls[i:], ctx.Err())
		}
		var se storeError
		if errors.As(perr, &se) {
			return b, s.release(urls[i:], perr)
		}
		if perr != nil {
			b.Failed++
			b.LastError = perr.Error()
			log.Warn("extraction failed", "url", u.URL, "err", perr)
			if err := s.Queue.MarkFailed(ctx, u.ID, perr.Error()); err != nil {
				return b, s.release(urls[i+1:], err)
			}
			continue
		}
		b.Succeeded++
		if err := s.Queue.MarkCompleted(ctx, u.ID); err != nil {
			return b, s.release(urls[i+1:], err)
		}
	}
	log.Info("extraction batch done", "claimed", b.Claimed, "ok", b.Succeeded, "failed", b.Failed)
	return b, nil
}

// release returns unprocessed urls to PENDING on a detached ctx (shutdown path).
func (s *Service) release(rest []crawl.DiscoveredURL, cause error) error {
	ids := make([]int64, len(rest))
	for i, u := range rest {
		ids[i] = u.ID
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Queue.Release(ctx, ids); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

// storeError = our side failed (disk, db). abort batch, do not blame the url.
type storeError struct{ error }

func (e storeError) Unwrap() error { return e.error }

func (s *Service) processURL(ctx context.Context, site crawl.Site, rules Rules, u crawl.DiscoveredURL) error {
	var (
		page  fetch.Page
		props Props
		snap  string
	)
	if rules.Strategy() == crawl.StrategyAJAX {
		if s.Renderer == nil {
			return errors.New("AJAX strategy needs a headless renderer")
		}
		timeout := s.RenderTimeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		res, err := s.Renderer.Render(ctx, fetch.RenderRequest{URL: u.URL, Intercept: rules.InterceptPatterns(), Timeout: timeout})
		if err != nil {
			return fmt.Errorf("render: %w", err)
		}
		if page, err = fetch.NewPage(u.URL, res.HTML, true); err != nil {
			return err
		}
		if snap, err = s.Snapshots.SaveHTML(site.ID, u.URL, res.HTML); err != nil {
			return storeError{err}
		}
		for _, it := range res.Intercepted {
			if _, err := s.Snapshots.SaveJSON(site.ID, u.URL+"#intercepted-"+it.URL, it.Body); err != nil {
				return storeError{err}
			}
		}
		props = rules.FromPage(page.Doc)
		rules.FromIntercepted(props, res.Intercepted)
	} else {
		var err error
		if page, err = s.Fetcher.Fetch(ctx, u.URL, ""); err != nil {
			return err
		}
		if snap, err = s.Snapshots.SaveHTML(site.ID, u.URL, page.HTML); err != nil {
			return storeError{err}
		}
		props = rules.FromPage(page.Doc)
	}
	// postgres text/jsonb reject \u0000: strip it, else one hostile page wedges the batch.
	_, err := s.Items.UpsertItem(ctx, crawl.Item{
		DiscoveredURLID: u.ID, SiteID: site.ID, Title: stripNUL(title(props, page)).(string),
		Properties: stripNUL(map[string]any(props)).(map[string]any), RawSnapshotPath: snap,
	})
	if err != nil {
		return storeError{err}
	}
	return nil
}

// title = props.title if set, else <title>. java parity.
func title(p Props, page fetch.Page) string {
	if v, ok := p["title"]; ok && v != nil {
		if s := strings.TrimSpace(fmt.Sprint(v)); s != "" {
			return s
		}
	}
	return normSpace(page.Doc.Find("title").First().Text())
}

// stripNUL removes \x00 from every string (keys too) of a decoded json tree.
func stripNUL(v any) any {
	switch t := v.(type) {
	case string:
		return strings.ReplaceAll(t, "\x00", "")
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[strings.ReplaceAll(k, "\x00", "")] = stripNUL(x)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = stripNUL(x)
		}
		return out
	default:
		return v
	}
}
