package postgres

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/platform/db"
)

// URLs = crawl.URLStore over postgres.
type URLs struct{ Pool *pgxpool.Pool }

var (
	_ crawl.URLStore = URLs{}
	_ crawl.URLQueue = URLs{}
)

const urlCols = `id, site_id, category_id, url, url_hash, status, retry_count, last_attempt_at, coalesce(last_error, ''), created_at`

func scanURL(row pgx.Row) (crawl.DiscoveredURL, error) {
	var u crawl.DiscoveredURL
	err := row.Scan(&u.ID, &u.SiteID, &u.CategoryID, &u.URL, &u.URLHash, &u.Status, &u.RetryCount, &u.LastAttemptAt, &u.LastError, &u.CreatedAt)
	return u, err
}

// InsertURL adds PENDING url. hash taken -> inserted=false, no error.
func (s URLs) InsertURL(ctx context.Context, u crawl.DiscoveredURL) (bool, error) {
	tag, err := db.Q(ctx, s.Pool).Exec(ctx,
		`INSERT INTO discovered_url (site_id, category_id, url, url_hash) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (url_hash) DO NOTHING`,
		u.SiteID, u.CategoryID, u.URL, u.URLHash)
	if err != nil {
		return false, fmt.Errorf("insert url: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// GetURLByHash = dedupe lookup.
func (s URLs) GetURLByHash(ctx context.Context, hash string) (crawl.DiscoveredURL, error) {
	u, err := scanURL(db.Q(ctx, s.Pool).QueryRow(ctx, `SELECT `+urlCols+` FROM discovered_url WHERE url_hash = $1`, hash))
	if err != nil {
		return crawl.DiscoveredURL{}, notFound(err, "url_not_found", "URL no encontrada")
	}
	return u, nil
}

// ListURLs of site in status, oldest first.
func (s URLs) ListURLs(ctx context.Context, siteID int64, status crawl.URLStatus, limit int) ([]crawl.DiscoveredURL, error) {
	rows, err := db.Q(ctx, s.Pool).Query(ctx,
		`SELECT `+urlCols+` FROM discovered_url WHERE site_id = $1 AND status = $2 ORDER BY id LIMIT $3`,
		siteID, status, limit)
	if err != nil {
		return nil, fmt.Errorf("list urls: %w", err)
	}
	defer rows.Close()
	out := []crawl.DiscoveredURL{}
	for rows.Next() {
		u, err := scanURL(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SiteStats = counts by status, items, last attempt. missing site -> not_found.
func (s URLs) SiteStats(ctx context.Context, siteID int64) (crawl.SiteStats, error) {
	st := crawl.SiteStats{SiteID: siteID}
	err := db.Q(ctx, s.Pool).QueryRow(ctx,
		`SELECT s.name,
		        count(u.id),
		        count(u.id) FILTER (WHERE u.status = 'PENDING'),
		        count(u.id) FILTER (WHERE u.status = 'IN_PROGRESS'),
		        count(u.id) FILTER (WHERE u.status = 'COMPLETED'),
		        count(u.id) FILTER (WHERE u.status = 'FAILED'),
		        max(u.last_attempt_at),
		        (SELECT count(*) FROM extracted_item i WHERE i.site_id = s.id)
		 FROM site s LEFT JOIN discovered_url u ON u.site_id = s.id
		 WHERE s.id = $1
		 GROUP BY s.id, s.name`, siteID).
		Scan(&st.SiteName, &st.Total, &st.Pending, &st.InProgress, &st.Completed, &st.Failed, &st.LastCrawl, &st.ExtractedItems)
	if err != nil {
		return crawl.SiteStats{}, siteNotFound(err)
	}
	return st, nil
}

// ResetFailed: FAILED + retry_count < max -> PENDING. retry_count kept (it is the budget).
func (s URLs) ResetFailed(ctx context.Context, maxRetries int) (int64, error) {
	tag, err := db.Q(ctx, s.Pool).Exec(ctx,
		`UPDATE discovered_url SET status = 'PENDING' WHERE status = 'FAILED' AND retry_count < $1`, maxRetries)
	if err != nil {
		return 0, fmt.Errorf("reset failed urls: %w", err)
	}
	return tag.RowsAffected(), nil
}

// ClaimPending: oldest first, FOR UPDATE SKIP LOCKED -> parallel workers split the queue.
func (s URLs) ClaimPending(ctx context.Context, siteID int64, limit int) ([]crawl.DiscoveredURL, error) {
	rows, err := db.Q(ctx, s.Pool).Query(ctx,
		`WITH picked AS (
		   SELECT id FROM discovered_url WHERE site_id = $1 AND status = 'PENDING'
		   ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED)
		 UPDATE discovered_url d SET status = 'IN_PROGRESS', last_attempt_at = now()
		 FROM picked WHERE d.id = picked.id
		 RETURNING d.id, d.site_id, d.category_id, d.url, d.url_hash, d.status, d.retry_count, d.last_attempt_at,
		           coalesce(d.last_error, ''), d.created_at`, siteID, limit)
	if err != nil {
		return nil, fmt.Errorf("claim urls: %w", err)
	}
	defer rows.Close()
	out := []crawl.DiscoveredURL{}
	for rows.Next() {
		u, err := scanURL(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// RETURNING order is not guaranteed.
	slices.SortFunc(out, func(a, b crawl.DiscoveredURL) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

// MarkCompleted: done. last_error cleared.
func (s URLs) MarkCompleted(ctx context.Context, id int64) error {
	if _, err := db.Q(ctx, s.Pool).Exec(ctx,
		`UPDATE discovered_url SET status = 'COMPLETED', last_error = NULL, last_attempt_at = now() WHERE id = $1`, id); err != nil {
		return fmt.Errorf("complete url %d: %w", id, err)
	}
	return nil
}

const maxErrLen = 1000

// MarkFailed: retry budget consumed. reason truncated (site errors can be huge).
func (s URLs) MarkFailed(ctx context.Context, id int64, reason string) error {
	if len(reason) > maxErrLen {
		reason = strings.ToValidUTF8(reason[:maxErrLen], "")
	}
	if _, err := db.Q(ctx, s.Pool).Exec(ctx,
		`UPDATE discovered_url SET status = 'FAILED', retry_count = retry_count + 1, last_error = $2, last_attempt_at = now() WHERE id = $1`,
		id, strings.ReplaceAll(reason, "\x00", "")); err != nil {
		return fmt.Errorf("fail url %d: %w", id, err)
	}
	return nil
}

// Release: IN_PROGRESS -> PENDING for given ids. no retry consumed.
func (s URLs) Release(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	if _, err := db.Q(ctx, s.Pool).Exec(ctx,
		`UPDATE discovered_url SET status = 'PENDING' WHERE id = ANY($1) AND status = 'IN_PROGRESS'`, ids); err != nil {
		return fmt.Errorf("release urls: %w", err)
	}
	return nil
}

// CountPending of site.
func (s URLs) CountPending(ctx context.Context, siteID int64) (int64, error) {
	var n int64
	if err := db.Q(ctx, s.Pool).QueryRow(ctx,
		`SELECT count(*) FROM discovered_url WHERE site_id = $1 AND status = 'PENDING'`, siteID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count pending: %w", err)
	}
	return n, nil
}
