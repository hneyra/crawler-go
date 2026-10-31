package postgres_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hneyra/crawler-go/internal/actor"
	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/identity"
	"github.com/hneyra/crawler-go/internal/platform/db"
	"github.com/hneyra/crawler-go/internal/platform/dbtest"
	"github.com/hneyra/crawler-go/internal/postgres"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newIdentity(t *testing.T) (*identity.Service, *clock) {
	pool := dbtest.New(t)
	c := &clock{t: time.Now().UTC().Truncate(time.Microsecond)}
	svc, err := identity.New(postgres.Identity{Pool: pool}, db.Runner{Pool: pool}, slog.New(slog.NewTextHandler(io.Discard, nil)),
		identity.Settings{SessionTTL: 24 * time.Hour, IdleTTL: time.Hour}, c.now)
	if err != nil {
		t.Fatal(err)
	}
	return svc, c
}

const pw = "correct horse battery"

func TestCreateUserRules(t *testing.T) {
	svc, _ := newIdentity(t)
	ctx := context.Background()
	if _, err := svc.CreateUser(ctx, "not-an-email", pw, true); !apperr.IsKind(err, apperr.KindInvalid) {
		t.Fatalf("bad email: %v", err)
	}
	if _, err := svc.CreateUser(ctx, "a@crawler.test", "short", true); !apperr.IsKind(err, apperr.KindInvalid) {
		t.Fatalf("weak pw: %v", err)
	}
	u, err := svc.CreateUser(ctx, "a@crawler.test", pw, true)
	if err != nil || !u.IsAdmin || u.PasswordHash == pw {
		t.Fatalf("create: %v %+v", err, u)
	}
	if _, err := svc.CreateUser(ctx, "A@Crawler.test", pw, false); !apperr.IsKind(err, apperr.KindConflict) {
		t.Fatalf("dup (case-insensitive): %v", err)
	}
	if err := svc.Promote(ctx, "missing@crawler.test"); !apperr.IsKind(err, apperr.KindNotFound) {
		t.Fatalf("promote missing: %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	svc, clk := newIdentity(t)
	ctx := context.Background()
	if _, err := svc.CreateUser(ctx, "a@crawler.test", pw, false); err != nil {
		t.Fatal(err)
	}
	res, err := svc.Login(ctx, actor.Actor{}, "a@crawler.test", pw)
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.ResolveSession(ctx, res.Token)
	if err != nil || !a.IsUser() || a.IsAdmin || a.CSRFToken != res.Session.CSRFToken {
		t.Fatalf("resolve: %v %+v", err, a)
	}
	if _, err := svc.ResolveSession(ctx, res.Token+"x"); !apperr.IsKind(err, apperr.KindUnauthorized) {
		t.Fatalf("bad token: %v", err)
	}
	// promote shows on next resolve.
	if err := svc.Promote(ctx, "a@crawler.test"); err != nil {
		t.Fatal(err)
	}
	if a, _ := svc.ResolveSession(ctx, res.Token); !a.IsAdmin {
		t.Fatal("promote not visible")
	}
	// re-login rotates: previous session revoked.
	res2, err := svc.Login(ctx, a, "a@crawler.test", pw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveSession(ctx, res.Token); !apperr.IsKind(err, apperr.KindUnauthorized) {
		t.Fatalf("old session alive: %v", err)
	}
	// idle timeout.
	clk.t = clk.t.Add(59 * time.Minute)
	if _, err := svc.ResolveSession(ctx, res2.Token); err != nil {
		t.Fatalf("within idle: %v", err) // touches last_seen
	}
	clk.t = clk.t.Add(61 * time.Minute)
	if _, err := svc.ResolveSession(ctx, res2.Token); !apperr.IsKind(err, apperr.KindUnauthorized) {
		t.Fatalf("idle expired: %v", err)
	}
	// set-password revokes everything.
	clk.t = clk.t.Add(time.Minute)
	res3, _ := svc.Login(ctx, actor.Actor{}, "a@crawler.test", pw)
	if err := svc.SetPassword(ctx, "a@crawler.test", "another long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ResolveSession(ctx, res3.Token); !apperr.IsKind(err, apperr.KindUnauthorized) {
		t.Fatalf("session after set-password: %v", err)
	}
	if _, err := svc.Login(ctx, actor.Actor{}, "a@crawler.test", pw); !apperr.IsKind(err, apperr.KindUnauthorized) {
		t.Fatalf("old password works: %v", err)
	}
}

func TestLoginThrottleUnlocksAfterBackoff(t *testing.T) {
	svc, clk := newIdentity(t)
	ctx := context.Background()
	if _, err := svc.CreateUser(ctx, "a@crawler.test", pw, true); err != nil {
		t.Fatal(err)
	}
	for range identity.LoginFreeAttempts {
		if _, err := svc.Login(ctx, actor.Actor{}, "a@crawler.test", "wrong password!!"); !apperr.IsKind(err, apperr.KindUnauthorized) {
			t.Fatalf("wrong: %v", err)
		}
	}
	if _, err := svc.Login(ctx, actor.Actor{}, "a@crawler.test", pw); !apperr.IsKind(err, apperr.KindRateLimited) {
		t.Fatalf("locked: %v", err)
	}
	clk.t = clk.t.Add(identity.LoginBackoff(identity.LoginFreeAttempts) + time.Second)
	if _, err := svc.Login(ctx, actor.Actor{}, "A@crawler.test", pw); err != nil {
		t.Fatalf("after backoff: %v", err)
	}
	// unknown emails throttled too (no existence oracle).
	for range identity.LoginFreeAttempts {
		_, _ = svc.Login(ctx, actor.Actor{}, "ghost@crawler.test", "whatever password")
	}
	if _, err := svc.Login(ctx, actor.Actor{}, "ghost@crawler.test", "whatever password"); !apperr.IsKind(err, apperr.KindRateLimited) {
		t.Fatalf("ghost locked: %v", err)
	}
}
