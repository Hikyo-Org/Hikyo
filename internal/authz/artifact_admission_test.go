package authz

import (
	"context"
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
)

func TestAdmitOperationConsumesTransportIndependentContract(t *testing.T) {
	contract, err := operation.NewContract(
		"mcp:echo", "key.list", []string{"read@project"}, []string{operation.ArtifactMachineCredential},
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := operation.WithContract(context.Background(), contract)

	allowed := Identity{
		Principal: "svc_a", Class: domain.ClassWorkload,
		CredentialID: "cred_a", Artifact: operation.ArtifactMachineCredential,
	}
	authorizer := &TxAuthorizer{}
	if err := authorizer.AdmitOperation(ctx, allowed); err != nil {
		t.Fatalf("machine credential refused: %v", err)
	}

	human := Identity{
		Principal: "usr_a", Class: domain.ClassHuman,
		SessionID: "ses_a", Artifact: operation.ArtifactHumanSession,
	}
	if err := authorizer.AdmitOperation(ctx, human); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("human session error = %v", err)
	}
	denials := authorizer.PendingDenials()
	if len(denials) != 1 || denials[0].Event.Object.Type != "api-operation" || denials[0].Event.Object.ID != "mcp:echo" {
		t.Fatalf("artifact denial = %#v", denials)
	}

	if err := (&TxAuthorizer{}).AdmitOperation(context.Background(), human); err != nil {
		t.Fatalf("in-process caller without network contract refused: %v", err)
	}
	if err := (&TxAuthorizer{}).AdmitOperation(operation.WithNetwork(context.Background()), human); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("network caller without a contract error = %v", err)
	}
}

// TestAdmitOperationEnrolmentGate proves the #760 enrolment gate at the layer it
// actually runs: a session flagged enrolment_required reaches only the enrolment
// allowlist. The class check must pass first (the contracts admit human-session),
// so the refusal is the gate, recorded with its own cause, and not a
// class-mismatch.
func TestAdmitOperationEnrolmentGate(t *testing.T) {
	flagged := Identity{
		Principal: "usr_a", Class: domain.ClassHuman,
		SessionID: "ses_a", Artifact: operation.ArtifactHumanSession,
		EnrolmentRequired: true,
	}

	// A non-allowlisted operation is refused with the uniform nonexistent shape,
	// and the denial names the enrolment gate as the cause.
	blocked, err := operation.NewContract(
		"key.list", "key.list", []string{"read@project"}, []string{operation.ArtifactHumanSession},
	)
	if err != nil {
		t.Fatal(err)
	}
	blockedCtx := operation.WithContract(context.Background(), blocked)

	authorizer := &TxAuthorizer{}
	if err := authorizer.AdmitOperation(blockedCtx, flagged); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("gated session on a non-allowlisted operation error = %v, want not-found", err)
	}
	denials := authorizer.PendingDenials()
	if len(denials) != 1 || denials[0].Event.Payload["cause"] != "enrolment-required" {
		t.Fatalf("enrolment denial = %#v, want one denial caused by enrolment-required", denials)
	}

	// The same operation admits the identical session once the flag is lifted:
	// deny-by-default turns off the moment a factor stands.
	unflagged := flagged
	unflagged.EnrolmentRequired = false
	if err := (&TxAuthorizer{}).AdmitOperation(blockedCtx, unflagged); err != nil {
		t.Fatalf("unflagged session on the same operation refused: %v", err)
	}

	// The allowlist the gate leaves open: the enrolment ceremony and whoami the
	// SPA needs to complete or read the gate.
	for _, id := range []string{"whoami", "enrolTotpStart"} {
		allowed, err := operation.NewContract(
			id, id, []string{"read@project"}, []string{operation.ArtifactHumanSession},
		)
		if err != nil {
			t.Fatal(err)
		}
		if err := (&TxAuthorizer{}).AdmitOperation(operation.WithContract(context.Background(), allowed), flagged); err != nil {
			t.Fatalf("gated session refused allowlisted operation %q: %v", id, err)
		}
	}
}
