package authz

import (
	"context"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// CallerActivity is the narrow, non-authorizing result the transport may
// retain until its handler returns. It contains no principal, proof, scope or
// transaction token: it can decide only whether the post-response idle-clock
// write is worth attempting. The write still re-authenticates inside its own
// transaction, so revocation remains authoritative.
type CallerActivity struct {
	lastSeenAt time.Time
	slideable  bool
}

// LastSeenAt is the timestamp the request's own authentication read resolved.
func (a CallerActivity) LastSeenAt() time.Time { return a.lastSeenAt }

// Slideable reports whether the resolved identity owns a session idle clock
// or service-account credential last-used stamp. Provisioning and instance
// connections own separate touch paths and preserve the old generic no-op.
func (a CallerActivity) Slideable() bool { return a.slideable }

type callerActivityKey struct{}

type callerActivityState struct {
	mu       sync.Mutex
	activity CallerActivity
	resolved bool
}

// TrackCallerActivity installs request-local storage for the authentication
// result. Authentication remains inside the operation transaction; only the
// non-authorizing slide hint survives until the response has been written.
func TrackCallerActivity(ctx context.Context) context.Context {
	return context.WithValue(ctx, callerActivityKey{}, &callerActivityState{})
}

// ResolvedCallerActivity returns the most recent successful caller resolution
// observed in this request. Fabricated and otherwise refused bearers leave it
// empty, so post-response work can stop before opening any transaction.
func ResolvedCallerActivity(ctx context.Context) (CallerActivity, bool) {
	state, _ := ctx.Value(callerActivityKey{}).(*callerActivityState)
	if state == nil {
		return CallerActivity{}, false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.activity, state.resolved
}

func recordCallerActivity(ctx context.Context, identity Identity) {
	state, _ := ctx.Value(callerActivityKey{}).(*callerActivityState)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.activity = CallerActivity{
		lastSeenAt: identity.LastSeenAt,
		slideable:  identity.SessionID != "" || domain.IsServiceAccountKind(identity.Class),
	}
	state.resolved = true
}
