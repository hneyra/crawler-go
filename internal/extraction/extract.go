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
	jsonPaths map[string]jp.Expr
}

// Compile validates regexes + jsonpaths. bad rule -> error (config bug, fail whole batch early).
func Compile(cfg crawl.ExtractionConfig) (Rules, error) {
	cfg = cfg.Normalize()
	r := Rules{cfg: cfg, jsonPaths: map[string]jp.Expr{}}
	for _, p := range cfg.ScriptPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return Rules{}, err
		}
		r.scripts = append(r.scripts, re)
	}
	for k, p := range cfg.JSONPaths {
		x, err := jp.ParseString(p)
		if err != nil {
			return Rules{}, err
		}
		r.jsonPaths[k] = x
	}
	return r, nil
}

// Strategy of compiled rules.
func (r Rules) Strategy() crawl.DetailStrategy { return r.cfg.DetailStrategy }

// InterceptPatterns for AJAX renders.
func (r Rules) InterceptPatterns() []string { return r.cfg.InterceptPatterns }

// FromPage runs html-side extractors: css selectors, script regex (SCRIPT_JSON), json-ld.
func (r Rules) FromPage(doc *goquery.Document) Props {
	p := Props{}
	r.selectors(doc, p)
	if r.cfg.DetailStrategy == crawl.StrategyScriptJSON {
		r.fromScripts(doc, p)
	}
	r.jsonLD(doc, p)
	return p
}

// FromIntercepted applies jsonpaths to each captured response + keeps raw json
// under ajax_<status>_<sanitized url> (java key format).
func (r Rules) FromIntercepted(p Props, resps []fetch.Intercepted) {
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
	doc.Find(`script[type="application/ld+json"]`).Each(func(_ int, s *goquery.Selection) {
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

// applyPaths: one match -> value, many -> list (indefinite path), none -> skip (jayway PathNotFound).
func (r Rules) applyPaths(data any, p Props) {
	for k, x := range r.jsonPaths {
		got := x.Get(data)
		switch len(got) {
		case 0:
		case 1:
			p[k] = got[0]
		default:
			p[k] = got
		}
	}
}

var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9]`)

func sanitizeKey(u string) string { return nonAlnum.ReplaceAllString(u, "_") }

// normSpace = jsoup text(): collapse whitespace runs, trim.
func normSpace(s string) string { return strings.Join(strings.Fields(s), " ") }
