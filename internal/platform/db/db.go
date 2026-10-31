// Package db = postgres pool, tx helper, migrations.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/hneyra/crawler-go/migrations"
)

// Querier = what both pool and tx can do. adapters take this.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// Options = pool tuning. zero value = pgx defaults.
type Options struct {
	// MaxConns = pool size. 0 -> pgx default max(4, NumCPU). url pool_max_conns loses to it.
	MaxConns int32
}

// Open makes pool + pings with default options. caller closes.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	return OpenWith(ctx, url, Options{})
}

// OpenWith makes pool with options + pings. caller closes.
func OpenWith(ctx context.Context, url string, o Options) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if o.MaxConns > 0 {
		cfg.MaxConns = o.MaxConns
	}
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.RuntimeParams["application_name"] = "crawler"
	// utc in db session. display tz = frontend job.
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	// jit off: short oltp queries; jit compile cost > gain. url ?jit=on keeps it.
	if _, ok := cfg.ConnConfig.RuntimeParams["jit"]; !ok {
		cfg.ConnConfig.RuntimeParams["jit"] = "off"
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// InTx runs fn in tx. commit on nil, rollback on error or panic.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) (err error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
		if err != nil {
			if rbErr := tx.Rollback(context.WithoutCancel(ctx)); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
			}
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// provider = goose over embedded sql. sql.DB via pgx stdlib.
func provider(pool *pgxpool.Pool) (*goose.Provider, *sql.DB, error) {
	sqlDB := stdlib.OpenDBFromPool(pool)
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, fsys())
	if err != nil {
		_ = sqlDB.Close()
		return nil, nil, fmt.Errorf("goose provider: %w", err)
	}
	return p, sqlDB, nil
}

func fsys() fs.FS { return migrations.FS }

// MigrateUp applies all pending. idempotent.
func MigrateUp(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	p, sqlDB, err := provider(pool)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	res, err := p.Up(ctx)
	for _, r := range res {
		log.InfoContext(ctx, "migration applied", "version", r.Source.Version, "path", r.Source.Path, "took", r.Duration)
	}
	if err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}

// MigrateDownTo rolls back to version (0 = all).
func MigrateDownTo(ctx context.Context, pool *pgxpool.Pool, version int64) error {
	p, sqlDB, err := provider(pool)
	if err != nil {
		return err
	}
	defer func() { _ = sqlDB.Close() }()
	if _, err := p.DownTo(ctx, version); err != nil {
		return fmt.Errorf("migrate down to %d: %w", version, err)
	}
	return nil
}

// MigrationStatus = current db version.
func MigrationStatus(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	p, sqlDB, err := provider(pool)
	if err != nil {
		return 0, err
	}
	defer func() { _ = sqlDB.Close() }()
	v, err := p.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("db version: %w", err)
	}
	return v, nil
}

// IsUniqueViolation reports pg 23505. optional constraint name match.
func IsUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

// IsForeignKeyViolation reports pg 23503.
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// IsCheckViolation reports pg 23514.
func IsCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
