package upgrade

import "context"

// MaintenanceConfigurationKeys is a read-only inventory capability scoped to
// an exact durable maintenance operation. Partially applied schemas may refuse
// its queries; callers must never substitute bootstrap configuration on error.
func (s *Session) MaintenanceConfigurationKeys(ctx context.Context, expected State) (*candidateKeys, error) {
	if expected.Validate() != nil || !expected.Maintenance || expected.Pending == nil {
		return nil, ErrConflict
	}
	pending := *expected.Pending
	if pending.Acceptance.Attestation != nil {
		attestation := *pending.Acceptance.Attestation
		pending.Acceptance.Attestation = &attestation
	}
	if pending.Preparation != nil {
		preparation := *pending.Preparation
		pending.Preparation = &preparation
	}
	expected.Pending = &pending
	reader := &candidateKeys{session: s, expected: expected, phase: pending.Phase, maintenanceConfiguration: true}
	if err := reader.check(ctx); err != nil {
		return nil, err
	}
	return reader, nil
}
