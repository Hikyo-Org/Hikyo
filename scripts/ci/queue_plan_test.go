package ci_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type docsQueueFixture struct {
	dir, base, head, candidate string
	metadata                   map[string]any
}

func (f *docsQueueFixture) git(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = f.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *docsQueueFixture) write(t *testing.T, path, content string) {
	t.Helper()
	path = filepath.Join(f.dir, path)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
}

func newDocsQueueFixture(t *testing.T, paths []string) *docsQueueFixture {
	t.Helper()
	f := &docsQueueFixture{dir: t.TempDir()}
	f.git(t, "init", "-q")
	f.git(t, "config", "user.name", "Fixture")
	f.git(t, "config", "user.email", "fixture@example.test")
	f.git(t, "config", "commit.gpgsign", "false")
	for _, path := range []string{"classify-changed-paths.sh", "ci-job-registry.json", "check-required-jobs.sh", "plan-merge-group.sh"} {
		data, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts/ci", path))
		if err != nil {
			t.Fatal(err)
		}
		f.write(t, "scripts/ci/"+path, string(data))
	}
	f.write(t, "docs/old.md", "base\n")
	f.write(t, "internal/example/value.go", "package example\n")
	f.git(t, "add", ".")
	f.git(t, "commit", "-qm", "base")
	f.base = f.git(t, "rev-parse", "HEAD")
	f.git(t, "checkout", "-qb", "feature")
	for _, path := range paths {
		if strings.HasPrefix(path, "delete:") {
			if err := os.Remove(filepath.Join(f.dir, strings.TrimPrefix(path, "delete:"))); err != nil {
				t.Fatal(err)
			}
		} else {
			f.write(t, path, "changed\n")
		}
	}
	f.git(t, "add", ".")
	f.git(t, "commit", "--allow-empty", "-qm", "change")
	f.head = f.git(t, "rev-parse", "HEAD")
	f.git(t, "checkout", "-q", "--detach", f.base)
	f.git(t, "merge", "--no-ff", "-qm", "queue candidate", f.head)
	f.candidate = f.git(t, "rev-parse", "HEAD")
	f.metadata = map[string]any{
		"number": 7, "state": "open", "labels": []any{},
		"base": map[string]any{"ref": "main", "repo": map[string]any{"full_name": "Hikyo-Org/Hikyo"}},
		"head": map[string]any{"sha": f.head, "repo": map[string]any{"id": 8}},
		"user": map[string]any{"id": 10, "login": "contributor", "type": "User"},
	}
	f.write(t, "bin/gh", "#!/bin/sh\nset -eu\ntest \"$1\" = api\ntest \"$2\" = repos/Hikyo-Org/Hikyo/pulls/7\ncat \"$QUEUE_FIXTURE_METADATA\"\n")
	return f
}

func (f *docsQueueFixture) env(t *testing.T) []string {
	t.Helper()
	data, err := json.Marshal(f.metadata)
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, "metadata.json", string(data))
	if err := os.MkdirAll(filepath.Join(f.dir, "runner"), 0700); err != nil {
		t.Fatal(err)
	}
	return append(os.Environ(),
		"PATH="+filepath.Join(f.dir, "bin")+":"+os.Getenv("PATH"),
		"QUEUE_FIXTURE_METADATA="+filepath.Join(f.dir, "metadata.json"),
		"BASE_SHA="+f.base, "SOURCE_REVISION="+f.candidate,
		"GITHUB_REPOSITORY=Hikyo-Org/Hikyo", "GITHUB_REF=refs/heads/gh-readonly-queue/main/pr-7-"+f.base,
		"EVENT_NAME=merge_group", "GITHUB_EVENT_NAME=merge_group", "FULL_CANDIDATE=false", "HEAD_SHA="+f.head,
		"RUNNER_TEMP="+filepath.Join(f.dir, "runner"), "GITHUB_OUTPUT="+filepath.Join(f.dir, "output"),
		"GITHUB_STEP_SUMMARY="+filepath.Join(f.dir, "summary"), "DEFAULT_BRANCH=main",
	)
}

