package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/jobs"
	"github.com/hneyra/crawler-go/internal/platform/db"
)

// JobQueue implements jobs.Queue over jobs table. skip locked + lease.
type JobQueue struct{ Pool *pgxpool.Pool }

var _ jobs.Queue = JobQueue{}

func (q JobQueue) Enqueue(ctx context.Context, kind string, payload any, o jobs.EnqueueOptions) (int64, bool, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, false, fmt.Errorf("job payload: %w", err)
	}
	if payload == nil {
		body = []byte("{}")
	}
	maxA := o.MaxAttempts
	if maxA <= 0 {
		maxA = jobs.DefaultMaxAttempts
	}
	var runAt *time.Time
	if !o.RunAt.IsZero() {
		runAt = &o.RunAt
	}
	var dedupe *string
	if o.DedupeKey != "" {
		dedupe = &o.DedupeKey
	}
	qr := db.Q(ctx, q.Pool)
	insert := func(key *string) (int64, bool, error) {
		var id int64
		err := qr.QueryRow(ctx, `
			INSERT INTO jobs (kind, payload, dedupe_key, run_at, max_attempts)
			VALUES ($1, $2, $3, coalesce($4, now()), $5)
			ON CONFLICT (kind, dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('queued', 'running') DO NOTHING
			RETURNING id`, kind, body, key, runAt, maxA).Scan(&id)
		if err == nil {
			return id, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, false, fmt.Errorf("enqueue %s: %w", kind, err)
		}
		return 0, false, nil
	}
	existing := func(key *string) (int64, string, error) {
		var id int64
		var st string
		err := qr.QueryRow(ctx, `SELECT id, status FROM jobs WHERE kind = $1 AND dedupe_key = $2 AND status IN ('queued', 'running')`, kind, key).Scan(&id, &st)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, "", fmt.Errorf("enqueue %s: %w", kind, jobs.ErrDedupeRaced)
		}
		if err != nil {
			return 0, "", fmt.Errorf("enqueue %s: find deduped: %w", kind, err)
		}
		return id, st, nil
	}
	id, ok, err := insert(dedupe)
	if err != nil || ok {
		return id, false, err
	}
	id, st, err := existing(dedupe)
	if err != nil || !o.FollowUp || st != "running" {
		return id, true, err
	}
	// key running: change may be missed by run in flight -> one queued follow-up.
	next := *dedupe + jobs.FollowUpSuffix
	if id, ok, err = insert(&next); err != nil || ok {
		return id, false, err
	}
	id, _, err = existing(&next)
	return id, true, err
}

func (q JobQueue) Claim(ctx context.Context, worker string, n int, lease time.Duration) ([]jobs.Job, error) {
	qr := db.Q(ctx, q.Pool)
	// crash loop guard: expired lease with no attempts left -> failed, not reclaimed forever.
	// linked crawl run failed in same statement: never stuck RUNNING.
	if _, err := qr.Exec(ctx, `
		WITH dead AS (
			UPDATE jobs SET status = 'failed', last_error = 'lease expired after last attempt', locked_by = NULL, locked_until = NULL, finished_at = now(), updated_at = now()
			WHERE status = 'running' AND locked_until < now() AND attempts >= max_attempts
			RETURNING id)
		UPDATE crawl_run SET status = 'FAILED', error = 'worker died repeatedly', finished_at = now()
		WHERE status IN ('QUEUED', 'RUNNING') AND job_id IN (SELECT id FROM dead)`); err != nil {
		return nil, fmt.Errorf("fail exhausted leases: %w", err)
	}
	rows, err := qr.Query(ctx, `
		UPDATE jobs SET status = 'running', attempts = attempts + 1, locked_by = $1,
		       locked_until = now() + $2 * interval '1 millisecond', updated_at = now()
		WHERE id IN (
			SELECT id FROM jobs
			WHERE (status = 'queued' AND run_at <= now()) OR (status = 'running' AND locked_until < now() AND attempts < max_attempts)
			ORDER BY run_at, id
			LIMIT $3
			FOR UPDATE SKIP LOCKED)
		RETURNING id, kind, payload, attempts, max_attempts`, worker, lease.Milliseconds(), n)
	if err != nil {
		return nil, fmt.Errorf("claim jobs: %w", err)
	}
	defer rows.Close()
	var out []jobs.Job
	for rows.Next() {
		var j jobs.Job
		if err := rows.Scan(&j.ID, &j.Kind, &j.Payload, &j.Attempts, &j.MaxAttempts); err != nil {
			return nil, fmt.Errorf("scan job: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim jobs rows: %w", err)
	}
	return out, nil
}

func (q JobQueue) owned(ctx context.Context, sql string, args ...any) error {
	tag, err := db.Q(ctx, q.Pool).Exec(ctx, sql, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return jobs.ErrLeaseLost
	}
	return nil
}

func (q JobQueue) Heartbeat(ctx context.Context, id int64, worker string, lease time.Duration) error {
	if err := q.owned(ctx, `UPDATE jobs SET locked_until = now() + $3 * interval '1 millisecond', updated_at = now()
		WHERE id = $1 AND locked_by = $2 AND status = 'running'`, id, worker, lease.Milliseconds()); err != nil {
		return fmt.Errorf("heartbeat job %d: %w", id, err)
	}
	return nil
}

func (q JobQueue) Complete(ctx context.Context, id int64, worker string) error {
	if err := q.owned(ctx, `UPDATE jobs SET status = 'succeeded', locked_by = NULL, locked_until = NULL, finished_at = now(), updated_at = now()
		WHERE id = $1 AND locked_by = $2 AND status = 'running'`, id, worker); err != nil {
		return fmt.Errorf("complete job %d: %w", id, err)
	}
	return nil
}

func (q JobQueue) Fail(ctx context.Context, id int64, worker string, msg string, retryAt time.Time) error {
	var at *time.Time
	if !retryAt.IsZero() {
		at = &retryAt
	}
	if err := q.owned(ctx, `
		UPDATE jobs SET
		  status = CASE WHEN $4::timestamptz IS NULL OR attempts >= max_attempts THEN 'failed' ELSE 'queued' END,
		  run_at = coalesce($4, run_at),
		  finished_at = CASE WHEN $4::timestamptz IS NULL OR attempts >= max_attempts THEN now() END,
		  last_error = left($3, 4000), locked_by = NULL, locked_until = NULL, updated_at = now()
		WHERE id = $1 AND locked_by = $2 AND status = 'running'`, id, worker, msg, at); err != nil {
		return fmt.Errorf("fail job %d: %w", id, err)
	}
	return nil
}

func (q JobQueue) Release(ctx context.Context, id int64, worker string) error {
	if err := q.owned(ctx, `UPDATE jobs SET status = 'queued', attempts = greatest(attempts - 1, 0), locked_by = NULL, locked_until = NULL, updated_at = now()
		WHERE id = $1 AND locked_by = $2 AND status = 'running'`, id, worker); err != nil {
		return fmt.Errorf("release job %d: %w", id, err)
	}
	return nil
}

// PurgeFinished deletes succeeded/failed jobs finished before cutoff. returns rows removed.
func (q JobQueue) PurgeFinished(ctx context.Context, before time.Time) (int64, error) {
	tag, err := db.Q(ctx, q.Pool).Exec(ctx, `DELETE FROM jobs WHERE finished_at < $1 AND status IN ('succeeded', 'failed', 'cancelled')`, before)
	if err != nil {
		return 0, fmt.Errorf("purge jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}
