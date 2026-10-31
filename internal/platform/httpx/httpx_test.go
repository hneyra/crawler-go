package httpx

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/hneyra/crawler-go/internal/apperr"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestWriteErrorHidesInternalDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, httptest.NewRequest("GET", "/", nil), discard, errors.New("pq: password=secret leaked"))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content type %q", ct)
	}
}

func TestWriteErrorMapsKind(t *testing.T) {
	rec := httptest.NewRecorder()
	err := apperr.Invalid("bad_input", "Datos inválidos", apperr.FieldError{Field: "url", Code: "invalid_url", Message: "x"})
	WriteError(rec, httptest.NewRequest("GET", "/", nil), discard, err)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"field":"url"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	wrapped := apperr.NotFound("site_not_found", "Sitio no encontrado").Wrap(errors.New("sql: no rows"))
	rec = httptest.NewRecorder()
	WriteError(rec, httptest.NewRequest("GET", "/", nil), discard, wrapped)
	if rec.Code != 404 || strings.Contains(rec.Body.String(), "sql") {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestClientIPTrustsOnlyConfiguredProxies(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	var got string
	h := WithClientIP(trusted)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = ClientIP(r.Context()) }))

	cases := []struct{ remote, xff, want string }{
		{"203.0.113.9:1234", "1.2.3.4", "203.0.113.9"},             // untrusted peer: ignore xff
		{"10.1.1.1:1234", "1.2.3.4", "1.2.3.4"},                    // trusted proxy
		{"10.1.1.1:1234", "6.6.6.6, 1.2.3.4, 10.2.2.2", "1.2.3.4"}, // spoofed leftmost ignored
		{"10.1.1.1:1234", "garbage", "10.1.1.1"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		r.Header.Set("X-Forwarded-For", c.xff)
		h.ServeHTTP(httptest.NewRecorder(), r)
		if got != c.want {
			t.Errorf("remote=%s xff=%q: got %s want %s", c.remote, c.xff, got, c.want)
		}
	}
}

func TestLimiterBurstThenDeny(t *testing.T) {
	l := NewLimiter(1, 3)
	for i := 0; i < 3; i++ {
		if !l.Allow("k") {
			t.Fatalf("token %d denied", i)
		}
	}
	if l.Allow("k") {
		t.Fatal("4th allowed past burst")
	}
	if !l.Allow("other") {
		t.Fatal("keys not isolated")
	}
}

func TestRecoverReturns500(t *testing.T) {
	h := WithRecover(discard)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 || strings.Contains(rec.Body.String(), "boom") {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}
