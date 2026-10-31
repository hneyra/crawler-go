package postgres_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hneyra/crawler-go/internal/jobs"
	"github.com/hneyra/crawler-go/internal/platform/dbtest"
	"github.com/hneyra/crawler-go/internal/postgres"
)

func TestJobQueueConcurrentClaimNoDoubleProcessing(t *testing.T) {
	pool := dbtest.New(t)
	q := postgres.JobQueue{Pool: pool}
	ctx := context.Background()
	const n = 60
	for i := range n {
		if _, _, err := q.Enqueue(ctx, "t", map[string]int{"i": i}, jobs.EnqueueOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	seen := map[int64]int{}
	var wg sync.WaitGroup
	for w := range 6 {
		wg.Go(func() {
			worker := fmt.Sprintf("w%d", w)
			for {
				got, err := q.Claim(ctx, worker, 3, time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				if len(got) == 0 {
					return
				}
				for _, j := range got {
					mu.Lock()
					seen[j.ID]++
					mu.Unlock()
					if err := q.Complete(ctx, j.ID, worker); err != nil {
						t.Error(err)
					}
				}
			}
		})
	}
	wg.Wait()
	if len(seen) != n {
		t.Fatalf("processed %d of %d", len(seen), n)
	}
	for id, c := range seen {
		if c != 1 {
			t.Fatalf("job %d processed %d times", id, c)
		}
	}
}

func TestJobQueueLeaseExpiryResumes(t *testing.T) {
	pool := dbtest.New(t)
	q := postgres.JobQueue{Pool: pool}
	ctx := context.Background()
	id, _, err := q.Enqueue(ctx, "t", nil, jobs.EnqueueOptions{MaxAttempts: 2})
	if err != nil {
		t.Fatal(err)
	}
	// long lease + explicit expiry: no timing race (was 50 ms lease + sleep, flaky under -race).
	got, _ := q.Claim(ctx, "dead", 1, time.Minute)
	if len(got) != 1 {
		t.Fatalf("claim %v", got)
	}
	// still leased -> nobody else gets it
	if again, _ := q.Claim(ctx, "other", 1, time.Minute); len(again) != 0 {
		t.Fatal("leased job reclaimed early")
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET locked_until = now() - interval '1 second' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	again, err := q.Claim(ctx, "other", 1, time.Minute)
	if err != nil || len(again) != 1 || again[0].ID != id || again[0].Attempts != 2 {
		t.Fatalf("resume: %v %v", again, err)
	}
	// dead worker cannot finish or heartbeat anymore
	if err := q.Complete(ctx, id, "dead"); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatalf("stale owner complete: %v", err)
	}
	if err := q.Heartbeat(ctx, id, "dead", time.Minute); !errors.Is(err, jobs.ErrLeaseLost) {
		t.Fatalf("stale owner heartbeat: %v", err)
	}
	if err := q.Heartbeat(ctx, id, "other", time.Minute); err != nil {
		t.Fatal(err)
	}
	// expire again on last attempt -> failed, not reclaimed forever
	if _, err := pool.Exec(ctx, `UPDATE jobs SET locked_until = now() - interval '1 second' WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if got, _ := q.Claim(ctx, "third", 1, time.Minute); len(got) != 0 {
		t.Fatalf("exhausted job reclaimed: %v", got)
	}
	var status string
	_ = pool.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1`, id).Scan(&status)
	if status != "failed" {
		t.Fatalf("status %s", status)
	}
}

func TestJobQueueRetriesBounded(t *testing.T) {
	pool := dbtest.New(t)
	q := postgres.JobQueue{Pool: pool}
	ctx := context.Background()
	id, _, _ := q.Enqueue(ctx, "t", nil, jobs.EnqueueOptions{MaxAttempts: 3})
	for attempt := 1; attempt <= 3; attempt++ {
		got, err := q.Claim(ctx, "w", 1, time.Minute)
		if err != nil || len(got) != 1 || got[0].Attempts != attempt {
			t.Fatalf("attempt %d: %v %v", attempt, got, err)
		}
		// retry asked even on last attempt -> queue still fails it
		if err := q.Fail(ctx, id, "w", "boom", time.Now().Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := q.Claim(ctx, "w", 1, time.Minute); len(got) != 0 {
		t.Fatalf("claimed after max attempts: %v", got)
	}
	var status, lastErr string
	_ = pool.QueryRow(ctx, `SELECT status, last_error FROM jobs WHERE id = $1`, id).Scan(&status, &lastErr)
	if status != "failed" || lastErr != "boom" {
		t.Fatalf("%s %q", status, lastErr)
	}
	// future retry not claimable before run_at
	id2, _, _ := q.Enqueue(ctx, "t", nil, jobs.EnqueueOptions{})
	got, _ := q.Claim(ctx, "w", 1, time.Minute)
	if len(got) != 1 || got[0].ID != id2 {
		t.Fatal(got)
	}
	_ = q.Fail(ctx, id2, "w", "later", time.Now().Add(time.Hour))
	if got, _ := q.Claim(ctx, "w", 1, time.Minute); len(got) != 0 {
		t.Fatal("backoff ignored")
	}
	// release on shutdown does not spend attempt
	id3, _, _ := q.Enqueue(ctx, "t", nil, jobs.EnqueueOptions{})
	got, _ = q.Claim(ctx, "w", 1, time.Minute)
	if len(got) != 1 || got[0].ID != id3 {
		t.Fatal(got)
	}
	if err := q.Release(ctx, id3, "w"); err != nil {
		t.Fatal(err)
	}
	got, _ = q.Claim(ctx, "w", 1, time.Minute)
	if len(got) != 1 || got[0].Attempts != 1 {
		t.Fatalf("after release: %v", got)
	}
}

func TestJobQueueDedupe(t *testing.T) {
	pool := dbtest.New(t)
	q := postgres.JobQueue{Pool: pool}
	ctx := context.Background()
	a, d1, err := q.Enqueue(ctx, "k", nil, jobs.EnqueueOptions{DedupeKey: "x"})
	if err != nil || d1 {
		t.Fatal(err, d1)
	}
	b, d2, err := q.Enqueue(ctx, "k", nil, jobs.EnqueueOptions{DedupeKey: "x"})
	if err != nil || !d2 || a != b {
		t.Fatalf("dedupe: %d %d %v %v", a, b, d2, err)
	}
	// other kind, same key -> separate
	if c, d, _ := q.Enqueue(ctx, "other", nil, jobs.EnqueueOptions{DedupeKey: "x"}); d || c == a {
		t.Fatal("dedupe crossed kinds")
	}
	// finished -> same key allowed again
	got, _ := q.Claim(ctx, "w", 5, time.Minute)
	for _, j := range got {
		_ = q.Complete(ctx, j.ID, "w")
	}
	if c, d, _ := q.Enqueue(ctx, "k", nil, jobs.EnqueueOptions{DedupeKey: "x"}); d || c == a {
		t.Fatal("finished job still dedupes")
	}
}

func TestRunnerEndToEndRetriesThenSucceeds(t *testing.T) {
	pool := dbtest.New(t)
	q := postgres.JobQueue{Pool: pool}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls atomic.Int32
	r := &jobs.Runner{Queue: q, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Concurrency: 3, Lease: time.Second,
		PollMin: 5 * time.Millisecond, PollMax: 20 * time.Millisecond, BackoffBase: time.Millisecond, BackoffMax: 5 * time.Millisecond}
	r.Register("flaky", func(context.Context, jobs.Job) error {
		if calls.Add(1) < 3 {
			return errors.New("flaky")
		}
		return nil
	})
	r.Register("crash", func(context.Context, jobs.Job) error { panic("bad") })
	id, _, _ := q.Enqueue(ctx, "flaky", nil, jobs.EnqueueOptions{MaxAttempts: 5})
	crash, _, _ := q.Enqueue(ctx, "crash", nil, jobs.EnqueueOptions{MaxAttempts: 2})
	done := make(chan struct{})
	go func() { _ = r.Run(ctx); close(done) }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var s1, s2 string
		_ = pool.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1`, id).Scan(&s1)
		_ = pool.QueryRow(ctx, `SELECT status FROM jobs WHERE id = $1`, crash).Scan(&s2)
		if s1 == "succeeded" && s2 == "failed" {
			cancel()
			<-done
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("jobs did not settle")
}
