package lint

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestObserversAreTestOnlyRepo(t *testing.T) {
	eachRepoContext(t, func(t *testing.T, pkgs []*packages.Package) {
		for _, finding := range CheckTestObservers(pkgs) {
			t.Error(finding)
		}
	})
}

func TestObserversCatchProductionReferences(t *testing.T) {
	eachContext(t, "./testdata/badobserver/...", func(t *testing.T, pkgs []*packages.Package) {
		owner := Module + "/internal/lint/testdata/badobserver/owner"
		findings := checkTestObserversIn(pkgs, map[string][]string{owner: {"SetQueryObserver", "SetMutationFailureObserver", "SetSCIMPhaseObserver"}})
		for file, markers := range map[string][]string{
			"owner/owner.go": {"func unsafeOwnerCall"},
			"caller.go":      {"func aliasCall", "var captured", "var mutation", "var phase"},
		} {
			for _, marker := range markers {
				line := fixtureLine(t, filepath.Join("testdata", "badobserver", filepath.FromSlash(file)), marker)
				assertFindings(t, findings, []string{fmt.Sprintf("%s:%d:", filepath.Base(file), line)})
			}
		}
		for _, finding := range findings {
			if strings.Contains(finding, "decoy.go") || strings.Contains(finding, "_test.go") {
				t.Errorf("unrelated method or allowed test reference flagged: %s", finding)
			}
		}
		missing := checkTestObserversIn(pkgs, map[string][]string{
			owner:              {"RenamedObserver"},
			owner + "/missing": {"SetQueryObserver"},
		})
		assertFindings(t, missing, []string{"installer " + owner + ".RenamedObserver not found", "owner package " + owner + "/missing not loaded"})
	})
}
