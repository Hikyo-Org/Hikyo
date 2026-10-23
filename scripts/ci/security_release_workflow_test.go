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

// Permit reviewed additions while conserving the complete earlier baseline.
// Missing, changed, duplicated or fail-open additions still fail this check.
func normalizeSecurityAdditions(workflow map[string]map[string]any) error {
	// Validate the entire isolated source job before excluding this additive
	// execution from the frozen inventory. Never regenerate baseline hashes.
	jobName := "source-artifact-security"
	job, exists := workflow[jobName]
	if !exists {
		return fmt.Errorf("source artifact security job is missing")
	}
	jobSteps, ok := job["steps"].([]any)
	guardSteps, guardOK := workflow["changes"]["steps"].([]any)
	if !ok || !guardOK || len(jobSteps) == 0 || len(guardSteps) == 0 || !reflect.DeepEqual(jobSteps[0], guardSteps[0]) {
		return fmt.Errorf("source artifact security lost the immutable caller guard")
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "source-artifact-security.yml"))
	if err != nil {
		return err
	}
	var expectedJob map[string]any
	if err := yaml.Unmarshal(fixture, &expectedJob); err != nil {
		return err
	}
	normalizedJob := make(map[string]any, len(job))
	for key, value := range job {
		normalizedJob[key] = value
	}
	normalizedJob["steps"] = jobSteps[1:]
	if !reflect.DeepEqual(normalizedJob, expectedJob) {
		return fmt.Errorf("source artifact security job lost its complete fail-closed configuration")
	}
	needs, ok := workflow["ci-required"]["needs"].([]any)
	if !ok {
		return fmt.Errorf("security job lost required aggregate dependencies")
	}
	keptNeeds := make([]any, 0, len(needs))
	found := 0
	for _, need := range needs {
		if need == jobName {
			found++
		} else {
			keptNeeds = append(keptNeeds, need)
		}
	}
	if found != 1 {
		return fmt.Errorf("source artifact security must gate ci-required exactly once")
	}
	workflow["ci-required"]["needs"] = keptNeeds
	delete(workflow, jobName)
	raw, err := os.ReadFile(filepath.Join("testdata", "security-additions.yml"))
	if err != nil {
		return err
	}
	var additions map[string][]map[string]any
	if err := yaml.Unmarshal(raw, &additions); err != nil {
		return err
	}
	for job, required := range additions {
		steps, ok := workflow[job]["steps"].([]any)
		if !ok {
			return fmt.Errorf("%s lost security checks", job)
		}
		kept := []any{}
		next := 0
		for _, value := range steps {
			step, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid %s step", job)
			}
			security := false
			for _, expected := range required {
				if step["name"] == expected["name"] {
					security = true
					break
				}
			}
			if security {
				if next >= len(required) || !reflect.DeepEqual(step, required[next]) {
					return fmt.Errorf("%s security check changed, reordered or duplicated", job)
				}
				next++
			} else {
				kept = append(kept, step)
			}
		}
		if next != len(required) {
			return fmt.Errorf("%s omitted mandatory security checks", job)
		}
		workflow[job]["steps"] = kept
	}
	for _, value := range workflow["lint"]["steps"].([]any) {
		step := value.(map[string]any)
		if step["name"] == "Shellcheck (pinned archive)" {
			run := step["run"].(string)
			if strings.Count(run, "scripts/security/*.sh") != 1 {
				return fmt.Errorf("security shell scripts lost canonical lint")
			}
			step["run"] = strings.Replace(run, " scripts/security/*.sh", "", 1)
		}
	}
	// These already reviewed full-main tools also run for admitted release tags.
	// Only the narrow event selector is normalized; all tool bodies stay hashed.
	for _, job := range []string{"analysis_shards", "race_shard"} {
		for _, value := range workflow[job]["steps"].([]any) {
			step := value.(map[string]any)
			run, _ := step["run"].(string)
			if strings.Contains(run, "^(workflow_dispatch|schedule|repository_dispatch|push)$") {
				run = strings.Replace(run, "^(workflow_dispatch|schedule|repository_dispatch|push)$", "^(workflow_dispatch|schedule|repository_dispatch)$", 1)
				run = strings.Replace(run, `[[ "$GITHUB_REF" = refs/heads/main || "$EVENT_NAME" = push ]]`, `test "$GITHUB_REF" = refs/heads/main`, 1)
				step["run"] = run
			}
		}
	}
	return nil
}

