package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"

	"github.com/hneyra/crawler-go/internal/httpapi"
	"github.com/hneyra/crawler-go/internal/metrics"
	"github.com/hneyra/crawler-go/internal/platform/httpx"
	"github.com/hneyra/crawler-go/internal/postgres"
)

func serve(ctx context.Context, log *slog.Logger) error {
	d, closeDB, err := open(ctx, log)
	if err != nil {
		return err
	}
	defer closeDB()
	m, err := build(d)
	if err != nil {
		return err
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	metrics.NewTotals(reg, postgres.Stats{Pool: d.pool})
	limits := httpapi.DefaultLimits()
	if !d.cfg.RateLimit {
		// CRAWLER_RATE_LIMIT=off: dev/test only. config refuses prod.
		log.Warn("rate limiting disabled (CRAWLER_RATE_LIMIT=off)")
		limits = httpapi.NoLimits()
	}
	h, err := httpapi.NewHandler(m.server, httpapi.Options{
		Log:            log,
		PublicURL:      d.cfg.PublicURL,
		TrustedProxies: d.cfg.TrustedProxies,
		Metrics:        httpx.NewMetrics(reg),
		Sessions:       m.sessions,
		Limits:         limits,
	})
	if err != nil {
		return fmt.Errorf("build handler: %w", err)
	}

	api := &http.Server{
		Addr:              d.cfg.HTTPAddr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	metrics := &http.Server{Addr: d.cfg.MetricsAddr, Handler: metricsMux, ReadHeaderTimeout: 5 * time.Second}

	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error { limits.Run(gctx); return nil })
	for _, s := range []*http.Server{api, metrics} {
		g.Go(func() error {
			log.Info("listening", "addr", s.Addr)
			if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("listen %s: %w", s.Addr, err)
			}
			return nil
		})
	}
	g.Go(func() error {
		<-gctx.Done()
		// drain in-flight requests. new ctx: parent already cancelled.
		sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		log.Info("shutting down")
		return errors.Join(api.Shutdown(sctx), metrics.Shutdown(sctx))
	})
	return g.Wait()
}
