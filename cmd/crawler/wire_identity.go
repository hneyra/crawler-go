package main

import (
	"github.com/hneyra/crawler-go/internal/httpapi"
	"github.com/hneyra/crawler-go/internal/identity"
	"github.com/hneyra/crawler-go/internal/platform/db"
	"github.com/hneyra/crawler-go/internal/postgres"
)

func identityService(d deps) (*identity.Service, error) {
	return identity.New(postgres.Identity{Pool: d.pool}, db.Runner{Pool: d.pool}, d.log, identity.Settings{
		SessionTTL:      d.cfg.SessionTTL,
		HashConcurrency: d.cfg.PasswordHashConcurrency,
	}, nil)
}

func wireAuth(d deps, svc *identity.Service) *httpapi.AuthAPI {
	return &httpapi.AuthAPI{Identity: svc, SessionTTL: d.cfg.SessionTTL, CookieSecure: d.cfg.CookieSecure}
}
