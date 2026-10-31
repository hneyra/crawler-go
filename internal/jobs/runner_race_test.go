package jobs_test

import (
	"context"
	"sync"
	"testing"

	"github.com/hneyra/crawler-go/internal/jobs"
)

// zero-config runner used from many goroutines: defaults set once, no data race (issue #63 L4). run with -race.
func TestRunOneDefaultsNoRace(t *testing.T) {
	q := &fakeQueue{}
	r := &jobs.Runner{Queue: q, Log: quiet()}
	r.Register("k", func(context.Context, jobs.Job) error { return nil })
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			r.RunOne(context.Background(), "w", jobs.Job{ID: int64(i), Kind: "k", Attempts: 1, MaxAttempts: 3})
		})
	}
	wg.Wait()
	if len(q.completed) != 8 {
		t.Fatalf("completed %d", len(q.completed))
	}
}
