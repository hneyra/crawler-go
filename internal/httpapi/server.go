// Package httpapi = http transport. generated from api/openapi.yaml (strict server).
// handlers map dto <-> domain. no business rules here.
package httpapi

import (
	"log/slog"
	"net/http"
	"net/netip"

	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/platform/httpx"
)

// Server = full StrictServerInterface. built from per-module handler sets.
type Server struct {
	*Ops
}

var _ StrictServerInterface = (*Server)(nil)

// Options for handler.
type Options struct {
	Log            *slog.Logger
	PublicURL      string // allowed Origin for unsafe browser requests
	TrustedProxies []netip.Prefix
	MaxBodyBytes   int64          // cap for json bodies
	Metrics        *httpx.Metrics // nil -> no http metrics
}

// NewHandler builds full http handler: middleware + routes from spec.
func NewHandler(s *Server, o Options) (http.Handler, error) {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.MaxBodyBytes == 0 {
		o.MaxBodyBytes = 1 << 20
	}
	writeErr := func(w http.ResponseWriter, r *http.Request, err error) { httpx.WriteError(w, r, o.Log, err) }
	badReq := func(w http.ResponseWriter, r *http.Request, err error) {
		httpx.WriteError(w, r, o.Log, apperr.Invalid("bad_request", "Solicitud inválida: "+sanitize(err.Error())))
	}
	strict := NewStrictHandlerWithOptions(s, nil,
		StrictHTTPServerOptions{RequestErrorHandlerFunc: badReq, ResponseErrorHandlerFunc: writeErr},
	)
	v, err := newRequestValidator(o.Log)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	routes := HandlerWithOptions(strict, StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: badReq, Middlewares: []MiddlewareFunc{v.Middleware}})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r, o.Log, apperr.NotFound("route_not_found", "Ruta no encontrada"))
	})
	// recover inside metrics + logging: panic still counted + access logged.
	routes = httpx.WithRecover(o.Log)(routes)
	if o.Metrics != nil {
		routes = o.Metrics.Middleware(routes)
	}
	return httpx.Chain(routes,
		httpx.WithRequestID,
		httpx.WithClientIP(o.TrustedProxies),
		httpx.WithLogging(o.Log),
		httpx.WithRecover(o.Log), // middleware panics
		httpx.WithSecurityHeaders,
		httpx.WithMaxBody(o.MaxBodyBytes),
	), nil
}

// sanitize keeps parse errors short. no echo of huge input.
func sanitize(s string) string {
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
