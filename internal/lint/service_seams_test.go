package lint

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestServiceSeamsRepo(t *testing.T) {
	eachRepoContext(t, func(t *testing.T, pkgs []*packages.Package) {
		for _, finding := range CheckServiceSeams(pkgs, Module+"/internal/service", Module+"/internal/store") {
			t.Error(finding)
		}
	})
}

func TestServiceSeamsCatchesBypasses(t *testing.T) {
	eachContext(t, "./testdata/badseam", func(t *testing.T, pkgs []*packages.Package) {
		path := Module + "/internal/lint/testdata/badseam"
		findings := CheckServiceSeams(pkgs, path, path)
		assertFindings(t, findings, []string{
			"ReauthWindow access outside effectiveReauthWindow",
			"BumpSchemaRevision access outside bumpSchemaRevision",
		})
		for _, marker := range []string{"func bypassWindow", "func bypassBudget", "func (s *ProjectSettings) otherSettingsMethod", "func bypassLocalInterface", "func bypassTypeAssertion"} {
			line := fixtureLine(t, filepath.Join("testdata", "badseam", "bypass.go"), marker)
			assertFindings(t, findings, []string{fmt.Sprintf("bypass.go:%d:", line)})
		}
		for _, finding := range findings {
			if strings.Contains(finding, "decoy.go") {
				t.Errorf("unrelated member flagged: %s", finding)
			}
		}
	})
}
