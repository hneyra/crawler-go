package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/metrics"
	"github.com/hneyra/crawler-go/internal/platform/db"
)

// Stats = global counts for metrics.
type Stats struct{ Pool *pgxpool.Pool }

var _ metrics.Counter = Stats{}

// GlobalCounts: sites, items, urls by status (lowercase).
func (s Stats) GlobalCounts(ctx context.Context) (metrics.Counts, error) {
	c := metrics.Counts{URLs: map[string]int64{}}
	q := db.Q(ctx, s.Pool)
	if err := q.QueryRow(ctx, `SELECT (SELECT count(*) FROM site), (SELECT count(*) FROM extracted_item)`).Scan(&c.Sites, &c.Items); err != nil {
		return c, fmt.Errorf("global counts: %w", err)
	}
	rows, err := q.Query(ctx, `SELECT status, count(*) FROM discovered_url GROUP BY status`)
	if err != nil {
		return c, fmt.Errorf("url counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var st string
		var n int64
		if err := rows.Scan(&st, &n); err != nil {
			return c, err
		}
		c.URLs[strings.ToLower(st)] = n
	}
	return c, rows.Err()
}
