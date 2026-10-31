package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/hneyra/crawler-go/internal/jobs"
	"github.com/hneyra/crawler-go/internal/metrics"
	"github.com/hneyra/crawler-go/internal/pipeline"
	"github.com/hneyra/crawler-go/internal/platform/db"
	"github.com/hneyra/crawler-go/internal/postgres"
)

// periodic job kinds. replace java FailedUrlRetryScheduler (+ repairs java never had).
const (
	jobRetryFailed = "maintenance.retry_failed" // FAILED below retry budget -> PENDING
	jobRescueStuck = "maintenance.rescue_stuck" // IN_PROGRESS of dead workers -> PENDING
	jobPurge       = "maintenance.purge"        // old finished jobs + idle login throttle rows
	purgeAfter     = 7 * 24 * time.Hour
	workerLease    = 30 * time.Second
	reseedEvery    = 30 * time.Minute
)

func pipelineService(d deps) *pipeline.Service {
	c := catalog(d)
	return &pipeline.Service{
		Tx: db.Runner{Pool: d.pool}, Runs: postgres.Runs{Pool: d.pool},
		Sites: c.Sites, Categories: c.Categories, Jobs: postgres.JobQueue{Pool: d.pool}, Log: d.log,
	}
}

// seededRunner enqueues periodic jobs once, then runs pool.
type seededRunner struct {
	r    *jobs.Runner
	seed func(ctx context.Context) error
}

func (s seededRunner) LastTick() time.Time { return s.r.LastTick() }

// Run seeds, then re-seeds every reseedEvery: a chain broken by a long db outage
// (re-enqueue out of attempts) comes back without a restart. dedupe per slot = no duplicates.
func (s seededRunner) Run(ctx context.Context) error {
	if err := s.seed(ctx); err != nil {
		return fmt.Errorf("seed periodic jobs: %w", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(reseedEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = s.seed(ctx) // next tick retries
			}
		}
	}()
	err := s.r.Run(ctx)
	<-done
	return err
}

// wireWorker = job runner with crawl + maintenance handlers. reg gets crawler_* metrics.
func wireWorker(ctx context.Context, d deps, reg prometheus.Registerer) (seededRunner, func()) {
	cr := wireCrawl(ctx, d)
	m := metrics.NewCrawl(reg)
	cr.fetcher.Observer = m
	cr.extraction.Observer = m

	p := pipelineService(d)
	p.Discovery, p.Extraction, p.Observer = cr.discovery, cr.extraction, m

	queue := postgres.JobQueue{Pool: d.pool}
	urls := postgres.URLs{Pool: d.pool}
	ids := postgres.Identity{Pool: d.pool}
	c := d.cfg.Crawl
	host, _ := os.Hostname()
	// lease 30s -> heartbeat (and liveness tick) every 10s: busy worker stays healthy under
	// workerHealthMaxAge (30s); dead worker's job reclaimed within 30s.
	r := &jobs.Runner{Queue: queue, Log: d.log, Worker: host + "-" + strconv.Itoa(os.Getpid()), Concurrency: d.cfg.WorkerConcurrency, Lease: workerLease}
	r.Register(pipeline.JobRun, p.Handle)

	stuckEvery := max(c.StuckAfter/2, time.Minute)
	r.Register(jobRetryFailed, jobs.Periodic(queue, jobRetryFailed, c.RetryInterval, func(ctx context.Context) error {
		n, err := urls.ResetFailed(ctx, c.RetryMax)
		if n > 0 {
			d.log.Info("failed urls re-queued", "count", n)
		}
		return err
	}))
	r.Register(jobRescueStuck, jobs.Periodic(queue, jobRescueStuck, stuckEvery, func(ctx context.Context) error {
		n, err := urls.RescueStuck(ctx, time.Now().Add(-c.StuckAfter))
		if n > 0 {
			d.log.Warn("stuck urls rescued", "count", n)
		}
		return err
	}))
	r.Register(jobPurge, jobs.Periodic(queue, jobPurge, time.Hour, func(ctx context.Context) error {
		if _, err := queue.PurgeFinished(ctx, time.Now().Add(-purgeAfter)); err != nil {
			return err
		}
		_, err := ids.PurgeLoginThrottle(ctx, time.Now().Add(-24*time.Hour))
		return err
	}))
	seed := func(ctx context.Context) error {
		now := time.Now()
		for kind, every := range map[string]time.Duration{jobRetryFailed: c.RetryInterval, jobRescueStuck: stuckEvery, jobPurge: time.Hour} {
			if err := jobs.EnqueuePeriodic(ctx, queue, kind, every, now); err != nil {
				return err
			}
		}
		return nil
	}
	return seededRunner{r: r, seed: seed}, cr.renderer.Close
}
