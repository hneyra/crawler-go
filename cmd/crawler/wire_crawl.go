package main

import (
	"context"

	"github.com/hneyra/crawler-go/internal/discovery"
	"github.com/hneyra/crawler-go/internal/extraction"
	"github.com/hneyra/crawler-go/internal/fetch"
	"github.com/hneyra/crawler-go/internal/postgres"
	"github.com/hneyra/crawler-go/internal/render/headless"
	"github.com/hneyra/crawler-go/internal/storage"
)

// crawlDeps = fetch + render + discovery + extraction. renderer lazy: chrome starts on first blocked/js page.
type crawlDeps struct {
	renderer   *headless.Lazy
	fetcher    *fetch.HTTPFetcher
	discovery  *discovery.Service
	extraction *extraction.Service
}

func wireCrawl(ctx context.Context, d deps) crawlDeps {
	c := d.cfg.Crawl
	r := &headless.Lazy{Ctx: ctx, Opts: headless.Options{
		ExecPath: c.ChromePath, ProfileDir: c.BrowserProfileDir, Headless: c.Headless, UserAgent: c.UserAgent, Log: d.log,
	}}
	f := &fetch.HTTPFetcher{UserAgent: c.UserAgent, Timeout: c.FetchTimeout, RenderTimeout: c.RenderTimeout, Renderer: r}
	urls := postgres.URLs{Pool: d.pool}
	return crawlDeps{
		renderer: r,
		fetcher:  f,
		extraction: &extraction.Service{
			Sites: postgres.Sites{Pool: d.pool}, Queue: urls, Items: postgres.Items{Pool: d.pool},
			Fetcher: f, Renderer: r, Snapshots: storage.Raw{Dir: c.RawDataDir},
			BatchSize: c.ExtractionBatchSize, RenderTimeout: c.RenderTimeout, Log: d.log,
		},
		discovery: &discovery.Service{
			Fetcher: f, Renderer: r, URLs: urls,
			MaxPages: c.DiscoveryMaxPages, RenderTimeout: c.RenderTimeout, Log: d.log,
		},
	}
}
