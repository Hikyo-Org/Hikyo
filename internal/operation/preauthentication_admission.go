package operation

import (
	"context"
	"sync"
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

// AdmitPreauthentication bounds artifact resolution before it can capture a
// durable refusal. Source identity comes from transport middleware, not tokens.
func AdmitPreauthentication(ctx context.Context) error {
	a, _ := ctx.Value(preauthenticationAdmissionKey{}).(*preauthenticationAdmission)
	if a == nil || a.charge == nil {
		return nil
	}
	a.once.Do(func() { a.err = a.charge() })
	return a.err
}
