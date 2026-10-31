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
	// GetSiteForUpdate locks row until tx end. read-modify-write without lost updates.
	GetSiteForUpdate(ctx context.Context, id int64) (Site, error)
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
	// GetCategoryForUpdate locks row until tx end.
	GetCategoryForUpdate(ctx context.Context, id int64) (Category, error)
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

// URLQueue = extraction work queue over discovered urls.
type URLQueue interface {
	// ClaimPending moves up to limit PENDING urls of site to IN_PROGRESS (skip locked rows,
	// concurrent workers never share a url). last_attempt_at = now.
	ClaimPending(ctx context.Context, siteID int64, limit int) ([]DiscoveredURL, error)
	MarkCompleted(ctx context.Context, id int64) error
	// MarkFailed: FAILED, retry_count+1, last_error kept (truncated).
	MarkFailed(ctx context.Context, id int64, reason string) error
	// Release puts claimed-but-unprocessed urls back to PENDING (shutdown mid batch).
	Release(ctx context.Context, ids []int64) error
	CountPending(ctx context.Context, siteID int64) (int64, error)
}

// ItemStore = extracted item persistence.
type ItemStore interface {
	// UpsertItem = one item per url. re-extract overwrites.
	UpsertItem(ctx context.Context, it Item) (Item, error)
	GetItem(ctx context.Context, id int64) (Item, error)
	// ListItems newest first. siteID 0 = all sites.
	ListItems(ctx context.Context, siteID int64, page, perPage int) ([]Item, int64, error)
}
