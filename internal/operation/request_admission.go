package operation

import (
	"context"
	"sync"
)

type requestAdmissionKey struct{}

type requestAdmission struct {
	once   sync.Once
	charge func(string) error
	err    error
}

// WithRequestAdmission attaches accounting, not identity or authorization,
// to one network request. Transaction retries and nested service reads share
// the receipt; authentication is still resolved live before every admission.
func WithRequestAdmission(ctx context.Context, charge func(sessionID string) error) context.Context {
	return context.WithValue(ctx, requestAdmissionKey{}, &requestAdmission{charge: charge})
}

// AdmitRequest charges only a transaction-authenticated human session. Invalid
// credentials and machine callers cannot allocate or spend human buckets.
func AdmitRequest(ctx context.Context, sessionID string) error {
	r, _ := ctx.Value(requestAdmissionKey{}).(*requestAdmission)
	if r == nil || r.charge == nil || sessionID == "" {
		return nil
	}
	r.once.Do(func() { r.err = r.charge(sessionID) })
	return r.err
}
