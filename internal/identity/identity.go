// Package identity = admin users, password login, server sessions, login throttle.
// no self sign-up, no email flows: users made by `crawler admin create`.
package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/hneyra/crawler-go/internal/actor"
	"github.com/hneyra/crawler-go/internal/apperr"
)

// User = account. PasswordHash never leaves adapters/service.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	IsAdmin      bool
	Disabled     bool
}

// Session = server session row. token itself never stored.
type Session struct {
	ID         int64
	UserID     int64
	TokenHash  []byte
	CSRFToken  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	Revoked    bool
}

// Repo = persistence port.
type Repo interface {
	CreateUser(ctx context.Context, email, passwordHash string, admin bool) (User, error) // Conflict email_taken
	UserByEmail(ctx context.Context, email string) (User, error)                          // NotFound
	UserByID(ctx context.Context, id int64) (User, error)                                 // NotFound
	SetPassword(ctx context.Context, userID int64, hash string) error
	SetAdmin(ctx context.Context, email string, admin bool) error // NotFound

	CreateSession(ctx context.Context, s Session) (int64, error)
	SessionByHash(ctx context.Context, hash []byte) (Session, User, error) // NotFound
	SessionByID(ctx context.Context, id int64) (Session, error)            // NotFound
	TouchSession(ctx context.Context, id int64, at time.Time) error
	RevokeSession(ctx context.Context, id int64, at time.Time) error
	RevokeUserSessions(ctx context.Context, userID int64, at time.Time) error

	LoginLockedUntil(ctx context.Context, emailNorm string) (time.Time, error) // zero = not locked
	RecordLoginFailure(ctx context.Context, emailNorm string, now time.Time) (failures int, err error)
	LockLogin(ctx context.Context, emailNorm string, until time.Time) error
	ClearLoginFailures(ctx context.Context, emailNorm string) error
}

// Tx = tx boundary. db.Runner fits.
type Tx interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Settings for service.
type Settings struct {
	SessionTTL time.Duration
	IdleTTL    time.Duration // default = SessionTTL
	// HashConcurrency = max parallel argon2 (64 MiB each). default 4.
	HashConcurrency int
	// HashWait = max wait for argon2 slot, then 503 busy. default 3s.
	HashWait time.Duration
}

// Service = identity use cases.
type Service struct {
	repo  Repo
	tx    Tx
	log   *slog.Logger
	set   Settings
	now   func() time.Time
	dummy string // hash for unknown emails. equalizes login timing.
	// hashSem bounds argon2 memory: HashConcurrency * 64 MiB.
	hashSem chan struct{}
}

// New builds service. now nil -> time.Now.
func New(repo Repo, tx Tx, log *slog.Logger, set Settings, now func() time.Time) (*Service, error) {
	if now == nil {
		now = time.Now
	}
	if set.SessionTTL <= 0 {
		return nil, fmt.Errorf("identity: session ttl required")
	}
	if set.IdleTTL <= 0 {
		set.IdleTTL = set.SessionTTL
	}
	if set.HashConcurrency <= 0 {
		set.HashConcurrency = 4
	}
	if set.HashWait <= 0 {
		set.HashWait = 3 * time.Second
	}
	pw, _ := randomToken()
	dummy, err := HashPassword(pw)
	if err != nil {
		return nil, err
	}
	return &Service{repo: repo, tx: tx, log: log, set: set, now: now, dummy: dummy,
		hashSem: make(chan struct{}, set.HashConcurrency)}, nil
}

var errBusy = apperr.New(apperr.KindUnavailable, "busy", "Servicio ocupado; inténtalo de nuevo en unos segundos")

// withHashSlot runs fn holding one argon2 slot. full for HashWait -> busy.
func (s *Service) withHashSlot(ctx context.Context, fn func()) error {
	t := time.NewTimer(s.set.HashWait)
	defer t.Stop()
	select {
	case s.hashSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return errBusy
	}
	defer func() { <-s.hashSem }()
	fn()
	return nil
}

