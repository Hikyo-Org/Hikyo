package ci_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Contract: every input of a supply-chain-checks fixture must reach the
// fixtures on both paths that run them, and each is guarded on its own:
//   - PR CI (ci-feedback.yml, select-supply-chain-fixtures.sh) runs them when a
//     diff touches an entry of scripts/ci/supply-chain-fixture-inputs.txt.
//     TestSupplyChainInputListCoversEveryInput requires an entry for each input.
//   - The merge queue and ci-dev run them when the changed-path classifier
//     selects the job. TestSupplyChainFixtureInputsSelectTheJob requires that for
//     each input, so a path a fixture reads but the classifier narrows away (as
//     `*.go`, `docs/*` and the kind-script cases did) cannot skip the job there.
//
// Inputs are derived from the job itself, not listed: the repository paths each
// `run:` step names (scripts, directories, requirements), then, through every
// shell, Python or hook file reached that way, every tracked file it names,
// resolved from the repository root or from its own directory
// (`$(dirname "$0")/..`). Only code files (.sh, .py, .bash, hooks) are read for
// further names, so a data file is an input but not a source of more. The one
// exclusion is supplyChainSampleSources: the classifier fixture's sample paths.

const (
	supplyChainJob          = "supply-chain-checks"
	supplyChainValidateStep = "Validate the immutable caller revision"
	// A step that names no repository path cannot be proven covered, so a new
	// one must be recognized here or made to name its inputs.
	supplyChainInline = "Chart renders and carries release substitution anchors"
)

// Go packages run by the job compile most of the module; no path list can
// close that. A package must select the job or the test domain (`test` runs
// every package), which the contract asserts instead of the job alone.
var supplyChainGoPackages = map[string]bool{
	"internal/app": true, "internal/releasetrust": true,
	"scripts/ci": true, "scripts/ci/pr-gate": true, "scripts/ci/queue-report": true,
	"scripts/ci/checksuites": true, "scripts/release": true,
}

// These scripts name repository paths as data: classifier patterns, planner
// samples and lane fixtures, not files they read. Only the code they reach is
// an input; their data paths are excluded on purpose, so a new script that
// merely mentions a path does not widen the trigger.
var supplyChainSampleSources = map[string]bool{
	"scripts/ci/classify-changed-paths.sh":      true,
	"scripts/ci/classify-changed-paths_test.sh": true,
	"scripts/ci/plan-merge-group.sh":            true,
	"scripts/ci/feedback-plan.sh":               true,
	"scripts/ci/fast-feedback.sh":               true,
	"scripts/ci/fast-feedback_test.sh":          true,
	"scripts/ci/fast-feedback-plan_test.sh":     true,
	"scripts/ci/fast-feedback-lanes_test.sh":    true,
	"scripts/ci/fast-feedback-deadline_test.sh": true,
	"scripts/ci/golangci-lint-new_test.sh":      true,
	// Comments and fixture payloads, not reads of the repository's own files.
	"scripts/ci/check-doc-status_test.sh":        true,
	"scripts/ci/discover-dev-prs.sh":             true,
	"scripts/ci/security-compose-rehearsal.sh":   true,
	"scripts/ci/security-pi-rehearsal.sh":        true,
	"scripts/release/prepare-image-root_test.sh": true,
}

// The root documents and LICENSE are read by check-oss-policy.sh, which the
// docs job also runs on every docs plan (ci.yml, verify-docs.sh). Selecting
// supply-chain-checks for them would end the docs-only merge-group fast path
// for no extra coverage, so the classifier guard accepts the docs plan for them.
// They still have list entries: a PR that edits one runs the fixtures.
var supplyChainCoveredByDocsJob = map[string]bool{
	"README.md": true, "CONTRIBUTING.md": true, "GOVERNANCE.md": true, "SECURITY.md": true,
	"SUPPORT.md": true, "TRADEMARK.md": true, "LICENSE": true,
}

// supplyChainGoDomain reports whether an input is Go code the feedback lanes
// already test: a listed package, or a single .go file. These select the job or
// the test domain in the classifier and have no entry in the input list.
func supplyChainGoDomain(p string) bool {
	return supplyChainGoPackages[p] || path.Ext(p) == ".go"
}

var supplyChainToken = regexp.MustCompile(`[A-Za-z0-9_.@/+-]+`)

type supplyChainRepo struct {
	files map[string]string // tracked path -> content ("" for non-code)
	dirs  map[string]bool
}

func newSupplyChainRepo(files map[string]string) supplyChainRepo {
	r := supplyChainRepo{files: files, dirs: map[string]bool{}}
	for f := range files {
		for d := path.Dir(f); d != "."; d = path.Dir(d) {
			r.dirs[d] = true
		}
	}
	return r
}

func (r supplyChainRepo) isDir(p string) bool { return r.dirs[p] }

func isCode(p string) bool {
	switch path.Ext(p) {
	case ".sh", ".py", ".bash":
		return true
	}
	return strings.HasPrefix(p, ".githooks/")
}

