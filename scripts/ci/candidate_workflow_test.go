package ci_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestCandidateWorkflowAdmissionAndCoverage(t *testing.T) {
	read := func(name string) mainValidationWorkflow {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		var result mainValidationWorkflow
		if err := yaml.Unmarshal(raw, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	candidate := read("ci-candidate.yml")
	if len(candidate.On) != 1 {
		t.Fatal("candidate execution must remain PR-scoped")
	}
	var trigger struct {
		Types []string `yaml:"types"`
	}
	pr := candidate.On["pull_request"]
	if err := pr.Decode(&trigger); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(trigger.Types, []string{"labeled"}) {
		t.Fatal("pushes and unrelated lifecycle events must not restart expensive work")
	}
	validation := candidate.Jobs["validation"]
	if validation.Uses != "./.github/workflows/ci.yml" || !reflect.DeepEqual(validation.With, map[string]string{"revision": "${{ github.sha }}", "candidate": "true"}) {
		t.Fatal("candidate must request complete validation of the authenticated merge result")
	}
	if !strings.Contains(validation.If, "github.event.label.name == 'ci:full'") || !strings.Contains(validation.If, "needs.vouch.result == 'success'") {
		t.Fatal("label requests work, and fork author admission remains mandatory")
	}
	proof := candidate.Jobs["candidate-proof"]
	needs, err := workflowNeeds(proof.Needs)
	if err != nil || !reflect.DeepEqual(needs, []string{"validation"}) {
		t.Fatal("proof must depend on the entire full suite")
	}
	if proof.If != "${{ always() && !cancelled() && github.event.label.name == 'ci:full' }}" || proof.Steps[0].Run != "test \"$VALIDATION_RESULT\" = success" {
		t.Fatal("failed or skipped candidate validation must fail the completion proof")
	}
	ci := read("ci.yml")
	classifier := ci.Jobs["changes"].Steps[2]
	if classifier.Env["BASE_SHA"] != "${{ github.event.pull_request.base.sha || github.event.merge_group.base_sha }}" {
		t.Fatal("initial classifier trust must come from the event base for legacy PRs and merge groups")
	}
	for _, required := range []string{"[ \"$FULL_CANDIDATE\" = true ]", "git rev-parse HEAD^1", "git rev-parse HEAD^2", "CI_JOB_REGISTRY=\"$trusted_registry\" \"$trusted_classifier\" --all"} {
		if !strings.Contains(classifier.Run, required) {
			t.Fatalf("missing candidate planning boundary: %s", required)
		}
	}
	checker := ci.Jobs["ci-required"].Steps[len(ci.Jobs["ci-required"].Steps)-1]
	if !strings.Contains(checker.Run, "[ \"$FULL_CANDIDATE\" = true ]") || !strings.Contains(checker.Run, "checker_event=merge_group") {
		t.Fatal("full candidates must reject selective plans independently of classification")
	}

	// Every trusted downstream tool must use the same resolved base as planning.
	for jobName, stepName := range map[string]string{
		"preflight":       "Require DCO sign-off on every PR commit",
		"analysis_shards": "Plan complete test, race, and fuzz shards",
		"race_shard":      "Race detector (all packages except the isolation suite)",
		"ci-required":     "Require every validation job",
	} {
		expected := "${{ inputs.candidate && needs.changes.outputs.base_sha || github.event.pull_request.base.sha || github.event.merge_group.base_sha }}"
		if jobName == "preflight" {
			expected = "${{ inputs.candidate && needs.changes.outputs.base_sha || github.event.pull_request.base.sha }}"
		}
		found := false
		for _, step := range ci.Jobs[jobName].Steps {
			if step.Name == stepName {
				found = true
				if step.Env["BASE_SHA"] != expected {
					t.Fatalf("%s trusted tooling lost the resolved candidate base", jobName)
				}
			}
		}
		if !found {
			t.Fatalf("%s trusted-tooling step missing", jobName)
		}
	}
	guard := ci.Jobs["changes"].Steps[0]
	for _, mutation := range []string{"valid", "head-only", "wrong-merge", "missing-base", "invalid-mode", "wrong-repository", "nondefault-target"} {
		t.Run(mutation, func(t *testing.T) {
			head, merge, base := strings.Repeat("a", 40), strings.Repeat("b", 40), strings.Repeat("c", 40)
			env := map[string]string{"GITHUB_EVENT_NAME": "pull_request", "GITHUB_REF": "refs/pull/1/merge", "GITHUB_SHA": merge, "GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "SOURCE_REVISION": merge, "FULL_CANDIDATE": "true", "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID": "10", "PR_NUMBER": "1", "PR_HEAD_SHA": head, "PR_MERGE_SHA": merge, "PR_BASE_SHA": base, "PR_BASE_REF": "main", "DEFAULT_BRANCH": "main", "PR_HEAD_REPOSITORY": "fork/Hikyo", "PR_HEAD_REPOSITORY_ID": "11", "PR_BASE_REPOSITORY": "Hikyo-Org/Hikyo", "PR_BASE_REPOSITORY_ID": "10"}
			switch mutation {
			case "head-only":
				env["SOURCE_REVISION"] = head
			case "wrong-merge":
				env["PR_MERGE_SHA"] = base
			case "missing-base":
				env["PR_BASE_SHA"] = ""
			case "invalid-mode":
				env["FULL_CANDIDATE"] = "yes"
			case "nondefault-target":
				env["PR_BASE_REF"] = "release"
			case "wrong-repository":
				env["PR_BASE_REPOSITORY"] = "other/Hikyo"
			}
			command := exec.Command("bash", "-euo", "pipefail", "-c", guard.Run)
			command.Env = os.Environ()
			for name, value := range env {
				command.Env = append(command.Env, name+"="+value)
			}
			output, err := command.CombinedOutput()
			if (err == nil) != (mutation == "valid") {
				t.Fatalf("candidate source allowed=%v: %v %s", err == nil, err, output)
			}
		})
	}
}

func TestCandidateBaseResolutionUsesLiveRefInsteadOfEventMetadata(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err = yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	script := strings.Split(workflow.Jobs["changes"].Steps[2].Run, "if [[ \"$EVENT_NAME\"")[0]
	for _, fresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "stale-event-base", true: "fresh-live-base"}[fresh], func(t *testing.T) {
			dir := t.TempDir()
			git := func(args ...string) string {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				out, e := cmd.CombinedOutput()
				if e != nil {
					t.Fatalf("git %v: %v %s", args, e, out)
				}
				return strings.TrimSpace(string(out))
			}
			git("init", "-q")
			git("config", "user.name", "Fixture")
			git("config", "user.email", "fixture@example.test")
			git("config", "commit.gpgsign", "false")
			if err := os.WriteFile(filepath.Join(dir, "file"), []byte("base"), 0600); err != nil {
				t.Fatal(err)
			}
			git("add", "file")
			git("commit", "-qm", "base")
			base := git("rev-parse", "HEAD")
			git("checkout", "-qb", "feature")
			if err := os.WriteFile(filepath.Join(dir, "feature"), []byte("head"), 0600); err != nil {
				t.Fatal(err)
			}
			git("add", "feature")
			git("commit", "-qm", "head")
			head := git("rev-parse", "HEAD")
			git("checkout", "-q", "--detach", base)
			git("merge", "--no-ff", "-qm", "candidate", head)
			live := strings.Repeat("d", 40)
			if fresh {
				live = base
			}
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nprintf '%s\\n' "+live+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, "output")
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "EVENT_NAME=pull_request", "FULL_CANDIDATE=true", "BASE_SHA="+strings.Repeat("e", 40), "HEAD_SHA="+head, "DEFAULT_BRANCH=main", "GITHUB_REPOSITORY=Hikyo-Org/Hikyo", "GITHUB_OUTPUT="+output)
			out, err := cmd.CombinedOutput()
			if (err == nil) != fresh {
				t.Fatalf("fresh=%v error=%v %s", fresh, err, out)
			}
			if fresh {
				bytes, err := os.ReadFile(output)
				if err != nil || string(bytes) != "base_sha="+base+"\n" {
					t.Fatal("actual base was not propagated")
				}
			}
		})
	}
}

