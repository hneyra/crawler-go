package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hneyra/crawler-go/internal/jobs"
)

type recQueue struct {
	fakeQueue
	keys  []string
	runAt []time.Time
}

func (r *recQueue) Enqueue(_ context.Context, _ string, _ any, o jobs.EnqueueOptions) (int64, bool, error) {
	r.keys = append(r.keys, o.DedupeKey)
	r.runAt = append(r.runAt, o.RunAt)
	return 1, false, nil
}

// next slot queued before work, even when work fails (chain never breaks).
func TestPeriodicQueuesNextBeforeWork(t *testing.T) {
	q := &recQueue{}
	h := jobs.Periodic(q, "k", 10*time.Minute, func(context.Context) error { return errors.New("db down") })
	if err := h(context.Background(), jobs.Job{}); err == nil {
		t.Fatal("work error lost")
	}
	if len(q.keys) != 1 || time.Until(q.runAt[0]) < 9*time.Minute {
		t.Fatalf("next: %v %v", q.keys, q.runAt)
	}
	now := time.Date(2026, 1, 1, 10, 7, 0, 0, time.UTC)
	_ = jobs.EnqueuePeriodic(context.Background(), q, "k", 10*time.Minute, now)
	_ = jobs.EnqueuePeriodic(context.Background(), q, "k", 10*time.Minute, now.Add(2*time.Minute))
	if q.keys[1] != q.keys[2] || q.keys[1] != "slot-2026-01-01T10:00:00Z" {
		t.Fatalf("slot keys %v", q.keys)
	}
}
