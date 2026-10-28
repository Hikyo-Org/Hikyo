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
