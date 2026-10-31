package crawl

import "context"

// Tx = use case transaction boundary. db.Runner fits.
type Tx interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// SiteStore = site persistence. missing -> apperr not_found. duplicate name -> apperr conflict.
type SiteStore interface {
	ListSites(ctx context.Context) ([]Site, error)
	GetSite(ctx context.Context, id int64) (Site, error)
	GetSiteByName(ctx context.Context, name string) (Site, error)
	CreateSite(ctx context.Context, s Site) (Site, error)
	UpdateSite(ctx context.Context, s Site) (Site, error)
	DeleteSite(ctx context.Context, id int64) error
}

// CategoryStore = category persistence. same error contract as SiteStore.
type CategoryStore interface {
	ListCategories(ctx context.Context, siteID int64) ([]Category, error)
	EnabledCategories(ctx context.Context, siteID int64) ([]Category, error)
	GetCategory(ctx context.Context, id int64) (Category, error)
	GetCategoryByName(ctx context.Context, siteID int64, name string) (Category, error)
	CreateCategory(ctx context.Context, c Category) (Category, error)
	UpdateCategory(ctx context.Context, c Category) (Category, error)
	DeleteCategory(ctx context.Context, id int64) error
}

// URLStore = discovered url persistence.
type URLStore interface {
	// InsertURL adds PENDING url. inserted=false when hash already known (dedupe).
	InsertURL(ctx context.Context, u DiscoveredURL) (inserted bool, err error)
	GetURLByHash(ctx context.Context, hash string) (DiscoveredURL, error)
	ListURLs(ctx context.Context, siteID int64, status URLStatus, limit int) ([]DiscoveredURL, error)
	SiteStats(ctx context.Context, siteID int64) (SiteStats, error)
	// ResetFailed moves FAILED urls with retry_count < maxRetries back to PENDING. returns count.
	ResetFailed(ctx context.Context, maxRetries int) (int64, error)
}

// ItemStore = extracted item persistence.
type ItemStore interface {
	// UpsertItem = one item per url. re-extract overwrites.
	UpsertItem(ctx context.Context, it Item) (Item, error)
	GetItem(ctx context.Context, id int64) (Item, error)
	// ListItems newest first. siteID 0 = all sites.
	ListItems(ctx context.Context, siteID int64, page, perPage int) ([]Item, int64, error)
}
