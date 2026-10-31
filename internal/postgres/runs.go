package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/platform/db"
)

// Runs = crawl.RunStore over postgres.
type Runs struct{ Pool *pgxpool.Pool }

var _ crawl.RunStore = Runs{}

const runCols = `id, site_id, coalesce(category_id, 0), kind, status, stats, coalesce(error, ''), coalesce(job_id, 0), created_at, started_at, finished_at`

func scanRun(row pgx.Row) (crawl.Run, error) {
	var r crawl.Run
	var stats []byte
	if err := row.Scan(&r.ID, &r.SiteID, &r.CategoryID, &r.Kind, &r.Status, &stats, &r.Error, &r.JobID, &r.CreatedAt, &r.StartedAt, &r.FinishedAt); err != nil {
		return crawl.Run{}, err
	}
	r.Stats = map[string]any{}
	if len(stats) > 0 {
		if err := json.Unmarshal(stats, &r.Stats); err != nil {
			return crawl.Run{}, fmt.Errorf("decode run %d stats: %w", r.ID, err)
		}
	}
	return r, nil
}

func runNotFound(err error) error { return notFound(err, "run_not_found", "Ejecución no encontrada") }

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func statsJSON(s map[string]any) ([]byte, error) {
	if s == nil {
		s = map[string]any{}
	}
	return json.Marshal(s)
}

// CreateRun inserts QUEUED run.
func (s Runs) CreateRun(ctx context.Context, r crawl.Run) (crawl.Run, error) {
	out, err := scanRun(db.Q(ctx, s.Pool).QueryRow(ctx,
		`INSERT INTO crawl_run (site_id, category_id, kind) VALUES ($1, $2, $3) RETURNING `+runCols,
		r.SiteID, nullID(r.CategoryID), r.Kind))
	if err != nil {
		if db.IsForeignKeyViolation(err) {
			return crawl.Run{}, siteNotFound(pgx.ErrNoRows)
		}
		return crawl.Run{}, fmt.Errorf("create run: %w", err)
	}
	return out, nil
}

// GetRun by id.
func (s Runs) GetRun(ctx context.Context, id int64) (crawl.Run, error) {
	r, err := scanRun(db.Q(ctx, s.Pool).QueryRow(ctx, `SELECT `+runCols+` FROM crawl_run WHERE id = $1`, id))
	if err != nil {
		return crawl.Run{}, runNotFound(err)
	}
	return r, nil
}

// ActiveRun = newest QUEUED/RUNNING run of kind+target.
func (s Runs) ActiveRun(ctx context.Context, kind crawl.RunKind, siteID, categoryID int64) (crawl.Run, error) {
	r, err := scanRun(db.Q(ctx, s.Pool).QueryRow(ctx,
		`SELECT `+runCols+` FROM crawl_run
		 WHERE kind = $1 AND site_id = $2 AND coalesce(category_id, 0) = $3 AND status IN ('QUEUED', 'RUNNING')
		 ORDER BY id DESC LIMIT 1`, kind, siteID, categoryID))
	if err != nil {
		return crawl.Run{}, runNotFound(err)
	}
	return r, nil
}

func (s Runs) exec(ctx context.Context, what, q string, args ...any) error {
	tag, err := db.Q(ctx, s.Pool).Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	if tag.RowsAffected() == 0 {
		return runNotFound(pgx.ErrNoRows)
	}
	return nil
}

// SetRunJob links job.
func (s Runs) SetRunJob(ctx context.Context, id, jobID int64) error {
	return s.exec(ctx, "link run job", `UPDATE crawl_run SET job_id = $2 WHERE id = $1`, id, jobID)
}

// StartRun: RUNNING. started_at kept from first attempt.
func (s Runs) StartRun(ctx context.Context, id int64) error {
	return s.exec(ctx, "start run", `UPDATE crawl_run SET status = 'RUNNING', started_at = coalesce(started_at, now()) WHERE id = $1`, id)
}

// UpdateRunStats = progress while running.
func (s Runs) UpdateRunStats(ctx context.Context, id int64, stats map[string]any) error {
	b, err := statsJSON(stats)
	if err != nil {
		return err
	}
	return s.exec(ctx, "update run stats", `UPDATE crawl_run SET stats = $2 WHERE id = $1`, id, b)
}

const maxRunErr = 4000

// FinishRun sets status + stats + error. terminal -> finished_at.
func (s Runs) FinishRun(ctx context.Context, id int64, status crawl.RunStatus, stats map[string]any, errMsg string) error {
	b, err := statsJSON(stats)
	if err != nil {
		return err
	}
	if len(errMsg) > maxRunErr {
		errMsg = strings.ToValidUTF8(errMsg[:maxRunErr], "")
	}
	var fin any
	if status == crawl.RunCompleted || status == crawl.RunFailed {
		fin = time.Now()
	}
	return s.exec(ctx, "finish run",
		`UPDATE crawl_run SET status = $2, stats = $3, error = nullif($4, ''), finished_at = $5 WHERE id = $1`,
		id, status, b, strings.ReplaceAll(errMsg, "\x00", ""), fin)
}

// ListRuns newest first.
func (s Runs) ListRuns(ctx context.Context, siteID int64, page, perPage int) ([]crawl.Run, int64, error) {
	q := db.Q(ctx, s.Pool)
	var total int64
	if err := q.QueryRow(ctx, `SELECT count(*) FROM crawl_run WHERE ($1::bigint = 0 OR site_id = $1)`, siteID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count runs: %w", err)
	}
	rows, err := q.Query(ctx, `SELECT `+runCols+` FROM crawl_run WHERE ($1::bigint = 0 OR site_id = $1)
		ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`, siteID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	out := []crawl.Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}
