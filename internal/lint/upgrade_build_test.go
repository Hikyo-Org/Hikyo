package lint

import (
	"testing"

	"golang.org/x/tools/go/packages"
)

func TestUpgradeBuildAuthorityRepo(t *testing.T) {
	eachRepoContext(t, func(t *testing.T, pkgs []*packages.Package) {
		for _, finding := range UpgradeBuildAuthority(pkgs) {
			t.Error(finding)
		}
	})
}

func TestUpgradeBuildAuthorityRejectsAliasedFunctionCapture(t *testing.T) {
	eachContext(t, Module+"/internal/lint/testdata/badupgradebuild", func(t *testing.T, pkgs []*packages.Package) {
		if got := UpgradeBuildAuthority(pkgs); len(got) != 1 {
			t.Fatalf("aliased build authority capture findings = %v, want one", got)
		}
	})
}
