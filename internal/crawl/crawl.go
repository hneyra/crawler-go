// Package crawl = core domain: sites, categories, discovered urls, items, runs.
// pure: no db, no http. adapters implement the ports below.
package crawl

import (
	"time"
)

// PaginationType = how listing pages advance.
type PaginationType string

const (
	PaginationPageParam  PaginationType = "PAGE_PARAM"  // ?page=N / next link
	PaginationCursor     PaginationType = "CURSOR"      // next link w/ cursor. same path as PAGE_PARAM
	PaginationScrollAJAX PaginationType = "SCROLL_AJAX" // infinite scroll. headless render
)

// Valid reports known value.
func (p PaginationType) Valid() bool {
	return p == PaginationPageParam || p == PaginationCursor || p == PaginationScrollAJAX
}

// LinkExtractionType = where item link lives on listing element.
type LinkExtractionType string

const (
	LinkHref          LinkExtractionType = "HREF"
	LinkOnclick       LinkExtractionType = "ONCLICK"
	LinkDataAttribute LinkExtractionType = "DATA_ATTRIBUTE"
)

// Valid reports known value.
func (l LinkExtractionType) Valid() bool {
	return l == LinkHref || l == LinkOnclick || l == LinkDataAttribute
}

// DetailStrategy = how item data is pulled from detail page.
type DetailStrategy string

const (
	StrategyHTML       DetailStrategy = "HTML"        // css selectors on dom
	StrategyScriptJSON DetailStrategy = "SCRIPT_JSON" // regex over <script> + jsonpath
	StrategyAJAX       DetailStrategy = "AJAX"        // intercepted xhr json + jsonpath
)

// Valid reports known value.
func (d DetailStrategy) Valid() bool {
	return d == StrategyHTML || d == StrategyScriptJSON || d == StrategyAJAX
}

// URLStatus = discovered url lifecycle.
type URLStatus string

const (
	URLPending    URLStatus = "PENDING"
	URLInProgress URLStatus = "IN_PROGRESS"
	URLCompleted  URLStatus = "COMPLETED"
	URLFailed     URLStatus = "FAILED"
)

// ExtractionConfig = per-site detail extraction rules. stored as jsonb.
type ExtractionConfig struct {
	DetailStrategy    DetailStrategy    `json:"detailStrategy"`
	FieldSelectors    map[string]string `json:"fieldSelectors"`
	ScriptPatterns    []string          `json:"scriptPatterns"`
	JSONPaths         map[string]string `json:"jsonPaths"`
	InterceptPatterns []string          `json:"interceptPatterns"`
}

// DefaultExtractionConfig = HTML, empty rules.
func DefaultExtractionConfig() ExtractionConfig {
	return ExtractionConfig{
		DetailStrategy:    StrategyHTML,
		FieldSelectors:    map[string]string{},
		ScriptPatterns:    []string{},
		JSONPaths:         map[string]string{},
		InterceptPatterns: []string{},
	}
}

// Normalize fills nil maps/slices + empty strategy. json never null.
func (c ExtractionConfig) Normalize() ExtractionConfig {
	if c.DetailStrategy == "" {
		c.DetailStrategy = StrategyHTML
	}
	if c.FieldSelectors == nil {
		c.FieldSelectors = map[string]string{}
	}
	if c.ScriptPatterns == nil {
		c.ScriptPatterns = []string{}
	}
	if c.JSONPaths == nil {
		c.JSONPaths = map[string]string{}
	}
	if c.InterceptPatterns == nil {
		c.InterceptPatterns = []string{}
	}
	return c
}

// Site = crawl target.
type Site struct {
	ID                int64
	Name              string
	BaseURL           string
	Enabled           bool
	PolitenessDelayMs int
	ExtractionConfig  ExtractionConfig
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// PolitenessDelay as duration.
func (s Site) PolitenessDelay() time.Duration {
	return time.Duration(s.PolitenessDelayMs) * time.Millisecond
}

// Category = listing page of a site. discovery entry point.
type Category struct {
	ID                 int64
	SiteID             int64
	Name               string
	URL                string
	PaginationType     PaginationType
	ItemLinkSelector   string
	NextPageSelector   string // empty = auto (rel=next, "Siguiente"...)
	LinkExtractionType LinkExtractionType
	LinkAttribute      string // DATA_ATTRIBUTE only
	Enabled            bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// DiscoveredURL = detail page found by discovery.
type DiscoveredURL struct {
	ID            int64
	SiteID        int64
	CategoryID    int64
	URL           string
	URLHash       string
	Status        URLStatus
	RetryCount    int
	LastAttemptAt *time.Time
	LastError     string
	CreatedAt     time.Time
}

// Item = data extracted from one discovered url. one per url (re-extract overwrites).
type Item struct {
	ID              int64
	DiscoveredURLID int64
	SiteID          int64
	URL             string // joined from discovered_url on reads
	SiteName        string // joined from site on reads
	Title           string
	Properties      map[string]any
	RawSnapshotPath string
	ExtractedAt     time.Time
}

// SiteStats = url counts by status + items.
type SiteStats struct {
	SiteID         int64
	SiteName       string
	Total          int64
	Pending        int64
	InProgress     int64
	Completed      int64
	Failed         int64
	ExtractedItems int64
	LastCrawl      *time.Time // max(last_attempt_at)
}

// RunKind = what a crawl run does.
type RunKind string

const (
	RunDiscoverCategory RunKind = "DISCOVER_CATEGORY"
	RunDiscoverSite     RunKind = "DISCOVER_SITE"
	RunExtractSite      RunKind = "EXTRACT_SITE"
	RunCrawlSite        RunKind = "CRAWL_SITE" // discover all enabled categories, then extract
)

// Valid reports known value.
func (k RunKind) Valid() bool {
	switch k {
	case RunDiscoverCategory, RunDiscoverSite, RunExtractSite, RunCrawlSite:
		return true
	}
	return false
}

// RunStatus = run lifecycle.
type RunStatus string

const (
	RunQueued    RunStatus = "QUEUED"
	RunRunning   RunStatus = "RUNNING"
	RunCompleted RunStatus = "COMPLETED"
	RunFailed    RunStatus = "FAILED"
)

// Run = one requested discover/extract/crawl. replaces spring batch job execution.
type Run struct {
	ID         int64
	SiteID     int64
	CategoryID int64 // DISCOVER_CATEGORY only; 0 otherwise
	Kind       RunKind
	Status     RunStatus
	Stats      map[string]any
	Error      string
	JobID      int64
	CreatedAt  time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}
