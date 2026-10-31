// Package extraction = turn a detail page into item properties. strategies = java DetailExtractionService:
// HTML (css selectors), SCRIPT_JSON (regex over <script> + jsonpath), AJAX (intercepted xhr + jsonpath),
// JSON-LD always. third-party content: parsed, never executed.
package extraction

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/ohler55/ojg/jp"
	"github.com/ohler55/ojg/oj"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/fetch"
)

// Props = extracted properties. later writes win (java LinkedHashMap.put).
type Props map[string]any

// Rules = compiled extraction config. compile once per site batch.
type Rules struct {
	cfg       crawl.ExtractionConfig
	scripts   []*regexp.Regexp
	jsonPaths map[string]path
}

type path struct {
	x          jp.Expr
	indefinite bool // wildcard/descent/filter/slice/union -> always list (jayway)
}

// Compile builds rules. bad regex/jsonpath dropped with a warning (java skipped bad paths per use);
// one typo must not stop the whole site.
func Compile(cfg crawl.ExtractionConfig) (Rules, []string) {
	cfg = cfg.Normalize()
	r := Rules{cfg: cfg, jsonPaths: map[string]path{}}
	var warn []string
	for _, p := range cfg.ScriptPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			warn = append(warn, "script pattern "+p+": "+err.Error())
			continue
		}
		r.scripts = append(r.scripts, re)
	}
	for k, p := range cfg.JSONPaths {
		x, err := jp.ParseString(p)
		if err != nil {
			warn = append(warn, "json path "+k+": "+err.Error())
			continue
		}
		r.jsonPaths[k] = path{x: x, indefinite: indefinite(x)}
	}
	return r, warn
}

func indefinite(x jp.Expr) bool {
	for _, f := range x {
		switch f.(type) {
		case jp.Wildcard, jp.Descent, *jp.Filter, jp.Slice, jp.Union:
			return true
		}
	}
	return false
}

// Strategy of compiled rules.
func (r Rules) Strategy() crawl.DetailStrategy { return r.cfg.DetailStrategy }

// InterceptPatterns for AJAX renders.
func (r Rules) InterceptPatterns() []string { return r.cfg.InterceptPatterns }

// Extract = java order: css selectors, script regex (SCRIPT_JSON), intercepted xhr (AJAX), json-ld last.
// later writes win.
func (r Rules) Extract(doc *goquery.Document, intercepted []fetch.Intercepted) Props {
	p := Props{}
	r.selectors(doc, p)
	if r.cfg.DetailStrategy == crawl.StrategyScriptJSON {
		r.fromScripts(doc, p)
	}
	r.fromIntercepted(p, intercepted)
	r.jsonLD(doc, p)
	return p
}

// fromIntercepted applies jsonpaths to each captured response + keeps raw json
// under ajax_<status>_<sanitized url> (java key format).
func (r Rules) fromIntercepted(p Props, resps []fetch.Intercepted) {
	for _, it := range resps {
		v, err := oj.ParseString(it.Body)
		if err != nil {
			continue
		}
		r.applyPaths(v, p)
		p["ajax_"+strconv.Itoa(it.Status)+"_"+sanitizeKey(it.URL)] = v
	}
}

// selectors: first match text, trimmed. java Element.text().
func (r Rules) selectors(doc *goquery.Document, p Props) {
	for k, sel := range r.cfg.FieldSelectors {
		s := doc.Find(sel).First()
		if s.Length() == 0 {
			continue
		}
		p[k] = normSpace(s.Text())
	}
}

func (r Rules) fromScripts(doc *goquery.Document, p Props) {
	if len(r.scripts) == 0 {
		return
	}
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		data := s.Text()
		if strings.TrimSpace(data) == "" {
			return
		}
		for _, re := range r.scripts {
			m := re.FindStringSubmatch(data)
			if m == nil {
				continue
			}
			src := data
			if len(m) > 1 {
				src = m[1]
			}
			if v, err := oj.ParseString(src); err == nil {
				r.applyPaths(v, p)
			}
		}
	})
}

// jsonLD: every ld+json block; last valid wins as "jsonLd"; jsonpaths applied to each.
func (r Rules) jsonLD(doc *goquery.Document, p Props) {
	doc.Find("script[type]").Each(func(_ int, s *goquery.Selection) {
		if t, _ := s.Attr("type"); !strings.EqualFold(strings.TrimSpace(t), "application/ld+json") {
			return // jsoup attr match is case-insensitive
		}
		raw := strings.TrimSpace(s.Text())
		if raw == "" {
			return
		}
		v, err := oj.ParseString(raw)
		if err != nil {
			return
		}
		p["jsonLd"] = v
		r.applyPaths(v, p)
	})
}

// applyPaths = jayway shapes: indefinite path -> list (maybe empty); definite -> value, missing -> skip.
func (r Rules) applyPaths(data any, p Props) {
	for k, x := range r.jsonPaths {
		got := x.x.Get(data)
		switch {
		case x.indefinite:
			if got == nil {
				got = []any{}
			}
			p[k] = got
		case len(got) > 0:
			p[k] = got[0]
		}
	}
}

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

func sanitizeKey(u string) string { return nonAlnum.ReplaceAllString(u, "_") }

// normSpace = jsoup text(): collapse whitespace runs, trim.
func normSpace(s string) string { return strings.Join(strings.Fields(s), " ") }
