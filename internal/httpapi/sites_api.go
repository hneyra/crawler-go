package httpapi

import (
	"context"

	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/crawl"
)

// SitesAPI = site + category config crud.
type SitesAPI struct {
	Catalog    crawl.Catalog
	Sites      crawl.SiteStore
	Categories crawl.CategoryStore
}

func (a *SitesAPI) ready() error {
	if a == nil {
		return apperr.NotImplemented()
	}
	return nil
}

func (a *SitesAPI) ListSites(ctx context.Context, _ ListSitesRequestObject) (ListSitesResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	sites, err := a.Sites.ListSites(ctx)
	if err != nil {
		return nil, err
	}
	out := SiteList{Items: make([]Site, 0, len(sites))}
	for _, s := range sites {
		out.Items = append(out.Items, siteDTO(s))
	}
	return ListSites200JSONResponse(out), nil
}

func (a *SitesAPI) CreateSite(ctx context.Context, req CreateSiteRequestObject) (CreateSiteResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	b := req.Body
	s := crawl.Site{
		Name:              b.Name,
		BaseURL:           b.BaseUrl,
		Enabled:           deref(b.Enabled, true),
		PolitenessDelayMs: deref(b.PolitenessDelayMs, 1000),
		ExtractionConfig:  configFromDTO(b.ExtractionConfig),
	}
	out, err := a.Catalog.CreateSite(ctx, s)
	if err != nil {
		return nil, err
	}
	return CreateSite201JSONResponse(siteDTO(out)), nil
}

func (a *SitesAPI) GetSite(ctx context.Context, req GetSiteRequestObject) (GetSiteResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	s, err := a.Sites.GetSite(ctx, req.SiteId)
	if err != nil {
		return nil, err
	}
	return GetSite200JSONResponse(siteDTO(s)), nil
}

func (a *SitesAPI) UpdateSite(ctx context.Context, req UpdateSiteRequestObject) (UpdateSiteResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	b := req.Body
	p := crawl.SitePatch{Name: b.Name, BaseURL: b.BaseUrl, Enabled: b.Enabled, PolitenessDelayMs: b.PolitenessDelayMs}
	if b.ExtractionConfig != nil {
		cfg := configFromDTO(b.ExtractionConfig)
		p.ExtractionConfig = &cfg
	}
	out, err := a.Catalog.UpdateSite(ctx, req.SiteId, p)
	if err != nil {
		return nil, err
	}
	return UpdateSite200JSONResponse(siteDTO(out)), nil
}

func (a *SitesAPI) DeleteSite(ctx context.Context, req DeleteSiteRequestObject) (DeleteSiteResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if err := a.Sites.DeleteSite(ctx, req.SiteId); err != nil {
		return nil, err
	}
	return DeleteSite204Response{}, nil
}

func (a *SitesAPI) ListCategories(ctx context.Context, req ListCategoriesRequestObject) (ListCategoriesResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	// missing site = 404, not empty list.
	if _, err := a.Sites.GetSite(ctx, req.SiteId); err != nil {
		return nil, err
	}
	cats, err := a.Categories.ListCategories(ctx, req.SiteId)
	if err != nil {
		return nil, err
	}
	out := CategoryList{Items: make([]Category, 0, len(cats))}
	for _, c := range cats {
		out.Items = append(out.Items, categoryDTO(c))
	}
	return ListCategories200JSONResponse(out), nil
}

func (a *SitesAPI) CreateCategory(ctx context.Context, req CreateCategoryRequestObject) (CreateCategoryResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	b := req.Body
	c := crawl.Category{
		SiteID:             req.SiteId,
		Name:               b.Name,
		URL:                b.Url,
		PaginationType:     crawl.PaginationType(b.PaginationType),
		ItemLinkSelector:   b.ItemLinkSelector,
		NextPageSelector:   deref(b.NextPageSelector, ""),
		LinkExtractionType: crawl.LinkExtractionType(deref(b.LinkExtractionType, HREF)),
		LinkAttribute:      deref(b.LinkAttribute, ""),
		Enabled:            deref(b.Enabled, true),
	}
	out, err := a.Catalog.CreateCategory(ctx, c)
	if err != nil {
		return nil, err
	}
	return CreateCategory201JSONResponse(categoryDTO(out)), nil
}

func (a *SitesAPI) GetCategory(ctx context.Context, req GetCategoryRequestObject) (GetCategoryResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	c, err := a.Categories.GetCategory(ctx, req.CategoryId)
	if err != nil {
		return nil, err
	}
	return GetCategory200JSONResponse(categoryDTO(c)), nil
}

func (a *SitesAPI) UpdateCategory(ctx context.Context, req UpdateCategoryRequestObject) (UpdateCategoryResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	b := req.Body
	p := crawl.CategoryPatch{
		Name: b.Name, URL: b.Url, ItemLinkSelector: b.ItemLinkSelector, NextPageSelector: b.NextPageSelector,
		LinkAttribute: b.LinkAttribute, Enabled: b.Enabled,
	}
	if b.PaginationType != nil {
		v := crawl.PaginationType(*b.PaginationType)
		p.PaginationType = &v
	}
	if b.LinkExtractionType != nil {
		v := crawl.LinkExtractionType(*b.LinkExtractionType)
		p.LinkExtractionType = &v
	}
	out, err := a.Catalog.UpdateCategory(ctx, req.CategoryId, p)
	if err != nil {
		return nil, err
	}
	return UpdateCategory200JSONResponse(categoryDTO(out)), nil
}

func (a *SitesAPI) DeleteCategory(ctx context.Context, req DeleteCategoryRequestObject) (DeleteCategoryResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if err := a.Categories.DeleteCategory(ctx, req.CategoryId); err != nil {
		return nil, err
	}
	return DeleteCategory204Response{}, nil
}
