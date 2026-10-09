package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Scheduled scans must rebuild the checked-out code with the pinned compiler,
// never borrow an untrusted PR binary or gain authority to apply proposals.
func TestGoSecurityWorkflowPolicy(t *testing.T) {
	var workflow struct {
		On          map[string]yaml.Node `yaml:"on"`
		Permissions map[string]string    `yaml:"permissions"`
		Jobs        map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Env         map[string]string `yaml:"env"`
			Steps       []struct {
				Name string            `yaml:"name"`
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "go-security.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.On) != 2 {
		t.Fatal("security scanning must only run on schedule or manual dispatch")
	}
	for _, event := range []string{"schedule", "workflow_dispatch"} {
		if _, ok := workflow.On[event]; !ok {
			t.Fatalf("missing %s trigger", event)
		}
	}
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		t.Fatal("security scan and patch proposal must have only contents read permission")
	}
	for name, job := range workflow.Jobs {
		if len(job.Permissions) != 0 {
			t.Fatalf("%s must inherit read-only permissions", name)
		}
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "actions/checkout@") && step.With["persist-credentials"] != "false" {
				t.Fatalf("%s checkout must not retain credentials", name)
			}
			if strings.Contains(step.Uses, "download-artifact") || strings.HasPrefix(step.Uses, "actions/cache@") || strings.HasPrefix(step.Uses, "actions/cache/save@") {
				t.Fatalf("%s must build its own binaries without importing PR artifacts or publishing caches", name)
			}
			if strings.HasPrefix(step.Uses, "actions/cache/restore@") && step.With["path"] != "~/go/pkg/mod" {
				t.Fatal("scheduled scan may only restore Go module downloads")
			}
		}
	}
	patchJob, ok := workflow.Jobs["go-patch"]
	if !ok {
		t.Fatal("missing read-only Go patch proposal job")
	}
	proposal := false
	for _, step := range patchJob.Steps {
		if step.Run == "./scripts/ci/check-go-patch.sh" {
			proposal = true
		}
		if strings.Contains(step.Run, "git push") || strings.Contains(step.Run, "gh pr create") || strings.Contains(step.Run, "git apply") {
			t.Fatal("toolchain proposal must not apply or publish changes")
		}
	}
	if !proposal {
		t.Fatal("Go patch job must run the checked-in proposal script")
	}
	binaryJob, ok := workflow.Jobs["binary-security"]
	if !ok {
		t.Fatal("missing binary security job")
	}
	if binaryJob.Env["GOTOOLCHAIN"] != "local" || binaryJob.Env["GOFLAGS"] != "-mod=readonly" {
		t.Fatal("scan must use the pinned compiler without mutating dependencies")
	}
	var build, scan string
	pinnedCompiler := false
	for _, step := range binaryJob.Steps {
		if strings.HasPrefix(step.Uses, "actions/setup-go@") {
			pinnedCompiler = step.With["go-version-file"] == "go.mod" && step.With["cache"] == "false"
		}
		switch step.Name {
		case "Build both linked binary shapes":
			build = step.Run
		case "Scan both linked binaries":
			scan = step.Run
		}
	}
	if !pinnedCompiler || !strings.Contains(build, `go build -trimpath -o "$RUNNER_TEMP/hikyo-core" ./cmd/hikyo`) || !strings.Contains(build, `go build -trimpath -tags ui -o "$RUNNER_TEMP/hikyo-ui" ./cmd/hikyo`) {
		t.Fatal("both release binary shapes must be rebuilt with the go.mod compiler")
	}
	if scan == "" {
		t.Fatal("missing binary vulnerability scan step")
	}
	// Execute the workflow's real shell step against a deterministic tool stub.
	// A vulnerability in either binary must fail the step after checking both.
	for _, failed := range []string{"", "hikyo-core", "hikyo-ui", "both"} {
		t.Run("scan_failure_"+failed, func(t *testing.T) {
			work := t.TempDir()
			toolDir := filepath.Join(work, "scripts", "ci")
			if err := os.MkdirAll(toolDir, 0o755); err != nil {
				t.Fatal(err)
			}
			stub := `#!/bin/sh
set -eu
[ "$1" = govulncheck ] && [ "$2" = -mode=binary ]
name=${3##*/}
printf '%s\n' "$name" >> "$SCAN_LOG"
[ "$FAIL_BINARY" != "$name" ] && [ "$FAIL_BINARY" != both ]
`
			if err := os.WriteFile(filepath.Join(toolDir, "run-go-tool.sh"), []byte(stub), 0o755); err != nil {
				t.Fatal(err)
			}
			log := filepath.Join(work, "scans")
			cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", scan)
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "RUNNER_TEMP="+work, "SCAN_LOG="+log, "FAIL_BINARY="+failed)
			output, err := cmd.CombinedOutput()
			if (err != nil) != (failed != "") {
				t.Fatalf("scan error = %v; failure mode %q: %s", err, failed, output)
			}
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if string(calls) != "hikyo-core\nhikyo-ui\n" {
				t.Fatalf("both binaries must be scanned even when the first fails; got %q", calls)
			}
		})
	}
}
