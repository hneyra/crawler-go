package main

import (
	"context"

	"github.com/hneyra/crawler-go/internal/discovery"
	"github.com/hneyra/crawler-go/internal/fetch"
	"github.com/hneyra/crawler-go/internal/postgres"
	"github.com/hneyra/crawler-go/internal/render/headless"
)

// crawlDeps = fetch + render + discovery. renderer lazy: chrome starts on first blocked/js page.
type crawlDeps struct {
	renderer  *headless.Lazy
	fetcher   *fetch.HTTPFetcher
	discovery *discovery.Service
}

func wireCrawl(ctx context.Context, d deps) crawlDeps {
	c := d.cfg.Crawl
	r := &headless.Lazy{Ctx: ctx, Opts: headless.Options{
		ExecPath: c.ChromePath, ProfileDir: c.BrowserProfileDir, Headless: c.Headless, UserAgent: c.UserAgent, Log: d.log,
	}}
	f := &fetch.HTTPFetcher{UserAgent: c.UserAgent, Timeout: c.FetchTimeout, RenderTimeout: c.RenderTimeout, Renderer: r}
	return crawlDeps{
		renderer: r,
		fetcher:  f,
		discovery: &discovery.Service{
			Fetcher: f, Renderer: r, URLs: postgres.URLs{Pool: d.pool},
			MaxPages: c.DiscoveryMaxPages, RenderTimeout: c.RenderTimeout, Log: d.log,
		},
	}
}
