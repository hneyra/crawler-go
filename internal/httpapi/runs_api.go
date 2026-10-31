package httpapi

import (
	"context"

	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/pipeline"
)

// RunsAPI = run requests, run status, site stats, items.
type RunsAPI struct {
	Pipeline *pipeline.Service
	Runs     crawl.RunStore
	URLs     crawl.URLStore
	Items    crawl.ItemStore
}

func (a *RunsAPI) ready() error {
	if a == nil || a.Pipeline == nil {
		return apperr.NotImplemented()
	}
	return nil
}

func (a *RunsAPI) request(ctx context.Context, kind crawl.RunKind, siteID, catID int64) (Run, error) {
	if err := a.ready(); err != nil {
		return Run{}, err
	}
	r, _, err := a.Pipeline.Request(ctx, kind, siteID, catID)
	if err != nil {
		return Run{}, err
	}
	return runDTO(r), nil
}

func (a *RunsAPI) DiscoverCategory(ctx context.Context, req DiscoverCategoryRequestObject) (DiscoverCategoryResponseObject, error) {
	r, err := a.request(ctx, crawl.RunDiscoverCategory, 0, req.CategoryId)
	if err != nil {
		return nil, err
	}
	return DiscoverCategory202JSONResponse(r), nil
}

func (a *RunsAPI) DiscoverSite(ctx context.Context, req DiscoverSiteRequestObject) (DiscoverSiteResponseObject, error) {
	r, err := a.request(ctx, crawl.RunDiscoverSite, req.SiteId, 0)
	if err != nil {
		return nil, err
	}
	return DiscoverSite202JSONResponse(r), nil
}

func (a *RunsAPI) ExtractSite(ctx context.Context, req ExtractSiteRequestObject) (ExtractSiteResponseObject, error) {
	r, err := a.request(ctx, crawl.RunExtractSite, req.SiteId, 0)
	if err != nil {
		return nil, err
	}
	return ExtractSite202JSONResponse(r), nil
}

func (a *RunsAPI) CrawlSite(ctx context.Context, req CrawlSiteRequestObject) (CrawlSiteResponseObject, error) {
	r, err := a.request(ctx, crawl.RunCrawlSite, req.SiteId, 0)
	if err != nil {
		return nil, err
	}
	return CrawlSite202JSONResponse(r), nil
}

func paging(page *PageParam, perPage *PerPageParam) (int, int) {
	return deref(page, 1), deref(perPage, 20)
}

func (a *RunsAPI) ListRuns(ctx context.Context, req ListRunsRequestObject) (ListRunsResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	page, per := paging(req.Params.Page, req.Params.PerPage)
	runs, total, err := a.Runs.ListRuns(ctx, deref(req.Params.SiteId, 0), page, per)
	if err != nil {
		return nil, err
	}
	out := RunList{Items: make([]Run, 0, len(runs)), Total: total, Page: page, PerPage: per}
	for _, r := range runs {
		out.Items = append(out.Items, runDTO(r))
	}
	return ListRuns200JSONResponse(out), nil
}

func (a *RunsAPI) GetRun(ctx context.Context, req GetRunRequestObject) (GetRunResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	r, err := a.Runs.GetRun(ctx, req.RunId)
	if err != nil {
		return nil, err
	}
	return GetRun200JSONResponse(runDTO(r)), nil
}

func (a *RunsAPI) GetSiteStats(ctx context.Context, req GetSiteStatsRequestObject) (GetSiteStatsResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	st, err := a.URLs.SiteStats(ctx, req.SiteId)
	if err != nil {
		return nil, err
	}
	return GetSiteStats200JSONResponse(SiteStats{
		SiteId: st.SiteID, SiteName: st.SiteName, TotalUrls: st.Total, Pending: st.Pending, InProgress: st.InProgress,
		Completed: st.Completed, Failed: st.Failed, ExtractedItems: st.ExtractedItems, LastCrawl: st.LastCrawl,
	}), nil
}

func (a *RunsAPI) ListItems(ctx context.Context, req ListItemsRequestObject) (ListItemsResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	page, per := paging(req.Params.Page, req.Params.PerPage)
	items, total, err := a.Items.ListItems(ctx, deref(req.Params.SiteId, 0), page, per)
	if err != nil {
		return nil, err
	}
	out := ItemList{Items: make([]Item, 0, len(items)), Total: total, Page: page, PerPage: per}
	for _, it := range items {
		out.Items = append(out.Items, itemDTO(it))
	}
	return ListItems200JSONResponse(out), nil
}

func (a *RunsAPI) GetItem(ctx context.Context, req GetItemRequestObject) (GetItemResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	it, err := a.Items.GetItem(ctx, req.ItemId)
	if err != nil {
		return nil, err
	}
	return GetItem200JSONResponse(itemDTO(it)), nil
}

func runDTO(r crawl.Run) Run {
	out := Run{
		Id: r.ID, SiteId: r.SiteID, Kind: RunKind(r.Kind), Status: RunStatus(r.Status), Stats: r.Stats,
		Error: optString(r.Error), CreatedAt: r.CreatedAt, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
	}
	if out.Stats == nil {
		out.Stats = map[string]any{}
	}
	if r.CategoryID != 0 {
		c := r.CategoryID
		out.CategoryId = &c
	}
	return out
}

func itemDTO(it crawl.Item) Item {
	props := it.Properties
	if props == nil {
		props = map[string]any{}
	}
	return Item{
		Id: it.ID, DiscoveredUrlId: it.DiscoveredURLID, SiteId: it.SiteID, SiteName: it.SiteName, Url: it.URL,
		Title: it.Title, Properties: props, RawSnapshotPath: optString(it.RawSnapshotPath), ExtractedAt: it.ExtractedAt,
	}
}
