package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/hneyra/crawler-go/internal/sitesconfig"
)

// syncSites loads sites-config.yml (default ./sites-config.yml) into db. upsert by name.
func syncSites(ctx context.Context, log *slog.Logger, args []string) error {
	path := "sites-config.yml"
	if len(args) > 1 {
		return usageError{}
	}
	if len(args) == 1 {
		path = args[0]
	}
	fh, err := os.Open(path) //nolint:gosec // operator-supplied path
	if err != nil {
		return fmt.Errorf("open %s: %w (copy sites-config.example.yml)", path, err)
	}
	defer func() { _ = fh.Close() }()
	f, err := sitesconfig.Parse(fh)
	if err != nil {
		return err
	}
	d, closeDB, err := open(ctx, log)
	if err != nil {
		return err
	}
	defer closeDB()
	res, err := sitesconfig.Sync(ctx, catalog(d), f)
	if err != nil {
		return fmt.Errorf("sync %s: %w", path, err)
	}
	log.Info("sites synced", "file", path,
		"sites_created", res.SitesCreated, "sites_updated", res.SitesUpdated,
		"categories_created", res.CategoriesCreated, "categories_updated", res.CategoriesUpdated)
	return nil
}
