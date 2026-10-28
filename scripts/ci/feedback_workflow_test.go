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

func TestFeedbackWorkflowPreservesLatestUsefulCommitAndImmutableBootstrap(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-feedback.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := requireFeedbackWorkflow(raw); err != nil {
		t.Fatal(err)
	}
}

func requireFeedbackWorkflow(raw []byte) error {
	var got struct {
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Name     string            `yaml:"name"`
			If       string            `yaml:"if"`
			Timeout  int               `yaml:"timeout-minutes"`
			Continue bool              `yaml:"continue-on-error"`
			Strategy map[string]any    `yaml:"strategy"`
			Env      map[string]string `yaml:"env"`
			Steps    []struct {
				Name      string            `yaml:"name"`
				ID        string            `yaml:"id"`
				Run       string            `yaml:"run"`
				Uses      string            `yaml:"uses"`
				If        string            `yaml:"if"`
				With      map[string]string `yaml:"with"`
				Env       map[string]string `yaml:"env"`
				Directory string            `yaml:"working-directory"`
				Timeout   int               `yaml:"timeout-minutes"`
				Continue  bool              `yaml:"continue-on-error"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &got); err != nil {
		return err
	}
	if got.Concurrency.Group != "ci-feedback-${{ github.event.pull_request.number }}-${{ github.event.action == 'edited' && github.event.changes.base.ref.from == '' && github.run_id || 'admitted' }}" || !got.Concurrency.Cancel {
		return fmt.Errorf("new useful PR revisions must supersede older feedback; skipped metadata edits must remain isolated")
	}
	if !reflect.DeepEqual(got.Permissions, map[string]string{"contents": "read"}) {
		return fmt.Errorf("feedback execution must remain read-only")
	}
	job := got.Jobs["feedback"]
	if job.Name != "${{ github.event.action == 'edited' && github.event.changes.base.ref.from == '' && 'metadata (ignored)' || 'feedback' }} (${{ matrix.lane }})" {
		return fmt.Errorf("metadata skips must have a separate check name so they cannot hide the actual feedback verdict")
	}
	if job.If != "github.event.action != 'edited' || github.event.changes.base.ref.from != ''" {
		return fmt.Errorf("push/open/reopen and genuine base edits must run; metadata-only edits must skip")
	}
	if len(got.Jobs) != 1 || job.Timeout != 4 {
		return fmt.Errorf("all feedback runners include setup in their four-minute budgets")
	}
	expectedStrategy := map[string]any{"fail-fast": false, "matrix": map[string]any{"lane": []any{"go", "tooling", "frontend"}}}
	if job.Continue || !reflect.DeepEqual(job.Strategy, expectedStrategy) {
		return fmt.Errorf("all complete lanes must run independently and succeed; failure must not cancel sibling evidence")
	}
	if len(job.Steps) < 4 {
		return fmt.Errorf("feedback lost its bootstrap or execution steps")
	}
	finalStep := job.Steps[len(job.Steps)-1]
	if finalStep.If != "" || finalStep.Continue || finalStep.Run != `bash "$FAST_FEEDBACK_TOOLS_DIR/fast-feedback.sh" --lane=${{ matrix.lane == 'tooling' && 'go' || matrix.lane }} --go-scope=${{ matrix.lane == 'go' && 'application' || matrix.lane == 'tooling' && 'tooling' || 'all' }}` {
		return fmt.Errorf("each runner must execute its actual selected lane, not rerun all checks or skip a lane")
	}
	if job.Env["GOFLAGS"] != "-gcflags=all=-dwarf=false" {
		return fmt.Errorf("feedback must select the compact compiler cache ABI")
	}
	if job.Env["GOCACHE"] != "${{ github.workspace }}/.ci-cache/go-feedback-compact" {
		return fmt.Errorf("feedback must compile using its dedicated bounded cache")
	}
	if job.Env["FAST_FEEDBACK_TOOLS_DIR"] != "${{ github.workspace }}/feedback-tools/scripts/ci" || job.Env["FAST_FEEDBACK_HEAD"] != "${{ github.event.pull_request.head.sha }}" {
		return fmt.Errorf("immutable informational helper and exact head must be separate")
	}
	if job.Steps[1].With["ref"] != "${{ github.sha }}" || job.Steps[1].With["path"] != "feedback-tools" || job.Steps[3].With["ref"] != "${{ github.event.pull_request.head.sha }}" || job.Steps[3].With["path"] != "source" {
		return fmt.Errorf("old PR heads must bootstrap feedback from the immutable merge revision without changing tested source")
	}
	if job.Steps[0].Directory != "${{ github.workspace }}" || job.Steps[0].Name != "Start the cumulative feedback budget" || !strings.Contains(job.Steps[0].Run, "+ 220") || job.Env["FAST_FEEDBACK_BASE"] != "" {
		return fmt.Errorf("setup must consume the cumulative deadline; stale event base metadata is not the comparison owner")
	}
	if job.Steps[1].With["fetch-depth"] != "2" {
		return fmt.Errorf("immutable tooling must retain the synthetic merge's actual parents")
	}
	var pinnedBase, buildReader, runnerABI bool
	buildReaders := 0
	for _, step := range job.Steps {
		if step.Name == "Select fast feedback domains" {
			pinnedBase = strings.Contains(step.Run, "rev-parse HEAD^1") && strings.Contains(step.Run, `test "$(git -C ../feedback-tools rev-parse HEAD^2)" = "$FAST_FEEDBACK_HEAD"`) && strings.Contains(step.Run, `export FAST_FEEDBACK_BASE="$base"`)
		}
		if step.Name == "Select Go build cache runner ABI" {
			runnerABI = step.ID == "runner-cache-abi" && step.If == "matrix.lane != 'frontend' && (fromJSON(steps.plan.outputs.plan).go || fromJSON(steps.plan.outputs.plan).workflows)" && step.Run == `bash "$FAST_FEEDBACK_TOOLS_DIR/feedback-budget.sh" "$FAST_FEEDBACK_TOOLS_DIR/export-runner-cache-abi.sh"`
		}
		if step.With["path"] == ".ci-cache/go-feedback-compact" && step.Name != "Restore bounded feedback Go cache" {
			return fmt.Errorf("every build-cache reader must satisfy the exact Go-only compatibility contract")
		}
		if step.Name == "Restore bounded feedback Go cache" {
			buildReaders++
			buildReader = step.Uses == "actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9" &&
				step.If == "matrix.lane != 'frontend' && (fromJSON(steps.plan.outputs.plan).go || fromJSON(steps.plan.outputs.plan).workflows)" &&
				!step.Continue && reflect.DeepEqual(step.With, map[string]string{
				"path":         ".ci-cache/go-feedback-compact",
				"key":          "go-feedback-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-${{ hashFiles('feedback-tools/go.mod', 'feedback-tools/go.sum', 'feedback-tools/scripts/ci/go-tool-modules.txt') }}-${{ env.FAST_FEEDBACK_BASE }}",
				"restore-keys": "go-feedback-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-",
			})
		}

	}
	if !pinnedBase || !buildReader || !runnerABI || buildReaders != 1 {
		return fmt.Errorf("feedback needs a verified integration base and bounded compatible read-only Go build cache")
	}
	shellcheckInstallers, releasePythonInstallers := 0, 0
	for index, step := range job.Steps {
		if strings.Contains(step.Run, "fast-feedback_test.sh") || strings.Contains(step.Run, "fast-feedback-deadline_test.sh") {
			return fmt.Errorf("refusal and deadline self-tests belong to mandatory full lint, not the per-commit budget")
		}
		if step.With["path"] == "~/go/pkg/mod" || (strings.Contains(step.Uses, "setup-go@") && step.With["cache"] != "false") {
			return fmt.Errorf("feedback must not restore bulk module archives or implicit setup-go caches")
		}
		if step.Uses != "" {
			if step.Timeout != 1 {
				return fmt.Errorf("external Actions require a one-minute bound")
			}
			if index > 1 && !strings.Contains(job.Steps[index-1].Run, "feedback-budget.sh\" --reserve 60") {
				return fmt.Errorf("external setup must reserve cumulative headroom first")
			}
		}
		if strings.Contains(step.Uses, "setup-go@") && step.If != "matrix.lane != 'frontend' && (fromJSON(steps.plan.outputs.plan).workflows || fromJSON(steps.plan.outputs.plan).go)" {
			return fmt.Errorf("only the Go/tooling lane prepares Go")
		}
		if strings.Contains(step.Uses, "setup-node@") && step.If != "(matrix.lane == 'frontend' && (fromJSON(steps.plan.outputs.plan).web || fromJSON(steps.plan.outputs.plan).client)) || (matrix.lane == 'tooling' && fromJSON(steps.plan.outputs.plan).docs)" {
			return fmt.Errorf("frontend and read-only docs must retain their pinned Node runtime")
		}
		if step.Name == "Install verified pinned ShellCheck" {
			shellcheckInstallers++
			if step.If != "matrix.lane == 'tooling' && (fromJSON(steps.plan.outputs.plan).shell || fromJSON(steps.plan.outputs.plan).workflows)" || step.Continue ||
				step.Env["SHELLCHECK_VERSION"] != "v0.11.0" || step.Env["SHELLCHECK_SHA256"] != "8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198" ||
				!strings.Contains(step.Run, "sha256sum --check --strict") {
				return fmt.Errorf("shell and workflow-only checks require verified pinned ShellCheck in the Go/tooling lane")
			}
		}
		if step.Name == "Prepare trusted release contract dependencies" {
			releasePythonInstallers++
			if step.If != "matrix.lane == 'tooling' && fromJSON(steps.plan.outputs.plan).releaseAdmission" || step.Continue ||
				!strings.Contains(step.Run, `bash "$FAST_FEEDBACK_TOOLS_DIR/feedback-budget.sh" --reserve 60`) ||
				!strings.Contains(step.Run, `bash "$FAST_FEEDBACK_TOOLS_DIR/feedback-budget.sh" python3 -m venv "$RUNNER_TEMP/release-admission-checks"`) ||
				!strings.Contains(step.Run, `bash "$FAST_FEEDBACK_TOOLS_DIR/feedback-budget.sh" "$RUNNER_TEMP/release-admission-checks/bin/python" -m pip install -r scripts/ci/release-admission/requirements.txt`) ||
				!strings.Contains(step.Run, `echo "$RUNNER_TEMP/release-admission-checks/bin" >> "$GITHUB_PATH"`) {
				return fmt.Errorf("release contracts require pinned dependencies in their tooling lane within the original setup budget")
			}
		}
		if strings.Contains(step.Run, "pnpm install") || strings.Contains(step.Run, "install-corepack.sh") || (strings.Contains(step.Uses, "cache/restore@") && step.Name != "Restore bounded feedback Go cache") || step.Name == "Select explicit pnpm cache path and ABI" {
			if !strings.HasPrefix(step.If, "matrix.lane == 'frontend' && ") {
				return fmt.Errorf("frontend dependency preparation must not compete with Go/tooling")
			}
		}
		if strings.Contains(step.Uses, "setup-node@") && !strings.Contains(step.If, "fromJSON(steps.plan.outputs.plan).docs") {
			return fmt.Errorf("docs-only feedback must select the pinned Node runtime")
		}
		if (strings.Contains(step.Run, "pnpm install") || strings.Contains(step.Run, "install-corepack.sh")) && strings.Contains(step.If, ".docs") {
			return fmt.Errorf("read-only docs preparation must not require Node dependency installation")
		}
		if strings.Contains(step.Uses, "cache/save") || strings.Contains(step.Uses, "/.github/workflows/ci.yml") || strings.Contains(step.Run, "bash scripts/ci/fast-feedback.sh") {
			return fmt.Errorf("feedback cannot write shared caches, run the full graph, or depend on a helper in an old head")
		}
		if strings.Contains(step.Uses, "checkout@") && step.With["persist-credentials"] != "false" {
			return fmt.Errorf("source and tooling must not persist credentials")
		}
	}
	if shellcheckInstallers != 1 {
		return fmt.Errorf("feedback requires exactly one pinned ShellCheck installer")
	}
	if releasePythonInstallers != 1 {
		return fmt.Errorf("feedback requires exactly one selected release-contract dependency setup")
	}
	return nil
}

func TestFeedbackWorkflowRejectsSkippedOrFailOpenLanes(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-feedback.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, mutation := range map[string]func(map[string]any){
		"release dependencies wrong lane": func(j map[string]any) {
			mutateReleasePython(j, "if", "matrix.lane == 'frontend'")
		},
		"release dependencies fail open": func(j map[string]any) {
			mutateReleasePython(j, "continue-on-error", true)
		},
		"release dependencies omit budget and PATH": func(j map[string]any) {
			mutateReleasePython(j, "run", "python3 -m pip install -r scripts/ci/release-admission/requirements.txt")
		},
		"release dependencies missing": func(j map[string]any) {
			steps := j["steps"].([]any)
			for i, value := range steps {
				if value.(map[string]any)["name"] == "Prepare trusted release contract dependencies" {
					j["steps"] = append(steps[:i], steps[i+1:]...)
					return
				}
			}
		},
		"missing compact compiler ABI": func(j map[string]any) { delete(j["env"].(map[string]any), "GOFLAGS") },
		"workflow-only loses shellcheck": func(j map[string]any) {
			mutateFeedbackShellcheck(j, "if", "matrix.lane == 'go' && fromJSON(steps.plan.outputs.plan).shell")
		},
		"shellcheck fail open": func(j map[string]any) { mutateFeedbackShellcheck(j, "continue-on-error", true) },
		"unpinned shellcheck": func(j map[string]any) {
			mutateFeedbackShellcheck(j, "env", map[string]any{"SHELLCHECK_VERSION": "v0.10.0"})
		},
		"bulk test fallback": func(j map[string]any) {
			mutateFeedbackReuseWith(j, "restore-keys", "go-test-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-")
		},
		"duplicate incompatible reader": func(j map[string]any) { duplicateFeedbackReuse(j, "go-feedback-v2-") },
		"duplicate compatible reader":   func(j map[string]any) { duplicateFeedbackReuse(j, "") },
		"module archive reintroduced":   func(j map[string]any) { mutateFeedbackReuseWith(j, "path", "~/go/pkg/mod") },
		"wrong runner image fallback": func(j map[string]any) {
			mutateFeedbackReuseWith(j, "restore-keys", "go-feedback-v2-${{ runner.os }}-${{ runner.arch }}-ubuntu22-")
		},
		"cache write": func(j map[string]any) {
			mutateFeedbackReuse(j, "uses", "actions/cache/save@55cc8345863c7cc4c66a329aec7e433d2d1c52a9")
		},
		"frontend build restore":      func(j map[string]any) { mutateFeedbackReuse(j, "if", "matrix.lane == 'frontend'") },
		"incompatible cache fallback": func(j map[string]any) { mutateFeedbackReuseWith(j, "restore-keys", "go-feedback-v2-") },
		"missing dependency key": func(j map[string]any) {
			mutateFeedbackReuseWith(j, "key", "go-feedback-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}")
		},
		"cache source override": func(j map[string]any) { mutateFeedbackReuseWith(j, "path", "source") },
		"skip compile on cache hit": func(j map[string]any) {
			steps := j["steps"].([]any)
			steps[len(steps)-1].(map[string]any)["if"] = "steps.go-cache.outputs.cache-hit != 'true'"
		},
		"missing explicit fail-fast": func(j map[string]any) { delete(j["strategy"].(map[string]any), "fail-fast") },
		"cancel sibling lane":        func(j map[string]any) { j["strategy"].(map[string]any)["fail-fast"] = true },
		"exclude frontend": func(j map[string]any) {
			j["strategy"].(map[string]any)["matrix"].(map[string]any)["exclude"] = []any{map[string]any{"lane": "frontend"}}
		},
		"include unrecognized lane": func(j map[string]any) {
			j["strategy"].(map[string]any)["matrix"].(map[string]any)["include"] = []any{map[string]any{"lane": "unknown"}}
		},
		"remove go lane": func(j map[string]any) {
			j["strategy"].(map[string]any)["matrix"].(map[string]any)["lane"] = []any{"frontend"}
		},
		"remove tooling lane": func(j map[string]any) {
			j["strategy"].(map[string]any)["matrix"].(map[string]any)["lane"] = []any{"go", "frontend"}
		},
		"drop scope partition": func(j map[string]any) {
			steps := j["steps"].([]any)
			steps[len(steps)-1].(map[string]any)["run"] = `bash "$FAST_FEEDBACK_TOOLS_DIR/fast-feedback.sh" --lane=${{ matrix.lane }}`
		},
		"serialize runners": func(j map[string]any) { j["strategy"].(map[string]any)["max-parallel"] = 1 },
		"job fail open":     func(j map[string]any) { j["continue-on-error"] = true },
		"skip final check": func(j map[string]any) {
			steps := j["steps"].([]any)
			steps[len(steps)-1].(map[string]any)["if"] = "false"
		},
		"step fail open": func(j map[string]any) {
			steps := j["steps"].([]any)
			steps[len(steps)-1].(map[string]any)["continue-on-error"] = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			var workflow map[string]any
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			mutation(workflow["jobs"].(map[string]any)["feedback"].(map[string]any))
			changed, err := yaml.Marshal(workflow)
			if err != nil {
				t.Fatal(err)
			}
			if requireFeedbackWorkflow(changed) == nil {
				t.Fatal("unsafe feedback configuration escaped actual workflow contract")
			}
		})
	}
}

func mutateReleasePython(j map[string]any, key string, value any) {
	for _, valueStep := range j["steps"].([]any) {
		step := valueStep.(map[string]any)
		if step["name"] == "Prepare trusted release contract dependencies" {
			step[key] = value
			return
		}
	}
}

func mutateFeedbackShellcheck(j map[string]any, key string, value any) {
	for _, valueStep := range j["steps"].([]any) {
		step := valueStep.(map[string]any)
		if step["name"] == "Install verified pinned ShellCheck" {
			step[key] = value
			return
		}
	}
}

func duplicateFeedbackReuse(j map[string]any, fallback string) {
	steps := j["steps"].([]any)
	for i, value := range steps {
		step := value.(map[string]any)
		if step["name"] != "Restore bounded feedback Go cache" {
			continue
		}
		duplicate := make(map[string]any)
		for key, value := range step {
			duplicate[key] = value
		}
		with := make(map[string]any)
		for key, value := range step["with"].(map[string]any) {
			with[key] = value
		}
		if fallback != "" {
			with["restore-keys"] = fallback
		}
		duplicate["with"] = with
		// Each reader has its own real reserve guard. The refusal must prove
		// duplicate identity/configuration, not just a missing budget step.
		changed := append([]any{}, steps[:i]...)
		changed = append(changed, duplicate, steps[i-1])
		j["steps"] = append(changed, steps[i:]...)
		return
	}
}

func mutateFeedbackReuse(j map[string]any, key string, value any) {
	for _, valueStep := range j["steps"].([]any) {
		step := valueStep.(map[string]any)
		if step["name"] == "Restore bounded feedback Go cache" {
			step[key] = value
			return
		}
	}
}
func mutateFeedbackReuseWith(j map[string]any, key string, value any) {
	for _, valueStep := range j["steps"].([]any) {
		step := valueStep.(map[string]any)
		if step["name"] == "Restore bounded feedback Go cache" {
			step["with"].(map[string]any)[key] = value
			return
		}
	}
}

func TestFeedbackConcurrencyMetadataCannotCancelOrEvictCommitVerdict(t *testing.T) {
	// GitHub allows one running and one pending run per group. A new run in
	// that group replaces the pending run even with cancel-in-progress=false.
	// Model both states: the validated workflow expression above must give
	// skipped edits a unique group before the job guard can skip their jobs.
	type event struct {
		action, head, base string
		baseChanged        bool
		runID              int
	}
	group := func(e event) string {
		suffix := "admitted"
		if e.action == "edited" && !e.baseChanged {
			suffix = fmt.Sprint(e.runID)
		}
		return fmt.Sprintf("ci-feedback-887-%s", suffix)
	}
	checkName := func(e event) string {
		if e.action == "edited" && !e.baseChanged {
			return "metadata (ignored)"
		}
		return "feedback"
	}
	commit := event{action: "synchronize", head: "head-a", base: "main-a", runID: 1}
	// GitHub treats a missing property and an explicit null identically. Decode
	// realistic edited payloads instead of assuming changes.base always exists.
	metadata := func(payload string, runID int) event {
		var decoded struct {
			Changes struct {
				Base struct {
					Ref struct {
						From *string `json:"from"`
					} `json:"ref"`
				} `json:"base"`
			} `json:"changes"`
		}
		if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
			t.Fatal(err)
		}
		return event{action: "edited", head: "head-a", base: "main-a", baseChanged: decoded.Changes.Base.Ref.From != nil && *decoded.Changes.Base.Ref.From != "", runID: runID}
	}
	cases := []struct {
		name       string
		incoming   event
		supersedes bool
	}{
		{"body edit", event{action: "edited", head: "head-a", base: "main-a", runID: 2}, false},
		{"title edit", event{action: "edited", head: "head-a", base: "main-a", runID: 3}, false},
		{"missing changes", metadata(`{}`, 8), false},
		{"null changes", metadata(`{"changes":null}`, 9), false},
		{"null base ref origin", metadata(`{"changes":{"base":{"ref":{"from":null}}}}`, 10), false},
		{"later push", event{action: "synchronize", head: "head-b", base: "main-a", runID: 4}, true},
		{"real base retarget", event{action: "edited", head: "head-a", base: "main-b", baseChanged: true, runID: 5}, true},
		{"duplicate useful push", event{action: "synchronize", head: "head-a", base: "main-a", runID: 6}, true},
		{"useful base edit duplicate", event{action: "edited", head: "head-a", base: "main-a", baseChanged: true, runID: 7}, true},
		{"opened duplicate", event{action: "opened", head: "head-a", base: "main-a", runID: 11}, true},
		{"reopened duplicate", event{action: "reopened", head: "head-a", base: "main-a", runID: 12}, true},
		{"decoded real base edit duplicate", metadata(`{"changes":{"base":{"ref":{"from":"previous-main"}}}}`, 13), true},
		{"empty previous ref", metadata(`{"changes":{"base":{"ref":{"from":""}}}}`, 14), false},
		{"numeric previous ref 0", metadata(`{"changes":{"base":{"ref":{"from":"0"}}}}`, 15), true},
		{"numeric previous ref 0.0", metadata(`{"changes":{"base":{"ref":{"from":"0.0"}}}}`, 16), true},
		{"numeric previous ref 0e1", metadata(`{"changes":{"base":{"ref":{"from":"0e1"}}}}`, 17), true},
	}
	for _, state := range []string{"running", "pending"} {
		for _, tc := range cases {
			t.Run(state+"/"+tc.name, func(t *testing.T) {
				if supersedes := group(commit) == group(tc.incoming); supersedes != tc.supersedes {
					t.Fatalf("incoming event supersedes %s commit verdict = %v, want %v", state, supersedes, tc.supersedes)
				}
				if tc.incoming.action == "edited" && !tc.incoming.baseChanged && checkName(commit) == checkName(tc.incoming) {
					t.Fatal("an ignored metadata skip must not replace the visible feedback check")
				}
			})
		}
	}
	if group(cases[0].incoming) == group(cases[1].incoming) {
		t.Fatal("ignored edits require per-run groups, not a shared metadata queue")
	}
}

func TestFeedbackSeedWorkflowTrustAndBounds(t *testing.T) {
	for _, name := range []string{"ci-feedback-cache.yml", "ci-feedback-backend-cache.yml"} {
		t.Run(name, func(t *testing.T) {
			compact := name == "ci-feedback-backend-cache.yml"
			requireFeedbackSeed := func(w map[string]any) error { return requireFeedbackSeedProfile(w, compact) }

			raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", name))
			if err != nil {
				t.Fatal(err)
			}
			var workflow map[string]any
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			if err := requireFeedbackSeed(workflow); err != nil {
				t.Fatal(err)
			}
			for name, mutation := range map[string]func(map[string]any){
				"wrong compiler ABI": func(w map[string]any) {
					w["jobs"].(map[string]any)["seed"].(map[string]any)["env"].(map[string]any)["GOFLAGS"] = "-gcflags=all=-dwarf=true"
				},
				"drop required seed root": func(w map[string]any) {
					steps := w["jobs"].(map[string]any)["seed"].(map[string]any)["steps"].([]any)
					first := "./internal/cli "
					if compact {
						first = "./internal/app "
					}
					steps[4].(map[string]any)["run"] = strings.Replace(steps[4].(map[string]any)["run"].(string), first, "", 1)
				},
				"masked seed job": func(w map[string]any) {
					w["jobs"].(map[string]any)["seed"].(map[string]any)["continue-on-error"] = true
				},
				"job write permission": func(w map[string]any) {
					w["jobs"].(map[string]any)["seed"].(map[string]any)["permissions"] = map[string]any{"contents": "write"}
				},
				"untrusted container": func(w map[string]any) {
					w["jobs"].(map[string]any)["seed"].(map[string]any)["container"] = "untrusted/image:latest"
				},
				"PR trigger":    func(w map[string]any) { w["on"].(map[string]any)["pull_request"] = nil },
				"write token":   func(w map[string]any) { w["permissions"] = map[string]any{"contents": "write"} },
				"untrusted ref": func(w map[string]any) { w["jobs"].(map[string]any)["seed"].(map[string]any)["if"] = "true" },
				"unbounded publisher": func(w map[string]any) {
					steps := w["jobs"].(map[string]any)["seed"].(map[string]any)["steps"].([]any)
					steps[4].(map[string]any)["run"] = "go test ./..."
				},
				"fail open bounds": func(w map[string]any) {
					steps := w["jobs"].(map[string]any)["seed"].(map[string]any)["steps"].([]any)
					steps[4].(map[string]any)["continue-on-error"] = true
				},
				"bootstrap touches seed cache": func(w map[string]any) {
					steps := w["jobs"].(map[string]any)["seed"].(map[string]any)["steps"].([]any)
					steps[1].(map[string]any)["env"] = map[string]any{"GOCACHE": "${{ github.workspace }}/.ci-cache/go-feedback"}
				},
				"bootstrap inherits seed cache": func(w map[string]any) {
					steps := w["jobs"].(map[string]any)["seed"].(map[string]any)["steps"].([]any)
					delete(steps[1].(map[string]any), "env")
				},
				"compiler reuses bootstrap cache": func(w map[string]any) {
					steps := w["jobs"].(map[string]any)["seed"].(map[string]any)["steps"].([]any)
					steps[4].(map[string]any)["env"] = map[string]any{"GOCACHE": "${{ runner.temp }}/feedback-bootstrap"}
				},
				"reuse oversized archive": func(w map[string]any) {
					steps := w["jobs"].(map[string]any)["seed"].(map[string]any)["steps"].([]any)
					delete(steps[3].(map[string]any)["with"].(map[string]any), "lookup-only")
				},
			} {
				t.Run(name, func(t *testing.T) {
					var changed map[string]any
					if err := yaml.Unmarshal(raw, &changed); err != nil {
						t.Fatal(err)
					}
					mutation(changed)
					if requireFeedbackSeed(changed) == nil {
						t.Fatal("accepted unsafe seed publisher")
					}
				})
			}

		})
	}
}

func requireFeedbackSeed(workflow map[string]any) error {
	return requireFeedbackSeedProfile(workflow, false)
}

func requireFeedbackSeedProfile(workflow map[string]any, compact bool) error {
	cachePath, cachePrefix := ".ci-cache/go-feedback", "go-feedback-v1-"
	expectedEnv := map[string]any{"GOCACHE": "${{ github.workspace }}/" + cachePath}
	packages := "go list -deps -test -export ./internal/cli ./scripts/ci"
	if compact {
		cachePath, cachePrefix = ".ci-cache/go-feedback-compact", "go-feedback-v2-"
		expectedEnv = map[string]any{"GOCACHE": "${{ github.workspace }}/" + cachePath, "GOFLAGS": "-gcflags=all=-dwarf=false"}
		packages = "go list -p 2 -deps -test -export ./internal/app ./internal/cli ./scripts/ci"
	}
	triggers := workflow["on"].(map[string]any)
	if len(triggers) != 2 {
		return fmt.Errorf("seed must run only daily or manually")
	}
	if _, ok := triggers["workflow_dispatch"]; !ok {
		return fmt.Errorf("missing manual seed")
	}
	if _, ok := triggers["schedule"]; !ok {
		return fmt.Errorf("missing daily seed")
	}
	if !reflect.DeepEqual(workflow["permissions"], map[string]any{"contents": "read"}) {
		return fmt.Errorf("seed must be read-only")
	}
	jobs := workflow["jobs"].(map[string]any)
	job := jobs["seed"].(map[string]any)
	if job["runs-on"] != "ubuntu-latest" || job["timeout-minutes"] != 15 || job["continue-on-error"] != nil || job["permissions"] != nil || job["container"] != nil || job["services"] != nil || job["strategy"] != nil || !reflect.DeepEqual(job["defaults"], map[string]any{"run": map[string]any{"working-directory": "source"}}) {
		return fmt.Errorf("seed must retain bounded native execution without masked or privileged overrides")
	}
	if len(jobs) != 1 || job["if"] != "(github.event_name == 'workflow_dispatch' || github.event_name == 'schedule') && github.ref == 'refs/heads/main'" {
		return fmt.Errorf("only trusted main may populate the seed")
	}
	if !reflect.DeepEqual(job["env"], expectedEnv) {
		return fmt.Errorf("seed must use the consumer directory")
	}
	steps := job["steps"].([]any)
	if len(steps) != 6 {
		return fmt.Errorf("unexpected seed preparation or publication")
	}
	checkout := steps[0].(map[string]any)["with"].(map[string]any)
	if checkout["ref"] != "${{ github.sha }}" || checkout["path"] != "source" || checkout["persist-credentials"] != false {
		return fmt.Errorf("seed must use the immutable main SHA without credentials")
	}
	if !reflect.DeepEqual(steps[1].(map[string]any)["env"], map[string]any{"GOCACHE": "${{ runner.temp }}/feedback-bootstrap"}) {
		return fmt.Errorf("setup diagnostics must use a separate temporary bootstrap cache")
	}
	setup := steps[1].(map[string]any)["with"].(map[string]any)
	if setup["cache"] != false || setup["go-version-file"] != "source/go.mod" {
		return fmt.Errorf("seed must select pinned Go without implicit caches")
	}
	lookup := steps[3].(map[string]any)
	if lookup["uses"] != "actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9" || !reflect.DeepEqual(lookup["with"], map[string]any{
		"path":        cachePath,
		"key":         cachePrefix + "${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-${{ hashFiles('source/go.mod', 'source/go.sum', 'source/scripts/ci/go-tool-modules.txt') }}-${{ github.sha }}",
		"lookup-only": true,
	}) {
		return fmt.Errorf("seed lookup must never restore historical entries or lose compatibility/source identity")
	}
	compile := steps[4].(map[string]any)
	want := "test ! -e \"$GOCACHE\"\nmkdir -p \"$GOCACHE\"\n" + packages + " >/dev/null\n./scripts/ci/run-go-tool.sh actionlint -version\ngo run ./scripts/ci/feedback-reuse-size \"$GOCACHE\"\n"
	if compile["run"] != want || compile["if"] != "steps.feedback-reuse.outputs.cache-hit != 'true'" || compile["continue-on-error"] != nil || compile["env"] != nil {
		return fmt.Errorf("fresh seed must compile without executing tests and pass size bounds")
	}
	save := steps[5].(map[string]any)
	if save["uses"] != "actions/cache/save@55cc8345863c7cc4c66a329aec7e433d2d1c52a9" || save["if"] != "success() && steps.feedback-reuse.outputs.cache-hit != 'true' && (github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'" || !reflect.DeepEqual(save["with"], map[string]any{"path": cachePath, "key": "${{ steps.feedback-reuse.outputs.cache-primary-key }}"}) {
		return fmt.Errorf("only successful bounded main seed may publish")
	}
	return nil
}

// setup-go's actual diagnostic invocation initializes GOCACHE. Exercise the
// workflow's real environment scopes and unchanged fresh-seed guard with Go.
func TestFeedbackSeedSetupDiagnosticsKeepSeedFresh(t *testing.T) {
	for _, name := range []string{"ci-feedback-cache.yml", "ci-feedback-backend-cache.yml"} {
		t.Run(name, func(t *testing.T) {

			raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", name))
			if err != nil {
				t.Fatal(err)
			}
			var workflow map[string]any
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			job := workflow["jobs"].(map[string]any)["seed"].(map[string]any)
			steps := job["steps"].([]any)
			setup := steps[1].(map[string]any)
			compile := steps[4].(map[string]any)["run"].(string)
			prefix, _, found := strings.Cut(compile, "go list ")
			if !found {
				t.Fatal("actual compile guard missing")
			}
			for _, existing := range []bool{false, true} {
				t.Run(fmt.Sprintf("existing-seed-%v", existing), func(t *testing.T) {
					dir := t.TempDir()
					runnerTemp := filepath.Join(dir, "runner-temp")
					workspace := filepath.Join(dir, "workspace")
					for _, p := range []string{runnerTemp, workspace} {
						if err := os.MkdirAll(p, 0700); err != nil {
							t.Fatal(err)
						}
					}
					resolve := func(value string) string {
						return strings.NewReplacer("${{ runner.temp }}", runnerTemp, "${{ github.workspace }}", workspace).Replace(value)
					}
					seed := resolve(job["env"].(map[string]any)["GOCACHE"].(string))
					bootstrap := seed
					if env, ok := setup["env"].(map[string]any); ok {
						bootstrap = resolve(env["GOCACHE"].(string))
					}
					sentinel := filepath.Join(seed, "existing-owned-entry")
					if existing {
						if err := os.MkdirAll(seed, 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					diagnostics := exec.CommandContext(ctx, "go", "env")
					diagnostics.Dir = workspace
					diagnostics.Env = append(os.Environ(), "GOCACHE="+bootstrap, "GOENV=off", "GOTOOLCHAIN=local")
					if flags, ok := job["env"].(map[string]any)["GOFLAGS"].(string); ok {
						diagnostics.Env = append(diagnostics.Env, "GOFLAGS="+flags)
					}
					if err := diagnostics.Run(); err != nil {
						t.Fatal(err)
					}
					if _, err := os.Stat(bootstrap); err != nil {
						t.Fatalf("real setup diagnostic did not initialize bootstrap cache: %v", err)
					}
					guard := exec.CommandContext(ctx, "bash", "-e", "-c", prefix)
					guard.Env = append(os.Environ(), "GOCACHE="+seed)
					output, err := guard.CombinedOutput()
					if (err != nil) != existing {
						t.Fatalf("fresh guard: existing=%v err=%v output=%s", existing, err, output)
					}
					if existing {
						data, err := os.ReadFile(sentinel)
						if err != nil || string(data) != "preserve" {
							t.Fatalf("existing seed modified: %q %v", data, err)
						}
					}
				})
			}

		})
	}
}
