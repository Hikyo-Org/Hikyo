package operation

import (
	"context"
	"slices"
	"testing"
)

func TestContractIsImmutableAndMovesThroughContext(t *testing.T) {
	formula := []string{"read@project"}
	artifacts := []string{ArtifactMachineCredential}
	contract, err := NewContract("mcp:hikyo_list_definitions", "key.list", formula, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	formula[0] = "edit@instance"
	artifacts[0] = ArtifactHumanSession

	ctx := WithContract(context.Background(), contract)
	if !IsNetwork(ctx) {
		t.Fatal("contract did not carry network provenance")
	}
	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("contract was not attached")
	}
	if got.ID != "mcp:hikyo_list_definitions" || got.AuthorizationOperation != "key.list" {
		t.Fatalf("contract = %#v", got)
	}
	if !slices.Equal(got.Formula(), []string{"read@project"}) {
		t.Fatalf("formula = %v", got.Formula())
	}
	if !got.AdmitsArtifact(ArtifactMachineCredential) || got.AdmitsArtifact(ArtifactHumanSession) {
		t.Fatalf("artifact allowlist = %v", got.Artifacts())
	}

	returned := got.Artifacts()
	returned[0] = ArtifactHumanSession
	if got.AdmitsArtifact(ArtifactHumanSession) {
		t.Fatal("artifact accessor exposed mutable policy")
	}
}

func TestNetworkProvenanceDoesNotInventAContract(t *testing.T) {
	ctx := WithNetwork(context.Background())
	if !IsNetwork(ctx) {
		t.Fatal("network provenance was not attached")
	}
	if _, ok := FromContext(ctx); ok {
		t.Fatal("network provenance invented an authorization contract")
	}
}

func TestContractRejectsIncompletePolicy(t *testing.T) {
	for _, tc := range []struct {
		name      string
		id        string
		operation string
		formula   []string
		artifacts []string
	}{
		{"missing id", "", "key.list", []string{"read@project"}, []string{ArtifactMachineCredential}},
		{"missing operation", "mcp:x", "", []string{"read@project"}, []string{ArtifactMachineCredential}},
		{"missing formula", "mcp:x", "key.list", nil, []string{ArtifactMachineCredential}},
		{"missing artifacts", "mcp:x", "key.list", []string{"read@project"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewContract(tc.id, tc.operation, tc.formula, tc.artifacts); err == nil {
				t.Fatal("incomplete contract accepted")
			}
		})
	}
}

func TestArtifactContractAllowsTransportOnlyOperation(t *testing.T) {
	contract, err := NewArtifactContract("getMeta", []string{ArtifactNone})
	if err != nil {
		t.Fatal(err)
	}
	if contract.AuthorizationOperation != "" || len(contract.Formula()) != 0 || !contract.AdmitsArtifact(ArtifactNone) {
		t.Fatalf("transport-only contract = %#v", contract)
	}
}

func TestWithContractRefusesEmptyContract(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("empty contract did not panic")
		}
	}()
	_ = WithContract(context.Background(), Contract{})
}
