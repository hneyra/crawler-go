package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hneyra/crawler-go/internal/httpapi"
)

func TestAuthFlowAndGuards(t *testing.T) {
	env := newEnv(t)
	anon := env.anon(t)
	var p problem

	// no session -> 401 on admin route and on session route.
	if st := anon.do("GET", "/api/v1/sites", nil, &p); st != 401 || p.Code != "login_required" {
		t.Fatalf("anon list: %d %+v", st, p)
	}
	if st := anon.do("GET", "/api/v1/auth/session", nil, nil); st != 401 {
		t.Fatalf("anon session: %d", st)
	}
	// health stays public.
	if st := anon.do("GET", "/healthz", nil, nil); st != 200 {
		t.Fatalf("healthz: %d", st)
	}
	// auth beats validation: anon with garbage body gets 401, not 400.
	if st := anon.do("POST", "/api/v1/sites", map[string]any{"junk": true}, nil); st != 401 {
		t.Fatalf("anon invalid body: %d", st)
	}

	// wrong password -> 401 generic.
	if st := anon.do("POST", "/api/v1/auth/login", map[string]any{"email": "nobody@x.test", "password": "whatever-long-pass"}, &p); st != 401 || p.Code != "invalid_credentials" {
		t.Fatalf("bad login: %d %+v", st, p)
	}

	// non-admin: session ok, admin routes 403.
	user := env.login(t, "user@crawler.test", false)
	var s httpapi.Session
	if st := user.do("GET", "/api/v1/auth/session", nil, &s); st != 200 || s.User.IsAdmin || s.User.Email != "user@crawler.test" {
		t.Fatalf("user session: %d %+v", st, s)
	}
	if st := user.do("GET", "/api/v1/sites", nil, &p); st != 403 || p.Code != "admin_required" {
		t.Fatalf("user list: %d %+v", st, p)
	}

	admin := env.login(t, "admin@crawler.test", true)
	if st := admin.do("GET", "/api/v1/sites", nil, nil); st != 200 {
		t.Fatalf("admin list: %d", st)
	}
	site := map[string]any{"name": "s", "base_url": "https://s.pe"}

	// csrf: missing / wrong -> 403.
	noCSRF := admin
	noCSRF.csrf = ""
	if st := noCSRF.do("POST", "/api/v1/sites", site, &p); st != 403 || p.Code != "csrf" {
		t.Fatalf("no csrf: %d %+v", st, p)
	}
	badCSRF := admin
	badCSRF.csrf = "nope"
	if st := badCSRF.do("POST", "/api/v1/sites", site, nil); st != 403 {
		t.Fatalf("bad csrf: %d", st)
	}
	// foreign origin -> 403 even with csrf.
	evil := admin
	evil.header = map[string]string{"Origin": "https://evil.test"}
	if st := evil.do("POST", "/api/v1/sites", site, &p); st != 403 || p.Code != "bad_origin" {
		t.Fatalf("bad origin: %d %+v", st, p)
	}
	ok := admin
	ok.header = map[string]string{"Origin": testOrigin}
	if st := ok.do("POST", "/api/v1/sites", site, nil); st != 201 {
		t.Fatalf("good origin: %d", st)
	}

	// logout revokes server side: old cookie dead.
	if st := admin.do("POST", "/api/v1/auth/logout", nil, nil); st != 204 {
		t.Fatalf("logout: %d", st)
	}
	if st := admin.do("GET", "/api/v1/sites", nil, nil); st != 401 {
		t.Fatalf("after logout: %d", st)
	}
}

func TestLoginCookieFlags(t *testing.T) {
	env := newEnv(t)
	if _, err := env.ids.CreateUser(t.Context(), "a@crawler.test", "correct horse battery", true); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(t.Context(), "POST", env.url+"/api/v1/auth/login",
		strings.NewReader(`{"email":"A@crawler.test","password":"correct horse battery"}`))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	c := res.Header.Get("Set-Cookie")
	if res.StatusCode != 200 || !strings.HasPrefix(c, httpapi.SessionCookie+"=") || !strings.Contains(c, "HttpOnly") || !strings.Contains(c, "SameSite=Lax") {
		t.Fatalf("login (case-insensitive email): %d %q", res.StatusCode, c)
	}
}

func TestLoginThrottle(t *testing.T) {
	env := newEnv(t)
	if _, err := env.ids.CreateUser(t.Context(), "a@crawler.test", "correct horse battery", true); err != nil {
		t.Fatal(err)
	}
	c := env.anon(t)
	for range 5 {
		c.do("POST", "/api/v1/auth/login", map[string]any{"email": "a@crawler.test", "password": "wrong password!!"}, nil)
	}
	var p problem
	// right password now locked out.
	if st := c.do("POST", "/api/v1/auth/login", map[string]any{"email": "a@crawler.test", "password": "correct horse battery"}, &p); st != 429 {
		t.Fatalf("throttle: %d %+v", st, p)
	}
}
