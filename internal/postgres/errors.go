// Package postgres = persistence adapters. hand-written sql over pgx. one file per module.
package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/platform/db"
)

// notFound maps no rows -> apperr not_found. other errors pass through.
func notFound(err error, code, msg string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound(code, msg).Wrap(err)
	}
	return err
}

// conflict maps unique violation -> apperr conflict.
func conflict(err error, constraint, code, msg string) error {
	if db.IsUniqueViolation(err, constraint) {
		return apperr.Conflict(code, msg).Wrap(err)
	}
	return err
}
