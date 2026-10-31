// Package httpapi = http transport. generated from api/openapi.yaml (strict server).
// handlers map dto <-> domain. no business rules here.
package httpapi

import (
	"log/slog"
	"net/http"
	"net/netip"

	"github.com/hneyra/crawler-go/internal/actor"
	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/platform/httpx"
)

// Server = full StrictServerInterface. built from per-module handler sets.
type Server struct {
	*Ops
	*AuthAPI  // login, logout, session
	*SitesAPI // sites + categories config
	*RunsAPI  // runs, stats, items
}

var _ StrictServerInterface = (*Server)(nil)

// Options for handler.
type Options struct {
	Log            *slog.Logger
	PublicURL      string // allowed Origin for unsafe browser requests
	TrustedProxies []netip.Prefix
	MaxBodyBytes   int64           // cap for json bodies
	Metrics        *httpx.Metrics  // nil -> no http metrics
	Sessions       SessionResolver // nil -> nobody logs in
	Limits         *Limits         // nil -> DefaultLimits
}

// NewHandler builds full http handler: middleware + routes from spec.
func NewHandler(s *Server, o Options) (http.Handler, error) {
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.MaxBodyBytes == 0 {
		o.MaxBodyBytes = 1 << 20
	}
	if o.Limits == nil {
		o.Limits = DefaultLimits()
	}
	rules, err := operationRules()
	if err != nil {
		return nil, err
	}
	writeErr := func(w http.ResponseWriter, r *http.Request, err error) { httpx.WriteError(w, r, o.Log, err) }
	badReq := func(w http.ResponseWriter, r *http.Request, err error) {
		httpx.WriteError(w, r, o.Log, apperr.Invalid("bad_request", "Solicitud inválida: "+sanitize(err.Error())))
	}
	strict := NewStrictHandlerWithOptions(s, []StrictMiddlewareFunc{authorize(rules, o.PublicURL), rateLimit(o.Limits)},
		StrictHTTPServerOptions{RequestErrorHandlerFunc: badReq, ResponseErrorHandlerFunc: writeErr},
	)
	v, err := newRequestValidator(o.Log, rules, o.PublicURL)
	if err != nil {
		return nil, err
	}
	// param binding (e.g. required X-CSRF-Token header) fails before strict middleware.
	// auth first: anonymous caller gets 401/403, not a 400 that leaks the contract.
	bindErr := func(w http.ResponseWriter, r *http.Request, err error) {
		if rule, ok := v.rules[r.Pattern]; ok {
			if aerr := checkAuth(rule, o.PublicURL, actor.From(r.Context()), r); aerr != nil {
				writeErr(w, r, aerr)
				return
			}
		}
		badReq(w, r, err)
	}
	mux := http.NewServeMux()
	routes := HandlerWithOptions(strict, StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: bindErr, Middlewares: []MiddlewareFunc{v.Middleware}})
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
		resolveActor(o),
	), nil
}

// sanitize keeps parse errors short. no echo of huge input.
func sanitize(s string) string {
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
