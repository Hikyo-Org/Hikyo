package authz

import (
	"context"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
)

// AdmitOperation enforces the artifact classes declared by the exact network
// operation attached to a request. Authentication still resolves the
// live identity inside this transaction; this check then refuses a resolved
// bearer whose class the contract excludes before handler logic can use it.
//
// An absent operation means an in-process caller. Each network adapter attaches
// a compiled contract before dispatch, so there is no request-derived fallback
// table and a new declaration takes effect without a second edit.
func (a *TxAuthorizer) AdmitOperation(ctx context.Context, caller Identity) error {
	op, ok := operation.FromContext(ctx)
	if !ok {
		if operation.IsNetwork(ctx) {
			return domain.ErrNotFound
		}
		return nil
	}
	class := ContractArtifactClass(caller)
	if !op.AdmitsArtifact(class) {
		return a.refuseAdmission(ctx, caller, op.ID, class, "class-mismatch")
	}

	// The enrolment gate (#760): a password-assured session minted for an
	// unenrolled account under a `required` second-factor policy is confined to
	// the enrolment allowlist. Deny-by-default — every other operation is refused
	// with the same uniform nonexistent shape until a factor stands and a
	// reissued session lifts the flag.
	if caller.EnrolmentRequired && !enrolmentGateAllows(op.ID) {
		return a.refuseAdmission(ctx, caller, op.ID, class, "enrolment-required")
	}
	return nil
}

// enrolmentGateAllows is the closed set of operations a session flagged
// enrolment_required may still reach: the factor-enrolment ceremonies plus the
// recovery-code, whoami and logout endpoints the SPA needs to complete or escape
// the gate. Deny-by-default (#760).
func enrolmentGateAllows(id string) bool {
	switch id {
	case "enrolTotpStart", "enrolTotpConfirm", "enrolPasskeyStart", "enrolPasskeyFinish",
		"regenerateRecoveryCodes", "whoami", "logout":
		return true
	default:
		return false
	}
}

// refuseAdmission records a named admission refusal on the security trail and
// returns the uniform nonexistent shape. The wire response never distinguishes a
// class mismatch from the enrolment gate; the cause lives only in the trail.
func (a *TxAuthorizer) refuseAdmission(ctx context.Context, caller Identity, operationID, class, cause string) error {
	id, err := audit.NewEventID()
	if err != nil {
		a.captureErr = errors.Join(a.captureErr, err)
		return domain.ErrNotFound
	}
	wire := audit.FromContext(ctx)
	credentialID := caller.CredentialID
	if credentialID == "" {
		credentialID = caller.SessionID
	}
	a.CaptureAudit(audit.TrailInstance, domain.Scope{}, audit.Event{
		ID:            id,
		Type:          audit.EventAuthArtifactClassRefused,
		SchemaVersion: 1,
		OccurredAt:    time.Now().UTC(),
		Actor: audit.Actor{
			ID:           string(caller.Principal),
			CredentialID: credentialID,
		},
		Object:    audit.Object{Type: "api-operation", ID: operationID},
		Outcome:   audit.OutcomeFailure,
		SourceIP:  wire.SourceIP,
		UserAgent: wire.UserAgent,
		Origin:    wire.Origin,
		Payload: audit.Payload{
			"operation":      operationID,
			"artifact_class": class,
			"cause":          cause,
		},
	})
	return domain.ErrNotFound
}
