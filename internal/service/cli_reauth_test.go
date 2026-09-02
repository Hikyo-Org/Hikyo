package service

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
)

func TestCLIRedirectIsExactEphemeralLoopbackCallback(t *testing.T) {
	for _, valid := range []string{
		"http://127.0.0.1:43123/callback",
		"http://[::1]:43123/callback",
	} {
		if !validCLILoopbackRedirect(valid) {
			t.Errorf("valid redirect refused: %s", valid)
		}
	}
	for _, invalid := range []string{
		"https://127.0.0.1:43123/callback",
		"http://localhost:43123/callback",
		"http://127.0.0.1/callback",
		"http://127.0.0.1:43123/other",
		"http://127.0.0.1:43123/callback?next=evil",
		"http://127.0.0.1:43123/callback#fragment",
		"http://127.0.0.2:43123/callback",
	} {
		if validCLILoopbackRedirect(invalid) {
			t.Errorf("invalid redirect accepted: %s", invalid)
		}
	}
}

func TestCLIReauthPurposeOperationIncludesApprovalDecisions(t *testing.T) {
	for _, tc := range []struct {
		purpose   ReauthPurpose
		operation authz.Operation
	}{
		{PurposePublish, authz.OpValueCopyDestination},
		{PurposeApprove, authz.OpApprovalVote},
		{PurposeReject, authz.OpApprovalVote},
		{PurposeBypass, authz.OpApprovalBypass},
	} {
		if !cliReauthPurposeOperation(tc.purpose, tc.operation) {
			t.Errorf("CLI reauth refused %s with %s", tc.purpose, tc.operation)
		}
		if !cliReauthKeyBound(string(tc.purpose)) {
			t.Errorf("CLI reauth did not bind %s to a key set", tc.purpose)
		}
	}
	if cliReauthPurposeOperation(PurposeReject, authz.OpApprovalBypass) {
		t.Error("reject accepted the bypass operation")
	}
}