// Execute the actual YAML classifier against a source checkout that poisons
// both local tooling files. Planning must use the immutable base's tools.
func TestTrustedClassifierUsesBaseToolsForEveryCandidateMode(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	script := workflow.Jobs["changes"].Steps[2].Run
	for _, name := range []string{"merge-group", "full-PR", "legacy-PR", "missing-registry", "missing-classifier"} {
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
			write("bin/gh", "#!/bin/sh\nprintf '%s\\n' "+base+"\n")
			output, runner := filepath.Join(dir, "output"), filepath.Join(dir, "runner")
			if err := os.Mkdir(runner, 0700); err != nil {
				t.Fatal(err)
			}
			event, full, mode := "pull_request", "false", "files"
			if name == "merge-group" {
				event, mode = "merge_group", "all"
			}
			if name == "full-PR" {
				full, mode = "true", "all"
			}
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", script)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "PATH="+filepath.Join(dir, "bin")+":"+os.Getenv("PATH"), "EVENT_NAME="+event, "FULL_CANDIDATE="+full, "BASE_SHA="+base, "HEAD_SHA="+head, "DEFAULT_BRANCH=main", "GITHUB_REPOSITORY=Hikyo-Org/Hikyo", "GITHUB_OUTPUT="+output, "RUNNER_TEMP="+runner)
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
			want := fmt.Sprintf("plan={\"trusted\":true,\"mode\":\"%s\"}\n", mode)
			if full == "true" {
				want = "base_sha=" + base + "\n" + want
			}
			if string(got) != want {
				t.Fatalf("planning output: got %q want %q", got, want)
			}
		})
	}
}
