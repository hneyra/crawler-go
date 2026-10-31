// Package pipeline = crawl runs on the job queue: request (api) -> job -> discovery/extraction (worker).
// replaces java spring batch crawlJob + async launchers + FailedUrlRetryScheduler.
package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/discovery"
	"github.com/hneyra/crawler-go/internal/extraction"
	"github.com/hneyra/crawler-go/internal/jobs"
)

// JobRun = job kind executing one crawl_run.
const JobRun = "crawl.run"

// Discoverer = discovery.Service.
type Discoverer interface {
	DiscoverCategory(ctx context.Context, site crawl.Site, cat crawl.Category) (discovery.Result, error)
}

// Extractor = extraction.Service.
type Extractor interface {
	ProcessBatch(ctx context.Context, siteID int64) (extraction.Batch, error)
}

// Service = run requests + job handler.
type Service struct {
	Tx         crawl.Tx
	Runs       crawl.RunStore
	Sites      crawl.SiteStore
	Categories crawl.CategoryStore
	Jobs       jobs.Enqueuer
	Discovery  Discoverer // worker only
	Extraction Extractor  // worker only
	Log        *slog.Logger
	Observer   Observer // metrics. nil = none
	// MaxBatches caps extract loop per run (runaway guard). default 10000.
	MaxBatches int
}

// Observer = metrics hook.
type Observer interface {
	DiscoveryDone(d time.Duration, r discovery.Result)
	BatchDone(d time.Duration, b extraction.Batch)
}

type payload struct {
	RunID int64 `json:"run_id"`
}

var errDeduped = errors.New("deduped")

// Request queues a run. same kind+target already QUEUED/RUNNING -> that run (deduped=true).
func (s *Service) Request(ctx context.Context, kind crawl.RunKind, siteID, categoryID int64) (crawl.Run, bool, error) {
	if !kind.Valid() {
		return crawl.Run{}, false, apperr.Invalid("invalid_run_kind", "Tipo de ejecución inválido")
	}
	if kind == crawl.RunDiscoverCategory {
		cat, err := s.Categories.GetCategory(ctx, categoryID)
		if err != nil {
			return crawl.Run{}, false, err
		}
		siteID = cat.SiteID
	} else {
		categoryID = 0
		if _, err := s.Sites.GetSite(ctx, siteID); err != nil {
			return crawl.Run{}, false, err
		}
	}
	key := string(kind) + ":" + strconv.FormatInt(siteID, 10)
	if categoryID != 0 {
		key += ":" + strconv.FormatInt(categoryID, 10)
	}
	var run crawl.Run
	err := s.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		if run, err = s.Runs.CreateRun(ctx, crawl.Run{SiteID: siteID, CategoryID: categoryID, Kind: kind}); err != nil {
			return err
		}
		jobID, deduped, err := s.Jobs.Enqueue(ctx, JobRun, payload{RunID: run.ID}, jobs.EnqueueOptions{DedupeKey: key, MaxAttempts: 3})
		if err != nil {
			return err
		}
		if deduped {
			return errDeduped // rollback new run
		}
		run.JobID = jobID
		return s.Runs.SetRunJob(ctx, run.ID, jobID)
	})
	if errors.Is(err, errDeduped) {
		active, aerr := s.Runs.ActiveRun(ctx, kind, siteID, categoryID)
		return active, true, aerr
	}
	if err != nil {
		return crawl.Run{}, false, fmt.Errorf("request %s run: %w", kind, err)
	}
	return run, false, nil
}

