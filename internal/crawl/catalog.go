package crawl

import (
	"context"
	"fmt"
	"strings"
)

// SitePatch = partial site update. nil = keep.
type SitePatch struct {
	Name              *string
	BaseURL           *string
	Enabled           *bool
	PolitenessDelayMs *int
	ExtractionConfig  *ExtractionConfig
}

// Apply returns s with patch applied.
func (s Site) Apply(p SitePatch) Site {
	if p.Name != nil {
		s.Name = strings.TrimSpace(*p.Name)
	}
	if p.BaseURL != nil {
		s.BaseURL = strings.TrimSpace(*p.BaseURL)
	}
	if p.Enabled != nil {
		s.Enabled = *p.Enabled
	}
	if p.PolitenessDelayMs != nil {
		s.PolitenessDelayMs = *p.PolitenessDelayMs
	}
	if p.ExtractionConfig != nil {
		s.ExtractionConfig = p.ExtractionConfig.Normalize()
	}
	return s
}

// CategoryPatch = partial category update. nil = keep.
type CategoryPatch struct {
	Name               *string
	URL                *string
	PaginationType     *PaginationType
	ItemLinkSelector   *string
	NextPageSelector   *string
	LinkExtractionType *LinkExtractionType
	LinkAttribute      *string
	Enabled            *bool
}

// Apply returns c with patch applied.
func (c Category) Apply(p CategoryPatch) Category {
	if p.Name != nil {
		c.Name = strings.TrimSpace(*p.Name)
	}
	if p.URL != nil {
		c.URL = strings.TrimSpace(*p.URL)
	}
	if p.PaginationType != nil {
		c.PaginationType = *p.PaginationType
	}
	if p.ItemLinkSelector != nil {
		c.ItemLinkSelector = strings.TrimSpace(*p.ItemLinkSelector)
	}
	if p.NextPageSelector != nil {
		c.NextPageSelector = strings.TrimSpace(*p.NextPageSelector)
	}
	if p.LinkExtractionType != nil {
		c.LinkExtractionType = *p.LinkExtractionType
	}
	if p.LinkAttribute != nil {
		c.LinkAttribute = strings.TrimSpace(*p.LinkAttribute)
	}
	if p.Enabled != nil {
		c.Enabled = *p.Enabled
	}
	return c
}

// Catalog = site + category use cases (config crud).
type Catalog struct {
	Tx         Tx
	Sites      SiteStore
	Categories CategoryStore
}

// CreateSite validates + stores.
func (c Catalog) CreateSite(ctx context.Context, s Site) (Site, error) {
	s.Name = strings.TrimSpace(s.Name)
	s.BaseURL = strings.TrimSpace(s.BaseURL)
	s.ExtractionConfig = s.ExtractionConfig.Normalize()
	if err := s.Validate(); err != nil {
		return Site{}, err
	}
	return c.Sites.CreateSite(ctx, s)
}

// UpdateSite applies patch in tx (read-modify-write).
func (c Catalog) UpdateSite(ctx context.Context, id int64, p SitePatch) (Site, error) {
	var out Site
	err := c.Tx.InTx(ctx, func(ctx context.Context) error {
		cur, err := c.Sites.GetSite(ctx, id)
		if err != nil {
			return err
		}
		next := cur.Apply(p)
		if err := next.Validate(); err != nil {
			return err
		}
		out, err = c.Sites.UpdateSite(ctx, next)
		return err
	})
	if err != nil {
		return Site{}, fmt.Errorf("update site %d: %w", id, err)
	}
	return out, nil
}

// CreateCategory validates + stores under existing site.
func (c Catalog) CreateCategory(ctx context.Context, cat Category) (Category, error) {
	cat.Name = strings.TrimSpace(cat.Name)
	cat.URL = strings.TrimSpace(cat.URL)
	cat.ItemLinkSelector = strings.TrimSpace(cat.ItemLinkSelector)
	cat.NextPageSelector = strings.TrimSpace(cat.NextPageSelector)
	cat.LinkAttribute = strings.TrimSpace(cat.LinkAttribute)
	if cat.LinkExtractionType == "" {
		cat.LinkExtractionType = LinkHref
	}
	if err := cat.Validate(); err != nil {
		return Category{}, err
	}
	if _, err := c.Sites.GetSite(ctx, cat.SiteID); err != nil {
		return Category{}, err
	}
	return c.Categories.CreateCategory(ctx, cat)
}

// UpdateCategory applies patch in tx.
func (c Catalog) UpdateCategory(ctx context.Context, id int64, p CategoryPatch) (Category, error) {
	var out Category
	err := c.Tx.InTx(ctx, func(ctx context.Context) error {
		cur, err := c.Categories.GetCategory(ctx, id)
		if err != nil {
			return err
		}
		next := cur.Apply(p)
		if err := next.Validate(); err != nil {
			return err
		}
		out, err = c.Categories.UpdateCategory(ctx, next)
		return err
	})
	if err != nil {
		return Category{}, fmt.Errorf("update category %d: %w", id, err)
	}
	return out, nil
}
