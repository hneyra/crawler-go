package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/platform/db"
)

// Items = crawl.ItemStore over postgres.
type Items struct{ Pool *pgxpool.Pool }

var _ crawl.ItemStore = Items{}

const itemSelect = `SELECT i.id, i.discovered_url_id, i.site_id, u.url, s.name, i.title, i.properties,
	coalesce(i.raw_snapshot_path, ''), i.extracted_at
	FROM extracted_item i
	JOIN discovered_url u ON u.id = i.discovered_url_id
	JOIN site s ON s.id = i.site_id`

func scanItem(row pgx.Row) (crawl.Item, error) {
	var it crawl.Item
	var props []byte
	if err := row.Scan(&it.ID, &it.DiscoveredURLID, &it.SiteID, &it.URL, &it.SiteName, &it.Title, &props, &it.RawSnapshotPath, &it.ExtractedAt); err != nil {
		return crawl.Item{}, err
	}
	it.Properties = map[string]any{}
	if len(props) > 0 {
		if err := json.Unmarshal(props, &it.Properties); err != nil {
			return crawl.Item{}, fmt.Errorf("decode properties of item %d: %w", it.ID, err)
		}
	}
	return it, nil
}

// UpsertItem: one row per url. re-extract overwrites + bumps extracted_at.
func (s Items) UpsertItem(ctx context.Context, it crawl.Item) (crawl.Item, error) {
	props := it.Properties
	if props == nil {
		props = map[string]any{}
	}
	raw, err := json.Marshal(props)
	if err != nil {
		return crawl.Item{}, fmt.Errorf("encode properties: %w", err)
	}
	var id int64
	err = db.Q(ctx, s.Pool).QueryRow(ctx,
		`INSERT INTO extracted_item (discovered_url_id, site_id, title, properties, raw_snapshot_path)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (discovered_url_id) DO UPDATE
		   SET title = excluded.title, properties = excluded.properties,
		       raw_snapshot_path = excluded.raw_snapshot_path, extracted_at = now()
		 RETURNING id`,
		it.DiscoveredURLID, it.SiteID, it.Title, raw, nullable(it.RawSnapshotPath)).Scan(&id)
	if err != nil {
		return crawl.Item{}, fmt.Errorf("upsert item for url %d: %w", it.DiscoveredURLID, err)
	}
	return s.GetItem(ctx, id)
}

// GetItem by id.
func (s Items) GetItem(ctx context.Context, id int64) (crawl.Item, error) {
	it, err := scanItem(db.Q(ctx, s.Pool).QueryRow(ctx, itemSelect+` WHERE i.id = $1`, id))
	if err != nil {
		return crawl.Item{}, notFound(err, "item_not_found", "Item no encontrado")
	}
	return it, nil
}

// ListItems newest first. siteID 0 = all. page from 1.
func (s Items) ListItems(ctx context.Context, siteID int64, page, perPage int) ([]crawl.Item, int64, error) {
	q := db.Q(ctx, s.Pool)
	var total int64
	if err := q.QueryRow(ctx, `SELECT count(*) FROM extracted_item WHERE ($1::bigint = 0 OR site_id = $1)`, siteID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count items: %w", err)
	}
	rows, err := q.Query(ctx,
		itemSelect+` WHERE ($1::bigint = 0 OR i.site_id = $1) ORDER BY i.extracted_at DESC, i.id DESC LIMIT $2 OFFSET $3`,
		siteID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("list items: %w", err)
	}
	defer rows.Close()
	out := []crawl.Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, it)
	}
	return out, total, rows.Err()
}
