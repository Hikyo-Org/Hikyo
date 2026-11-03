package operation

import (
	"context"
	"sync"

	"github.com/Hikyo-Org/hikyo/internal/audit"
)

type preauthenticationAdmissionKey struct{}
type preauthenticationAdmission struct {
	once   sync.Once
	charge func() error
	err    error
}

// WithPreauthenticationAdmission attaches trusted source admission to one
// request. Nested reads and transaction retries share accounting, never an
// authentication or authorization result.
func WithPreauthenticationAdmission(ctx context.Context, charge func() error) context.Context {
	return context.WithValue(ctx, preauthenticationAdmissionKey{}, &preauthenticationAdmission{charge: charge})
}

// AdmitPreauthentication bounds artifact resolution before its first datastore
// read. Call after cheap grammar checks, independently of credential liveness.
// Source identity comes from transport middleware, not tokens. Nested reads and
// retries reuse the admission outcome, including a refusal.
func AdmitPreauthentication(ctx context.Context) error {
	a, _ := ctx.Value(preauthenticationAdmissionKey{}).(*preauthenticationAdmission)
	if a == nil || a.charge == nil {
		return nil
	}
	a.once.Do(func() { a.err = a.charge() })
	if a.err != nil {
		audit.NoteRefusal(ctx, audit.RefusalAdmission)
	}
	return a.err
}

type liveSessionsKey struct{}

// LiveSessions is one node's memory of the sessions it has proven live. It
// decides only which limit a presentation spends, never whether it is accepted.
type LiveSessions interface {
	KnownLiveSession(verifier []byte) (sessionID string, known bool)
	NoteLiveSession(verifier []byte, sessionID string)
	ForgetSession(verifier []byte)
}

// WithLiveSessions moves a proven session from the source allowance, which bounds
// credential guessing, to its own request budget.
func WithLiveSessions(ctx context.Context, sessions LiveSessions) context.Context {
	return context.WithValue(ctx, liveSessionsKey{}, sessions)
}

// AdmitSessionPresentation bounds a session artifact's reads. A session known live
// spends its request budget; anything else spends the source allowance. Either is
// spent before the first read. A session that died while known keeps spending its
// budget until one resolution finds it dead and forgets it.
func AdmitSessionPresentation(ctx context.Context, verifier []byte) error {
	if sessions, _ := ctx.Value(liveSessionsKey{}).(LiveSessions); sessions != nil {
		if sessionID, known := sessions.KnownLiveSession(verifier); known {
			return AdmitRequest(ctx, sessionID)
		}
	}
	return AdmitPreauthentication(ctx)
}

// NoteSessionLive records that resolution proved the session live.
func NoteSessionLive(ctx context.Context, verifier []byte, sessionID string) {
	if sessions, _ := ctx.Value(liveSessionsKey{}).(LiveSessions); sessions != nil {
		sessions.NoteLiveSession(verifier, sessionID)
	}
}

// ForgetSession records that resolution found the session dead, so its next
// presentation is charged to the source again. A fault proves nothing, and the
// session's budget bounds any retries, so a fault leaves the memory alone.
func ForgetSession(ctx context.Context, verifier []byte) {
	if sessions, _ := ctx.Value(liveSessionsKey{}).(LiveSessions); sessions != nil {
		sessions.ForgetSession(verifier)
	}
}
