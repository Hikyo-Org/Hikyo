package lint

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestRecoveryLookupRepo(t *testing.T) {
	eachRepoContext(t, func(t *testing.T, pkgs []*packages.Package) {
		for _, finding := range CheckRecoveryLookup(pkgs, RecoveryLookupRules) {
			t.Error(finding)
		}
	})
}

func TestRecoveryLookupCatchesBypasses(t *testing.T) {
	eachContext(t, "./testdata/badrecovery", func(t *testing.T, pkgs []*packages.Package) {
		path := Module + "/internal/lint/testdata/badrecovery"
		findings := CheckRecoveryLookup(pkgs, []RecoveryRule{
			{Name: "RecoveryAccountByID", Allowed: map[string][]string{path: {"ResetAllowed", "NeverCalled", "licensedType.Reset"}}},
		})
		file := filepath.Join("testdata", "badrecovery", "badrecovery.go")
		for _, marker := range []string{"func SignIn", "func ViaAssertion", "func ViaMethodValue", "func (*otherType) Reset"} {
			assertFindings(t, findings, []string{fmt.Sprintf("badrecovery.go:%d:", fixtureLine(t, file, marker))})
		}
		assertFindings(t, findings, []string{"NeverCalled (stale allowlist entry)"})
		for _, licensed := range []string{"func ResetAllowed", "func (licensedType) Reset"} {
			for _, finding := range findings {
				if strings.Contains(finding, fmt.Sprintf("badrecovery.go:%d:", fixtureLine(t, file, licensed))) {
					t.Errorf("licensed caller flagged: %s", finding)
				}
			}
		}
	})
}

func TestContainmentSettersRepo(t *testing.T) {
	eachRepoContext(t, func(t *testing.T, pkgs []*packages.Package) {
		for _, finding := range CheckContainmentSetters(pkgs, ContainmentSetterRules) {
			t.Error(finding)
		}
	})
}

func TestContainmentSettersCatchesBypasses(t *testing.T) {
	eachContext(t, "./testdata/badsetter", func(t *testing.T, pkgs []*packages.Package) {
		path := Module + "/internal/lint/testdata/badsetter"
		findings := CheckContainmentSetters(pkgs, []RecoveryRule{
			{Name: "ContainPrincipal", Allowed: map[string][]string{path: {"Licensed", "NeverCalled"}}},
			{Name: "ReleasePrincipalContainment", Allowed: map[string][]string{}},
		})
		file := filepath.Join("testdata", "badsetter", "badsetter.go")
		for _, marker := range []string{"func ServiceContain", "func ServiceRelease", "func ViaAssertion", "func ViaMethodValue"} {
			assertFindings(t, findings, []string{fmt.Sprintf("badsetter.go:%d:", fixtureLine(t, file, marker))})
		}
		assertFindings(t, findings, []string{"containment setter: ContainPrincipal never referenced in " + path + ".NeverCalled (stale allowlist entry)"})
		for _, finding := range findings {
			if strings.Contains(finding, fmt.Sprintf("badsetter.go:%d:", fixtureLine(t, file, "func Licensed"))) {
				t.Errorf("licensed caller flagged: %s", finding)
			}
		}
	})
}
