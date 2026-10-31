package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type fakeTicker struct{ t atomic.Int64 }

func (f *fakeTicker) LastTick() time.Time {
	if n := f.t.Load(); n != 0 {
		return time.Unix(0, n)
	}
	return time.Time{}
}

func TestWorkerHealthHandler(t *testing.T) {
	ft := &fakeTicker{}
	h := healthHandler(ft, 30*time.Second)
	get := func(path string) int {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec.Code
	}
	if c := get("/healthz"); c != http.StatusServiceUnavailable {
		t.Fatalf("no tick yet: %d", c)
	}
	ft.t.Store(time.Now().UnixNano())
	if c := get("/healthz"); c != http.StatusOK {
		t.Fatalf("fresh tick: %d", c)
	}
	ft.t.Store(time.Now().Add(-31 * time.Second).UnixNano())
	if c := get("/healthz"); c != http.StatusServiceUnavailable {
		t.Fatalf("stale tick: %d", c)
	}
	if c := get("/other"); c != http.StatusNotFound {
		t.Fatalf("other path: %d", c)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post: %d", rec.Code)
	}
}

func TestWorkerHealthServerLifecycle(t *testing.T) {
	ft := &fakeTicker{}
	ft.t.Store(time.Now().UnixNano())
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stop := startHealth(log, ln, healthHandler(ft, time.Minute))
	url := "http://" + ln.Addr().String() + "/healthz"
	resp, err := http.Get(url) //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	// probe command used by compose/k8s against same listener.
	if err := healthcheck(context.Background(), []string{url}); err != nil {
		t.Fatalf("healthcheck cmd: %v", err)
	}
	// default transport may dial spare conn that never sends a request (StateNew).
	// Shutdown waits 5s for those -> flaky deadline under -race. drop client side first.
	http.DefaultClient.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := stop(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if resp, err := http.Get(url); err == nil { //nolint:noctx // test
		_ = resp.Body.Close()
		t.Fatal("listener still up after shutdown")
	}
}
