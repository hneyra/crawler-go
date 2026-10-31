package crawl

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/ohler55/ojg/jp"

	"github.com/hneyra/crawler-go/internal/apperr"
)

// limits mirror db checks. app answers 400 before db answers 500.
const (
	maxName       = 255
	maxURL        = 2048
	maxSelector   = 500
	maxAttribute  = 255
	maxDelayMs    = 600000
	maxRuleKey    = 100
	maxRules      = 200
	maxPatternLen = 2000
)

// AbsoluteHTTPURL reports http(s) url with host.
func AbsoluteHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// hasNUL: postgres text/jsonb reject \x00 -> 500. refuse as 400 first.
func hasNUL(ss ...string) bool {
	for _, s := range ss {
		if strings.ContainsRune(s, 0) {
			return true
		}
	}
	return false
}

func configHasNUL(c ExtractionConfig) bool {
	all := append(append([]string{string(c.DetailStrategy)}, c.ScriptPatterns...), c.InterceptPatterns...)
	for k, v := range c.FieldSelectors {
		all = append(all, k, v)
	}
	for k, v := range c.JSONPaths {
		all = append(all, k, v)
	}
	return hasNUL(all...)
}

// Validate checks site fields. all errors at once.
func (s Site) Validate() error {
	var f apperr.Fields
	if hasNUL(s.Name, s.BaseURL) || configHasNUL(s.ExtractionConfig) {
		f.Add("body", "invalid_char", "Texto con caracteres no permitidos")
		return f.Err("invalid_site", "Sitio inválido")
	}
	s.validateInto(&f)
	return f.Err("invalid_site", "Sitio inválido")
}

func (s Site) validateInto(f *apperr.Fields) {
	if n := strings.TrimSpace(s.Name); n == "" || len(n) > maxName {
		f.Add("name", "invalid_name", "Nombre requerido (máx. 255)")
	}
	if len(s.BaseURL) > maxURL || !AbsoluteHTTPURL(s.BaseURL) {
		f.Add("base_url", "invalid_url", "URL base debe ser http(s) absoluta")
	}
	if s.PolitenessDelayMs < 0 || s.PolitenessDelayMs > maxDelayMs {
		f.Add("politeness_delay_ms", "out_of_range", "Delay entre 0 y 600000 ms")
	}
	s.ExtractionConfig.validateInto(f)
}

func (c ExtractionConfig) validateInto(f *apperr.Fields) {
	const p = "extraction_config."
	if !c.DetailStrategy.Valid() {
		f.Add(p+"detail_strategy", "invalid_enum", "Estrategia debe ser HTML, SCRIPT_JSON o AJAX")
	}
	validRules(f, p+"field_selectors", c.FieldSelectors, func(v string) bool { return len(v) <= maxSelector })
	validRules(f, p+"json_paths", c.JSONPaths, func(v string) bool {
		if !strings.HasPrefix(v, "$") || len(v) > maxSelector {
			return false
		}
		// jayway extras (.length(), .min(), =~) not supported: reject at config time.
		_, err := jp.ParseString(v)
		return err == nil
	})
	if len(c.ScriptPatterns) > maxRules {
		f.Add(p+"script_patterns", "too_many", "Demasiados patrones")
	}
	for _, pat := range c.ScriptPatterns {
		if pat == "" || len(pat) > maxPatternLen {
			f.Add(p+"script_patterns", "invalid_pattern", "Patrón vacío o demasiado largo")
			break
		}
		// go regexp = RE2: linear time, no catastrophic backtracking on hostile html.
		if _, err := regexp.Compile(pat); err != nil {
			f.Add(p+"script_patterns", "invalid_regex", "Regex inválida: "+short(err.Error()))
			break
		}
	}
	if len(c.InterceptPatterns) > maxRules {
		f.Add(p+"intercept_patterns", "too_many", "Demasiados patrones")
	}
	for _, pat := range c.InterceptPatterns {
		if pat == "" || len(pat) > maxPatternLen {
			f.Add(p+"intercept_patterns", "invalid_pattern", "Patrón vacío o demasiado largo")
			break
		}
	}
}

func validRules(f *apperr.Fields, field string, m map[string]string, ok func(string) bool) {
	if len(m) > maxRules {
		f.Add(field, "too_many", "Demasiadas reglas")
		return
	}
	for k, v := range m {
		if k == "" || len(k) > maxRuleKey || strings.TrimSpace(v) == "" || !ok(v) {
			f.Add(field, "invalid_rule", "Regla inválida: "+short(k))
			return
		}
	}
}

// Validate checks category fields. all errors at once.
func (c Category) Validate() error {
	var f apperr.Fields
	if hasNUL(c.Name, c.URL, string(c.PaginationType), c.ItemLinkSelector, c.NextPageSelector, string(c.LinkExtractionType), c.LinkAttribute) {
		f.Add("body", "invalid_char", "Texto con caracteres no permitidos")
		return f.Err("invalid_category", "Categoría inválida")
	}
	if n := strings.TrimSpace(c.Name); n == "" || len(n) > maxName {
		f.Add("name", "invalid_name", "Nombre requerido (máx. 255)")
	}
	if len(c.URL) > maxURL || !AbsoluteHTTPURL(c.URL) {
		f.Add("url", "invalid_url", "URL debe ser http(s) absoluta")
	}
	if !c.PaginationType.Valid() {
		f.Add("pagination_type", "invalid_enum", "Paginación debe ser PAGE_PARAM, CURSOR o SCROLL_AJAX")
	}
	if s := strings.TrimSpace(c.ItemLinkSelector); s == "" || len(s) > maxSelector {
		f.Add("item_link_selector", "invalid_selector", "Selector requerido (máx. 500)")
	}
	if len(c.NextPageSelector) > maxSelector {
		f.Add("next_page_selector", "invalid_selector", "Selector demasiado largo")
	}
	if !c.LinkExtractionType.Valid() {
		f.Add("link_extraction_type", "invalid_enum", "Tipo debe ser HREF, ONCLICK o DATA_ATTRIBUTE")
	}
	if len(c.LinkAttribute) > maxAttribute {
		f.Add("link_attribute", "too_long", "Atributo demasiado largo")
	}
	if c.LinkExtractionType == LinkDataAttribute && strings.TrimSpace(c.LinkAttribute) == "" {
		f.Add("link_attribute", "required", "Atributo requerido para DATA_ATTRIBUTE")
	}
	return f.Err("invalid_category", "Categoría inválida")
}

func short(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return s
}
