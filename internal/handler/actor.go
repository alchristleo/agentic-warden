package handler

import (
	"context"
	"net/http"
)

type actorKey struct{}

// withActor records who is making an admin request: the console user, or
// "token:<X-Applied-By>" for the admin token. Handlers read it with
// actorOf and put it on revisions, snapshots and audit events.
func withActor(r *http.Request, actor string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), actorKey{}, actor))
}

// actorOf returns the request's actor. Every admin route sets one, so an
// empty result means a handler was mounted without requireAdmin.
func actorOf(ctx context.Context) string {
	actor, _ := ctx.Value(actorKey{}).(string)
	return actor
}
