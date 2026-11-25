package ci_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func registryListsDesktop(t *testing.T, data []byte) bool {
	t.Helper()
	var parsed registry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	_, listed := parsed.Jobs["desktop"]
	return listed
}

// The shim in ci-required is dead code once neither long-lived branch registers
// desktop. Fail then, so it is removed (issue #1595) instead of lingering. Skips
// only where the branch refs are not fetched.
func TestDesktopRemovalShimIsStillNeeded(t *testing.T) {
	root := repositoryRoot(t)
	ci, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ci), "ci-job-registry.without-desktop.json") {
		t.Skip("the desktop-removal shim is already gone")
	}
	listed := map[string]bool{}
	for _, branch := range []string{"origin/main", "origin/dev"} {
		cmd := exec.CommandContext(t.Context(), "git", "show", branch+":scripts/ci/ci-job-registry.json")
		cmd.Dir = root
		out, err := cmd.Output()
		if err != nil {
			t.Skipf("%s is not fetched here: %v", branch, err)
		}
		listed[branch] = registryListsDesktop(t, out)
	}
	if !listed["origin/main"] && !listed["origin/dev"] {
		t.Fatal("neither origin/main nor origin/dev registers desktop any more: delete the desktop-removal shim in ci.yml and the migration tests (issue #1595)")
	}
}

func TestRegistryListsDesktopDetection(t *testing.T) {
	if !registryListsDesktop(t, []byte(`{"version":2,"jobs":{"desktop":{"required_gate":"planned"}}}`)) {
		t.Fatal("did not see a registered desktop job")
	}
	if registryListsDesktop(t, []byte(`{"version":2,"jobs":{"docs":{"required_gate":"planned"}}}`)) {
		t.Fatal("saw a desktop job that is not registered")
	}
}
