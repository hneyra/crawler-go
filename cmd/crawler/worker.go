package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// workerHealthMaxAge: idle loop ticks every <= PollMax (5s), busy job heartbeats every lease/3.
// 30s silence = loop stuck (db hang, deadlock) -> 503 -> orchestrator restarts.
const workerHealthMaxAge = 30 * time.Second

func worker(ctx context.Context, log *slog.Logger) error {
	d, closeDB, err := open(ctx, log)
	if err != nil {
		return err
	}
	defer closeDB()
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	runner, closeBrowser := wireWorker(ctx, d, reg)
	defer closeBrowser()

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", d.cfg.WorkerHealthAddr)
	if err != nil {
		return fmt.Errorf("worker health listen %s: %w", d.cfg.WorkerHealthAddr, err)
	}
	mux := healthHandler(runner, workerHealthMaxAge)
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	stop := startHealth(log, ln, mux)
	log.Info("worker started", "concurrency", d.cfg.WorkerConcurrency, "health_addr", ln.Addr().String())
	runErr := runner.Run(ctx)
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return errors.Join(runErr, stop(sctx))
}

// ticker = runner liveness source.
type ticker interface {
	LastTick() time.Time
}

// healthHandler: GET /healthz 200 iff runner ticked within maxAge. else 503.
func healthHandler(t ticker, maxAge time.Duration) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		last := t.LastTick()
		if last.IsZero() || time.Since(last) > maxAge {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("stale\n"))
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	return mux
}

// startHealth serves h on ln. returned stop = graceful shutdown, waits serve goroutine.
func startHealth(log *slog.Logger, ln net.Listener, h http.Handler) func(context.Context) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("worker health server", "err", err)
		}
	}()
	return func(ctx context.Context) error {
		err := srv.Shutdown(ctx)
		<-done
		return err
	}
}
