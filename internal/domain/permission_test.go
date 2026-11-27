package domain

import (
	"testing"
)

// The audit-model ADR's amendment part 3 names automation only; scope and
// project confinement remain the grant writer's independent live checks.
func TestMachineAuditReadEligibility(t *testing.T) {
	for _, class := range []PrincipalClass{
		ClassAutomation, ClassWorkload, ClassProvisioning, ClassInstanceConn, "unknown",
	} {
		t.Run(string(class), func(t *testing.T) {
			if got, want := MachineMayHold(class, CapAuditRead), class == ClassAutomation; got != want {
				t.Fatalf("audit-read eligibility = %t, want %t", got, want)
			}
		})
	}
}

func TestInstanceAdminTemplateKeepsTheV1WireName(t *testing.T) {
	if TemplateInstanceAdmin != "operator" {
		t.Fatalf("instance administrator template wire name = %q, want operator until v2", TemplateInstanceAdmin)
	}
}
