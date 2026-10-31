// Package fetch = get a page as dom. plain http first; blocked/challenged -> headless render.
// talks http by nature (archtest allows net/http here). no db, no transport layer.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// Page = fetched html as dom. Doc.Url = base for relative links.
type Page struct {
	URL      string
	HTML     string
	Doc      *goquery.Document
	Rendered bool // true = came from headless browser
}

// RenderRequest = headless render options.
type RenderRequest struct {
	URL             string
	WaitForSelector string        // "" = no wait
	Intercept       []string      // glob patterns (* = any). matching xhr/fetch responses captured
	ScrollToBottom  bool          // infinite scroll
	MaxScrolls      int           // cap for ScrollToBottom
	Timeout         time.Duration // whole render
}

// Intercepted = captured network response.
type Intercepted struct {
	URL    string
	Status int
	Body   string
}

// RenderResult = final dom html + captured responses.
type RenderResult struct {
	HTML        string
	Intercepted []Intercepted
}

// ClickTarget = what to click. Text != "" -> first <a> whose trimmed text == Text (selector ignored).
type ClickTarget struct {
	Selector string
	Text     string
}

// Renderer = headless browser port. adapter: internal/render/chromedp.
type Renderer interface {
	Render(ctx context.Context, req RenderRequest) (RenderResult, error)
	// ClickNavigate opens url, clicks target, waits for url change. returns new url.
	ClickNavigate(ctx context.Context, url string, target ClickTarget, timeout time.Duration) (string, error)
}

// Observer = metrics hook. nil-safe via noop.
type Observer interface {
	FetchDone(d time.Duration, err error)
	CloudflareFallback()
}

type noop struct{}

func (noop) FetchDone(time.Duration, error) {}
func (noop) CloudflareFallback()            {}

// HTTPFetcher = plain GET with render fallback.
type HTTPFetcher struct {
	Client        *http.Client // nil -> default w/ Timeout
	UserAgent     string
	Timeout       time.Duration // plain fetch. default 15s
	RenderTimeout time.Duration // fallback render. default 30s
	MaxBytes      int64         // body cap. default 16 MiB
	Renderer      Renderer      // nil -> no fallback
	Observer      Observer
}

// StatusError = non-2xx answer from site.
type StatusError struct {
	URL    string
	Status int
}

func (e *StatusError) Error() string { return fmt.Sprintf("GET %s: status %d", e.URL, e.Status) }

func (f *HTTPFetcher) obs() Observer {
	if f.Observer == nil {
		return noop{}
	}
	return f.Observer
}

// Fetch GETs url. 403/429/503 or cloudflare challenge -> render (waits for waitSelector).
func (f *HTTPFetcher) Fetch(ctx context.Context, rawURL, waitSelector string) (Page, error) {
	start := time.Now()
	p, err := f.fetch(ctx, rawURL, waitSelector)
	f.obs().FetchDone(time.Since(start), err)
	return p, err
}

func (f *HTTPFetcher) fetch(ctx context.Context, rawURL, waitSelector string) (Page, error) {
	html, finalURL, status, err := f.get(ctx, rawURL)
	if err != nil {
		return Page{}, err
	}
	if IsBlockedStatus(status) {
		return f.render(ctx, rawURL, waitSelector, fmt.Sprintf("status %d", status))
	}
	if status < 200 || status > 299 {
		return Page{}, &StatusError{URL: rawURL, Status: status}
	}
	// base = url after redirects (jsoup parity): /list -> /list/ changes relative links.
	p, err := NewPage(finalURL, html, false)
	if err != nil {
		return Page{}, err
	}
	if IsChallenge(p.Doc) {
		return f.render(ctx, rawURL, waitSelector, "cloudflare challenge")
	}
	return p, nil
}

// get returns body, final url (after redirects), status.
func (f *HTTPFetcher) get(ctx context.Context, rawURL string) (string, string, int, error) {
	timeout := f.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", 0, fmt.Errorf("build request %s: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", f.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "es-PE,es;q=0.9,en;q=0.8")
	client := f.Client
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req) //nolint:gosec // G107: crawling arbitrary configured urls is the job
	if err != nil {
		return "", "", 0, fmt.Errorf("GET %s: %w", rawURL, err)
	}
	defer func() { _ = res.Body.Close() }()
	limit := f.MaxBytes
	if limit <= 0 {
		limit = 16 << 20
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return "", "", 0, fmt.Errorf("read %s: %w", rawURL, err)
	}
	if int64(len(body)) > limit {
		return "", "", 0, fmt.Errorf("GET %s: body larger than %d bytes", rawURL, limit)
	}
	final := rawURL
	if res.Request != nil && res.Request.URL != nil {
		final = res.Request.URL.String()
	}
	return string(body), final, res.StatusCode, nil
}

// ErrNoRenderer = blocked page and no headless fallback configured.
var ErrNoRenderer = errors.New("page blocked and no renderer configured")

func (f *HTTPFetcher) render(ctx context.Context, rawURL, waitSelector, why string) (Page, error) {
	f.obs().CloudflareFallback()
	if f.Renderer == nil {
		return Page{}, fmt.Errorf("GET %s (%s): %w", rawURL, why, ErrNoRenderer)
	}
	timeout := f.RenderTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	res, err := f.Renderer.Render(ctx, RenderRequest{URL: rawURL, WaitForSelector: waitSelector, Timeout: timeout})
	if err != nil {
		return Page{}, fmt.Errorf("render %s (%s): %w", rawURL, why, err)
	}
	return NewPage(rawURL, res.HTML, true)
}

// NewPage parses html. Doc.Url = link base: <base href> resolved against rawURL, else rawURL.
func NewPage(rawURL, html string, rendered bool) (Page, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return Page{}, fmt.Errorf("parse %s: %w", rawURL, err)
	}
	if u, err := url.Parse(rawURL); err == nil {
		doc.Url = u
		if href, ok := doc.Find("base[href]").First().Attr("href"); ok && strings.TrimSpace(href) != "" {
			if b, err := u.Parse(strings.TrimSpace(href)); err == nil && (b.Scheme == "http" || b.Scheme == "https") {
				doc.Url = b
			}
		}
	}
	return Page{URL: rawURL, HTML: html, Doc: doc, Rendered: rendered}, nil
}