func TestReleaseTagsRequireFullValidationAndNativePostgres(t *testing.T) {
	w := readConsolidatedWorkflow(t, "release")
	// Decode reusable fields separately because this fixture type focuses on jobs.
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var release mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &release); err != nil {
		t.Fatal(err)
	}
	if release.Jobs["validation"].Uses != "./.github/workflows/ci.yml" || release.Jobs["validation"].With["revision"] != "${{ github.sha }}" {
		t.Fatal("release source is not exact full CI")
	}
	needs, err := workflowNeeds(w.Jobs["build-artifacts"].Needs)
	if err != nil || !contains(needs, "validation") {
		t.Fatal("signing may run before full validation")
	}
	for _, s := range w.Jobs["build-artifacts"].Steps {
		if s.Name == "Test release source" {
			if s.Env["RELEASE_SCHEMA_CONTAINER"] != "hikyo-ci-postgres" {
				t.Fatal("release uses undeclared PostgreSQL service")
			}
			return
		}
	}
	t.Fatal("release source checks absent")
}
func contains(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func TestCanonicalTagRevisionAdmission(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var w mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	guard := w.Jobs["changes"].Steps[0].Run
	sha := strings.Repeat("a", 40)
	for name, change := range map[string]map[string]string{
		"stable": {}, "prerelease": {"GITHUB_REF": "refs/tags/v1.2.3-rc.1"}, "branch": {"GITHUB_REF": "refs/heads/main"}, "nightly": {"GITHUB_REF": "refs/tags/v1.2.3-nightly.20261014.1.gaaaaaaaa"}, "deleted": {"TAG_DELETED": "true"}, "candidate": {"FULL_CANDIDATE": "true"}, "wrong-sha": {"SOURCE_REVISION": strings.Repeat("b", 40)}, "empty-tag": {"GITHUB_REF": "refs/tags/"}, "leading-zero": {"GITHUB_REF": "refs/tags/v01.2.3"}, "empty-identifier": {"GITHUB_REF": "refs/tags/v1.2.3-rc..1"}, "numeric-leading-zero": {"GITHUB_REF": "refs/tags/v1.2.3-rc.01"}, "wrong-default": {"DEFAULT_BRANCH": "feature"},
	} {
		t.Run(name, func(t *testing.T) {
			env := map[string]string{"GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/tags/v1.2.3", "GITHUB_SHA": sha, "SOURCE_REVISION": sha, "GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID": "10", "DEFAULT_BRANCH": "main", "FULL_CANDIDATE": "false", "TAG_DELETED": "false"}
			for k, v := range change {
				env[k] = v
			}
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", guard)
			cmd.Env = os.Environ()
			for k, v := range env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			output, err := cmd.CombinedOutput()
			want := name == "stable" || name == "prerelease"
			if (err == nil) != want {
				t.Fatalf("allowed %v want %v: %s", err == nil, want, output)
			}
		})
	}
}

func TestReleaseTagPlanRequiresPeeledCommitAndMainAncestry(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var w mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"exact-main-tag", "moved-tag", "outside-main"} {
		t.Run(scenario, func(t *testing.T) {
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
			git("init", "-q", "-b", "main")
			git("config", "user.name", "Fixture")
			git("config", "user.email", "fixture@example.test")
			git("config", "commit.gpgsign", "false")
			if err := os.MkdirAll(filepath.Join(dir, "scripts/ci"), 0700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"classify-changed-paths.sh", "ci-job-registry.json"} {
				raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts/ci", name))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "scripts/ci", name), raw, 0700); err != nil {
					t.Fatal(err)
				}
			}
			git("add", ".")
			git("commit", "-qm", "trusted-main")
			source := git("rev-parse", "HEAD")
			git("update-ref", "refs/remotes/origin/main", source)
			if scenario == "outside-main" {
				git("checkout", "-q", "--orphan", "orphan")
				git("commit", "-qm", "outside-main")
				source = git("rev-parse", "HEAD")
			}
			git("tag", "v1.2.3")
			if scenario == "moved-tag" {
				source = strings.Repeat("b", 40)
			}
			output := filepath.Join(dir, "output")
			cmd := exec.Command("bash", "-euo", "pipefail", "-c", w.Jobs["changes"].Steps[2].Run)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "EVENT_NAME=push", "FULL_CANDIDATE=false", "SOURCE_REVISION="+source, "GITHUB_REF=refs/tags/v1.2.3", "GITHUB_OUTPUT="+output)
			out, err := cmd.CombinedOutput()
			if scenario != "exact-main-tag" {
				if err == nil {
					t.Fatal("unbound tag selected a plan")
				}
				return
			}
			if err != nil {
				t.Fatalf("plan: %v %s", err, out)
			}
			result, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(result), "plan={") || strings.Contains(string(result), ":false") {
				t.Fatalf("tag did not select every validation domain: %s", result)
			}
		})
	}
}

