// Package sitesconfig = sites-config.yml (same format as java crawler) -> db.
// sync = upsert by name. sites/categories missing from file are left alone (never deleted).
package sitesconfig

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/crawl"
)

// File = root of sites-config.yml.
type File struct {
	Sites []SiteDef `yaml:"sites"`
}

// SiteDef = one site. camelCase keys = java format.
type SiteDef struct {
	Name              string         `yaml:"name"`
	BaseURL           string         `yaml:"baseUrl"`
	Enabled           *bool          `yaml:"enabled"`
	PolitenessDelayMs *int           `yaml:"politenessDelayMs"`
	ExtractionConfig  *ExtractionDef `yaml:"extractionConfig"`
	Categories        []CategoryDef  `yaml:"categories"`
	Extra             map[string]any `yaml:",inline"` // unknown keys -> error
}

// ExtractionDef = extraction rules.
type ExtractionDef struct {
	DetailStrategy    string            `yaml:"detailStrategy"`
	FieldSelectors    map[string]string `yaml:"fieldSelectors"`
	ScriptPatterns    []string          `yaml:"scriptPatterns"`
	JSONPaths         map[string]string `yaml:"jsonPaths"`
	InterceptPatterns []string          `yaml:"interceptPatterns"`
}

// CategoryDef = one listing page.
type CategoryDef struct {
	Name               string `yaml:"name"`
	URL                string `yaml:"url"`
	PaginationType     string `yaml:"paginationType"`
	ItemLinkSelector   string `yaml:"itemLinkSelector"`
	NextPageSelector   string `yaml:"nextPageSelector"`
	LinkExtractionType string `yaml:"linkExtractionType"`
	LinkAttribute      string `yaml:"linkAttribute"`
	Enabled            *bool  `yaml:"enabled"`
}

// Parse reads yaml. strict: unknown keys fail (typo = silent misconfig otherwise).
func Parse(r io.Reader) (File, error) {
	raw, err := io.ReadAll(io.LimitReader(r, 4<<20))
	if err != nil {
		return File{}, fmt.Errorf("read sites config: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return File{}, fmt.Errorf("parse sites config: %w", err)
	}
	for _, s := range f.Sites {
		if len(s.Extra) > 0 {
			return File{}, fmt.Errorf("parse sites config: site %q: unknown keys", s.Name)
		}
	}
	return f, nil
}

// Site maps def -> domain. defaults = java: enabled true, delay 1000, HTML, PAGE_PARAM, HREF.
func (d SiteDef) Site() crawl.Site {
	s := crawl.Site{
		Name:              d.Name,
		BaseURL:           d.BaseURL,
		Enabled:           deref(d.Enabled, true),
		PolitenessDelayMs: deref(d.PolitenessDelayMs, 1000),
		ExtractionConfig:  crawl.DefaultExtractionConfig(),
	}
	if e := d.ExtractionConfig; e != nil {
		s.ExtractionConfig = crawl.ExtractionConfig{
			DetailStrategy:    crawl.DetailStrategy(e.DetailStrategy),
			FieldSelectors:    e.FieldSelectors,
			ScriptPatterns:    e.ScriptPatterns,
			JSONPaths:         e.JSONPaths,
			InterceptPatterns: e.InterceptPatterns,
		}.Normalize()
	}
	return s
}

// Category maps def -> domain under siteID.
func (d CategoryDef) Category(siteID int64) crawl.Category {
	c := crawl.Category{
		SiteID:             siteID,
		Name:               d.Name,
		URL:                d.URL,
		PaginationType:     crawl.PaginationType(d.PaginationType),
		ItemLinkSelector:   d.ItemLinkSelector,
		NextPageSelector:   d.NextPageSelector,
		LinkExtractionType: crawl.LinkExtractionType(d.LinkExtractionType),
		LinkAttribute:      d.LinkAttribute,
		Enabled:            deref(d.Enabled, true),
	}
	if c.PaginationType == "" {
		c.PaginationType = crawl.PaginationPageParam
	}
	if c.LinkExtractionType == "" {
		c.LinkExtractionType = crawl.LinkHref
	}
	return c
}

// Result = what sync did.
type Result struct {
	SitesCreated, SitesUpdated           int
	CategoriesCreated, CategoriesUpdated int
}

// Sync upserts every site + category of f in one tx. any invalid entry -> nothing written.
func Sync(ctx context.Context, cat crawl.Catalog, f File) (Result, error) {
	var res Result
	err := cat.Tx.InTx(ctx, func(ctx context.Context) error {
		res = Result{}
		for _, sd := range f.Sites {
			site, created, err := upsertSite(ctx, cat, sd.Site())
			if err != nil {
				return fmt.Errorf("site %q: %w", sd.Name, err)
			}
			if created {
				res.SitesCreated++
			} else {
				res.SitesUpdated++
			}
			for _, cd := range sd.Categories {
				created, err := upsertCategory(ctx, cat, cd.Category(site.ID))
				if err != nil {
					return fmt.Errorf("site %q category %q: %w", sd.Name, cd.Name, err)
				}
				if created {
					res.CategoriesCreated++
				} else {
					res.CategoriesUpdated++
				}
			}
		}
		return nil
	})
	return res, err
}

func upsertSite(ctx context.Context, cat crawl.Catalog, s crawl.Site) (crawl.Site, bool, error) {
	cur, err := cat.Sites.GetSiteByName(ctx, s.Name)
	if apperr.IsKind(err, apperr.KindNotFound) {
		out, err := cat.CreateSite(ctx, s)
		return out, true, err
	}
	if err != nil {
		return crawl.Site{}, false, err
	}
	cfg := s.ExtractionConfig
	out, err := cat.UpdateSite(ctx, cur.ID, crawl.SitePatch{
		BaseURL: &s.BaseURL, Enabled: &s.Enabled, PolitenessDelayMs: &s.PolitenessDelayMs, ExtractionConfig: &cfg,
	})
	return out, false, err
}

func upsertCategory(ctx context.Context, cat crawl.Catalog, c crawl.Category) (bool, error) {
	cur, err := cat.Categories.GetCategoryByName(ctx, c.SiteID, c.Name)
	if apperr.IsKind(err, apperr.KindNotFound) {
		_, err := cat.CreateCategory(ctx, c)
		return true, err
	}
	if err != nil {
		return false, err
	}
	_, err = cat.UpdateCategory(ctx, cur.ID, crawl.CategoryPatch{
		URL: &c.URL, PaginationType: &c.PaginationType, ItemLinkSelector: &c.ItemLinkSelector,
		NextPageSelector: &c.NextPageSelector, LinkExtractionType: &c.LinkExtractionType,
		LinkAttribute: &c.LinkAttribute, Enabled: &c.Enabled,
	})
	return false, err
}

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}