func (s *Service) verify(ctx context.Context, pw, phc string) (bool, error) {
	ok := false
	err := s.withHashSlot(ctx, func() { ok = VerifyPassword(pw, phc) })
	return ok, err
}

// login throttle: LoginFreeAttempts failures, then lock base*2^n capped.
const (
	LoginFreeAttempts = 5
	loginBackoffBase  = 30 * time.Second
	loginBackoffMax   = 15 * time.Minute
)

// LoginBackoff = lock length after n failures. 0 below threshold.
func LoginBackoff(failures int) time.Duration {
	if failures < LoginFreeAttempts {
		return 0
	}
	d := loginBackoffBase
	for i := LoginFreeAttempts; i < failures && d < loginBackoffMax; i++ {
		d *= 2
	}
	return min(d, loginBackoffMax)
}

var errThrottled = apperr.RateLimited("Demasiados intentos fallidos; espera unos minutos e inténtalo de nuevo")

// loginFailed records failure + lock. errors logged: never change user answer.
func (s *Service) loginFailed(ctx context.Context, norm string) {
	now := s.now()
	n, err := s.repo.RecordLoginFailure(ctx, norm, now)
	if err == nil {
		if d := LoginBackoff(n); d > 0 {
			err = s.repo.LockLogin(ctx, norm, now.Add(d))
		}
	}
	if err != nil {
		s.log.ErrorContext(ctx, "record login failure", "err", err)
	}
}

// randomToken = 32 random bytes base64url + sha256 of it.
func randomToken() (string, []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand never fails on supported os
	raw := base64.RawURLEncoding.EncodeToString(b)
	return raw, HashToken(raw)
}

// HashToken = sha256(raw). what db stores.
func HashToken(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}

var errCreds = apperr.Unauthorized("invalid_credentials", "Email o contraseña incorrectos")

// NormalizeEmail trims + validates plain address. no display names.
func NormalizeEmail(s string) (string, error) {
	s = strings.TrimSpace(s)
	bad := apperr.Invalid("invalid_email", "Email no válido", apperr.FieldError{Field: "email", Code: "invalid", Message: "Email no válido"})
	if len(s) < 3 || len(s) > 254 {
		return "", bad
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || a.Name != "" {
		return "", bad
	}
	at := strings.LastIndexByte(s, '@')
	if at < 1 || !strings.Contains(s[at+1:], ".") {
		return "", bad
	}
	return s, nil
}

// CreateUser = cli. validates email + password strength.
func (s *Service) CreateUser(ctx context.Context, email, password string, admin bool) (User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	if err := ValidatePassword(password, email); err != nil {
		return User{}, err
	}
	var h string
	var herr error
	if err := s.withHashSlot(ctx, func() { h, herr = HashPassword(password) }); err != nil {
		return User{}, err
	}
	if herr != nil {
		return User{}, herr
	}
	return s.repo.CreateUser(ctx, email, h, admin)
}

// SetPassword = cli. revokes all sessions of user.
func (s *Service) SetPassword(ctx context.Context, email, password string) error {
	u, err := s.repo.UserByEmail(ctx, strings.TrimSpace(email))
	if err != nil {
		return err
	}
	if err := ValidatePassword(password, u.Email); err != nil {
		return err
	}
	var h string
	var herr error
	if err := s.withHashSlot(ctx, func() { h, herr = HashPassword(password) }); err != nil {
		return err
	}
	if herr != nil {
		return herr
	}
	now := s.now()
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.repo.SetPassword(ctx, u.ID, h); err != nil {
			return err
		}
		if err := s.repo.ClearLoginFailures(ctx, strings.ToLower(u.Email)); err != nil {
			return err
		}
		return s.repo.RevokeUserSessions(ctx, u.ID, now)
	})
}

// Promote sets admin flag. cli only.
func (s *Service) Promote(ctx context.Context, email string) error {
	return s.repo.SetAdmin(ctx, strings.TrimSpace(email), true)
}

// LoginResult = raw token for cookie + session row.
type LoginResult struct {
	Token   string
	Session Session
	User    User
}

