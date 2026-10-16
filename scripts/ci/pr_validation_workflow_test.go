package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPRValidationWorkflowAdmission(t *testing.T) {
	var workflow struct {
		Name    string `yaml:"name"`
		RunName string `yaml:"run-name"`
		On      map[string]struct {
			Types []string `yaml:"types"`
		} `yaml:"on"`
		Concurrency struct {
			Group string `yaml:"group"`
		} `yaml:"concurrency"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			If    string `yaml:"if"`
			Needs string `yaml:"needs"`
		} `yaml:"jobs"`
	}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci-candidate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.Name != "pr-candidate" {
		t.Fatal("explicit full admission must retain the candidate workflow identity")
	}
	if len(workflow.On) != 1 || !slices.Equal(workflow.On["pull_request"].Types, []string{"labeled", "synchronize", "edited", "closed"}) {
		t.Fatal("full validation needs label admission plus runner-free cancellation on head/base changes and closure")
	}
	if workflow.Concurrency.Group != "${{ (github.event.label.name == 'ci:full' || github.event.action == 'synchronize' || github.event.action == 'closed' || (github.event.action == 'edited' && github.event.changes.base.ref.from != '')) && 'candidate-ci' || 'pr-metadata' }}-${{ github.event.pull_request.number }}-${{ github.event.action == 'synchronize' && (github.event.before || github.run_id) || github.event.pull_request.head.sha }}-${{ github.event.action == 'edited' && github.event.changes.base.ref.from != '' && (github.event.changes.base.sha.from || github.run_id) || github.event.pull_request.base.sha }}" {
		t.Fatal("only explicit admission or invalidating lifecycle events may cancel an admitted full candidate")
	}
	// The trusted default-branch controller binds full runs to this identity.
	if workflow.RunName != "${{ github.event.label.name == 'ci:full' && 'candidate-ci' || 'pr-metadata' }} #${{ github.event.pull_request.number }}" {
		t.Fatal("retain the exact run identity used by trusted candidate verification")
	}
	if len(workflow.Permissions) != 2 || workflow.Permissions["actions"] != "read" || workflow.Permissions["contents"] != "read" {
		t.Fatal("PR execution must remain read-only")
	}
	if workflow.Jobs["vouch"].If != "github.event.label.name == 'ci:full' && github.event.pull_request.base.ref == github.event.repository.default_branch && github.event.pull_request.head.repo.full_name != github.repository" {
		t.Fatal("only forks should allocate an author-vouch runner")
	}
	validation := workflow.Jobs["validation"]
	if validation.Needs != "vouch" {
		t.Fatal("fork validation must still depend on author admission")
	}
	if validation.If != "${{ !cancelled() && github.event.label.name == 'ci:full' && github.event.pull_request.base.ref == github.event.repository.default_branch && (needs.vouch.result == 'success' || (needs.vouch.result == 'skipped' && github.event.pull_request.head.repo.full_name == github.repository)) }}" {
		t.Fatal("only explicit default-branch admission with successful fork vouch or same-repo identity may validate")
	}
}

func TestCandidateLifecycleTargetsPredecessorWithoutCancellingNewPair(t *testing.T) {
	type event struct{ action, label, before, head, base, priorRef, priorBase, runID string }
	group := func(e event) string {
		prefix := "pr-metadata"
		if e.label == "ci:full" || e.action == "synchronize" || e.action == "closed" || (e.action == "edited" && e.priorRef != "") {
			prefix = "candidate-ci"
		}
		head, base := e.head, e.base
		if e.action == "synchronize" {
			head = e.before
			if head == "" {
				head = e.runID
			}
		}
		if e.action == "edited" && e.priorRef != "" {
			base = e.priorBase
			if base == "" {
				base = e.runID
			}
		}
		return fmt.Sprintf("%s-7-%s-%s", prefix, head, base)
	}
	oldHead := event{action: "labeled", label: "ci:full", head: "head-a", base: "base-a", runID: "1"}
	newHead := event{action: "labeled", label: "ci:full", head: "head-b", base: "base-a", runID: "2"}
	newBase := event{action: "labeled", label: "ci:full", head: "head-b", base: "base-b", runID: "3"}
	for _, tc := range []struct {
		name     string
		incoming event
		target   *event
	}{
		{"late synchronize", event{action: "synchronize", before: "head-a", head: "head-b", base: "base-a", runID: "4"}, &oldHead},
		{"late base edit", event{action: "edited", head: "head-b", base: "base-b", priorRef: "main", priorBase: "base-a", runID: "5"}, &newHead},
		{"missing prior head", event{action: "synchronize", head: "head-b", base: "base-a", runID: "6"}, nil},
		{"missing prior base", event{action: "edited", head: "head-b", base: "base-b", priorRef: "main", runID: "7"}, nil},
		{"metadata edit", event{action: "edited", head: "head-b", base: "base-b", runID: "8"}, nil},
		{"close current pair", event{action: "closed", head: "head-b", base: "base-a", runID: "9"}, &newHead},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := group(tc.incoming)
			if tc.target != nil && got != group(*tc.target) {
				t.Fatal("failed to target the authenticated obsolete pair")
			}
			for _, candidate := range []event{oldHead, newHead, newBase} {
				if tc.target != nil && group(candidate) == group(*tc.target) {
					continue
				}
				if got == group(candidate) {
					t.Fatalf("old callback can cancel unrelated/new pair: %s", got)
				}
			}
		})
	}
	// Advancing main independently can make synchronize's current base differ
	// from the original admission. Preserve that run rather than guess an old
	// base; its existing exact-parent proof still refuses stale completion.
	movedBase := event{action: "synchronize", before: "head-a", head: "head-b", base: "base-b", runID: "10"}
	if group(movedBase) == group(oldHead) || group(movedBase) == group(newBase) {
		t.Fatal("a missing old admission base must not target another pair")
	}

	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var reusable struct {
		Concurrency struct {
			Group string `yaml:"group"`
		} `yaml:"concurrency"`
	}
	if err := yaml.Unmarshal(raw, &reusable); err != nil {
		t.Fatal(err)
	}
	if reusable.Concurrency.Group != "ci-${{ github.workflow }}-${{ github.run_id }}" {
		t.Fatal("reusable runs must not reintroduce cross-pair cancellation below the caller")
	}
}

func TestCandidateLifecycleCancelsObsoleteWorkWithoutAllocatingValidation(t *testing.T) {
	for _, tc := range []struct {
		name, action, label, previousBase string
		cancels, validates                bool
	}{
		{"explicit admission", "labeled", "ci:full", "", true, true},
		{"new head", "synchronize", "", "", true, false},
		{"closed or merged", "closed", "", "", true, false},
		{"retarget", "edited", "", "main", true, false},
		{"numeric prior branch", "edited", "", "0", true, false},
		{"title edit", "edited", "", "", false, false},
		{"unrelated label", "labeled", "bug", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The exact YAML expressions are pinned in the admission contract.
			// Compare scheduling before job guards: even a zero-job callback can
			// replace a running or pending workflow in its concurrency group.
			for _, state := range []string{"running", "pending"} {
				admitted := tc.label == "ci:full"
				invalidates := tc.action == "synchronize" || tc.action == "closed" || (tc.action == "edited" && tc.previousBase != "")
				group := "pr-metadata"
				if admitted || invalidates {
					group = "candidate-ci"
				}
				if (group == "candidate-ci") != tc.cancels || admitted != tc.validates {
					t.Fatalf("%s: group=%s validation=%v", state, group, admitted)
				}
				// Trusted candidate callbacks authenticate the stable display title.
				// Lifecycle cancellation must retain metadata identity even when it
				// intentionally shares the candidate's concurrency group.
				title := "pr-metadata"
				if admitted {
					title = "candidate-ci"
				}
				if invalidates && title != "pr-metadata" {
					t.Fatal("lifecycle callback masquerades as candidate proof")
				}
			}
		})
	}
}