func TestReleaseEvidenceOnlyUploadsAfterVerificationAndCleanup(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/security-release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Steps []mainValidationStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	if len(w.Permissions) != 2 || w.Permissions["contents"] != "read" || w.Permissions["actions"] != "read" {
		t.Fatal("evidence workflow may not mutate releases or statuses")
	}
	stages := map[string]int{}
	for i, s := range w.Jobs["validate"].Steps {
		stages[s.Name] = i
		if s.Env["GH_TOKEN"] != "" && s.Name != "Download and authenticate actual release bytes" {
			t.Fatal("repository credential reaches runtime step")
		}
		if strings.Contains(s.Uses, "upload-artifact") && s.Name != "Retain version-bound failure diagnostic receipts" && (s.If != "" || !strings.HasPrefix(s.With["name"], "security-pass-${{ needs.resolve.outputs.tag }}-${{ needs.resolve.outputs.source }}")) {
			t.Fatal("receipt uploads must be success-only and version-bound")
		}
		if strings.Contains(s.Run, "gh release upload") || strings.Contains(s.Run, "gh release edit") {
			t.Fatal("postrelease workflow mutates release")
		}
	}
	ordered := []string{"Download and authenticate actual release bytes", "Vulnerability scan of authenticated actual binary", "Native release adversarial campaign on both datastores", "Browser attacks and oracle controls against the same actual binary", "Remove disposable PostgreSQL before any PASS publication", "Validate complete mandatory receipts and render PASS evidence", "Store version-bound passing JSON and self-contained HTML"}
	prev := -1
	for _, name := range ordered {
		i, ok := stages[name]
		if !ok || i <= prev {
			t.Fatalf("mandatory stage absent or reordered: %s", name)
		}
		prev = i
	}
	resolve := w.Jobs["resolve"].Steps[0].Run
	if strings.Contains(resolve, "target_commitish") && !strings.Contains(resolve, "git/ref/tags/$tag") {
		t.Fatal("release identity uses mutable target")
	}
	if !strings.Contains(resolve, "git/ref/heads/main") || !strings.Contains(resolve, "git/tags/$source") {
		t.Fatal("trusted harness and peeled tag must be independently resolved")
	}
	installed := false
	for _, step := range w.Jobs["validate"].Steps {
		if step.Name == "Install pinned verifier for published stable OCI signatures" {
			installed = step.Uses == "sigstore/cosign-installer@6f9f17788090df1f26f669e9d70d6ae9567deba6" && step.With["cosign-release"] == "v3.1.3" && step.If == "${{ !contains(needs.resolve.outputs.tag, '-nightly.') }}"
		}
		if step.Name == "Download and authenticate actual release bytes" && !installed {
			t.Fatal("stable published verifier requires pinned cosign before download verification")
		}
	}
}

// Failure diagnostics must stay a narrow metadata allowlist, isolated from the
// complete receipt validation required for a PASS artifact.
func TestReleaseFailureDiagnosticReceiptPolicy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/security-release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		Jobs map[string]struct {
			Steps []mainValidationStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, step := range w.Jobs["validate"].Steps {
		if step.Name != "Retain version-bound failure diagnostic receipts" {
			continue
		}
		count++
		want := map[string]string{
			"name":              "security-failure-diagnostics-${{ needs.resolve.outputs.tag }}-${{ needs.resolve.outputs.source }}-${{ github.run_id }}-${{ github.run_attempt }}",
			"path":              "${{ runner.temp }}/security-release/prepared.json\n${{ runner.temp }}/security-release/binary-scan.json\n${{ runner.temp }}/security-release/scan-completed-at\n${{ runner.temp }}/security-release/attacks.json\n${{ runner.temp }}/security-release/browser.json\n",
			"if-no-files-found": "ignore", "retention-days": "7",
		}
		if step.If != "${{ failure() }}" || step.Uses != "actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9" || step.Run != "" || len(step.Env) != 0 || !reflect.DeepEqual(step.With, want) {
			t.Fatalf("failure diagnostics lost their failure-only metadata boundary: %+v", step)
		}
	}
	if count != 1 {
		t.Fatalf("want exactly one diagnostic receipt upload, got %d", count)
	}
}
