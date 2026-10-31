package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/hneyra/crawler-go/internal/extraction"
)

// discoverCmd runs discovery for one category in the foreground. debug/ops tool;
// the worker runs the same service from the job queue.
func discoverCmd(ctx context.Context, log *slog.Logger, args []string) error {
	if len(args) != 1 {
		return usageError{}
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		return fmt.Errorf("bad category id %q", args[0])
	}
	d, closeDB, err := open(ctx, log)
	if err != nil {
		return err
	}
	defer closeDB()
	cat := catalog(d)
	c, err := cat.Categories.GetCategory(ctx, id)
	if err != nil {
		return err
	}
	site, err := cat.Sites.GetSite(ctx, c.SiteID)
	if err != nil {
		return err
	}
	cr := wireCrawl(ctx, d)
	defer cr.renderer.Close()
	res, err := cr.discovery.DiscoverCategory(ctx, site, c)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(res)
}

// extractCmd processes PENDING urls of one site until none left (or ctx done). foreground tool.
func extractCmd(ctx context.Context, log *slog.Logger, args []string) error {
	if len(args) != 1 {
		return usageError{}
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		return fmt.Errorf("bad site id %q", args[0])
	}
	d, closeDB, err := open(ctx, log)
	if err != nil {
		return err
	}
	defer closeDB()
	cr := wireCrawl(ctx, d)
	defer cr.renderer.Close()
	var total extraction.Batch
	for {
		b, err := cr.extraction.ProcessBatch(ctx, id)
		total.Add(b)
		if err != nil {
			return err
		}
		if b.Claimed == 0 {
			break
		}
	}
	return json.NewEncoder(os.Stdout).Encode(total)
}
