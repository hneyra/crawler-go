// Package headless = fetch.Renderer over headless chrome (CDP). replaces java node/patchright service.
// one browser per process, one tab per render. tabs share cookies -> cloudflare clearance reused.
package headless

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/hneyra/crawler-go/internal/fetch"
)

// Options = browser launch settings.
type Options struct {
	ExecPath   string // "" -> chromedp finds chrome
	ProfileDir string // "" -> temp profile (no persistent cookies)
	Headless   bool
	UserAgent  string
	// MaxBodyBytes caps each intercepted response body. default 4 MiB.
	MaxBodyBytes int
	// Log gets cdp protocol noise at debug (new chrome events unknown to pinned cdproto). nil = discard.
	Log *slog.Logger
}

// Renderer = fetch.Renderer. safe for concurrent use. Close when done.
type Renderer struct {
	opts       Options
	allocClose context.CancelFunc
	browser    context.Context
	browserEnd context.CancelFunc
}

var _ fetch.Renderer = (*Renderer)(nil)

// New starts chrome. ctx bounds browser lifetime.
func New(ctx context.Context, o Options) (*Renderer, error) {
	if o.MaxBodyBytes <= 0 {
		o.MaxBodyBytes = 4 << 20
	}
	flags := append(chromedp.DefaultExecAllocatorOptions[:0:0], chromedp.DefaultExecAllocatorOptions[:]...)
	flags = append(flags,
		chromedp.Flag("headless", o.Headless),
		chromedp.Flag("lang", "es-PE"),
		chromedp.Flag("no-sandbox", true), // containers run as non-root w/o user namespaces
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.WindowSize(1920, 1080),
	)
	if o.ExecPath != "" {
		flags = append(flags, chromedp.ExecPath(o.ExecPath))
	}
	if o.ProfileDir != "" {
		flags = append(flags, chromedp.UserDataDir(o.ProfileDir))
	}
	if o.UserAgent != "" {
		flags = append(flags, chromedp.UserAgent(o.UserAgent))
	}
	allocCtx, allocClose := chromedp.NewExecAllocator(context.WithoutCancel(ctx), flags...)
	logf := func(string, ...any) {}
	if o.Log != nil {
		logf = func(f string, a ...any) { o.Log.Debug("cdp", "msg", fmt.Sprintf(f, a...)) }
	}
	browser, browserEnd := chromedp.NewContext(allocCtx, chromedp.WithErrorf(logf), chromedp.WithLogf(logf))
	// first Run starts the browser. fail fast on missing chrome.
	if err := chromedp.Run(browser); err != nil {
		browserEnd()
		allocClose()
		return nil, fmt.Errorf("start chrome: %w", err)
	}
	return &Renderer{opts: o, allocClose: allocClose, browser: browser, browserEnd: browserEnd}, nil
}

// Close stops chrome.
func (r *Renderer) Close() {
	r.browserEnd()
	r.allocClose()
}

// tab = new target in shared browser, bounded by ctx + timeout.
func (r *Renderer) tab(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tctx, tcancel := chromedp.NewContext(r.browser)
	// caller ctx cancel -> tab closes too.
	stop := context.AfterFunc(ctx, tcancel)
	dctx, dcancel := context.WithTimeout(tctx, timeout)
	return dctx, func() {
		dcancel()
		stop()
		tcancel()
	}
}

