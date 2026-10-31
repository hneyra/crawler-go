// Package httpx = shared http plumbing: problem json, middleware, client ip, rate limit.
package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strings"
	"time"

	"github.com/hneyra/crawler-go/internal/apperr"
)

// Problem = RFC 9457 body.
type Problem struct {
	Type   string              `json:"type"`
	Title  string              `json:"title"`
	Status int                 `json:"status"`
	Detail string              `json:"detail,omitempty"`
	Code   string              `json:"code,omitempty"`
	Errors []apperr.FieldError `json:"errors,omitempty"`
}

// StatusOf maps app error kind -> http status.
func StatusOf(k apperr.Kind) int {
	switch k {
	case apperr.KindInvalid:
		return http.StatusBadRequest
	case apperr.KindNotFound:
		return http.StatusNotFound
	case apperr.KindConflict:
		return http.StatusConflict
	case apperr.KindUnauthorized:
		return http.StatusUnauthorized
	case apperr.KindForbidden:
		return http.StatusForbidden
	case apperr.KindRateLimited:
		return http.StatusTooManyRequests
	case apperr.KindTooLarge:
		return http.StatusRequestEntityTooLarge
	case apperr.KindUnavailable:
		return http.StatusServiceUnavailable
	case apperr.KindNotImplemented:
		return http.StatusNotImplemented
	default:
		return http.StatusInternalServerError
	}
}

// WriteError writes problem json. internal errors logged, never leaked.
func WriteError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	ae, ok := apperr.As(err)
	if !ok {
		log.ErrorContext(r.Context(), "internal error", "err", err, "path", r.URL.Path)
		WriteProblem(w, Problem{Type: "about:blank", Title: "Error interno", Status: 500, Code: "internal"})
		return
	}
	status := StatusOf(ae.Kind)
	if status >= 500 {
		log.ErrorContext(r.Context(), "server error", "err", err, "path", r.URL.Path)
	}
	WriteProblem(w, Problem{
		Type:   "https://crawler.example/problems/" + ae.Code,
		Title:  ae.Message,
		Status: status,
		Code:   ae.Code,
		Errors: ae.Fields,
	})
}

// WriteProblem writes body with problem content type.
func WriteProblem(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

type ctxKey int

const (
	keyRequestID ctxKey = iota
	keyClientIP
)

// RequestID from ctx.
func RequestID(ctx context.Context) string { s, _ := ctx.Value(keyRequestID).(string); return s }

// ClientIP from ctx. set by ClientIPMiddleware.
func ClientIP(ctx context.Context) string { s, _ := ctx.Value(keyClientIP).(string); return s }

// Middleware = std wrapper.
type Middleware func(http.Handler) http.Handler

// Chain applies outermost first.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// WithRequestID sets X-Request-Id. incoming id ignored -> no log injection.
func WithRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		id := hex.EncodeToString(b)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyRequestID, id)))
	})
}

// WithClientIP trusts X-Forwarded-For only from trusted proxies. rightmost untrusted hop wins.
func WithClientIP(trusted []netip.Prefix) Middleware {
	isTrusted := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				host = r.RemoteAddr
			}
			ip := host
			if remote, err := netip.ParseAddr(host); err == nil && isTrusted(remote) {
				hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
				for i := len(hops) - 1; i >= 0; i-- {
					a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
					if err != nil {
						break
					}
					ip = a.String()
					if !isTrusted(a) {
						break
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyClientIP, ip)))
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// WithLogging logs one line per request. no query string (may hold tokens).
// request_id added by ContextHandler (log must use it).
func WithLogging(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			log.InfoContext(r.Context(), "http",
				"method", r.Method, "path", r.URL.Path, "status", rec.status,
				"bytes", rec.bytes, "dur_ms", time.Since(start).Milliseconds())
		})
	}
}

// ContextHandler adds request_id from ctx to every record. wrap root handler once.
type ContextHandler struct{ slog.Handler }

// NewContextHandler wraps h.
func NewContextHandler(h slog.Handler) *ContextHandler { return &ContextHandler{Handler: h} }

// Handle adds request_id when ctx has one.
func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs keeps wrapper.
func (h *ContextHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return &ContextHandler{Handler: h.Handler.WithAttrs(as)}
}

// WithGroup keeps wrapper.
func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{Handler: h.Handler.WithGroup(name)}
}

// WithRecover turns panic into 500. stack to log only.
func WithRecover(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					if err, ok := p.(error); ok && errors.Is(err, http.ErrAbortHandler) {
						panic(p)
					}
					log.ErrorContext(r.Context(), "panic", "panic", p, "stack", string(debug.Stack()))
					WriteProblem(w, Problem{Type: "about:blank", Title: "Error interno", Status: 500, Code: "internal"})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// WithSecurityHeaders = api responses. html csp lives in frontend.
func WithSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// WithMaxBody caps request body. multipart routes set own bigger cap.
func WithMaxBody(n int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}
