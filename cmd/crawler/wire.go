package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/httpapi"
	"github.com/hneyra/crawler-go/internal/platform/config"
	"github.com/hneyra/crawler-go/internal/platform/db"
	"github.com/hneyra/crawler-go/internal/postgres"
)

// deps = shared infra for module wiring. explicit, no container.
type deps struct {
	cfg  config.Config
	log  *slog.Logger
	pool *pgxpool.Pool
}

func open(ctx context.Context, log *slog.Logger) (deps, func(), error) {
	cfg, err := config.FromEnv()
	if err != nil {
		return deps{}, nil, fmt.Errorf("config: %w", err)
	}
	pool, err := db.OpenWith(ctx, cfg.DatabaseURL, db.Options{MaxConns: int32(cfg.DBMaxConns)}) //nolint:gosec // validated 1..1000
	if err != nil {
		return deps{}, nil, err
	}
	return deps{cfg: cfg, log: log, pool: pool}, pool.Close, nil
}

// modules = what each module wiring returns.
type modules struct {
	server   *httpapi.Server
	sessions httpapi.SessionResolver
}

// build wires every module. one wireX per module as they land.
func build(d deps) (modules, error) {
	ids, err := identityService(d)
	if err != nil {
		return modules{}, fmt.Errorf("wire identity: %w", err)
	}
	return modules{
		server: &httpapi.Server{
			Ops:      &httpapi.Ops{DB: d.pool, Version: version},
			AuthAPI:  wireAuth(d, ids),
			SitesAPI: wireSites(d),
			RunsAPI: &httpapi.RunsAPI{
				Pipeline: pipelineService(d), Runs: postgres.Runs{Pool: d.pool},
				URLs: postgres.URLs{Pool: d.pool}, Items: postgres.Items{Pool: d.pool},
			},
		},
		sessions: ids,
	}, nil
}
