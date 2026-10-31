package httpapi

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hneyra/crawler-go/internal/actor"
	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/platform/httpx"
)

// SessionCookie name. HttpOnly, set by auth handlers.
const SessionCookie = "crawler_session"

// SessionResolver: session token -> actor. identity.Service fits.
type SessionResolver interface {
	ResolveSession(ctx context.Context, token string) (actor.Actor, error)
}

// auth requirement per operation. derived from contract, not hand-kept.
type requirement int

const (
	reqPublic  requirement = iota // security: []
	reqSession                    // tag auth: any logged-in user
	reqAdmin                      // everything else: admin only
)

func operationRules() (map[string]requirement, error) {
	spec, err := GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load embedded spec: %w", err)
	}
	rules := map[string]requirement{}
	for _, item := range spec.Paths.Map() {
		for _, op := range item.Operations() {
			sec := op.Security
			if sec == nil {
				sec = &spec.Security
			}
			r := reqAdmin
			switch {
			case len(*sec) == 0:
				r = reqPublic
			case hasTag(op.Tags, "auth"):
				r = reqSession
			}
			rules[goName(op.OperationID)] = r
		}
	}
	return rules, nil
}

func hasTag(tags []string, t string) bool {
	for _, x := range tags {
		if x == t {
			return true
		}
	}
	return false
}

func goName(opID string) string {
	if opID == "" {
		return ""
	}
	return strings.ToUpper(opID[:1]) + opID[1:]
}

// resolveActor reads session cookie. bad/expired -> anonymous. route rules decide.
func resolveActor(o Options) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			a := actor.Actor{}
			if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" && o.Sessions != nil {
				got, err := o.Sessions.ResolveSession(ctx, c.Value)
				if err == nil {
					a = got
				} else if !apperr.IsKind(err, apperr.KindUnauthorized) {
					httpx.WriteError(w, r, o.Log, err)
					return
				}
			}
			next.ServeHTTP(w, r.WithContext(actor.With(ctx, a)))
		})
	}
}

func isUnsafe(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

// authorize enforces route auth class + csrf.
func authorize(rules map[string]requirement, publicURL string) StrictMiddlewareFunc {
	return func(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
		rule, known := rules[operationID]
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			if !known {
				return nil, errors.New("operation without auth rule: " + operationID)
			}
			if err := checkAuth(rule, publicURL, actor.From(ctx), r); err != nil {
				return nil, err
			}
			return f(ctx, w, r, req)
		}
	}
}

// checkAuth = auth class + origin + csrf. shared by authorize and request validator
// (validator skips work when auth fails -> 401/403 keep precedence over 400).
func checkAuth(rule requirement, publicURL string, a actor.Actor, r *http.Request) error {
	if isUnsafe(r.Method) {
		// browser unsafe request from other origin -> refuse. no Origin = non-browser client.
		if origin := r.Header.Get("Origin"); origin != "" && origin != publicURL {
			return apperr.Forbidden("bad_origin", "Origen no permitido")
		}
		if sfs := r.Header.Get("Sec-Fetch-Site"); sfs == "cross-site" || sfs == "same-site" {
			return apperr.Forbidden("bad_origin", "Origen no permitido")
		}
	}
	if rule == reqPublic {
		return nil
	}
	if !a.IsUser() {
		return apperr.Unauthorized("login_required", "Inicia sesión")
	}
	if rule == reqAdmin && !a.IsAdmin {
		return apperr.Forbidden("admin_required", "Solo administradores")
	}
	if isUnsafe(r.Method) {
		tok := r.Header.Get("X-CSRF-Token")
		if tok == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(a.CSRFToken)) != 1 {
			return apperr.Forbidden("csrf", "Token CSRF inválido")
		}
	}
	return nil
}

// rateLimit: login per ip, writes per user. reads unlimited (admin-only api).
func rateLimit(l *Limits) StrictMiddlewareFunc {
	return func(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			ok := true
			switch {
			case operationID == "Login":
				ok = l.Auth.Allow(httpx.ClientIP(ctx))
			case isUnsafe(r.Method):
				if a := actor.From(ctx); a.IsUser() {
					ok = l.Write.Allow(fmt.Sprint(a.UserID))
				}
			}
			if !ok {
				return nil, apperr.RateLimited("Demasiadas solicitudes; inténtalo de nuevo en unos segundos")
			}
			return f(ctx, w, r, req)
		}
	}
}
