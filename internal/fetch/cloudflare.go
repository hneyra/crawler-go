package fetch

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// signals = java CloudflareDetector.
var (
	challengeTitles = []string{"just a moment", "attention required", "sorry, you have been blocked"}
	challengeMarks  = []string{"cf-browser-verification", "cf_chl_prog", "jschl-answer", "__cf_chl_jschl_tk__", "cf-spinner", "cf-im-under-attack"}
)

// IsBlockedStatus = status that usually means bot wall. retry with real browser.
func IsBlockedStatus(status int) bool {
	return status == 403 || status == 429 || status == 503
}

// IsChallenge reports cloudflare interstitial in a 200 page.
func IsChallenge(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	title := strings.ToLower(doc.Find("title").First().Text())
	for _, s := range challengeTitles {
		if strings.Contains(title, s) {
			return true
		}
	}
	body, _ := doc.Find("body").Html()
	for _, s := range challengeMarks {
		if strings.Contains(body, s) {
			return true
		}
	}
	return false
}
