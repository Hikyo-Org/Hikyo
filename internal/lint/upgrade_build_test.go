package lint

import "testing"

func TestUpgradeBuildAuthorityRepo(t *testing.T) {
	pkgs, err := LoadRepo()
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range UpgradeBuildAuthority(pkgs) {
		t.Error(finding)
	}
}

func TestUpgradeBuildAuthorityRejectsAliasedFunctionCapture(t *testing.T) {
	pkgs, err := Load(Module + "/internal/lint/testdata/badupgradebuild")
	if err != nil {
		t.Fatal(err)
	}
	if got := UpgradeBuildAuthority(pkgs); len(got) != 1 {
		t.Fatalf("aliased build authority capture findings = %v, want one", got)
	}
}