// Login: generic error. unknown email still hashes (timing). prev session revoked (rotation).
func (s *Service) Login(ctx context.Context, prev actor.Actor, email, password string) (LoginResult, error) {
	email = strings.TrimSpace(email)
	if len(password) > PasswordMax || len(email) > 254 {
		return LoginResult{}, errCreds
	}
	norm := strings.ToLower(email)
	until, err := s.repo.LoginLockedUntil(ctx, norm)
	if err != nil {
		return LoginResult{}, err
	}
	if s.now().Before(until) {
		return LoginResult{}, errThrottled
	}
	u, err := s.repo.UserByEmail(ctx, email)
	if err != nil {
		if !apperr.IsKind(err, apperr.KindNotFound) {
			return LoginResult{}, err
		}
		if _, err := s.verify(ctx, password, s.dummy); err != nil {
			return LoginResult{}, err
		}
		s.loginFailed(ctx, norm)
		return LoginResult{}, errCreds
	}
	ok, err := s.verify(ctx, password, u.PasswordHash)
	if err != nil {
		return LoginResult{}, err
	}
	if !ok || u.Disabled {
		s.loginFailed(ctx, norm)
		return LoginResult{}, errCreds
	}
	if err := s.repo.ClearLoginFailures(ctx, norm); err != nil {
		return LoginResult{}, err
	}
	raw, hash := randomToken()
	csrf, _ := randomToken()
	now := s.now()
	sess := Session{UserID: u.ID, TokenHash: hash, CSRFToken: csrf, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(s.set.SessionTTL)}
	err = s.tx.InTx(ctx, func(ctx context.Context) error {
		if prev.IsUser() && prev.SessionID != 0 {
			if err := s.repo.RevokeSession(ctx, prev.SessionID, now); err != nil {
				return err
			}
		}
		sess.ID, err = s.repo.CreateSession(ctx, sess)
		return err
	})
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{Token: raw, Session: sess, User: u}, nil
}

// Logout revokes current session.
func (s *Service) Logout(ctx context.Context, a actor.Actor) error {
	if !a.IsUser() || a.SessionID == 0 {
		return apperr.Unauthorized("login_required", "Inicia sesión")
	}
	return s.repo.RevokeSession(ctx, a.SessionID, s.now())
}

var errSession = apperr.Unauthorized("session_invalid", "Sesión inválida o caducada")

// ResolveSession: token -> actor. revoked/expired/idle/disabled -> Unauthorized.
func (s *Service) ResolveSession(ctx context.Context, token string) (actor.Actor, error) {
	if token == "" || len(token) > 128 {
		return actor.Actor{}, errSession
	}
	sess, u, err := s.repo.SessionByHash(ctx, HashToken(token))
	if err != nil {
		if apperr.IsKind(err, apperr.KindNotFound) {
			return actor.Actor{}, errSession
		}
		return actor.Actor{}, err
	}
	now := s.now()
	if sess.Revoked || u.Disabled || !now.Before(sess.ExpiresAt) || now.Sub(sess.LastSeenAt) >= s.set.IdleTTL {
		return actor.Actor{}, errSession
	}
	// touch at most once per minute. less write load.
	if now.Sub(sess.LastSeenAt) >= time.Minute {
		if err := s.repo.TouchSession(ctx, sess.ID, now); err != nil {
			return actor.Actor{}, err
		}
	}
	return actor.Actor{Kind: actor.User, UserID: u.ID, Email: u.Email, IsAdmin: u.IsAdmin, SessionID: sess.ID, CSRFToken: sess.CSRFToken}, nil
}

// Current = user + session of actor. for GET /auth/session.
func (s *Service) Current(ctx context.Context, a actor.Actor) (User, Session, error) {
	if !a.IsUser() || a.SessionID == 0 {
		return User{}, Session{}, apperr.Unauthorized("login_required", "Inicia sesión")
	}
	u, err := s.repo.UserByID(ctx, a.UserID)
	if err != nil {
		return User{}, Session{}, err
	}
	sess, err := s.repo.SessionByID(ctx, a.SessionID)
	if err != nil {
		return User{}, Session{}, err
	}
	return u, sess, nil
}
