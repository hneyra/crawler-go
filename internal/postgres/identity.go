package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hneyra/crawler-go/internal/apperr"
	"github.com/hneyra/crawler-go/internal/identity"
	"github.com/hneyra/crawler-go/internal/platform/db"
)

// Identity implements identity.Repo.
type Identity struct{ Pool *pgxpool.Pool }

var _ identity.Repo = Identity{}

const userCols = `u.id, u.email, u.password_hash, u.is_admin, u.disabled`

func scanUser(row pgx.Row) (identity.User, error) {
	var u identity.User
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.Disabled)
	return u, err
}

var errUserNotFound = apperr.NotFound("user_not_found", "Usuario no encontrado")

func (r Identity) CreateUser(ctx context.Context, email, hash string, admin bool) (identity.User, error) {
	u, err := scanUser(db.Q(ctx, r.Pool).QueryRow(ctx,
		`INSERT INTO users AS u (email, password_hash, is_admin) VALUES ($1,$2,$3) RETURNING `+userCols, email, hash, admin))
	if err != nil {
		if db.IsUniqueViolation(err, "users_email_key") {
			return identity.User{}, apperr.Conflict("email_taken", "Ya existe una cuenta con ese email")
		}
		return identity.User{}, fmt.Errorf("insert user: %w", err)
	}
	return u, nil
}

func (r Identity) user(ctx context.Context, where string, arg any) (identity.User, error) {
	u, err := scanUser(db.Q(ctx, r.Pool).QueryRow(ctx, `SELECT `+userCols+` FROM users u WHERE `+where, arg))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.User{}, errUserNotFound
	}
	if err != nil {
		return identity.User{}, fmt.Errorf("load user: %w", err)
	}
	return u, nil
}

func (r Identity) UserByEmail(ctx context.Context, email string) (identity.User, error) {
	return r.user(ctx, `u.email = $1`, email)
}

func (r Identity) UserByID(ctx context.Context, id int64) (identity.User, error) {
	return r.user(ctx, `u.id = $1`, id)
}

func (r Identity) exec(ctx context.Context, what, sql string, args ...any) (int64, error) {
	tag, err := db.Q(ctx, r.Pool).Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", what, err)
	}
	return tag.RowsAffected(), nil
}

func (r Identity) SetPassword(ctx context.Context, id int64, hash string) error {
	_, err := r.exec(ctx, "set password", `UPDATE users SET password_hash=$2, updated_at=now() WHERE id=$1`, id, hash)
	return err
}

func (r Identity) SetAdmin(ctx context.Context, email string, admin bool) error {
	n, err := r.exec(ctx, "set admin", `UPDATE users SET is_admin=$2, updated_at=now() WHERE email=$1`, email, admin)
	if err == nil && n == 0 {
		return errUserNotFound
	}
	return err
}

func (r Identity) CreateSession(ctx context.Context, s identity.Session) (int64, error) {
	var id int64
	err := db.Q(ctx, r.Pool).QueryRow(ctx,
		`INSERT INTO sessions (token_hash, user_id, csrf_token, created_at, last_seen_at, expires_at) VALUES ($1,$2,$3,$4,$4,$5) RETURNING id`,
		s.TokenHash, s.UserID, s.CSRFToken, s.CreatedAt, s.ExpiresAt).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert session: %w", err)
	}
	return id, nil
}

const sessCols = `s.id, s.user_id, s.token_hash, s.csrf_token, s.created_at, s.last_seen_at, s.expires_at, s.revoked_at IS NOT NULL`

var errSessNotFound = apperr.NotFound("session_not_found", "Sesión no encontrada")

func (r Identity) SessionByHash(ctx context.Context, hash []byte) (identity.Session, identity.User, error) {
	var s identity.Session
	var u identity.User
	err := db.Q(ctx, r.Pool).QueryRow(ctx, `SELECT `+sessCols+`, `+userCols+` FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash=$1`, hash).
		Scan(&s.ID, &s.UserID, &s.TokenHash, &s.CSRFToken, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.Revoked,
			&u.ID, &u.Email, &u.PasswordHash, &u.IsAdmin, &u.Disabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, u, errSessNotFound
	}
	if err != nil {
		return s, u, fmt.Errorf("load session: %w", err)
	}
	return s, u, nil
}

func (r Identity) SessionByID(ctx context.Context, id int64) (identity.Session, error) {
	var s identity.Session
	err := db.Q(ctx, r.Pool).QueryRow(ctx, `SELECT `+sessCols+` FROM sessions s WHERE s.id=$1`, id).
		Scan(&s.ID, &s.UserID, &s.TokenHash, &s.CSRFToken, &s.CreatedAt, &s.LastSeenAt, &s.ExpiresAt, &s.Revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, errSessNotFound
	}
	if err != nil {
		return s, fmt.Errorf("load session: %w", err)
	}
	return s, nil
}

func (r Identity) TouchSession(ctx context.Context, id int64, at time.Time) error {
	_, err := r.exec(ctx, "touch session", `UPDATE sessions SET last_seen_at=$2 WHERE id=$1 AND last_seen_at < $2`, id, at)
	return err
}

func (r Identity) RevokeSession(ctx context.Context, id int64, at time.Time) error {
	_, err := r.exec(ctx, "revoke session", `UPDATE sessions SET revoked_at=$2 WHERE id=$1 AND revoked_at IS NULL`, id, at)
	return err
}

func (r Identity) RevokeUserSessions(ctx context.Context, userID int64, at time.Time) error {
	_, err := r.exec(ctx, "revoke user sessions", `UPDATE sessions SET revoked_at=$2 WHERE user_id=$1 AND revoked_at IS NULL`, userID, at)
	return err
}

func (r Identity) LoginLockedUntil(ctx context.Context, email string) (time.Time, error) {
	var until *time.Time
	err := db.Q(ctx, r.Pool).QueryRow(ctx, `SELECT locked_until FROM login_throttle WHERE email_norm = $1`, email).Scan(&until)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && until == nil) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("login lock: %w", err)
	}
	return *until, nil
}

// RecordLoginFailure: counter resets after 24h quiet.
func (r Identity) RecordLoginFailure(ctx context.Context, email string, now time.Time) (int, error) {
	var n int
	err := db.Q(ctx, r.Pool).QueryRow(ctx, `
		INSERT INTO login_throttle AS l (email_norm, failures, updated_at) VALUES ($1, 1, $2)
		ON CONFLICT (email_norm) DO UPDATE SET
		  failures = CASE WHEN l.updated_at < $2 - interval '24 hours' THEN 1 ELSE l.failures + 1 END,
		  updated_at = $2
		RETURNING failures`, email, now).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("record login failure: %w", err)
	}
	return n, nil
}

func (r Identity) LockLogin(ctx context.Context, email string, until time.Time) error {
	if _, err := db.Q(ctx, r.Pool).Exec(ctx, `UPDATE login_throttle SET locked_until = $2 WHERE email_norm = $1`, email, until); err != nil {
		return fmt.Errorf("lock login: %w", err)
	}
	return nil
}

func (r Identity) ClearLoginFailures(ctx context.Context, email string) error {
	if _, err := db.Q(ctx, r.Pool).Exec(ctx, `DELETE FROM login_throttle WHERE email_norm = $1`, email); err != nil {
		return fmt.Errorf("clear login failures: %w", err)
	}
	return nil
}
