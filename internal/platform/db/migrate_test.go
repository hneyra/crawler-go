package db_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/hneyra/crawler-go/internal/platform/db"
	"github.com/hneyra/crawler-go/internal/platform/dbtest"
)

// up -> down all -> up again on clean db. proves down sql valid + reversible.
func TestMigrationsUpDownUp(t *testing.T) {
	pool, _ := dbtest.NewEmpty(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := db.MigrateUp(ctx, pool, log); err != nil {
		t.Fatalf("first up: %v", err)
	}
	top, err := db.MigrationStatus(ctx, pool)
	if err != nil || top == 0 {
		t.Fatalf("status after up = %d, %v", top, err)
	}
	if err := db.MigrateDownTo(ctx, pool, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname='public' AND tablename <> 'goose_db_version'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("tables left after full down: %d", n)
	}
	if err := db.MigrateUp(ctx, pool, log); err != nil {
		t.Fatalf("second up: %v", err)
	}
	again, _ := db.MigrationStatus(ctx, pool)
	if again != top {
		t.Fatalf("version %d != %d", again, top)
	}
}
