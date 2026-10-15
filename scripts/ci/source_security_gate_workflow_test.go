package ci_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// The relative ci-candidate/ci-merge-group calls select the candidate's ci.yml,
// but its aggregate must execute protected-base policy. These legacy fixtures
// are exact git-show bytes from 4f1d1ec63, not a modeled permissive checker.
func TestSourceSecurityGateTrustedBaseMigration(t *testing.T) {
	root := repositoryRoot(t)
	read := func(path string) []byte {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	var ci mainValidationWorkflow
	if err := yaml.Unmarshal(read(".github/workflows/ci.yml"), &ci); err != nil {
		t.Fatal(err)
	}
	gate := ""
	for _, step := range ci.Jobs["ci-required"].Steps {
		if step.Name == "Require every validation job" {
			gate = step.Run
		}
	}
	if gate == "" {
		t.Fatal("missing executable required-job gate")
	}
	var current registry
	if err := json.Unmarshal(read("scripts/ci/ci-job-registry.json"), &current); err != nil {
		t.Fatal(err)
	}
	encode := func(value any) []byte {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	for _, baseKind := range []string{"4f1d1ec63", "current", "unknown-web-go"} {
		checker := read("scripts/ci/check-required-jobs.sh")
		baseRegistry := read("scripts/ci/ci-job-registry.json")
		if baseKind != "current" {
			checker = read("scripts/ci/testdata/source-security-trusted-base/check-required-jobs.sh")
			baseRegistry = read("scripts/ci/testdata/source-security-trusted-base/ci-job-registry.json")
		}
		if baseKind == "unknown-web-go" {
			var base registry
			if err := json.Unmarshal(baseRegistry, &base); err != nil {
				t.Fatal(err)
			}
			base.Jobs["web-go"] = jobRule{RequiredGate: "planned", PlanJobs: []string{"web"}}
			baseRegistry = encode(base)
		}
		for _, event := range []string{"pull_request", "merge_group"} {
			for _, scenario := range []string{"full-success", "failure", "cancelled", "skipped", "missing", "unknown-extra", "legacy-job-failed", "malformed-plan", "missing-plan", "docs-success", "docs-unplanned-ran", "docs-missing"} {
				t.Run(baseKind+"/"+event+"/"+scenario, func(t *testing.T) {
					docs := strings.HasPrefix(scenario, "docs-")
					plan := make(map[string]any)
					for _, key := range current.PathClasses["full"] {
						plan[key] = !docs || key == "docs"
					}
					results := make(map[string]map[string]string)
					for name, rule := range current.Jobs {
						if !directGate(rule.RequiredGate) {
							continue
						}
						result := "success"
						if rule.RequiredGate == "planned" {
							result = "skipped"
							for _, key := range rule.PlanJobs {
								if plan[key] == true {
									result = "success"
								}
							}
						}
						results[name] = map[string]string{"result": result}
					}
					switch scenario {
					case "failure", "cancelled", "skipped":
						results["source-artifact-security"]["result"] = scenario
					case "missing", "docs-missing":
						delete(results, "source-artifact-security")
					case "unknown-extra":
						results["future-required-job"] = map[string]string{"result": "success"}
					case "legacy-job-failed":
						results["web-go"]["result"] = "failure"
					case "malformed-plan":
						plan["web-go"] = "true"
					case "missing-plan":
						delete(plan, "web-go")
					case "docs-unplanned-ran":
						results["source-artifact-security"]["result"] = "success"
					}
					work := t.TempDir()
					scriptDir := filepath.Join(work, "scripts", "ci")
					if err := os.MkdirAll(scriptDir, 0700); err != nil {
						t.Fatal(err)
					}
					write := func(name string, raw []byte) {
						t.Helper()
						if err := os.WriteFile(filepath.Join(scriptDir, name), raw, 0700); err != nil {
							t.Fatal(err)
						}
					}
					write("check-required-jobs.sh", checker)
					write("ci-job-registry.json", baseRegistry)
					git := func(args ...string) string {
						t.Helper()
						cmd := exec.Command("git", args...)
						cmd.Dir = work
						out, err := cmd.CombinedOutput()
						if err != nil {
							t.Fatalf("git %v: %v: %s", args, err, out)
						}
						return strings.TrimSpace(string(out))
					}
					git("init", "--quiet")
					git("add", "scripts/ci")
					baseTree := git("write-tree")
					// Prove the PR checkout cannot replace base policy.
					write("check-required-jobs.sh", []byte("#!/bin/sh\nexit 0\n"))
					write("ci-job-registry.json", []byte("{}"))
					runnerTemp := t.TempDir()
					candidate := "true"
					if docs {
						candidate = "false"
					}
					cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", gate)
					cmd.Dir = work
					cmd.Env = append(os.Environ(), "RUNNER_TEMP="+runnerTemp,
						"GITHUB_EVENT_NAME="+event, "FULL_CANDIDATE="+candidate,
						"BASE_SHA="+baseTree, "NEEDS_JSON="+string(encode(results)),
						"PLAN_JSON="+string(encode(plan)))
					out, err := cmd.CombinedOutput()
					accept := baseKind != "unknown-web-go" && (scenario == "full-success" || (scenario == "docs-success" && event == "pull_request"))
					if (err == nil) != accept {
						t.Fatalf("accepted %v want %v: %s", err == nil, accept, out)
					}
					for name, expected := range map[string][]byte{"check-required-jobs.sh": checker, "ci-job-registry.json": baseRegistry} {
						actual, err := os.ReadFile(filepath.Join(runnerTemp, name))
						if err != nil || string(actual) != string(expected) {
							t.Fatalf("trusted base %s was replaced or changed: %v", name, err)
						}
					}
				})
			}
		}
	}
}

func TestSourceArtifactRestoresBoundedCompilerEntries(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Env   map[string]string `yaml:"env"`
			Steps []struct {
				Name     string            `yaml:"name"`
				Uses     string            `yaml:"uses"`
				If       string            `yaml:"if"`
				Continue yaml.Node         `yaml:"continue-on-error"`
				Timeout  int               `yaml:"timeout-minutes"`
				With     map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	job := workflow.Jobs["source-artifact-security"]
	if job.Env["GOCACHE"] != "${{ github.workspace }}/.ci-cache/go-feedback" {
		t.Fatal("source fixture compilation must use the bounded compiler archive")
	}
	readers := 0
	for _, step := range job.Steps {
		if strings.Contains(step.Uses, "actions/cache/save@") || step.With["path"] == "~/.cache/go-build" {
			t.Fatal("source fixture must never publish or extract the full-test Go cache")
		}
		if step.With["path"] != ".ci-cache/go-feedback" {
			continue
		}
		readers++
		if step.Name != "Restore bounded source artifact Go cache" || step.Timeout != 2 || step.If != "" || step.Continue.Kind != 0 || step.Uses != "actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9" || !reflect.DeepEqual(step.With, map[string]string{
			"path":         ".ci-cache/go-feedback",
			"key":          "go-feedback-v1-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-${{ hashFiles('go.mod', 'go.sum', 'scripts/ci/go-tool-modules.txt') }}-${{ inputs.revision }}",
			"restore-keys": "go-feedback-v1-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-",
		}) {
			t.Fatal("source fixture cache must retain bounded compatible read-only restore and the existing two-minute deadline")
		}
	}
	if readers != 1 {
		t.Fatalf("want one bounded source compiler reader, got %d", readers)
	}
}

func TestMainValidationWorkflowPolicySourceArtifactCampaignUsesPrebuiltHarness(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		Jobs map[string]struct {
			Timeout int `yaml:"timeout-minutes"`
			Steps   []struct {
				Name     string    `yaml:"name"`
				Run      string    `yaml:"run"`
				If       string    `yaml:"if"`
				Continue yaml.Node `yaml:"continue-on-error"`
				Timeout  int       `yaml:"timeout-minutes"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	job := w.Jobs["source-artifact-security"]
	buildIndex, campaignIndex, total := -1, -1, 0
	for i, step := range job.Steps {
		total += step.Timeout
		switch step.Name {
		case "Build stamped production fixture with ephemeral test trust":
			buildIndex = i
		case "Native artifact adversarial campaign on both datastores":
			campaignIndex = i
		}
	}
	if buildIndex < 0 || campaignIndex != buildIndex+1 {
		t.Fatal("bounded artifact build must immediately precede its campaign")
	}
	build, campaign := job.Steps[buildIndex], job.Steps[campaignIndex]
	if job.Timeout != 35 || total != 32 || build.Timeout != 5 || campaign.Timeout != 11 || w.Jobs["web-go"].Timeout != 15 {
		t.Fatal("source security must preserve independent build/campaign bounds and original web-go budget")
	}
	if build.If != "" || campaign.If != "" || build.Continue.Kind != 0 || campaign.Continue.Kind != 0 {
		t.Fatal("artifact compilation and campaign must retain fail-closed success ordering")
	}
	for _, tt := range []struct {
		name  string
		calls int
		pass  bool
	}{{"success", 3, true}, {"fixture failure", 1, false}, {"UI compilation failure", 2, false}, {"harness compilation failure", 3, false}} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			bin, runner := filepath.Join(dir, "bin"), filepath.Join(dir, "runner")
			for _, path := range []string{bin, runner, filepath.Join(dir, "ci-artifacts")} {
				if err := os.MkdirAll(path, 0700); err != nil {
					t.Fatal(err)
				}
			}
			write := func(name, content string, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			write("count", "0\n", 0600)
			for i, args := range [][]string{
				{"test", "-count=1", "-run", "^TestWriteChartFixture$", "./scripts/ci/chartfixture"},
				{"build", "-tags", "ui", "-ldflags", "fixture flags", "-o", "ci-artifacts/hikyo-security-ui", "./cmd/hikyo"},
				{"build", "-o", filepath.Join(runner, "security-artifact-harness"), "./scripts/security/artifact-harness"},
			} {
				write(fmt.Sprintf("%d.args", i+1), strings.Join(args, "\n")+"\n", 0600)
			}
			write("campaign.args", strings.Join([]string{
				"--binary", filepath.Join(dir, "ci-artifacts/hikyo-security-ui"),
				"--bundle", filepath.Join(runner, "security-fixture/public/bundle"),
				"--postgres-dsn", "postgres://hikyo:hikyo@127.0.0.1:5432/hikyo_test?sslmode=disable",
				"--steps", "24", "--timeout", "10m", "--output", filepath.Join(runner, "source-artifact-attacks.json"),
			}, "\n")+"\n", 0600)
			write("bin/go", `#!/bin/bash
set -euo pipefail
count=$(cat "$FIXTURE_ROOT/count")
count=$((count + 1))
printf '%s\n' "$count" > "$FIXTURE_ROOT/count"
if [ "$(printf '%s\n' "$@")" != "$(cat "$FIXTURE_ROOT/$count.args")" ]; then
  touch "$FIXTURE_ROOT/unexpected-command"
  exit 1
fi
case "$count" in
  1)
    test "$SCENARIO" != 'fixture failure'
    mkdir -p "$RUNNER_TEMP/security-fixture/public/bundle"
    printf 'fixture flags' > "$RUNNER_TEMP/security-fixture/ldflags"
    ;;
  2)
    test "$SCENARIO" != 'UI compilation failure'
    touch "$GITHUB_WORKSPACE/ci-artifacts/hikyo-security-ui"
    ;;
  3)
    test "$SCENARIO" != 'harness compilation failure'
    cp "$FIXTURE_ROOT/harness-shim" "$RUNNER_TEMP/security-artifact-harness"
    chmod 700 "$RUNNER_TEMP/security-artifact-harness"
    ;;
  *) touch "$FIXTURE_ROOT/unexpected-command"; exit 1 ;;
esac
`, 0700)
			write("harness-shim", `#!/bin/bash
set -euo pipefail
test "$0" = "$RUNNER_TEMP/security-artifact-harness"
test "$(cat "$FIXTURE_ROOT/count")" = 3
test "$(printf '%s\n' "$@")" = "$(cat "$FIXTURE_ROOT/campaign.args")"
test -f "$GITHUB_WORKSPACE/ci-artifacts/hikyo-security-ui"
test -d "$RUNNER_TEMP/security-fixture/public/bundle"
printf '{}' > "$RUNNER_TEMP/source-artifact-attacks.json"
`, 0700)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// Execute the extracted commands with the runner's fail-fast shell.
			// Default success-only step conditions above prevent continuation
			// after any failed compile; no developer credential is inherited.
			cmd := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", build.Run+"\n"+campaign.Run)
			cmd.Dir = dir
			cmd.Env = []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "RUNNER_TEMP=" + runner, "GITHUB_WORKSPACE=" + dir, "FIXTURE_ROOT=" + dir, "SCENARIO=" + tt.name}
			output, runErr := cmd.CombinedOutput()
			if ctx.Err() != nil || (runErr == nil) != tt.pass {
				t.Fatalf("pass=%t, error=%v, context=%v: %s", tt.pass, runErr, ctx.Err(), output)
			}
			if _, err := os.Stat(filepath.Join(dir, "unexpected-command")); !os.IsNotExist(err) {
				t.Fatalf("unexpected Go command: %v: %s", err, output)
			}
			count, err := os.ReadFile(filepath.Join(dir, "count"))
			if err != nil || string(count) != fmt.Sprintf("%d\n", tt.calls) {
				t.Fatalf("Go calls=%q, expected=%d, error=%v", count, tt.calls, err)
			}
			_, receiptErr := os.Stat(filepath.Join(runner, "source-artifact-attacks.json"))
			if tt.pass && receiptErr != nil || !tt.pass && !os.IsNotExist(receiptErr) {
				t.Fatalf("campaign receipt does not match build success: %v", receiptErr)
			}
		})
	}
}
