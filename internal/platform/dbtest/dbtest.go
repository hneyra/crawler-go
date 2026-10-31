// Package dbtest gives each test a fresh migrated postgres db. real db, no mocks.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/platform/db"
)

// EnvURL = admin url of test server. make test sets it.
const EnvURL = "CRAWLER_TEST_DATABASE_URL"

// New creates db, migrates up, drops on cleanup.
// no url: skip locally, fail in CI. integration tests must run in CI.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool, _ := NewWithURL(t, true)
	return pool
}

// NewEmpty creates db without migrations. for migration tests.
func NewEmpty(t testing.TB) (*pgxpool.Pool, string) {
	t.Helper()
	return NewWithURL(t, false)
}

// NewWithURL = New, also returns db url.
func NewWithURL(t testing.TB, migrate bool) (*pgxpool.Pool, string) {
	t.Helper()
	admin := os.Getenv(EnvURL)
	if admin == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s not set in CI", EnvURL)
		}
		t.Skipf("%s not set; run `make test` (needs compose db)", EnvURL)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	adminConn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	name := "crawler_t_" + randHex(6)
	if _, err := adminConn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create db: %v", err)
	}
	_ = adminConn.Close(ctx)

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	u.Path = "/" + name
	pool, err := db.Open(ctx, u.String())
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		cctx, ccancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer ccancel()
		c, err := pgx.Connect(cctx, admin)
		if err != nil {
			t.Logf("cleanup connect: %v", err)
			return
		}
		defer func() { _ = c.Close(cctx) }()
		if _, err := c.Exec(cctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Logf("drop db %s: %v", name, err)
		}
	})
	if migrate {
		if err := db.MigrateUp(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	return pool, u.String()
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
