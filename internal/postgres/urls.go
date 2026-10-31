package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/platform/db"
)

// URLs = crawl.URLStore over postgres.
type URLs struct{ Pool *pgxpool.Pool }

var _ crawl.URLStore = URLs{}

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
