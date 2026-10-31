package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hneyra/crawler-go/internal/httpapi"
)

type pinger struct{ err error }

func (p pinger) Ping(context.Context) error { return p.err }

func newServer(t *testing.T, db httpapi.Pinger) *httptest.Server {
	t.Helper()
	h, err := httpapi.NewHandler(&httpapi.Server{Ops: &httpapi.Ops{DB: db, Version: "v-test"}},
		httpapi.Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

type resp struct {
	StatusCode int
	Header     http.Header
}

func get(t *testing.T, url string) (resp, map[string]any) {
	t.Helper()
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	return resp{StatusCode: res.StatusCode, Header: res.Header}, body
}

func TestHealthz(t *testing.T) {
	srv := newServer(t, pinger{})
	res, body := get(t, srv.URL+"/healthz")
	if res.StatusCode != 200 || body["status"] != "ok" || body["version"] != "v-test" {
		t.Fatalf("got %d %v", res.StatusCode, body)
	}
	if res.Header.Get("X-Request-Id") == "" || res.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing middleware headers: %v", res.Header)
	}
}

func TestReadyz(t *testing.T) {
	res, _ := get(t, newServer(t, pinger{}).URL+"/readyz")
	if res.StatusCode != 200 {
		t.Fatalf("ready: %d", res.StatusCode)
	}
	res, body := get(t, newServer(t, pinger{err: errors.New("down")}).URL+"/readyz")
	if res.StatusCode != 503 || res.Header.Get("Content-Type") != "application/problem+json" || body["status"] != float64(503) {
		t.Fatalf("not ready: %d %v", res.StatusCode, body)
	}
}

func TestUnknownRouteIsProblem404(t *testing.T) {
	res, body := get(t, newServer(t, pinger{}).URL+"/nope")
	if res.StatusCode != 404 || body["code"] != "route_not_found" {
		t.Fatalf("got %d %v", res.StatusCode, body)
	}
}
