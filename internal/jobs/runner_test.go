package jobs_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/hneyra/crawler-go/internal/jobs"
)

// fakeQueue records outcomes. claim pops from list.
type fakeQueue struct {
	mu        sync.Mutex
	ready     []jobs.Job
	completed []int64
	failed    map[int64]time.Time
	released  []int64
	hbErr     error
	hbs       int
}

func (f *fakeQueue) Enqueue(context.Context, string, any, jobs.EnqueueOptions) (int64, bool, error) {
	return 0, false, nil
}

func (f *fakeQueue) Claim(_ context.Context, _ string, n int, _ time.Duration) ([]jobs.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.ready) == 0 {
		return nil, nil
	}
	n = min(n, len(f.ready))
	out := f.ready[:n]
	f.ready = f.ready[n:]
	return out, nil
}

func (f *fakeQueue) Heartbeat(context.Context, int64, string, time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hbs++
	return f.hbErr
}

func (f *fakeQueue) Complete(_ context.Context, id int64, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.completed = append(f.completed, id)
	return nil
}

func (f *fakeQueue) Fail(_ context.Context, id int64, _ string, _ string, retryAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failed == nil {
		f.failed = map[int64]time.Time{}
	}
	f.failed[id] = retryAt
	return nil
}

func (f *fakeQueue) Release(_ context.Context, id int64, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, id)
	return nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newRunner(q *fakeQueue) *jobs.Runner {
	return &jobs.Runner{Queue: q, Log: quiet(), Worker: "w", Lease: 30 * time.Millisecond, PollMin: time.Millisecond, PollMax: 5 * time.Millisecond, Grace: 50 * time.Millisecond}
}

func TestRunOneOutcomes(t *testing.T) {
	q := &fakeQueue{}
	r := newRunner(q)
	r.Register("ok", func(context.Context, jobs.Job) error { return nil })
	r.Register("boom", func(context.Context, jobs.Job) error { return errors.New("transient") })
	r.Register("perm", func(context.Context, jobs.Job) error { return jobs.Permanent(errors.New("bad input")) })
	r.Register("panic", func(context.Context, jobs.Job) error { panic("kaboom") })
	ctx := context.Background()

	r.RunOne(ctx, "w", jobs.Job{ID: 1, Kind: "ok", Attempts: 1, MaxAttempts: 3})
	r.RunOne(ctx, "w", jobs.Job{ID: 2, Kind: "boom", Attempts: 1, MaxAttempts: 3})
	r.RunOne(ctx, "w", jobs.Job{ID: 3, Kind: "boom", Attempts: 3, MaxAttempts: 3})
	r.RunOne(ctx, "w", jobs.Job{ID: 4, Kind: "perm", Attempts: 1, MaxAttempts: 3})
	r.RunOne(ctx, "w", jobs.Job{ID: 5, Kind: "panic", Attempts: 1, MaxAttempts: 3})
	r.RunOne(ctx, "w", jobs.Job{ID: 6, Kind: "unknown", Attempts: 1, MaxAttempts: 3})

	if len(q.completed) != 1 || q.completed[0] != 1 {
		t.Fatalf("completed %v", q.completed)
	}
	if q.failed[2].IsZero() {
		t.Fatal("transient error must schedule retry")
	}
	for _, id := range []int64{3, 4, 6} {
		at, ok := q.failed[id]
		if !ok || !at.IsZero() {
			t.Fatalf("job %d: want final failure, got %v %v", id, ok, at)
		}
	}
	if at, ok := q.failed[5]; !ok || at.IsZero() {
		t.Fatalf("panic must fail with retry, got %v %v", ok, at)
	}
}

func TestHeartbeatAndLeaseLoss(t *testing.T) {
	q := &fakeQueue{}
	r := newRunner(q)
	r.Register("slow", func(ctx context.Context, _ jobs.Job) error {
		<-ctx.Done()
		return ctx.Err()
	})
	q.hbErr = jobs.ErrLeaseLost
	done := make(chan struct{})
	go func() {
		r.RunOne(context.Background(), "w", jobs.Job{ID: 9, Kind: "slow", Attempts: 1, MaxAttempts: 3})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("lease loss must cancel job")
	}
	if len(q.completed) != 0 || len(q.failed) != 0 || q.hbs == 0 {
		t.Fatalf("lease lost: no bookkeeping expected. completed=%v failed=%v hbs=%d", q.completed, q.failed, q.hbs)
	}
}

func TestShutdownReleasesStuckJob(t *testing.T) {
	q := &fakeQueue{ready: []jobs.Job{{ID: 7, Kind: "stuck", Attempts: 1, MaxAttempts: 3}}}
	r := newRunner(q)
	started := make(chan struct{})
	r.Register("stuck", func(ctx context.Context, _ jobs.Job) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Run(ctx); close(done) }()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not stop")
	}
	if len(q.released) != 1 || q.released[0] != 7 {
		t.Fatalf("released %v failed %v", q.released, q.failed)
	}
}

func TestShutdownLetsShortJobFinish(t *testing.T) {
	q := &fakeQueue{ready: []jobs.Job{{ID: 8, Kind: "short", Attempts: 1, MaxAttempts: 3}}}
	r := newRunner(q)
	r.Grace = time.Second
	started := make(chan struct{})
	r.Register("short", func(ctx context.Context, _ jobs.Job) error {
		close(started)
		time.Sleep(20 * time.Millisecond)
		return ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = r.Run(ctx); close(done) }()
	<-started
	cancel()
	<-done
	if len(q.completed) != 1 {
		t.Fatalf("job inside grace must complete: %v released=%v", q.completed, q.released)
	}
}

func TestBackoffBoundedWithJitter(t *testing.T) {
	base, maxD := time.Second, 10*time.Second
	for attempt := 1; attempt < 20; attempt++ {
		d := jobs.Backoff(attempt, base, maxD)
		if d <= 0 || d > maxD {
			t.Fatalf("attempt %d: %v", attempt, d)
		}
	}
	if d := jobs.Backoff(1, base, maxD); d < base/2 || d > base {
		t.Fatalf("first backoff %v", d)
	}
}

// liveness: loop ticks while idle; busy job heartbeat ticks too; zero before run.
func TestLastTickHeartbeat(t *testing.T) {
	q := &fakeQueue{}
	r := newRunner(q)
	if !r.LastTick().IsZero() {
		t.Fatal("tick before run")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = r.Run(ctx)
	}()
	deadline := time.Now().Add(time.Second)
	for r.LastTick().IsZero() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	first := r.LastTick()
	if first.IsZero() {
		t.Fatal("no tick")
	}
	time.Sleep(20 * time.Millisecond)
	if !r.LastTick().After(first) {
		t.Fatal("idle loop stopped ticking")
	}
	cancel()
	<-done
	stopped := r.LastTick()
	time.Sleep(20 * time.Millisecond)
	if !r.LastTick().Equal(stopped) {
		t.Fatal("tick after stop")
	}

	// long job: job heartbeat keeps tick fresh.
	r2 := newRunner(q)
	block := make(chan struct{})
	r2.Register("slow", func(context.Context, jobs.Job) error { <-block; return nil })
	ran := make(chan struct{})
	go func() {
		defer close(ran)
		r2.RunOne(context.Background(), "w", jobs.Job{ID: 9, Kind: "slow", MaxAttempts: 1, Attempts: 1})
	}()
	time.Sleep(100 * time.Millisecond)
	if age := time.Since(r2.LastTick()); age > 60*time.Millisecond {
		t.Fatalf("busy runner tick stale: %v", age)
	}
	close(block)
	<-ran
}
