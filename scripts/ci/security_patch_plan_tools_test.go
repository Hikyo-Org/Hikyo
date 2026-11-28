package ci_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The issue's commands run the planner, its helpers and the Go verifiers from the
// commit that is tagged. When the main tip is not that commit, the plan and the
// recheck accept it only while none of those tools differs from the tip's, so the
// list (tool_paths) must hold everything those tools are made of. A shell tool or a
// Go package that is missing from it would let a changed verifier run unreviewed.
func planScript(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts", "release", "security-patch-plan.sh"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func planToolPaths(t *testing.T) []string {
	t.Helper()
	match := regexp.MustCompile(`(?m)^tool_paths='([^']+)'$`).FindStringSubmatch(planScript(t))
	if match == nil {
		t.Fatal("security-patch-plan.sh lost its tool_paths list")
	}
	return strings.Fields(match[1])
}

func covered(paths []string, rel string) bool {
	for _, path := range paths {
		if rel == path || strings.HasPrefix(rel, path+"/") {
			return true
		}
	}
	return false
}

func TestSecurityPatchPlanToolPathsCoverWhatTheIssueRuns(t *testing.T) {
	root := repositoryRoot(t)
	paths := planToolPaths(t)
	for _, path := range paths {
		if _, err := os.Stat(filepath.Join(root, path)); err != nil {
			t.Errorf("tool_paths names %s, which does not exist: %v", path, err)
		}
	}

	// Every Go package the verifiers build from, and the module files that pin them.
	command := exec.CommandContext(context.Background(), "go", "list", "-deps", "-f", "{{if not .Standard}}{{.Dir}}{{end}}",
		"./scripts/release/stable", "./scripts/ci/source-proof")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	checked := 0
	for _, dir := range outputLines(string(output)) {
		rel, err := filepath.Rel(root, dir)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue // a dependency outside this module is pinned by go.sum
		}
		checked++
		if !covered(paths, filepath.ToSlash(rel)) {
			t.Errorf("tool_paths does not cover %s, which a verifier the issue runs is built from", rel)
		}
	}
	if checked < 3 {
		t.Fatalf("go list found only %d module packages; the dependency walk is broken", checked)
	}
	for _, file := range []string{"go.mod", "go.sum"} {
		if !covered(paths, file) {
			t.Errorf("tool_paths does not cover %s", file)
		}
	}

	// The shell tools the planner runs by default, and what they source or call.
	script := planScript(t)
	required := []string{"scripts/lib/release.sh", "scripts/ci/check-fallback-channel-test.sh", "scripts/ci/check-commit-signatures.sh", "scripts/git", ".githooks"}
	for _, match := range regexp.MustCompile(`: "\$\{[A-Z]+_BIN:=\$script_dir/([A-Za-z0-9._-]+\.sh)\}"`).FindAllStringSubmatch(script, -1) {
		required = append(required, "scripts/release/"+match[1])
	}
	for _, tool := range []string{"require-green-main.sh", "require-synced-trust.sh", "check-compatibility-unchanged.sh", "security-patch-plan.sh"} {
		required = append(required, "scripts/release/"+tool)
	}
	for _, rel := range required {
		if !covered(paths, rel) {
			t.Errorf("tool_paths does not cover %s", rel)
		}
	}
}

// The planner runs the fallback-channel gate the way release.yml does, on the same
// record for the same address: a tag whose build dies on that gate is burned.
func TestSecurityPatchPlanRunsTheFallbackGateReleaseRuns(t *testing.T) {
	root := repositoryRoot(t)
	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	gate := regexp.MustCompile(`\./scripts/ci/check-fallback-channel-test\.sh\s*\\\s*\n\s*(\S+)\s*\\\s*\n\s*(\S+)`).FindStringSubmatch(string(workflow))
	if gate == nil {
		t.Fatal("release.yml no longer asserts the fallback channel gate in the expected form")
	}
	script := planScript(t)
	record := regexp.MustCompile(`(?m)^fallback_record=(\S+)$`).FindStringSubmatch(script)
	address := regexp.MustCompile(`(?m)^fallback_address=(\S+)$`).FindStringSubmatch(script)
	if record == nil || address == nil {
		t.Fatal("security-patch-plan.sh lost its fallback gate constants")
	}
	if record[1] != gate[1] || address[1] != gate[2] {
		t.Fatalf("the planner checks %s for %s, release.yml checks %s for %s", record[1], address[1], gate[1], gate[2])
	}
}

// outputLines splits command output on newlines only: a checkout path may contain spaces.
func outputLines(output string) []string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestOutputLinesKeepPathsWithSpaces(t *testing.T) {
	got := outputLines("/work/my checkout/internal/a\n\n/work/my checkout/internal/b\n")
	if len(got) != 2 || got[0] != "/work/my checkout/internal/a" || got[1] != "/work/my checkout/internal/b" {
		t.Fatalf("paths were split on spaces: %q", got)
	}
}