func TestDocsQueuePlanFallsBackForEveryUncertainBoundary(t *testing.T) {
	for _, tt := range []struct {
		name, mode string
		paths      []string
		mutate     func(*docsQueueFixture)
	}{
		{name: "readme", mode: "docs", paths: []string{"README.md"}},
		{name: "image", mode: "docs", paths: []string{"docs/assets/sponsors/logo.png"}},
		{name: "landing", mode: "docs", paths: []string{"docs/site/src/pages/index.astro"}},
		{name: "deleted docs", mode: "docs", paths: []string{"delete:docs/old.md"}},
		{name: "backend", mode: "full", paths: []string{"internal/example/value.go"}},
		{name: "code moved to docs", mode: "full", paths: []string{"delete:internal/example/value.go", "docs/old-code.md"}},
		{name: "mixed", mode: "full", paths: []string{"README.md", "web/src/page.tsx"}},
		{name: "site dependency", mode: "full", paths: []string{"docs/site/package.json"}},
		{name: "site config", mode: "full", paths: []string{"docs/site/astro.config.mjs"}},
		{name: "shared app CSP checker", mode: "full", paths: []string{"docs/site/scripts/test-csp.mjs"}},
		{name: "unknown site executable helper", mode: "full", paths: []string{"docs/site/scripts/new-build-helper.mjs"}},
		{name: "floor policy", mode: "full", paths: []string{"docs/release/measurements/derate.json"}},
		{name: "acceptance ledger", mode: "full", paths: []string{"docs/release/acceptance-1.0.md"}},
		{name: "MVP acceptance inputs", mode: "full", paths: []string{"docs/adr/mvp-boundary.md"}},
		{name: "SAML acceptance inputs", mode: "full", paths: []string{"docs/adr/saml-sp.md"}},
		{name: "SCIM acceptance inputs", mode: "full", paths: []string{"docs/adr/scim-provisioning.md"}},
		{name: "scanning acceptance inputs", mode: "full", paths: []string{"docs/adr/secret-scanning.md"}},
		{name: "runtime image", mode: "full", paths: []string{"web/public/logo.png"}},
		{name: "newline path", mode: "full", paths: []string{"docs/site/file\nREADME.md"}},
		{name: "empty", mode: "full"},
		{name: "dependabot human rerun", mode: "full", paths: []string{"README.md"}, mutate: func(f *docsQueueFixture) {
			f.metadata["user"] = map[string]any{"id": 42, "login": "dependabot[bot]", "type": "Bot"}
		}},
		{name: "unknown author", mode: "full", paths: []string{"README.md"}, mutate: func(f *docsQueueFixture) { delete(f.metadata, "user") }},
		{name: "force full", mode: "full", paths: []string{"README.md"}, mutate: func(f *docsQueueFixture) {
			f.metadata["labels"] = []any{map[string]any{"name": "ci:full"}}
		}},
		{name: "changed head", mode: "full", paths: []string{"README.md"}, mutate: func(f *docsQueueFixture) {
			f.metadata["head"] = map[string]any{"sha": strings.Repeat("f", 40), "repo": map[string]any{"id": 8}}
		}},
		{name: "other PR", mode: "full", paths: []string{"README.md"}, mutate: func(f *docsQueueFixture) { f.metadata["number"] = 8 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newDocsQueueFixture(t, tt.paths)
			if tt.mutate != nil {
				tt.mutate(f)
			}
			cmd := exec.Command("bash", filepath.Join(repositoryRoot(t), "scripts/ci/plan-merge-group.sh"))
			cmd.Dir, cmd.Env = f.dir, f.env(t)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("plan: %v %s", err, out)
			}
			var result struct {
				Mode string `json:"mode"`
			}
			if err := json.Unmarshal(out, &result); err != nil || result.Mode != tt.mode {
				t.Fatalf("want %s, got %s (%v)", tt.mode, out, err)
			}
		})
	}
}

func TestDocsQueueWorkflowUsesTrustedPlanAndRecomputesGate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	var classify, gate string
	for _, step := range workflow.Jobs["changes"].Steps {
		if step.Name == "Classify changed paths" {
			classify = step.Run
		}
	}
	for _, step := range workflow.Jobs["ci-required"].Steps {
		if step.Name == "Require every validation job" {
			gate = step.Run
		}
	}
	if classify == "" || gate == "" {
		t.Fatal("missing workflow steps")
	}
	f := newDocsQueueFixture(t, []string{"README.md"})
	// Poison the checkout copies: both actual YAML steps must extract base tools.
	for _, name := range []string{"plan-merge-group.sh", "classify-changed-paths.sh", "check-required-jobs.sh"} {
		f.write(t, "scripts/ci/"+name, "#!/bin/sh\necho candidate-tool-executed >&2\nexit 91\n")
	}
	run := func(script string, extra ...string) ([]byte, error) {
		cmd := exec.Command("bash", "-euo", "pipefail", "-c", script)
		cmd.Dir, cmd.Env = f.dir, append(f.env(t), extra...)
		return cmd.CombinedOutput()
	}
	if out, err := run(classify); err != nil {
		t.Fatalf("trusted classify: %v %s", err, out)
	}
	output, err := os.ReadFile(filepath.Join(f.dir, "output"))
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.TrimSpace(strings.TrimPrefix(string(output), "plan="))
	results := map[string]map[string]string{}
	var registry registry
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts/ci/ci-job-registry.json"))
	if err != nil || json.Unmarshal(data, &registry) != nil {
		t.Fatal("read registry")
	}
	for name, rule := range registry.Jobs {
		if rule.RequiredGate != "always" && rule.RequiredGate != "planned" {
			continue
		}
		result := "skipped"
		if rule.RequiredGate == "always" || name == "docs" {
			result = "success"
		}
		results[name] = map[string]string{"result": result}
	}
	needs, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := run(gate, "PLAN_JSON="+plan, "NEEDS_JSON="+string(needs)); err != nil {
		t.Fatalf("trusted docs gate: %v %s", err, out)
	}
	// Adding ci:full between planning and aggregation invalidates docs skips.
	f.metadata["labels"] = []any{map[string]any{"name": "ci:full"}}
	if out, err := run(gate, "PLAN_JSON="+plan, "NEEDS_JSON="+string(needs)); err == nil || strings.Contains(string(out), "candidate-tool-executed") {
		t.Fatalf("changed policy must block without candidate tools: %v %s", err, out)
	}
}

