package jobs

import (
	"context"
	"time"
)

// EnqueuePeriodic queues kind for slot of at. dedupe per slot -> many workers seed once.
func EnqueuePeriodic(ctx context.Context, q Enqueuer, kind string, every time.Duration, at time.Time) error {
	key := "slot-" + at.UTC().Truncate(every).Format(time.RFC3339)
	_, _, err := q.Enqueue(ctx, kind, nil, EnqueueOptions{DedupeKey: key, RunAt: at, MaxAttempts: 3})
	return err
}

// Periodic = self re-queuing handler. next run queued BEFORE work: failing work never breaks chain.
func Periodic(q Enqueuer, kind string, every time.Duration, work func(ctx context.Context) error) Handler {
	return func(ctx context.Context, _ Job) error {
		if err := EnqueuePeriodic(ctx, q, kind, every, time.Now().Add(every)); err != nil {
			return err
		}
		return work(ctx)
	}
}
