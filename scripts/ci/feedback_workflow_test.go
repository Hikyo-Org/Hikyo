package ci_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestFeedbackWorkflowPreservesEveryCommitAndImmutableBootstrap(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-feedback.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel bool   `yaml:"cancel-in-progress"`
		} `yaml:"concurrency"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Name    string            `yaml:"name"`
			If      string            `yaml:"if"`
			Timeout int               `yaml:"timeout-minutes"`
			Env     map[string]string `yaml:"env"`
			Steps   []struct {
				Name      string            `yaml:"name"`
				Run       string            `yaml:"run"`
				Uses      string            `yaml:"uses"`
				If        string            `yaml:"if"`
				With      map[string]string `yaml:"with"`
				Directory string            `yaml:"working-directory"`
				Timeout   int               `yaml:"timeout-minutes"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Concurrency.Group != "ci-feedback-${{ github.event.pull_request.number }}-${{ github.event.pull_request.head.sha }}-${{ github.event.pull_request.base.sha }}-${{ github.event.action == 'edited' && github.event.changes.base.ref.from == '' && github.run_id || 'admitted' }}" || !got.Concurrency.Cancel {
		t.Fatal("distinct commits and skipped metadata edits must not cancel or evict useful feedback; only duplicate admitted inputs may supersede")
	}
	if !reflect.DeepEqual(got.Permissions, map[string]string{"contents": "read"}) {
		t.Fatal("feedback execution must remain read-only")
	}
	job := got.Jobs["feedback"]
	if job.Name != "${{ github.event.action == 'edited' && github.event.changes.base.ref.from == '' && 'metadata (ignored)' || 'feedback' }}" {
		t.Fatal("metadata skips must have a separate check name so they cannot hide the actual feedback verdict")
	}
	if job.If != "github.event.action != 'edited' || github.event.changes.base.ref.from != ''" {
		t.Fatal("push/open/reopen and genuine base edits must run; metadata-only edits must skip")
	}
	if len(got.Jobs) != 1 || job.Timeout != 4 {
		t.Fatal("one bounded feedback runner includes setup in its four-minute budget")
	}
	if job.Env["FAST_FEEDBACK_TOOLS_DIR"] != "${{ github.workspace }}/feedback-tools/scripts/ci" || job.Env["FAST_FEEDBACK_HEAD"] != "${{ github.event.pull_request.head.sha }}" {
		t.Fatal("immutable informational helper and exact head must be separate")
	}
	if job.Steps[1].With["ref"] != "${{ github.sha }}" || job.Steps[1].With["path"] != "feedback-tools" || job.Steps[3].With["ref"] != "${{ github.event.pull_request.head.sha }}" || job.Steps[3].With["path"] != "source" {
		t.Fatal("old PR heads must bootstrap feedback from the immutable merge revision without changing tested source")
	}
	if job.Steps[0].Directory != "${{ github.workspace }}" || job.Steps[0].Name != "Start the cumulative feedback budget" || !strings.Contains(job.Steps[0].Run, "+ 220") || job.Env["FAST_FEEDBACK_BASE"] != "" {
		t.Fatal("setup must consume the cumulative deadline; stale event base metadata is not the comparison owner")
	}
	if job.Steps[1].With["fetch-depth"] != "2" {
		t.Fatal("immutable tooling must retain the synthetic merge's actual parents")
	}
	var pinnedBase, buildReader bool
	for _, step := range job.Steps {
		if step.Name == "Select fast feedback domains" {
			pinnedBase = strings.Contains(step.Run, "rev-parse HEAD^1") && strings.Contains(step.Run, `test "$(git -C ../feedback-tools rev-parse HEAD^2)" = "$FAST_FEEDBACK_HEAD"`) && strings.Contains(step.Run, `export FAST_FEEDBACK_BASE="$base"`)
		}
		if step.Name == "Restore trusted-main Go build cache" {
			buildReader = step.With["path"] == "~/.cache/go-build" && strings.HasPrefix(step.With["key"], "go-test-v2-") && strings.Contains(step.With["key"], "hashFiles('feedback-tools/go.mod'") && strings.Contains(step.Uses, "cache/restore@")
		}
	}
	if !pinnedBase || !buildReader {
		t.Fatal("feedback needs a verified integration base and read-only existing main build bucket")
	}
	for index, step := range job.Steps {
		if step.Uses != "" {
			if step.Timeout != 1 {
				t.Fatal("external Actions require a one-minute bound")
			}
			if index > 1 && !strings.Contains(job.Steps[index-1].Run, "feedback-budget.sh\" --reserve 60") {
				t.Fatal("external setup must reserve cumulative headroom first")
			}
		}
		if strings.Contains(step.Uses, "setup-node@") && !strings.Contains(step.If, "fromJSON(steps.plan.outputs.plan).docs") {
			t.Fatal("docs-only feedback must select the pinned Node runtime")
		}
		if (strings.Contains(step.Run, "pnpm install") || strings.Contains(step.Run, "install-corepack.sh")) && strings.Contains(step.If, ".docs") {
			t.Fatal("read-only docs preparation must not require Node dependency installation")
		}
		if strings.Contains(step.Uses, "cache/save") || strings.Contains(step.Uses, "/.github/workflows/ci.yml") || strings.Contains(step.Run, "bash scripts/ci/fast-feedback.sh") {
			t.Fatal("feedback cannot write shared caches, run the full graph, or depend on a helper in an old head")
		}
		if strings.Contains(step.Uses, "checkout@") && step.With["persist-credentials"] != "false" {
			t.Fatal("source and tooling must not persist credentials")
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
		return fmt.Sprintf("ci-feedback-887-%s-%s-%s", e.head, e.base, suffix)
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
		{"later push", event{action: "synchronize", head: "head-b", base: "main-a", runID: 4}, false},
		{"real base retarget", event{action: "edited", head: "head-a", base: "main-b", baseChanged: true, runID: 5}, false},
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
