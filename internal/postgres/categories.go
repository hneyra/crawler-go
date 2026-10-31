package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/platform/db"
)

// Categories = crawl.CategoryStore over postgres.
type Categories struct{ Pool *pgxpool.Pool }

var _ crawl.CategoryStore = Categories{}

const categoryCols = `id, site_id, name, url, pagination_type, item_link_selector,
	coalesce(next_page_selector, ''), link_extraction_type, coalesce(link_attribute, ''), enabled, created_at, updated_at`

func scanCategory(row pgx.Row) (crawl.Category, error) {
	var c crawl.Category
	err := row.Scan(&c.ID, &c.SiteID, &c.Name, &c.URL, &c.PaginationType, &c.ItemLinkSelector,
		&c.NextPageSelector, &c.LinkExtractionType, &c.LinkAttribute, &c.Enabled, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

func categoryNotFound(err error) error {
	return notFound(err, "category_not_found", "Categoría no encontrada")
}

func categoryConflict(err error) error {
	err = conflict(err, "category_site_name_key", "category_name_taken", "Ya existe una categoría con ese nombre en el sitio")
	if db.IsForeignKeyViolation(err) {
		return siteNotFound(pgx.ErrNoRows)
	}
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (c Categories) list(ctx context.Context, q string, args ...any) ([]crawl.Category, error) {
	rows, err := db.Q(ctx, c.Pool).Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list categories: %w", err)
	}
	defer rows.Close()
	out := []crawl.Category{}
	for rows.Next() {
		cat, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cat)
	}
	return out, rows.Err()
}

// ListCategories of site, by id.
func (c Categories) ListCategories(ctx context.Context, siteID int64) ([]crawl.Category, error) {
	return c.list(ctx, `SELECT `+categoryCols+` FROM category WHERE site_id = $1 ORDER BY id`, siteID)
}

// EnabledCategories of site, by id.
func (c Categories) EnabledCategories(ctx context.Context, siteID int64) ([]crawl.Category, error) {
	return c.list(ctx, `SELECT `+categoryCols+` FROM category WHERE site_id = $1 AND enabled ORDER BY id`, siteID)
}

// GetCategory by id.
func (c Categories) GetCategory(ctx context.Context, id int64) (crawl.Category, error) {
	cat, err := scanCategory(db.Q(ctx, c.Pool).QueryRow(ctx, `SELECT `+categoryCols+` FROM category WHERE id = $1`, id))
	if err != nil {
		return crawl.Category{}, categoryNotFound(err)
	}
	return cat, nil
}

// GetCategoryForUpdate = GetCategory + row lock. call inside tx.
func (c Categories) GetCategoryForUpdate(ctx context.Context, id int64) (crawl.Category, error) {
	cat, err := scanCategory(db.Q(ctx, c.Pool).QueryRow(ctx, `SELECT `+categoryCols+` FROM category WHERE id = $1 FOR UPDATE`, id))
	if err != nil {
		return crawl.Category{}, categoryNotFound(err)
	}
	return cat, nil
}

// GetCategoryByName within site.
func (c Categories) GetCategoryByName(ctx context.Context, siteID int64, name string) (crawl.Category, error) {
	cat, err := scanCategory(db.Q(ctx, c.Pool).QueryRow(ctx,
		`SELECT `+categoryCols+` FROM category WHERE site_id = $1 AND name = $2`, siteID, name))
	if err != nil {
		return crawl.Category{}, categoryNotFound(err)
	}
	return cat, nil
}

// CreateCategory inserts.
func (c Categories) CreateCategory(ctx context.Context, in crawl.Category) (crawl.Category, error) {
	out, err := scanCategory(db.Q(ctx, c.Pool).QueryRow(ctx,
		`INSERT INTO category (site_id, name, url, pagination_type, item_link_selector, next_page_selector,
		                       link_extraction_type, link_attribute, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+categoryCols,
		in.SiteID, in.Name, in.URL, in.PaginationType, in.ItemLinkSelector, nullable(in.NextPageSelector),
		in.LinkExtractionType, nullable(in.LinkAttribute), in.Enabled))
	if err != nil {
		return crawl.Category{}, categoryConflict(err)
	}
	return out, nil
}

// UpdateCategory overwrites mutable fields. site_id immutable.
func (c Categories) UpdateCategory(ctx context.Context, in crawl.Category) (crawl.Category, error) {
	out, err := scanCategory(db.Q(ctx, c.Pool).QueryRow(ctx,
		`UPDATE category SET name = $2, url = $3, pagination_type = $4, item_link_selector = $5,
		        next_page_selector = $6, link_extraction_type = $7, link_attribute = $8, enabled = $9, updated_at = now()
		 WHERE id = $1 RETURNING `+categoryCols,
		in.ID, in.Name, in.URL, in.PaginationType, in.ItemLinkSelector, nullable(in.NextPageSelector),
		in.LinkExtractionType, nullable(in.LinkAttribute), in.Enabled))
	if err != nil {
		return crawl.Category{}, categoryConflict(categoryNotFound(err))
	}
	return out, nil
}

// DeleteCategory cascades to its urls (+ items).
func (c Categories) DeleteCategory(ctx context.Context, id int64) error {
	tag, err := db.Q(ctx, c.Pool).Exec(ctx, `DELETE FROM category WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete category %d: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return categoryNotFound(pgx.ErrNoRows)
	}
	return nil
}