// GlobRegexp turns "*/api/*" into anchored regexp. only * is special.
func GlobRegexp(glob string) (*regexp.Regexp, error) {
	parts := strings.Split(glob, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.Compile("^" + strings.Join(parts, ".*") + "$")
}

type capture struct {
	mu       sync.Mutex
	wg       sync.WaitGroup
	patterns []*regexp.Regexp
	pending  map[network.RequestID]fetch.Intercepted
	done     []fetch.Intercepted
	maxBody  int
}

func (c *capture) matches(u string) bool {
	for _, p := range c.patterns {
		if p.MatchString(u) {
			return true
		}
	}
	return false
}

// listen records matching responses; body fetched once loading finished.
func (c *capture) listen(tctx context.Context) {
	chromedp.ListenTarget(tctx, func(ev any) {
		switch e := ev.(type) {
		case *network.EventResponseReceived:
			if e.Response != nil && c.matches(e.Response.URL) {
				c.mu.Lock()
				c.pending[e.RequestID] = fetch.Intercepted{URL: e.Response.URL, Status: int(e.Response.Status)}
				c.mu.Unlock()
			}
		case *network.EventLoadingFinished:
			c.mu.Lock()
			it, ok := c.pending[e.RequestID]
			delete(c.pending, e.RequestID)
			c.mu.Unlock()
			if !ok {
				return
			}
			// listener must not block: fetch body on own goroutine. ends with tab ctx.
			c.wg.Add(1)
			go func() {
				defer c.wg.Done()
				var body []byte
				err := chromedp.Run(tctx, chromedp.ActionFunc(func(ctx context.Context) error {
					var err error
					body, err = network.GetResponseBody(e.RequestID).Do(ctx)
					return err
				}))
				if err != nil || len(body) > c.maxBody {
					return
				}
				it.Body = string(body)
				c.mu.Lock()
				c.done = append(c.done, it)
				c.mu.Unlock()
			}()
		}
	})
}

// Render loads url, waits challenge + selector, optional scroll, returns dom + captures.
func (r *Renderer) Render(ctx context.Context, req fetch.RenderRequest) (fetch.RenderResult, error) {
	tctx, cancel := r.tab(ctx, req.Timeout)
	defer cancel()
	deadline, _ := tctx.Deadline()

	c := &capture{pending: map[network.RequestID]fetch.Intercepted{}, maxBody: r.opts.MaxBodyBytes}
	for _, g := range req.Intercept {
		re, err := GlobRegexp(g)
		if err != nil {
			return fetch.RenderResult{}, fmt.Errorf("intercept pattern %q: %w", g, err)
		}
		c.patterns = append(c.patterns, re)
	}
	actions := []chromedp.Action{network.Enable()}
	if len(c.patterns) > 0 {
		c.listen(tctx)
	}
	actions = append(actions, chromedp.Navigate(req.URL))
	if err := chromedp.Run(tctx, actions...); err != nil {
		return fetch.RenderResult{}, fmt.Errorf("navigate %s: %w", req.URL, err)
	}
	waitChallenge(tctx, min(15*time.Second, time.Until(deadline)))
	if req.WaitForSelector != "" {
		// soft wait: listing may legitimately be empty. dom returned either way.
		wait := max(5*time.Second, time.Until(deadline)/2)
		wctx, wcancel := context.WithTimeout(tctx, wait)
		_ = chromedp.Run(wctx, chromedp.WaitReady(req.WaitForSelector, chromedp.ByQuery))
		wcancel()
	}
	if req.ScrollToBottom {
		autoScroll(tctx, req.MaxScrolls)
	}
	var html string
	if err := chromedp.Run(tctx, chromedp.Evaluate(`document.documentElement.outerHTML`, &html)); err != nil {
		return fetch.RenderResult{}, fmt.Errorf("read dom %s: %w", req.URL, err)
	}
	// let in-flight body reads finish (bounded by tab deadline).
	waitGroup(tctx, &c.wg)
	c.mu.Lock()
	out := append([]fetch.Intercepted(nil), c.done...)
	c.mu.Unlock()
	return fetch.RenderResult{HTML: html, Intercepted: out}, nil
}

func waitGroup(ctx context.Context, wg *sync.WaitGroup) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// challenge signals = java renderer.
const challengeJS = `(() => {
  const t = document.title || "";
  return t.includes("Just a moment") || t.includes("Attention Required") || t.includes("Checking your browser")
    || location.href.includes("/cdn-cgi/challenge-platform");
})()`

// waitChallenge polls until cloudflare interstitial gone or max elapsed. best effort.
func waitChallenge(ctx context.Context, maxWait time.Duration) {
	end := time.Now().Add(maxWait)
	for time.Now().Before(end) {
		var on bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(challengeJS, &on)); err != nil || !on {
			return
		}
		if sleep(ctx, 500*time.Millisecond) != nil {
			return
		}
	}
}

// autoScroll: scroll to bottom until height stops growing for 3s or maxScrolls.
func autoScroll(ctx context.Context, maxScrolls int) {
	if maxScrolls <= 0 {
		maxScrolls = 20
	}
	height := func() int {
		var h int
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.body ? document.body.scrollHeight : 0`, &h))
		return h
	}
	last := height()
	for range maxScrolls {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`window.scrollTo(0, document.body.scrollHeight)`, nil)); err != nil {
			return
		}
		grown := false
		for end := time.Now().Add(3 * time.Second); time.Now().Before(end); {
			if sleep(ctx, 250*time.Millisecond) != nil {
				return
			}
			if h := height(); h > last {
				last, grown = h, true
				break
			}
		}
		if !grown {
			return
		}
		if sleep(ctx, 500*time.Millisecond) != nil {
			return
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ErrNoNavigation = click did not change the url in time.
var ErrNoNavigation = errors.New("click did not navigate")

// clickJS marks target with data attr so chromedp can click it by selector. returns found.
const clickJS = `((sel, text) => {
  let el = null;
  if (text) {
    el = Array.from(document.querySelectorAll("a")).find(a => (a.textContent || "").trim() === text) || null;
  } else {
    el = document.querySelector(sel);
  }
  if (!el) return false;
  el.setAttribute("data-crawler-click", "1");
  return true;
})(%q, %q)`

// ClickNavigate opens url, clicks target, waits for location change.
func (r *Renderer) ClickNavigate(ctx context.Context, rawURL string, target fetch.ClickTarget, timeout time.Duration) (string, error) {
	tctx, cancel := r.tab(ctx, timeout)
	defer cancel()
	deadline, _ := tctx.Deadline()
	if err := chromedp.Run(tctx, chromedp.Navigate(rawURL)); err != nil {
		return "", fmt.Errorf("navigate %s: %w", rawURL, err)
	}
	waitChallenge(tctx, min(15*time.Second, time.Until(deadline)))
	var found bool
	for end := time.Now().Add(min(10*time.Second, time.Until(deadline))); ; {
		if err := chromedp.Run(tctx, chromedp.Evaluate(fmt.Sprintf(clickJS, target.Selector, target.Text), &found)); err != nil {
			return "", fmt.Errorf("find click target: %w", err)
		}
		if found || time.Now().After(end) {
			break
		}
		if err := sleep(tctx, 200*time.Millisecond); err != nil {
			return "", err
		}
	}
	if !found {
		return "", fmt.Errorf("click target %+v not found on %s", target, rawURL)
	}
	var before string
	if err := chromedp.Run(tctx, chromedp.Location(&before), chromedp.Click(`[data-crawler-click="1"]`, chromedp.ByQuery)); err != nil {
		return "", fmt.Errorf("click: %w", err)
	}
	for {
		var now string
		if err := chromedp.Run(tctx, chromedp.Location(&now)); err == nil && now != before {
			return now, nil
		}
		if err := sleep(tctx, 200*time.Millisecond); err != nil {
			return "", fmt.Errorf("%w: %s", ErrNoNavigation, rawURL)
		}
	}
}
