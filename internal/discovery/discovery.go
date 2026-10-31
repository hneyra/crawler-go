package discovery

import (
	"context"

	"fmt"
	"log/slog"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/fetch"
	"github.com/hneyra/crawler-go/internal/urlx"
)

// Fetcher = plain fetch w/ render fallback. fetch.HTTPFetcher fits.
type Fetcher interface {
	Fetch(ctx context.Context, url, waitSelector string) (fetch.Page, error)
}

// URLSink = where found urls go. crawl.URLStore fits.
type URLSink interface {
	InsertURL(ctx context.Context, u crawl.DiscoveredURL) (bool, error)
}

// Result = one category run. LastError = why the walk stopped early ("" = clean end).
type Result struct {
	Pages      int    `json:"pages"`
	New        int    `json:"new_urls"`
	Duplicates int    `json:"duplicate_urls"`
	Errors     int    `json:"errors"`
	LastError  string `json:"last_error,omitempty"`
}

// Add sums results (site = sum of categories).
func (r *Result) Add(o Result) {
	r.Pages += o.Pages
	r.New += o.New
	r.Duplicates += o.Duplicates
	r.Errors += o.Errors
	if o.LastError != "" {
		r.LastError = o.LastError
	}
}

// Service = category discovery.
type Service struct {
	Fetcher       Fetcher
	Renderer      fetch.Renderer // SCROLL_AJAX + click pagination. nil -> those paths fail soft
	URLs          URLSink
	MaxPages      int           // listing pages (or scrolls) per run. default 50
	RenderTimeout time.Duration // default 30s
	Log           *slog.Logger
	// Sleep = politeness wait. nil -> real timer. tests stub it.
	Sleep func(ctx context.Context, d time.Duration) error
}

func (s *Service) maxPages() int {
	if s.MaxPages <= 0 {
		return 50
	}
	return s.MaxPages
}

func (s *Service) renderTimeout() time.Duration {
	if s.RenderTimeout <= 0 {
		return 30 * time.Second
	}
	return s.RenderTimeout
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

// DiscoverCategory walks listing pages of cat. site errors stop the walk but are not returned
// (counted in Result); only ctx cancel and storage failures return error.
func (s *Service) DiscoverCategory(ctx context.Context, site crawl.Site, cat crawl.Category) (Result, error) {
	log := s.log().With("site_id", site.ID, "category_id", cat.ID)
	if cat.PaginationType == crawl.PaginationScrollAJAX {
		return s.discoverScroll(ctx, log, cat)
	}
	return s.discoverPages(ctx, log, site, cat)
}

func (s *Service) discoverPages(ctx context.Context, log *slog.Logger, site crawl.Site, cat crawl.Category) (Result, error) {
	var res Result
	visited := map[string]bool{}
	current := cat.URL
	for current != "" && res.Pages < s.maxPages() {
		visited[key(current)] = true
		res.Pages++
		page, err := s.Fetcher.Fetch(ctx, current, cat.ItemLinkSelector)
		if err != nil {
			if ctx.Err() != nil {
				return res, ctx.Err()
			}
			res.Errors++
			res.LastError = err.Error()
			log.Warn("discovery fetch failed; stopping category", "url", current, "err", err)
			break
		}
		if err := s.storeLinks(ctx, page, cat, &res); err != nil {
			return res, err
		}
		next := NextPageURL(page.Doc, cat.NextPageSelector)
		if next != "" && visited[key(next)] {
			// href points back (js pagination, "#"): real browser click.
			next = s.clickNext(ctx, log, page, cat, &res)
		}
		current = next
		if current != "" && res.Pages < s.maxPages() {
			if err := s.sleep(ctx, site.PolitenessDelay()); err != nil {
				return res, err
			}
		}
	}
	log.Info("discovery done", "pages", res.Pages, "new", res.New, "duplicates", res.Duplicates, "errors", res.Errors)
	return res, nil
}

func (s *Service) clickNext(ctx context.Context, log *slog.Logger, page fetch.Page, cat crawl.Category, res *Result) string {
	sel, text, ok := NextClickTarget(page.Doc, cat.NextPageSelector)
	if !ok || s.Renderer == nil {
		return ""
	}
	next, err := s.Renderer.ClickNavigate(ctx, page.URL, fetch.ClickTarget{Selector: sel, Text: text}, s.renderTimeout())
	if err != nil {
		res.Errors++
		res.LastError = err.Error()
		log.Warn("click pagination failed; stopping category", "url", page.URL, "err", err)
		return ""
	}
	return next
}

func (s *Service) discoverScroll(ctx context.Context, log *slog.Logger, cat crawl.Category) (Result, error) {
	res := Result{Pages: 1}
	if s.Renderer == nil {
		res.Errors++
		res.LastError = "SCROLL_AJAX needs a headless renderer"
		return res, nil
	}
	out, err := s.Renderer.Render(ctx, fetch.RenderRequest{
		URL: cat.URL, WaitForSelector: cat.ItemLinkSelector, ScrollToBottom: true,
		MaxScrolls: s.maxPages(), Timeout: s.renderTimeout(),
	})
	if err != nil {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		res.Errors++
		res.LastError = err.Error()
		log.Warn("scroll render failed", "url", cat.URL, "err", err)
		return res, nil
	}
	page, err := fetch.NewPage(cat.URL, out.HTML, true)
	if err != nil {
		res.Errors++
		res.LastError = err.Error()
		return res, nil
	}
	if err := s.storeLinks(ctx, page, cat, &res); err != nil {
		return res, err
	}
	log.Info("scroll discovery done", "new", res.New, "duplicates", res.Duplicates)
	return res, nil
}

func (s *Service) storeLinks(ctx context.Context, page fetch.Page, cat crawl.Category, res *Result) error {
	base := docBase(page.Doc)
	var storeErr error
	page.Doc.Find(cat.ItemLinkSelector).EachWithBreak(func(_ int, el *goquery.Selection) bool {
		raw := ExtractLink(el, cat.LinkExtractionType, cat.LinkAttribute, base)
		if raw == "" {
			return true
		}
		norm, err := urlx.Normalize(raw)
		if err != nil {
			return true
		}
		inserted, err := s.URLs.InsertURL(ctx, crawl.DiscoveredURL{SiteID: cat.SiteID, CategoryID: cat.ID, URL: norm, URLHash: urlx.SHA256(norm)})
		if err != nil {
			storeErr = fmt.Errorf("store url %s: %w", norm, err)
			return false
		}
		if inserted {
			res.New++
		} else {
			res.Duplicates++
		}
		return true
	})
	return storeErr
}

// key = visited-set key. fragment-insensitive ("#" links count as same page).
func key(u string) string {
	if n, err := urlx.Normalize(u); err == nil {
		return n
	}
	return u
}
