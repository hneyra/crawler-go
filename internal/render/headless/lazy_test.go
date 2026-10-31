package headless_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/hneyra/crawler-go/internal/fetch"
	"github.com/hneyra/crawler-go/internal/render/headless"
)

// chrome dies (simulated by closing the inner renderer) -> next use starts a new one.
func TestLazyRestartsDeadBrowser(t *testing.T) {
	l := &headless.Lazy{Ctx: context.Background(), Opts: headless.Options{ExecPath: chromePath(t), Headless: true}}
	defer l.Close()
	srv := shop()
	defer srv.Close()
	render := func() error {
		res, err := l.Render(context.Background(), fetch.RenderRequest{URL: srv.URL + "/paged", Timeout: 15 * time.Second})
		if err == nil && !strings.Contains(res.HTML, "Siguiente") {
			t.Fatalf("html: %q", res.HTML)
		}
		return err
	}
	if err := render(); err != nil {
		t.Fatal(err)
	}
	headless.KillForTest(l)
	if err := render(); err != nil {
		t.Fatalf("after crash: %v", err)
	}
}
