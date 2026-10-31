package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"

	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/platform/httpx"
)

// maxValidationFields caps field errors in one problem.
const maxValidationFields = 20

// requestValidator enforces spec constraints (maxLength, maximum, maxItems, enum...) at runtime.
// route found by mux pattern -> no second router. auth = ours, not spec security.
type requestValidator struct {
	routes map[string]*routers.Route // "GET /api/v1/sites" -> route
	log    *slog.Logger
}

func newRequestValidator(log *slog.Logger) (*requestValidator, error) {
	spec, err := GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load embedded spec: %w", err)
	}
	spec.Servers = nil
	v := &requestValidator{routes: map[string]*routers.Route{}, log: log}
	for path, item := range spec.Paths.Map() {
		for method, op := range item.Operations() {
			// security checked by authorize middleware. empty = skip in filter.
			cp := *op
			cp.Security = &openapi3.SecurityRequirements{}
			v.routes[method+" "+path] = &routers.Route{Spec: spec, Path: path, PathItem: item, Method: method, Operation: &cp}
		}
	}
	return v, nil
}

// Middleware runs inside mux (r.Pattern set), before param binding + strict handler.
func (v *requestValidator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := v.routes[r.Pattern]
		if route == nil {
			next.ServeHTTP(w, r)
			return
		}
		opts := &openapi3filter.Options{
			MultiError:          true,
			SkipSettingDefaults: true, // request stays as sent
			AuthenticationFunc:  openapi3filter.NoopAuthenticationFunc,
		}
		in := &openapi3filter.RequestValidationInput{Request: r, PathParams: pathParams(r, route), Route: route, Options: opts}
		if err := openapi3filter.ValidateRequest(r.Context(), in); err != nil {
			httpx.WriteError(w, r, v.log, validationProblem(err))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func pathParams(r *http.Request, route *routers.Route) map[string]string {
	out := map[string]string{}
	add := func(ps openapi3.Parameters) {
		for _, p := range ps {
			if p.Value != nil && p.Value.In == openapi3.ParameterInPath {
				out[p.Value.Name] = r.PathValue(p.Value.Name)
			}
		}
	}
	add(route.PathItem.Parameters)
	add(route.Operation.Parameters)
	return out
}

// validationProblem -> 400 with field errors. reasons only, never input values.
func validationProblem(err error) error {
	var fields []apperr.FieldError
	collectFields(err, "", &fields)
	if len(fields) == 0 {
		fields = append(fields, apperr.FieldError{Field: "request", Code: "invalid", Message: "Solicitud inválida"})
	}
	if len(fields) > maxValidationFields {
		fields = fields[:maxValidationFields]
	}
	return apperr.Invalid("validation_failed", "La solicitud no cumple el contrato de la API", fields...)
}

func collectFields(err error, field string, out *[]apperr.FieldError) {
	if err == nil || len(*out) >= maxValidationFields {
		return
	}
	// type switch, not errors.As: wrappers unwrap into each other and lose param names.
	switch e := err.(type) { //nolint:errorlint // exact node of kin error tree wanted; errors.As unwraps past param names
	case openapi3.MultiError:
		for _, x := range e {
			collectFields(x, field, out)
		}
	case *openapi3filter.RequestError:
		f := "body"
		if e.Parameter != nil {
			f = e.Parameter.Name
		}
		switch e.Err.(type) { //nolint:errorlint // same: direct child only
		case openapi3.MultiError, *openapi3.SchemaError:
			collectFields(e.Err, f, out)
		default:
			*out = append(*out, apperr.FieldError{Field: f, Code: "invalid", Message: short(e.Reason)})
		}
	case *openapi3.SchemaError:
		f := field
		if ptr := e.JSONPointer(); len(ptr) > 0 {
			p := strings.Join(ptr, ".")
			if f == "" || f == "body" {
				f = p
			} else {
				f += "." + p
			}
		}
		if f == "" {
			f = "body"
		}
		*out = append(*out, apperr.FieldError{Field: short(f), Code: "schema", Message: short(e.Reason)})
	default:
		var se *openapi3.SchemaError
		if errors.As(err, &se) {
			collectFields(se, field, out)
			return
		}
		if field == "" {
			field = "request"
		}
		*out = append(*out, apperr.FieldError{Field: field, Code: "invalid", Message: "Valor no válido"})
	}
}

// short caps text echoed back. reasons never contain the value, cap anyway.
func short(s string) string {
	if s == "" {
		return "Valor no válido"
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
