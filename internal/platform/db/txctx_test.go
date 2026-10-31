package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hneyra/crawler-go/internal/platform/db"
	"github.com/hneyra/crawler-go/internal/platform/dbtest"
)

func TestRunnerCommitRollbackNested(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	r := db.Runner{Pool: pool}
	if _, err := pool.Exec(ctx, `CREATE TABLE tx_probe (slug text NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM tx_probe`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	ins := func(ctx context.Context, slug string) error {
		_, err := db.Q(ctx, pool).Exec(ctx, `INSERT INTO tx_probe (slug) VALUES ($1)`, slug)
		return err
	}
	if err := r.InTx(ctx, func(ctx context.Context) error {
		if err := ins(ctx, "a"); err != nil {
			return err
		}
		// nested = same tx
		return r.InTx(ctx, func(ctx context.Context) error { return ins(ctx, "b") })
	}); err != nil {
		t.Fatal(err)
	}
	if count() != 2 {
		t.Fatalf("commit: %d", count())
	}
	boom := errors.New("boom")
	err := r.InTx(ctx, func(ctx context.Context) error {
		if err := ins(ctx, "c"); err != nil {
			return err
		}
		return r.InTx(ctx, func(ctx context.Context) error { return boom })
	})
	if !errors.Is(err, boom) || count() != 2 {
		t.Fatalf("rollback: err=%v count=%d", err, count())
	}
}
