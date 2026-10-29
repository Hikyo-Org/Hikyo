package api_test

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
)

func TestRemotePairingOperationMinimumRevision(t *testing.T) {
	operations, err := api.Operations()
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{
		"prepareRemotePairing":      true,
		"createRemotePairingInvite": true,
		"listRemotePairingInvites":  true,
		"revokeRemotePairingInvite": true,
		"pairRemote":                true,
		"startRemotePairing":        true,
		"finishRemotePairing":       true,
	}
	if api.Revision < 12 {
		t.Fatalf("server advertises API revision %d, want at least 12", api.Revision)
	}
	for name := range expected {
		operation, ok := operations[name]
		if !ok {
			t.Errorf("pairing operation %s is missing", name)
			continue
		}
		if operation.MinRevision != 12 {
			t.Errorf("%s minimum revision=%d, want first appearance at 12", name, operation.MinRevision)
		}
	}
	for name, operation := range operations {
		if operation.MinRevision == 12 && !expected[name] {
			t.Errorf("unreviewed operation %s claims pairing's first-appearance revision 12", name)
		}
	}
}
