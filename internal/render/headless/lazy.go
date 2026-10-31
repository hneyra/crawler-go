package headless

import (
	"context"
	"sync"
	"time"

	"github.com/hneyra/crawler-go/internal/fetch"
)

// Lazy starts chrome on first use. most sites never need it -> no browser cost.
// start failure is retried on next use (chrome may come back).
type Lazy struct {
	Ctx  context.Context // startup ctx; stop with Close
	Opts Options

	mu sync.Mutex
	r  *Renderer
}

var _ fetch.Renderer = (*Lazy)(nil)

func (l *Lazy) get() (*Renderer, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.r != nil && l.r.Alive() {
		return l.r, nil
	}
	if l.r != nil {
		// chrome died (crash, oom kill): restart instead of failing forever.
		l.r.Close()
		l.r = nil
	}
	r, err := New(l.Ctx, l.Opts)
	if err != nil {
		return nil, err
	}
	l.r = r
	return r, nil
}

// Render = Renderer.Render after lazy start.
func (l *Lazy) Render(ctx context.Context, req fetch.RenderRequest) (fetch.RenderResult, error) {
	r, err := l.get()
	if err != nil {
		return fetch.RenderResult{}, err
	}
	return r.Render(ctx, req)
}

// ClickNavigate = Renderer.ClickNavigate after lazy start.
func (l *Lazy) ClickNavigate(ctx context.Context, url string, t fetch.ClickTarget, timeout time.Duration) (string, error) {
	r, err := l.get()
	if err != nil {
		return "", err
	}
	return r.ClickNavigate(ctx, url, t, timeout)
}

// Close stops chrome if started.
func (l *Lazy) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.r != nil {
		l.r.Close()
		l.r = nil
	}
}
