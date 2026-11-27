package domain

import (
	"slices"
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

func TestOperatorTemplateIsADeprecatedAliasOfInstanceAdmin(t *testing.T) {
	want, err := ExpandTemplate(TemplateInstanceAdmin, LevelNone)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ExpandTemplate("operator", LevelNone)
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("ExpandTemplate(operator) = %v, %v; want %v", got, err, want)
	}
	if Template("operator").Canonical() != TemplateInstanceAdmin || slices.Contains(Templates(), "operator") {
		t.Fatal("operator must canonicalise to instance-admin and never be listed")
	}
}
