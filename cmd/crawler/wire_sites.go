package main

import (
	"github.com/hneyra/crawler-go/internal/crawl"
	"github.com/hneyra/crawler-go/internal/httpapi"
	"github.com/hneyra/crawler-go/internal/platform/db"
	"github.com/hneyra/crawler-go/internal/postgres"
)

func catalog(d deps) crawl.Catalog {
	return crawl.Catalog{
		Tx:         db.Runner{Pool: d.pool},
		Sites:      postgres.Sites{Pool: d.pool},
		Categories: postgres.Categories{Pool: d.pool},
	}
}

func wireSites(d deps) *httpapi.SitesAPI {
	c := catalog(d)
	return &httpapi.SitesAPI{Catalog: c, Sites: c.Sites, Categories: c.Categories}
}
