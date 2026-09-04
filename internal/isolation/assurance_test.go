package isolation

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// The MFA-mandatory set is the ADR's, restated in Go. A capability quietly
// dropping out of it would silently ungate an operation the moment
// enforcement turns on, which is the worst possible time to discover it.
func TestMFAMandatorySetMatchesTheADR(t *testing.T) {
	// `instance-directory` is the multi-instance ADR's addition (#71). Its
	// amendment to #16 restates "every instance capability is MFA-mandatory" as
	// binding HUMAN SESSIONS, with the instance-connection machine principal as
	// the single named exemption — and that exemption is structural rather than
	// a row here: assuranceInadequate requires a non-empty SessionID, which a
	// machine principal never has.
	want := []string{
		"reveal", "reveal-history", "manage-members", "credential-reset",
		"instance-config", "instance-directory",
	}
	if len(authz.MFAMandatory) != len(want) {
		t.Fatalf("the MFA-mandatory set has %d members, the ADR names %d: %v",
			len(authz.MFAMandatory), len(want), authz.MFAMandatory)
	}
	for _, capability := range want {
		if !authz.MFAMandatory[domain.Capability(capability)] {
			t.Errorf("%q is MFA-mandatory in the ADR but not in authz.MFAMandatory", capability)
		}
	}
}
