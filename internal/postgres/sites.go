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

// Sites = crawl.SiteStore over postgres.
type Sites struct{ Pool *pgxpool.Pool }

var _ crawl.SiteStore = Sites{}

const siteCols = `id, name, base_url, enabled, politeness_delay_ms, extraction_config, created_at, updated_at`

func scanSite(row pgx.Row) (crawl.Site, error) {
	var s crawl.Site
	var cfg []byte
	if err := row.Scan(&s.ID, &s.Name, &s.BaseURL, &s.Enabled, &s.PolitenessDelayMs, &cfg, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return crawl.Site{}, err
	}
	s.ExtractionConfig = crawl.DefaultExtractionConfig()
	if len(cfg) > 0 {
		if err := json.Unmarshal(cfg, &s.ExtractionConfig); err != nil {
			return crawl.Site{}, fmt.Errorf("decode extraction_config of site %d: %w", s.ID, err)
		}
	}
	s.ExtractionConfig = s.ExtractionConfig.Normalize()
	return s, nil
}

func siteNotFound(err error) error { return notFound(err, "site_not_found", "Sitio no encontrado") }

func siteConflict(err error) error {
	return conflict(err, "site_name_key", "site_name_taken", "Ya existe un sitio con ese nombre")
}

// ListSites ordered by id.
func (s Sites) ListSites(ctx context.Context) ([]crawl.Site, error) {
	rows, err := db.Q(ctx, s.Pool).Query(ctx, `SELECT `+siteCols+` FROM site ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	defer rows.Close()
	out := []crawl.Site{}
	for rows.Next() {
		site, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, site)
	}
	return out, rows.Err()
}

// GetSite by id.
func (s Sites) GetSite(ctx context.Context, id int64) (crawl.Site, error) {
	site, err := scanSite(db.Q(ctx, s.Pool).QueryRow(ctx, `SELECT `+siteCols+` FROM site WHERE id = $1`, id))
	if err != nil {
		return crawl.Site{}, siteNotFound(err)
	}
	return site, nil
}

// GetSiteForUpdate = GetSite + row lock. call inside tx.
func (s Sites) GetSiteForUpdate(ctx context.Context, id int64) (crawl.Site, error) {
	site, err := scanSite(db.Q(ctx, s.Pool).QueryRow(ctx, `SELECT `+siteCols+` FROM site WHERE id = $1 FOR UPDATE`, id))
	if err != nil {
		return crawl.Site{}, siteNotFound(err)
	}
	return site, nil
}

// GetSiteByName exact match.
func (s Sites) GetSiteByName(ctx context.Context, name string) (crawl.Site, error) {
	site, err := scanSite(db.Q(ctx, s.Pool).QueryRow(ctx, `SELECT `+siteCols+` FROM site WHERE name = $1`, name))
	if err != nil {
		return crawl.Site{}, siteNotFound(err)
	}
	return site, nil
}

// CreateSite inserts. id + timestamps from db.
func (s Sites) CreateSite(ctx context.Context, in crawl.Site) (crawl.Site, error) {
	cfg, err := json.Marshal(in.ExtractionConfig.Normalize())
	if err != nil {
		return crawl.Site{}, err
	}
	out, err := scanSite(db.Q(ctx, s.Pool).QueryRow(ctx,
		`INSERT INTO site (name, base_url, enabled, politeness_delay_ms, extraction_config)
		 VALUES ($1, $2, $3, $4, $5) RETURNING `+siteCols,
		in.Name, in.BaseURL, in.Enabled, in.PolitenessDelayMs, cfg))
	if err != nil {
		return crawl.Site{}, siteConflict(err)
	}
	return out, nil
}

// UpdateSite overwrites all mutable fields.
func (s Sites) UpdateSite(ctx context.Context, in crawl.Site) (crawl.Site, error) {
	cfg, err := json.Marshal(in.ExtractionConfig.Normalize())
	if err != nil {
		return crawl.Site{}, err
	}
	out, err := scanSite(db.Q(ctx, s.Pool).QueryRow(ctx,
		`UPDATE site SET name = $2, base_url = $3, enabled = $4, politeness_delay_ms = $5,
		        extraction_config = $6, updated_at = now()
		 WHERE id = $1 RETURNING `+siteCols,
		in.ID, in.Name, in.BaseURL, in.Enabled, in.PolitenessDelayMs, cfg))
	if err != nil {
		return crawl.Site{}, siteConflict(siteNotFound(err))
	}
	return out, nil
}

// DeleteSite cascades to categories, urls, items, runs.
func (s Sites) DeleteSite(ctx context.Context, id int64) error {
	tag, err := db.Q(ctx, s.Pool).Exec(ctx, `DELETE FROM site WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete site %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return siteNotFound(pgx.ErrNoRows)
	}
	return nil
}
