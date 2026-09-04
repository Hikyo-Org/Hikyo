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
