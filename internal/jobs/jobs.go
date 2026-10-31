// Package jobs = postgres work queue contract + runner (same design as costea-backend ADR-0008). skip locked + lease. no redis.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Job = one claimed unit of work. Attempts counts this run.
type Job struct {
	ID          int64
	Kind        string
	Payload     json.RawMessage
	Attempts    int
	MaxAttempts int
}

// LastAttempt reports no retry left after this run.
func (j Job) LastAttempt() bool { return j.Attempts >= j.MaxAttempts }

// EnqueueOptions. zero = run now, default attempts, no dedupe.
type EnqueueOptions struct {
	DedupeKey   string // same kind+key queued/running -> no new job
	RunAt       time.Time
	MaxAttempts int // 0 -> DefaultMaxAttempts
	// FollowUp: dedupe only against queued. key running -> one follow-up job queued
	// (key + FollowUpSuffix). change seen during a run is not lost.
	FollowUp bool
}

// FollowUpSuffix marks follow-up dedupe key.
const FollowUpSuffix = "#next"

// DefaultMaxAttempts when not given.
const DefaultMaxAttempts = 5

// ErrDedupeRaced = enqueue hit dedupe but the conflicting job finished before it could be read.
// retry the enqueue.
var ErrDedupeRaced = errors.New("dedupe target finished concurrently")

// ErrLeaseLost = job no longer owned by worker (expired + reclaimed, or finished).
var ErrLeaseLost = errors.New("job lease lost")

// Enqueuer = enqueue only. runs in caller tx (ctx) so state change + job commit together.
type Enqueuer interface {
	// Enqueue returns job id. deduped=true -> existing queued/running job id.
	Enqueue(ctx context.Context, kind string, payload any, o EnqueueOptions) (id int64, deduped bool, err error)
}

// Queue = full queue port. postgres implements.
type Queue interface {
	Enqueuer
	// Claim up to n ready jobs (queued + due, or running with expired lease). attempts++.
	Claim(ctx context.Context, worker string, n int, lease time.Duration) ([]Job, error)
	// Heartbeat extends lease. ErrLeaseLost if not owner anymore.
	Heartbeat(ctx context.Context, id int64, worker string, lease time.Duration) error
	// Complete marks succeeded.
	Complete(ctx context.Context, id int64, worker string) error
	// Fail: retryAt zero or attempts exhausted -> failed. else queued again at retryAt.
	Fail(ctx context.Context, id int64, worker string, msg string, retryAt time.Time) error
	// Release gives job back without spending attempt (shutdown).
	Release(ctx context.Context, id int64, worker string) error
}

// permanent wraps error that must not retry.
type permanent struct{ err error }

func (p permanent) Error() string { return p.err.Error() }
func (p permanent) Unwrap() error { return p.err }

// Permanent marks err as non retryable.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanent{err: err}
}

// IsPermanent reports Permanent wrapper.
func IsPermanent(err error) bool {
	var p permanent
	return errors.As(err, &p)
}
