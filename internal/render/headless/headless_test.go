package headless_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/hneyra/crawler-go/internal/fetch"
	"github.com/hneyra/crawler-go/internal/render/headless"
)

// chrome from CRAWLER_TEST_CHROME, else PATH. none -> skip (CI image has none yet).
func chromePath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("CRAWLER_TEST_CHROME"); p != "" {
		return p
	}
	for _, n := range []string{"chromium", "chromium-browser", "google-chrome", "headless-shell"} {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	t.Skip("no chrome: set CRAWLER_TEST_CHROME")
	return ""
}

func newRenderer(t *testing.T) *headless.Renderer {
	t.Helper()
	r, err := headless.New(context.Background(), headless.Options{ExecPath: chromePath(t), Headless: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

// fake shop: listing loads 3 batches of items via fetch() on scroll; detail page has xhr json.
func shop() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/list", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `<!doctype html><html><body style="margin:0">
<div id="items"></div>
<script>
let batch = 0;
async function load() {
  if (batch >= 3) return;
  const res = await fetch("/api/items?b=" + batch);
  const items = await res.json();
  for (const it of items) {
    const a = document.createElement("a");
    a.className = "item"; a.href = it.url; a.textContent = it.name;
    a.style.display = "block"; a.style.height = "900px";
    document.getElementById("items").appendChild(a);
  }
  batch++;
}
load();
window.addEventListener("scroll", () => {
  if (window.innerHeight + window.scrollY >= document.body.scrollHeight - 10) load();
});
</script></body></html>`)
	})
	mux.HandleFunc("/api/items", func(w http.ResponseWriter, r *http.Request) {
		b := r.URL.Query().Get("b")
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `[{"url":"/p/%s-1","name":"a"},{"url":"/p/%s-2","name":"b"}]`, b, b)
	})
	mux.HandleFunc("/paged", func(w http.ResponseWriter, r *http.Request) {
		// js pagination: href="#", click changes location.
		_, _ = fmt.Fprint(w, `<html><body><a href="#" onclick="location.href='/paged?page=2';return false;">Siguiente</a></body></html>`)
	})
	return httptest.NewServer(mux)
}

func TestRenderScrollAndIntercept(t *testing.T) {
	r := newRenderer(t)
	srv := shop()
	defer srv.Close()
	res, err := r.Render(context.Background(), fetch.RenderRequest{
		URL: srv.URL + "/list", WaitForSelector: "a.item", ScrollToBottom: true, MaxScrolls: 10,
		Intercept: []string{"*/api/items*"}, Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(res.HTML, `class="item"`); n != 6 {
		t.Fatalf("items after scroll = %d, want 6", n)
	}
	if len(res.Intercepted) != 3 {
		t.Fatalf("intercepted %d, want 3: %+v", len(res.Intercepted), res.Intercepted)
	}
	for _, it := range res.Intercepted {
		if it.Status != 200 || !strings.Contains(it.Body, `"url":"/p/`) {
			t.Fatalf("intercept: %+v", it)
		}
	}
}

func TestRenderNoScrollLoadsFirstBatchOnly(t *testing.T) {
	r := newRenderer(t)
	srv := shop()
	defer srv.Close()
	res, err := r.Render(context.Background(), fetch.RenderRequest{URL: srv.URL + "/list", WaitForSelector: "a.item", Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(res.HTML, `class="item"`); n != 2 || len(res.Intercepted) != 0 {
		t.Fatalf("items=%d intercepted=%d", n, len(res.Intercepted))
	}
}

func TestClickNavigate(t *testing.T) {
	r := newRenderer(t)
	srv := shop()
	defer srv.Close()
	got, err := r.ClickNavigate(context.Background(), srv.URL+"/paged", fetch.ClickTarget{Text: "Siguiente"}, 20*time.Second)
	if err != nil || got != srv.URL+"/paged?page=2" {
		t.Fatalf("by text: %q %v", got, err)
	}
	if _, err := r.ClickNavigate(context.Background(), srv.URL+"/paged", fetch.ClickTarget{Selector: "a.missing"}, 15*time.Second); err == nil {
		t.Fatal("missing target accepted")
	}
}

func TestGlobRegexp(t *testing.T) {
	re, err := headless.GlobRegexp("*/api/items?b=*")
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("http://x/api/items?b=1") || re.MatchString("http://x/api/itemsXb=1") {
		t.Fatal("glob")
	}
}
