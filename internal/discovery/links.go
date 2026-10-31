// Package discovery = walk listing pages of a category, store detail urls (deduped by hash).
package discovery

import (
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/urlx"
)

// onclick patterns = java OnclickLinkExtractor.
var (
	onclickNav      = regexp.MustCompile(`(?:location\.href|window\.location|window\.open|location\.assign|location\.replace)\s*[=(]\s*['"]([^'"]+)['"]`)
	onclickFallback = regexp.MustCompile(`['"]((https?://|/)[^'"]+)['"]`)
)

// ExtractLink = item url from one listing element. "" = none.
func ExtractLink(sel *goquery.Selection, kind crawl.LinkExtractionType, attr, base string) string {
	switch kind {
	case crawl.LinkOnclick:
		js, _ := sel.Attr("onclick")
		if m := onclickNav.FindStringSubmatch(js); m != nil {
			return urlx.Resolve(m[1], base)
		}
		if m := onclickFallback.FindStringSubmatch(js); m != nil {
			return urlx.Resolve(m[1], base)
		}
		return ""
	case crawl.LinkDataAttribute:
		v, _ := sel.Attr(attr)
		if strings.TrimSpace(v) == "" {
			return ""
		}
		return urlx.Resolve(v, base)
	default: // HREF
		v, ok := sel.Attr("href")
		if !ok || strings.TrimSpace(v) == "" {
			return ""
		}
		return urlx.Resolve(v, base)
	}
}

// nextTexts = link texts meaning "next page" (java ListDiscoveryService).
func isNextText(s string) bool {
	s = strings.TrimSpace(s)
	return strings.EqualFold(s, "Siguiente") || strings.EqualFold(s, "Next") || s == "›" || s == "»"
}

// NextPageURL: custom selector, else a[rel=next], else link text. "" = last page.
func NextPageURL(doc *goquery.Document, custom string) string {
	base := docBase(doc)
	href := func(s *goquery.Selection) string {
		v, ok := s.Attr("href")
		if !ok || strings.TrimSpace(v) == "" {
			return ""
		}
		return urlx.Resolve(v, base)
	}
	if custom != "" {
		return href(doc.Find(custom).First())
	}
	if u := href(doc.Find("a[rel=next]").First()); u != "" {
		return u
	}
	out := ""
	doc.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		if isNextText(a.Text()) {
			out = href(a)
			return out == ""
		}
		return true
	})
	return out
}

// NextClickTarget = what to click when next href is useless (js pagination). nil = give up.
func NextClickTarget(doc *goquery.Document, custom string) (sel, text string, ok bool) {
	if custom != "" {
		return custom, "", true
	}
	if doc.Find("a[rel=next]").Length() > 0 {
		return "a[rel=next]", "", true
	}
	doc.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		if t := strings.TrimSpace(a.Text()); isNextText(t) {
			text, ok = t, true
			return false
		}
		return true
	})
	return "", text, ok
}

func docBase(doc *goquery.Document) string {
	if doc.Url == nil {
		return ""
	}
	return doc.Url.String()
}
