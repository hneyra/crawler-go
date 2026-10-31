package httpapi

import "github.com/hneyra/crawler-go/internal/crawl"

func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func siteDTO(s crawl.Site) Site {
	c := s.ExtractionConfig.Normalize()
	return Site{
		Id:                s.ID,
		Name:              s.Name,
		BaseUrl:           s.BaseURL,
		Enabled:           s.Enabled,
		PolitenessDelayMs: s.PolitenessDelayMs,
		ExtractionConfig: ExtractionConfig{
			DetailStrategy:    DetailStrategy(c.DetailStrategy),
			FieldSelectors:    RuleMap(c.FieldSelectors),
			ScriptPatterns:    c.ScriptPatterns,
			JsonPaths:         RuleMap(c.JSONPaths),
			InterceptPatterns: c.InterceptPatterns,
		},
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

// configFromDTO: nil input -> defaults. omitted fields -> empty.
func configFromDTO(in *ExtractionConfigInput) crawl.ExtractionConfig {
	c := crawl.DefaultExtractionConfig()
	if in == nil {
		return c
	}
	if in.DetailStrategy != nil {
		c.DetailStrategy = crawl.DetailStrategy(*in.DetailStrategy)
	}
	if in.FieldSelectors != nil {
		c.FieldSelectors = map[string]string(*in.FieldSelectors)
	}
	if in.ScriptPatterns != nil {
		c.ScriptPatterns = *in.ScriptPatterns
	}
	if in.JsonPaths != nil {
		c.JSONPaths = map[string]string(*in.JsonPaths)
	}
	if in.InterceptPatterns != nil {
		c.InterceptPatterns = *in.InterceptPatterns
	}
	return c.Normalize()
}

func categoryDTO(c crawl.Category) Category {
	return Category{
		Id:                 c.ID,
		SiteId:             c.SiteID,
		Name:               c.Name,
		Url:                c.URL,
		PaginationType:     PaginationType(c.PaginationType),
		ItemLinkSelector:   c.ItemLinkSelector,
		NextPageSelector:   optString(c.NextPageSelector),
		LinkExtractionType: LinkExtractionType(c.LinkExtractionType),
		LinkAttribute:      optString(c.LinkAttribute),
		Enabled:            c.Enabled,
		CreatedAt:          c.CreatedAt,
		UpdatedAt:          c.UpdatedAt,
	}
}
