package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/hneyra/crawler-go/internal/platform/db"
)

// migrate needs only db url. no full config -> usable as k8s job.
func migrate(ctx context.Context, log *slog.Logger, args []string) error {
	if len(args) == 0 {
		return usageError{}
	}
	url := os.Getenv("CRAWLER_DATABASE_URL")
	if url == "" {
		return fmt.Errorf("CRAWLER_DATABASE_URL required")
	}
	pool, err := db.Open(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	switch args[0] {
	case "up":
		return db.MigrateUp(ctx, pool, log)
	case "status":
		v, err := db.MigrationStatus(ctx, pool)
		if err != nil {
			return err
		}
		fmt.Println(v)
		return nil
	case "down-to":
		if len(args) != 2 {
			return usageError{}
		}
		v, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil || v < 0 {
			return fmt.Errorf("bad version %q", args[1])
		}
		// migrate skips config.Load -> unknown env ("production", "") treated as prod.
		if env := os.Getenv("CRAWLER_ENV"); env != "dev" && env != "test" && os.Getenv("CRAWLER_ALLOW_DOWN") != "yes" {
			return fmt.Errorf("down-to refused outside CRAWLER_ENV=dev|test without CRAWLER_ALLOW_DOWN=yes")
		}
		return db.MigrateDownTo(ctx, pool, v)
	default:
		return usageError{}
	}
}
