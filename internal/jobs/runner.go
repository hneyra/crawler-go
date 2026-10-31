package jobs

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// Handler runs one job. ctx canceled on shutdown grace end or lease loss.
// return nil = done. error = retry with backoff. Permanent(err) = no retry.
type Handler func(ctx context.Context, j Job) error

// Runner = worker pool over Queue. registry kind -> handler.
type Runner struct {
	Queue       Queue
	Log         *slog.Logger
	Worker      string        // lock owner id. unique per process.
	Concurrency int           // goroutines
	Lease       time.Duration // lease per claim. heartbeat every Lease/3
	PollMin     time.Duration // idle poll start
	PollMax     time.Duration // idle poll cap
	BackoffBase time.Duration // retry delay base. doubles per attempt
	BackoffMax  time.Duration
	Grace       time.Duration // shutdown: running job gets this long, then canceled + released

	mu       sync.RWMutex
	handlers map[string]Handler
	tick     atomic.Int64 // unix nano of last loop pass / job heartbeat. liveness.
	once     sync.Once    // defaults normalized once: RunOne called from many goroutines
}

// LastTick = last sign of life (idle poll or running job heartbeat). zero before Run.
func (r *Runner) LastTick() time.Time {
	n := r.tick.Load()
	if n == 0 {
		return time.Time{}
	}
	return time.Unix(0, n)
}

func (r *Runner) beat() { r.tick.Store(time.Now().UnixNano()) }

// Register handler for kind. call before Run.
func (r *Runner) Register(kind string, h Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handlers == nil {
		r.handlers = map[string]Handler{}
	}
	r.handlers[kind] = h
}

func (r *Runner) handler(kind string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.handlers[kind]
	return h, ok
}

func (r *Runner) defaults() { r.once.Do(r.setDefaults) }

func (r *Runner) setDefaults() {
	if r.Log == nil {
		r.Log = slog.Default()
	}
	if r.Concurrency < 1 {
		r.Concurrency = 1
	}
	if r.Lease <= 0 {
		r.Lease = 2 * time.Minute
	}
	if r.PollMin <= 0 {
		r.PollMin = 200 * time.Millisecond
	}
	if r.PollMax < r.PollMin {
		r.PollMax = 5 * time.Second
	}
	if r.BackoffBase <= 0 {
		r.BackoffBase = 10 * time.Second
	}
	if r.BackoffMax < r.BackoffBase {
		r.BackoffMax = 30 * time.Minute
	}
	if r.Grace <= 0 {
		r.Grace = 20 * time.Second
	}
	if r.Worker == "" {
		r.Worker = "worker-" + randHex()
	}
}

// Run blocks until ctx done and all workers stopped.
func (r *Runner) Run(ctx context.Context) error {
	r.defaults()
	var wg sync.WaitGroup
	for i := range r.Concurrency {
		wg.Go(func() { r.loop(ctx, fmt.Sprintf("%s/%d", r.Worker, i)) })
	}
	wg.Wait()
	return nil
}

func (r *Runner) loop(ctx context.Context, worker string) {
	wait := r.PollMin
	for ctx.Err() == nil {
		r.beat()
		got, err := r.Queue.Claim(ctx, worker, 1, r.Lease)
		if err != nil {
			if ctx.Err() == nil {
				r.Log.ErrorContext(ctx, "claim jobs", "err", err)
			}
			wait = min(wait*2, r.PollMax)
			sleep(ctx, wait)
			continue
		}
		if len(got) == 0 {
			sleep(ctx, wait)
			wait = min(wait*2, r.PollMax)
			continue
		}
		wait = r.PollMin
		for _, j := range got {
			r.RunOne(ctx, worker, j)
		}
	}
}