// Handle = jobs.Handler for JobRun.
func (s *Service) Handle(ctx context.Context, j jobs.Job) error {
	var p payload
	if err := json.Unmarshal(j.Payload, &p); err != nil || p.RunID == 0 {
		return jobs.Permanent(fmt.Errorf("bad crawl.run payload: %s", j.Payload))
	}
	run, err := s.Runs.GetRun(ctx, p.RunID)
	if apperr.IsKind(err, apperr.KindNotFound) {
		return nil // site deleted (cascade): nothing to do
	}
	if err != nil {
		return err
	}
	if run.Status == crawl.RunCompleted || run.Status == crawl.RunFailed {
		return nil
	}
	if err := s.Runs.StartRun(ctx, run.ID); err != nil {
		return err
	}
	log := s.log().With("run_id", run.ID, "site_id", run.SiteID, "kind", run.Kind)
	log.Info("run started", "attempt", j.Attempts)
	stats := map[string]any{}
	err = s.execute(ctx, run, stats)

	// bookkeeping survives shutdown.
	bctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	switch {
	case err == nil:
		log.Info("run completed")
		return s.Runs.FinishRun(bctx, run.ID, crawl.RunCompleted, stats, "")
	case ctx.Err() != nil:
		// shutdown: runner releases job, run goes back to queue.
		_ = s.Runs.FinishRun(bctx, run.ID, crawl.RunQueued, stats, "interrumpida por apagado del worker")
		return err
	case apperr.IsKind(err, apperr.KindNotFound):
		_ = s.Runs.FinishRun(bctx, run.ID, crawl.RunFailed, stats, err.Error())
		return jobs.Permanent(err)
	case jobs.IsPermanent(err) || j.LastAttempt():
		log.Warn("run failed", "err", err)
		_ = s.Runs.FinishRun(bctx, run.ID, crawl.RunFailed, stats, err.Error())
		return err
	default:
		log.Warn("run failed; will retry", "err", err)
		_ = s.Runs.FinishRun(bctx, run.ID, crawl.RunQueued, stats, err.Error())
		return err
	}
}

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Log
}

func (s *Service) execute(ctx context.Context, run crawl.Run, stats map[string]any) error {
	site, err := s.Sites.GetSite(ctx, run.SiteID)
	if err != nil {
		return err
	}
	switch run.Kind {
	case crawl.RunDiscoverCategory:
		cat, err := s.Categories.GetCategory(ctx, run.CategoryID)
		if err != nil {
			return err
		}
		res, err := s.discover(ctx, site, cat)
		stats["discovery"] = res
		return err
	case crawl.RunDiscoverSite:
		return s.discoverSite(ctx, run, site, stats)
	case crawl.RunExtractSite:
		return s.extractSite(ctx, run, stats)
	case crawl.RunCrawlSite:
		if err := s.discoverSite(ctx, run, site, stats); err != nil {
			return err
		}
		return s.extractSite(ctx, run, stats)
	default:
		return jobs.Permanent(fmt.Errorf("unknown run kind %q", run.Kind))
	}
}

func (s *Service) discover(ctx context.Context, site crawl.Site, cat crawl.Category) (discovery.Result, error) {
	start := time.Now()
	res, err := s.Discovery.DiscoverCategory(ctx, site, cat)
	if s.Observer != nil {
		s.Observer.DiscoveryDone(time.Since(start), res)
	}
	return res, err
}

func (s *Service) discoverSite(ctx context.Context, run crawl.Run, site crawl.Site, stats map[string]any) error {
	cats, err := s.Categories.EnabledCategories(ctx, site.ID)
	if err != nil {
		return err
	}
	var total discovery.Result
	for _, c := range cats {
		res, err := s.discover(ctx, site, c)
		total.Add(res)
		stats["discovery"] = total
		stats["categories"] = len(cats)
		if err != nil {
			return err
		}
		_ = s.Runs.UpdateRunStats(ctx, run.ID, stats) // progress only
	}
	stats["discovery"] = total
	stats["categories"] = len(cats)
	return nil
}

func (s *Service) extractSite(ctx context.Context, run crawl.Run, stats map[string]any) error {
	maxB := s.MaxBatches
	if maxB <= 0 {
		maxB = 10000
	}
	var total extraction.Batch
	for range maxB {
		start := time.Now()
		b, err := s.Extraction.ProcessBatch(ctx, run.SiteID)
		if s.Observer != nil {
			s.Observer.BatchDone(time.Since(start), b)
		}
		total.Add(b)
		stats["extraction"] = total
		if err != nil {
			return err
		}
		if b.Claimed == 0 {
			return nil
		}
		_ = s.Runs.UpdateRunStats(ctx, run.ID, stats)
	}
	return nil
}
