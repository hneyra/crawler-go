// Package actor = who makes request. passed into use cases for authz. no http here.
package actor

import "context"

// Kind of actor.
type Kind int

const (
	Anonymous Kind = iota
	User
	System
)

// Actor = authenticated principal. zero value = anonymous.
type Actor struct {
	Kind      Kind
	UserID    int64
	Email     string
	IsAdmin   bool
	SessionID int64
	CSRFToken string
}

// SystemActor = worker, cli. never from http.
var SystemActor = Actor{Kind: System}

// IsUser reports logged-in user.
func (a Actor) IsUser() bool { return a.Kind == User && a.UserID != 0 }

// IsAdminUser reports logged-in admin.
func (a Actor) IsAdminUser() bool { return a.IsUser() && a.IsAdmin }

type ctxKey struct{}

// With stores actor in ctx.
func With(ctx context.Context, a Actor) context.Context { return context.WithValue(ctx, ctxKey{}, a) }

// From reads actor. missing -> anonymous.
func From(ctx context.Context) Actor {
	a, _ := ctx.Value(ctxKey{}).(Actor)
	return a
}
