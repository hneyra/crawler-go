package crawl_test

import (
	"testing"

	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/crawl"
)

func fields(t *testing.T, err error) map[string]string {
	t.Helper()
	ae, ok := apperr.As(err)
	if !ok || ae.Kind != apperr.KindInvalid {
		t.Fatalf("want invalid, got %v", err)
	}
	out := map[string]string{}
	for _, f := range ae.Fields {
		out[f.Field] = f.Code
	}
	return out
}

func TestSiteValidate(t *testing.T) {
	ok := crawl.Site{Name: "Tienda", BaseURL: "https://tienda.pe", PolitenessDelayMs: 1000, ExtractionConfig: crawl.DefaultExtractionConfig()}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := crawl.Site{Name: " ", BaseURL: "ftp://x", PolitenessDelayMs: -1, ExtractionConfig: crawl.ExtractionConfig{
		DetailStrategy: "XML",
		ScriptPatterns: []string{"(unclosed"},
		JSONPaths:      map[string]string{"price": "price"},
		FieldSelectors: map[string]string{"title": ""},
	}}
	got := fields(t, bad.Validate())
	for _, f := range []string{"name", "base_url", "politeness_delay_ms", "extraction_config.detail_strategy", "extraction_config.script_patterns", "extraction_config.json_paths", "extraction_config.field_selectors"} {
		if got[f] == "" {
			t.Errorf("missing field error %s in %v", f, got)
		}
	}
}

func TestCategoryValidate(t *testing.T) {
	c := crawl.Category{Name: "c", URL: "https://x.pe/c", PaginationType: crawl.PaginationPageParam, ItemLinkSelector: "a", LinkExtractionType: crawl.LinkHref}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.LinkExtractionType = crawl.LinkDataAttribute
	if got := fields(t, c.Validate()); got["link_attribute"] != "required" {
		t.Fatalf("data attribute needs attribute: %v", got)
	}
	c.LinkAttribute = "data-to-posting"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := crawl.Category{PaginationType: "X", LinkExtractionType: "Y"}
	got := fields(t, bad.Validate())
	for _, f := range []string{"name", "url", "pagination_type", "item_link_selector", "link_extraction_type"} {
		if got[f] == "" {
			t.Errorf("missing %s in %v", f, got)
		}
	}
}

func TestApplyPatch(t *testing.T) {
	s := crawl.Site{Name: "a", BaseURL: "https://a.pe", Enabled: true, PolitenessDelayMs: 1000, ExtractionConfig: crawl.DefaultExtractionConfig()}
	name, off := "  b ", false
	got := s.Apply(crawl.SitePatch{Name: &name, Enabled: &off, ExtractionConfig: &crawl.ExtractionConfig{DetailStrategy: crawl.StrategyAJAX}})
	if got.Name != "b" || got.Enabled || got.BaseURL != "https://a.pe" || got.PolitenessDelayMs != 1000 ||
		got.ExtractionConfig.DetailStrategy != crawl.StrategyAJAX || got.ExtractionConfig.JSONPaths == nil {
		t.Fatalf("patch: %+v", got)
	}
}
