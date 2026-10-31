package extraction_test

import (
	"testing"

	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/extraction"
	"github.com/hneyra/crawler-go/internal/fetch"
)

func doc(t *testing.T, html string) fetch.Page {
	t.Helper()
	p, err := fetch.NewPage("https://shop.pe/p/1", html, false)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func rules(t *testing.T, cfg crawl.ExtractionConfig) extraction.Rules {
	t.Helper()
	r, err := extraction.Compile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestHTMLSelectors(t *testing.T) {
	r := rules(t, crawl.ExtractionConfig{DetailStrategy: crawl.StrategyHTML, FieldSelectors: map[string]string{"title": "h1", "price": "span.price", "missing": ".nope"}})
	p := r.FromPage(doc(t, `<h1>  My
	Product </h1><span class="price">$9.99</span><span class="price">$1</span>`).Doc)
	if p["title"] != "My Product" || p["price"] != "$9.99" {
		t.Fatalf("%v", p)
	}
	if _, ok := p["missing"]; ok {
		t.Fatal("missing selector set")
	}
}

// adondevivir shape: const avisoInfo = {...}; + jsonpaths.
func TestScriptJSON(t *testing.T) {
	r := rules(t, crawl.ExtractionConfig{
		DetailStrategy: crawl.StrategyScriptJSON,
		ScriptPatterns: []string{`const\s+avisoInfo\s*=\s*(\{.*?\});\s*$`},
		JSONPaths: map[string]string{
			"title": "$.units[0].title", "price": "$.units[0].prices[0].amount",
			"pics": "$.units[0].pictures[*].url", "nope": "$.units[0].missing",
		},
		FieldSelectors: map[string]string{"h1": "h1"},
	})
	html := `<html><head><title>Página</title><script>var x = 1;</script>
<script>
  const avisoInfo = {"units":[{"title":"Depa en San Miguel","prices":[{"amount":1500}],"pictures":[{"url":"a.jpg"},{"url":"b.jpg"}]}]};
</script></head><body><h1>Depa</h1></body></html>`
	p := r.FromPage(doc(t, html).Doc)
	if p["title"] != "Depa en San Miguel" || p["price"] != int64(1500) || p["h1"] != "Depa" {
		t.Fatalf("%#v", p)
	}
	pics, ok := p["pics"].([]any)
	if !ok || len(pics) != 2 || pics[1] != "b.jpg" {
		t.Fatalf("pics %#v", p["pics"])
	}
	if _, ok := p["nope"]; ok {
		t.Fatal("missing path set")
	}
	// HTML strategy ignores script patterns.
	r2 := rules(t, crawl.ExtractionConfig{DetailStrategy: crawl.StrategyHTML, ScriptPatterns: []string{`avisoInfo = (\{.*\})`}, JSONPaths: map[string]string{"title": "$.units[0].title"}})
	if _, ok := r2.FromPage(doc(t, html).Doc)["title"]; ok {
		t.Fatal("html strategy ran scripts")
	}
}

func TestJSONLDAlwaysAndLastWins(t *testing.T) {
	r := rules(t, crawl.ExtractionConfig{DetailStrategy: crawl.StrategyHTML, JSONPaths: map[string]string{"sku": "$.sku"}})
	p := r.FromPage(doc(t, `<script type="application/ld+json">{"@type":"Organization"}</script>
<script type="application/ld+json">{"@type":"Product","sku":"X1"}</script>
<script type="application/ld+json">not json</script>`).Doc)
	ld, ok := p["jsonLd"].(map[string]any)
	if !ok || ld["@type"] != "Product" || p["sku"] != "X1" {
		t.Fatalf("%#v", p)
	}
}

func TestIntercepted(t *testing.T) {
	r := rules(t, crawl.ExtractionConfig{DetailStrategy: crawl.StrategyAJAX, JSONPaths: map[string]string{"stock": "$.stock"}})
	p := extraction.Props{}
	r.FromIntercepted(p, []fetch.Intercepted{
		{URL: "https://api.shop.pe/v1/item?id=1", Status: 200, Body: `{"stock":3}`},
		{URL: "https://api.shop.pe/bad", Status: 500, Body: `<html>`},
	})
	raw, ok := p["ajax_200_https___api_shop_pe_v1_item_id_1"].(map[string]any)
	if !ok || p["stock"] != int64(3) || raw["stock"] != int64(3) || len(p) != 2 {
		t.Fatalf("%#v", p)
	}
}

func TestCompileRejectsBadRules(t *testing.T) {
	if _, err := extraction.Compile(crawl.ExtractionConfig{ScriptPatterns: []string{"(bad"}}); err == nil {
		t.Fatal("bad regex")
	}
	if _, err := extraction.Compile(crawl.ExtractionConfig{JSONPaths: map[string]string{"x": "$[[["}}); err == nil {
		t.Fatal("bad jsonpath")
	}
}
