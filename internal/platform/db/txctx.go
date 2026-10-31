package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txKey struct{}

// Runner runs fn in tx carried by ctx. domain sees only InTx(ctx, fn(ctx)). no pgx leak.
// nested InTx reuses outer tx.
type Runner struct{ Pool *pgxpool.Pool }

// InTx = tx boundary for use cases.
func (r Runner) InTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	return InTx(ctx, r.Pool, func(tx pgx.Tx) error {
		return fn(context.WithValue(ctx, txKey{}, tx))
	})
}

// Active reports ctx carries open tx. caches skip tx reads (uncommitted rows never cached).
func (r Runner) Active(ctx context.Context) bool {
	_, ok := ctx.Value(txKey{}).(pgx.Tx)
	return ok
}

// Q = tx from ctx if any, else pool. adapters call this every query.
func Q(ctx context.Context, pool *pgxpool.Pool) Querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}