// RunOne executes claimed job with heartbeat, panic guard, retry policy. exported for tests.
func (r *Runner) RunOne(parent context.Context, worker string, j Job) {
	r.defaults()
	// job ctx outlives parent by Grace: lets job finish on shutdown.
	jobCtx, cancel := context.WithCancelCause(context.WithoutCancel(parent))
	defer cancel(nil)
	stopGrace := context.AfterFunc(parent, func() {
		t := time.NewTimer(r.Grace)
		defer t.Stop()
		select {
		case <-t.C:
			cancel(errShutdown)
		case <-jobCtx.Done():
		}
	})
	defer stopGrace()

	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		r.heartbeat(jobCtx, worker, j, cancel)
	}()

	err := r.call(jobCtx, j)
	cause := context.Cause(jobCtx)
	cancel(nil)
	<-hbDone

	// bookkeeping must survive shutdown -> own short ctx.
	bctx, bcancel := context.WithTimeout(context.WithoutCancel(parent), 10*time.Second)
	defer bcancel()
	log := r.Log.With("job_id", j.ID, "kind", j.Kind, "attempt", j.Attempts)
	switch {
	case errors.Is(cause, ErrLeaseLost):
		log.WarnContext(bctx, "job lease lost; other worker may resume")
	case err == nil:
		if cerr := r.Queue.Complete(bctx, j.ID, worker); cerr != nil {
			log.ErrorContext(bctx, "complete job", "err", cerr)
		}
	case errors.Is(cause, errShutdown):
		if rerr := r.Queue.Release(bctx, j.ID, worker); rerr != nil {
			log.ErrorContext(bctx, "release job", "err", rerr)
		}
		log.InfoContext(bctx, "job released on shutdown")
	default:
		retryAt := time.Time{}
		if !IsPermanent(err) && !j.LastAttempt() {
			retryAt = time.Now().Add(Backoff(j.Attempts, r.BackoffBase, r.BackoffMax))
		}
		msg := err.Error()
		if len(msg) > 4000 {
			msg = msg[:4000]
		}
		if ferr := r.Queue.Fail(bctx, j.ID, worker, msg, retryAt); ferr != nil {
			log.ErrorContext(bctx, "fail job", "err", ferr)
		}
		log.WarnContext(bctx, "job failed", "err", err, "retry_at", retryAt)
	}
}

var errShutdown = errors.New("worker shutdown")

// call runs handler. panic -> error, process survives.
func (r *Runner) call(ctx context.Context, j Job) (err error) {
	h, ok := r.handler(j.Kind)
	if !ok {
		return Permanent(fmt.Errorf("no handler for job kind %q", j.Kind))
	}
	defer func() {
		if p := recover(); p != nil {
			r.Log.ErrorContext(ctx, "job panic", "job_id", j.ID, "panic", p, "stack", string(debug.Stack()))
			err = fmt.Errorf("handler panic: %v", p)
		}
	}()
	return h(ctx, j)
}

func (r *Runner) heartbeat(ctx context.Context, worker string, j Job, cancel context.CancelCauseFunc) {
	t := time.NewTicker(max(r.Lease/3, 10*time.Millisecond))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.beat() // busy worker still alive
			err := r.Queue.Heartbeat(ctx, j.ID, worker, r.Lease)
			if errors.Is(err, ErrLeaseLost) {
				cancel(ErrLeaseLost)
				return
			}
			if err != nil && ctx.Err() == nil {
				r.Log.WarnContext(ctx, "heartbeat", "job_id", j.ID, "err", err)
			}
		}
	}
}

// Backoff = base * 2^(attempt-1), capped, full jitter in [d/2, d].
func Backoff(attempt int, base, maxD time.Duration) time.Duration {
	d := base
	for i := 1; i < attempt && d < maxD; i++ {
		d *= 2
	}
	d = min(d, maxD)
	half := d / 2
	if half <= 0 {
		return d
	}
	j, err := rand.Int(rand.Reader, big.NewInt(int64(half)+1))
	if err != nil {
		return d
	}
	return half + time.Duration(j.Int64())
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func randUint64() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return binary.LittleEndian.Uint64(b[:])
}

func randHex() string {
	return fmt.Sprintf("%016x", randUint64())
}