// resolve maps a token to a tracked path, from the repository root or from the
// directory of the file that names it.
func (r supplyChainRepo) resolve(token, from string) (string, bool) {
	token = strings.TrimRight(token, ".,:;")
	var candidates []string
	if from != "" {
		candidates = append(candidates, path.Join(path.Dir(from), token))
	}
	root := token
	for strings.HasPrefix(root, "./") || strings.HasPrefix(root, "../") {
		root = strings.TrimPrefix(strings.TrimPrefix(root, "./"), "../")
	}
	candidates = append(candidates, root)
	for _, c := range candidates {
		if _, ok := r.files[c]; ok {
			return c, true
		}
		if r.isDir(c) {
			return c, true
		}
	}
	return "", false
}

// supplyChainInputs returns every derived input path and the step that led to it.
func supplyChainInputs(r supplyChainRepo, runs map[string]string) (map[string]string, error) {
	inputs := map[string]string{}
	var queue []string
	add := func(p, origin string) {
		if _, ok := inputs[p]; !ok {
			inputs[p] = origin
			queue = append(queue, p)
		}
	}
	for name, run := range runs {
		found := false
		for _, token := range supplyChainToken.FindAllString(run, -1) {
			if p, ok := r.resolve(token, ""); ok && strings.Contains(p, "/") {
				add(p, name)
				found = true
			}
		}
		if !found && name != supplyChainInline {
			return nil, fmt.Errorf("step %q names no repository path; name its inputs or extend the contract", name)
		}
	}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		var codes []string
		if _, ok := r.files[p]; ok {
			codes = []string{p}
		} else {
			for f := range r.files {
				if strings.HasPrefix(f, p+"/") {
					codes = append(codes, f)
				}
			}
		}
		for _, code := range codes {
			if !isCode(code) {
				continue
			}
			for _, token := range supplyChainToken.FindAllString(r.files[code], -1) {
				q, ok := r.resolve(token, code)
				if !ok || r.isDir(q) || (!isCode(q) && supplyChainSampleSources[code]) {
					continue
				}
				add(q, code)
			}
		}
	}
	return inputs, nil
}

func supplyChainRuns(t *testing.T, ci []byte) map[string]string {
	t.Helper()
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(ci, &workflow); err != nil {
		t.Fatal(err)
	}
	runs := map[string]string{}
	for _, s := range workflow.Jobs[supplyChainJob].Steps {
		if s.Run != "" && s.Name != supplyChainValidateStep {
			runs[s.Name] = s.Run
		}
	}
	if len(runs) < 30 {
		t.Fatalf("%s lost its fixture steps: %d", supplyChainJob, len(runs))
	}
	return runs
}

func supplyChainRepository(t *testing.T, root string) supplyChainRepo {
	t.Helper()
	list, err := exec.CommandContext(context.Background(), "git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range strings.Split(strings.TrimRight(string(list), "\x00"), "\x00") {
		if f == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			continue // tracked but deleted in this worktree
		}
		if isCode(f) {
			data, err := os.ReadFile(filepath.Join(root, f))
			if err != nil {
				t.Fatal(err)
			}
			files[f] = string(data)
		} else {
			files[f] = ""
		}
	}
	return newSupplyChainRepo(files)
}

// supplyChainUncovered classifies each input with the given classifier and
// returns those that do not select the job (or, for Go packages, the test
// domain).
func supplyChainUncovered(t *testing.T, classifier, registry string, r supplyChainRepo, inputs map[string]string) []string {
	t.Helper()
	var bad []string
	for p, origin := range inputs {
		probe, want := p, []string{supplyChainJob}
		if r.isDir(p) {
			probe = p + "/probe"
		}
		if supplyChainGoPackages[p] {
			probe = p + "/probe.go"
		}
		if supplyChainGoDomain(p) {
			want = []string{supplyChainJob, "test"}
		}
		if supplyChainCoveredByDocsJob[p] {
			want = []string{"docs"}
		}
		cmd := exec.CommandContext(context.Background(), "sh", classifier, "--files")
		cmd.Env = append(os.Environ(), "CI_JOB_REGISTRY="+registry)
		cmd.Stdin = strings.NewReader(probe + "\n")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("classify %s: %v", probe, err)
		}
		var plan map[string]bool
		if err := yaml.Unmarshal(out, &plan); err != nil {
			t.Fatal(err)
		}
		selected := false
		for _, job := range want {
			selected = selected || plan[job]
		}
		if !selected {
			bad = append(bad, fmt.Sprintf("%s (probe %s, from %s) does not select %s", p, probe, origin, strings.Join(want, " or ")))
		}
	}
	sort.Strings(bad)
	return bad
}

func TestSupplyChainFixtureInputsSelectTheJob(t *testing.T) {
	root := repositoryRoot(t)
	ci, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	repo := supplyChainRepository(t, root)
	runs := supplyChainRuns(t, ci)
	inputs, err := supplyChainInputs(repo, runs)
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{".githooks/pre-push", "scripts/git/install-hooks.sh", "scripts/ci/check-commit-signatures.sh", "scripts/release", "chart/hikyo/values.yaml"} {
		if _, ok := inputs[must]; !ok {
			t.Errorf("derivation lost known input %s (%d inputs)", must, len(inputs))
		}
	}
	classifier := filepath.Join(root, "scripts/ci/classify-changed-paths.sh")
	registry := filepath.Join(root, "scripts/ci/ci-job-registry.json")
	for _, bad := range supplyChainUncovered(t, classifier, registry, repo, inputs) {
		t.Error(bad)
	}
}
