package httpx

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiter = per-key token bucket. in-process -> per replica limit (ADR-0006).
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	limit   rate.Limit
	burst   int
	ttl     time.Duration
}

type bucket struct {
	l    *rate.Limiter
	seen time.Time
}

// NewLimiter: perMinute steady rate, burst max.
func NewLimiter(perMinute float64, burst int) *Limiter {
	return &Limiter{
		buckets: map[string]*bucket{},
		limit:   rate.Limit(perMinute / 60),
		burst:   burst,
		ttl:     10 * time.Minute,
	}
}

// Allow takes one token for key. nil limiter = no limit.
func (l *Limiter) Allow(key string) bool {
	if l == nil {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{l: rate.NewLimiter(l.limit, l.burst)}
		l.buckets[key] = b
	}
	b.seen = now
	return b.l.AllowN(now, 1)
}

// Run evicts idle buckets until ctx done. caller owns goroutine lifetime.
func (l *Limiter) Run(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			l.mu.Lock()
			for k, b := range l.buckets {
				if now.Sub(b.seen) > l.ttl {
					delete(l.buckets, k)
				}
			}
			l.mu.Unlock()
		}
	}
}
