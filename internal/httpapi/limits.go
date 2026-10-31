package httpapi

import (
	"context"
	"sync"

	"github.com/hneyra/crawler-go/internal/platform/httpx"
)

// Limits = rate classes. per replica. nil limiter = unlimited.
type Limits struct {
	Auth  *httpx.Limiter // login: per ip
	Write *httpx.Limiter // unsafe ops: per user
}

// DefaultLimits = sane values. tune with measurements.
func DefaultLimits() *Limits {
	return &Limits{Auth: httpx.NewLimiter(10, 5), Write: httpx.NewLimiter(120, 30)}
}

// NoLimits = unlimited. tests/load only; config refuses it in prod.
func NoLimits() *Limits { return &Limits{} }

// Run evicts idle buckets until ctx done.
func (l *Limits) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, x := range []*httpx.Limiter{l.Auth, l.Write} {
		if x != nil {
			wg.Go(func() { x.Run(ctx) })
		}
	}
	wg.Wait()
}
