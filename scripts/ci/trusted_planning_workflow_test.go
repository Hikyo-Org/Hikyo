package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every trusted downstream tool must use the same immutable base as planning.
func TestTrustedToolsUseTheMergeGroupBase(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var ci mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &ci); err != nil {
		t.Fatal(err)
	}
	for jobName, stepName := range map[string]string{
		"changes":         "Classify changed paths",
		"analysis_shards": "Plan complete test, race, and fuzz shards",
		"race_shard":      "Race detector (all packages except the isolation suite)",
		"ci-required":     "Require every validation job",
	} {
		found := false
		for _, step := range ci.Jobs[jobName].Steps {
			if step.Name == stepName {
				found = true
				if step.Env["BASE_SHA"] != "${{ github.event.merge_group.base_sha }}" {
					t.Fatalf("%s trusted tooling lost the merge group base", jobName)
				}
			}
		}
		if !found {
			t.Fatalf("%s trusted-tooling step missing", jobName)
		}
	}
	guard := ci.Jobs["changes"].Steps[0]
	for _, event := range []string{"pull_request", "pull_request_target", "workflow_run"} {
		command := exec.Command("bash", "-euo", "pipefail", "-c", guard.Run)
		command.Env = append(os.Environ(), "GITHUB_EVENT_NAME="+event, "GITHUB_REPOSITORY=Hikyo-Org/Hikyo", "EVENT_REPOSITORY=Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID=10", "SOURCE_REVISION="+strings.Repeat("a", 40), "GITHUB_SHA="+strings.Repeat("a", 40), "DEVELOPMENT=false")
		if out, err := command.CombinedOutput(); err == nil || !strings.Contains(string(out), "Validation refuses this source context.") {
			t.Fatalf("%s reached the validation graph: %v %s", event, err, out)
		}
	}
}

// Execute the actual YAML classifier against a source checkout that poisons
// both local tooling files. Planning must use the immutable base's tools.
func TestTrustedClassifierUsesBaseTools(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	script := workflow.Jobs["changes"].Steps[2].Run
	for _, name := range []string{"merge-group", "missing-registry", "missing-classifier"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				out, err := cmd.CombinedOutput()
				if err != nil {
					t.Fatalf("git %v: %v %s", args, err, out)
				}
				return strings.TrimSpace(string(out))
			}
			write := func(path, content string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0700); err != nil {
					t.Fatal(err)
				}
			}
			git("init", "-q")
			git("config", "user.name", "Fixture")
			git("config", "user.email", "fixture@example.test")
			git("config", "commit.gpgsign", "false")
			write("file", "base")
			if name != "missing-classifier" {
				write("scripts/ci/classify-changed-paths.sh", "#!/bin/sh\nset -eu\ntest \"$(cat \"$CI_JOB_REGISTRY\")\" = trusted-registry\ncase \"$1\" in --all) mode=all ;; --files) cat >/dev/null; mode=files ;; *) exit 92 ;; esac\nprintf '{\"trusted\":true,\"mode\":\"%s\"}\\n' \"$mode\"\n")
			}
			if name != "missing-registry" {
				write("scripts/ci/ci-job-registry.json", "trusted-registry\n")
			}
			git("add", ".")
			git("commit", "-qm", "trusted base")
			base := git("rev-parse", "HEAD")
			git("checkout", "-qb", "feature")
			write("scripts/ci/classify-changed-paths.sh", "#!/bin/sh\necho source-tool-executed >&2\nexit 91\n")
			write("scripts/ci/ci-job-registry.json", "source-registry\n")
			git("add", ".")
			git("commit", "-qm", "poison source tools")
			head := git("rev-parse", "HEAD")
			git("checkout", "-q", "--detach", base)
			git("merge", "--no-ff", "-qm", "candidate", head)
			output, runner := filepath.Join(dir, "output"), filepath.Join(dir, "runner")
			if err := os.Mkdir(runner, 0700); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", script)
			cmd.Dir = dir
			cmd.Env = append(append(os.Environ(), "DEVELOPMENT=false"), "EVENT_NAME=merge_group", "BASE_SHA="+base, "GITHUB_REPOSITORY=Hikyo-Org/Hikyo", "GITHUB_OUTPUT="+output, "RUNNER_TEMP="+runner)
			out, err := cmd.CombinedOutput()
			if strings.HasPrefix(name, "missing-") {
				if err == nil || !strings.Contains(string(out), "trusted CI classifier or job registry is missing") || strings.Contains(string(out), "source-tool-executed") {
					t.Fatalf("missing trusted tooling must fail without local fallback: %v %s", err, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("trusted planning: %v %s", err, out)
			}
			got, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			want := "plan={\"trusted\":true,\"mode\":\"all\"}\n"
			if string(got) != want {
				t.Fatalf("planning output: got %q want %q", got, want)
			}
		})
	}
}
