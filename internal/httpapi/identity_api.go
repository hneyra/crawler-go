package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/hneyra/crawler-go/internal/actor"
	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/identity"
)

// AuthAPI = login/logout/session. cookie lives here, never in identity.
type AuthAPI struct {
	Identity     *identity.Service
	SessionTTL   time.Duration
	CookieSecure bool
}

var errNoBody = apperr.Invalid("missing_body", "Falta el cuerpo de la solicitud")

func (a *AuthAPI) ready() error {
	if a == nil || a.Identity == nil {
		return apperr.NotImplemented()
	}
	return nil
}

func sessionDTO(u identity.User, s identity.Session) Session {
	return Session{User: User{Id: u.ID, Email: u.Email, IsAdmin: u.IsAdmin}, CsrfToken: s.CSRFToken, ExpiresAt: s.ExpiresAt.UTC()}
}

func (a *AuthAPI) cookie(value string, maxAge int, expires time.Time) string {
	// Secure from config; prod default true.
	c := &http.Cookie{Name: SessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: a.CookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: maxAge, Expires: expires} //nolint:gosec // G124: Secure set from config
	return c.String()
}

func (a *AuthAPI) Login(ctx context.Context, req LoginRequestObject) (LoginResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, errNoBody
	}
	res, err := a.Identity.Login(ctx, actor.From(ctx), req.Body.Email, req.Body.Password)
	if err != nil {
		return nil, err
	}
	c := a.cookie(res.Token, int(a.SessionTTL.Seconds()), res.Session.ExpiresAt)
	return noStoreLogin{Login200JSONResponse{Body: sessionDTO(res.User, res.Session), Headers: Login200ResponseHeaders{SetCookie: &c}}}, nil
}

// noStore wrappers: body carries csrf token (+ cookie). never cached by proxy/browser.
type noStoreLogin struct{ Login200JSONResponse }

func (r noStoreLogin) VisitLoginResponse(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "no-store")
	return r.Login200JSONResponse.VisitLoginResponse(w)
}

type noStoreSession struct{ GetSession200JSONResponse }

func (r noStoreSession) VisitGetSessionResponse(w http.ResponseWriter) error {
	w.Header().Set("Cache-Control", "no-store")
	return r.GetSession200JSONResponse.VisitGetSessionResponse(w)
}

// logoutResponse = 204 + cookie clear. generated 204 type has no headers.
type logoutResponse struct{ cookie string }

func (r logoutResponse) VisitLogoutResponse(w http.ResponseWriter) error {
	w.Header().Add("Set-Cookie", r.cookie)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *AuthAPI) Logout(ctx context.Context, _ LogoutRequestObject) (LogoutResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if err := a.Identity.Logout(ctx, actor.From(ctx)); err != nil {
		return nil, err
	}
	return logoutResponse{cookie: a.cookie("", -1, time.Unix(0, 0))}, nil
}

func (a *AuthAPI) GetSession(ctx context.Context, _ GetSessionRequestObject) (GetSessionResponseObject, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	u, s, err := a.Identity.Current(ctx, actor.From(ctx))
	if err != nil {
		return nil, err
	}
	return noStoreSession{GetSession200JSONResponse(sessionDTO(u, s))}, nil
}