func TestDocsQueueFullValidationDoesNotRequireMetadataReauthorization(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	var classify, gate string
	for _, step := range workflow.Jobs["changes"].Steps {
		if step.Name == "Classify changed paths" {
			classify = step.Run
		}
	}
	for _, step := range workflow.Jobs["ci-required"].Steps {
		if step.Name == "Require every validation job" {
			gate = step.Run
		}
	}
	if classify == "" || gate == "" {
		t.Fatal("missing workflow steps")
	}
	var rules registry
	data, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts/ci/ci-job-registry.json"))
	if err != nil || json.Unmarshal(data, &rules) != nil {
		t.Fatal("read registry")
	}
	full := map[string]bool{}
	for _, key := range rules.PathClasses["full"] {
		full[key] = true
	}
	results := map[string]map[string]string{}
	for name, rule := range rules.Jobs {
		if rule.RequiredGate == "always" || rule.RequiredGate == "planned" {
			results[name] = map[string]string{"result": "success"}
		}
	}
	encode := func(value any) string {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for _, scenario := range []string{"full with docs metadata", "metadata API recovers", "ci:full label removed"} {
		t.Run(scenario, func(t *testing.T) {
			f := newDocsQueueFixture(t, []string{"README.md"})
			gh, err := os.ReadFile(filepath.Join(f.dir, "bin/gh"))
			if err != nil {
				t.Fatal(err)
			}
			run := func(script string, extra ...string) ([]byte, error) {
				cmd := exec.Command("bash", "-euo", "pipefail", "-c", script)
				cmd.Dir, cmd.Env = f.dir, append(f.env(t), extra...)
				return cmd.CombinedOutput()
			}
			plan := encode(full)
			if scenario != "full with docs metadata" {
				if scenario == "metadata API recovers" {
					f.write(t, "bin/gh", "#!/bin/sh\nexit 1\n")
				} else {
					f.metadata["labels"] = []any{map[string]any{"name": "ci:full"}}
				}
				if out, err := run(classify); err != nil {
					t.Fatalf("initial full planning: %v %s", err, out)
				}
				output, err := os.ReadFile(filepath.Join(f.dir, "output"))
				if err != nil {
					t.Fatal(err)
				}
				plan = strings.TrimSpace(strings.TrimPrefix(string(output), "plan="))
				var actual map[string]bool
				if err := json.Unmarshal([]byte(plan), &actual); err != nil || encode(actual) != encode(full) {
					t.Fatalf("expected actual workflow to retain full plan, got %s: %v", plan, err)
				}
			}
			// Metadata now permits a docs-only plan, but every job already ran.
			f.metadata["labels"] = []any{}
			f.write(t, "bin/gh", strings.Replace(string(gh), "set -eu\n", "set -eu\ntouch \"$QUEUE_FIXTURE_METADATA.queried\"\n", 1))
			if out, err := run(gate, "PLAN_JSON="+plan, "NEEDS_JSON="+encode(results)); err != nil {
				t.Fatalf("successful full validation must survive metadata changes: %v %s", err, out)
			}
			if _, err := os.Stat(filepath.Join(f.dir, "metadata.json.queried")); !os.IsNotExist(err) {
				t.Fatal("full validation unnecessarily queried PR metadata")
			}
			// Skipping metadata authorization must not skip trusted plan/result validation.
			for _, invalid := range []string{"not-json", "{}", `{"docs":true}`, strings.Replace(plan, `"docs":true`, `"docs":null`, 1), strings.ReplaceAll(plan, "true", "false")} {
				if out, err := run(gate, "PLAN_JSON="+invalid, "NEEDS_JSON="+encode(results)); err == nil {
					t.Fatalf("invalid plan accepted: %s %s", invalid, out)
				}
			}
			results["docs"]["result"] = "failure"
			if out, err := run(gate, "PLAN_JSON="+plan, "NEEDS_JSON="+encode(results)); err == nil {
				t.Fatalf("failed full-suite job accepted: %s", out)
			}
			results["docs"]["result"] = "success"
		})
	}
}
