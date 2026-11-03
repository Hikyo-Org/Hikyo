package ci_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type promoteWorkflow struct {
	On          map[string]yaml.Node `yaml:"on"`
	Permissions map[string]string    `yaml:"permissions"`
	Jobs        map[string]struct {
		If          string            `yaml:"if"`
		Environment string            `yaml:"environment"`
		Permissions map[string]string `yaml:"permissions"`
		Concurrency map[string]any    `yaml:"concurrency"`
		Steps       []struct {
			Name string            `yaml:"name"`
			ID   string            `yaml:"id"`
			Uses string            `yaml:"uses"`
			Run  string            `yaml:"run"`
			Env  map[string]string `yaml:"env"`
			With map[string]string `yaml:"with"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

func readPromoteWorkflow(t *testing.T, name string) (promoteWorkflow, string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var workflow promoteWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow, string(raw)
}

func TestNightlyPromotionWorkflowBoundaries(t *testing.T) {
	workflow, raw := readPromoteWorkflow(t, "nightly-promote.yml")
	if !strings.Contains(raw, "\nname: nightly-promote\n") {
		t.Fatal("the workflow display name changed")
	}
	if len(workflow.On) != 3 || len(workflow.Permissions) != 0 {
		t.Fatal("promotion must have only schedule, manual and landing triggers and no default token permissions")
	}
	var schedule []struct {
		Cron string `yaml:"cron"`
	}
	scheduleNode := workflow.On["schedule"]
	if err := scheduleNode.Decode(&schedule); err != nil || len(schedule) != 1 || schedule[0].Cron != "17 22 * * *" {
		t.Fatalf("promotion must run once a day well before the 02:00 nightly: %v %v", schedule, err)
	}
	if dispatch := workflow.On["workflow_dispatch"]; dispatch.Kind != yaml.ScalarNode || dispatch.Tag != "!!null" {
		t.Fatal("the manual trigger must take no inputs")
	}
	var landing struct {
		Branches []string `yaml:"branches"`
		Types    []string `yaml:"types"`
	}
	landingNode := workflow.On["pull_request_target"]
	if err := landingNode.Decode(&landing); err != nil || !reflect.DeepEqual(landing.Branches, []string{"main"}) || !reflect.DeepEqual(landing.Types, []string{"closed"}) {
		t.Fatalf("the landing handler must see only closed pull requests into main: %+v %v", landing, err)
	}

	promote := workflow.Jobs["promote"]
	if len(workflow.Jobs) != 2 || promote.If != "(github.event_name == 'schedule' || github.event_name == 'workflow_dispatch') && github.ref == 'refs/heads/main'" {
		t.Fatal("promotion must run only for the schedule or a manual run on main, never for a closed pull request")
	}
	if promote.Environment != "nightly-release" || promote.Concurrency["group"] != "nightly-promote" || promote.Concurrency["cancel-in-progress"] != false ||
		!reflect.DeepEqual(promote.Permissions, map[string]string{"actions": "read", "contents": "read", "pull-requests": "read"}) {
		t.Fatal("promotion must hold the protected environment, run one at a time and read with the workflow token only")
	}
	var minted, ran bool
	for _, step := range promote.Steps {
		switch {
		case strings.HasPrefix(step.Uses, "actions/checkout@"):
			// Trusted tooling at the triggering commit, never a dev ref.
			if step.With["ref"] != "${{ github.sha }}" || step.With["persist-credentials"] != "false" {
				t.Fatalf("promotion may only check out main's own commit without credentials: %v", step.With)
			}
		case strings.HasPrefix(step.Uses, "actions/setup-go@"):
			if step.With["cache"] != "false" {
				t.Fatal("promotion must not use Go caches")
			}
		case strings.HasPrefix(step.Uses, "actions/create-github-app-token@"):
			minted = step.With["client-id"] == "${{ vars.NIGHTLY_RELEASE_APP_CLIENT_ID }}" &&
				step.With["private-key"] == "${{ secrets.NIGHTLY_RELEASE_APP_PRIVATE_KEY }}" &&
				step.With["permission-contents"] == "write" && step.With["permission-pull-requests"] == "write" && len(step.With) == 6
		case step.Run != "":
			ran = step.Run == "./scripts/release/nightly-promote.sh" &&
				reflect.DeepEqual(step.Env, map[string]string{"GH_TOKEN": "${{ github.token }}", "APP_TOKEN": "${{ steps.app.outputs.token }}", "GH_REPO": "${{ github.repository }}"})
		default:
			t.Fatalf("unexpected promotion step %q", step.Name)
		}
	}
	if !minted || !ran {
		t.Fatal("promotion must mint a contents and pull-requests App token and run only the reviewed script")
	}
	if strings.Contains(raw, "permission-workflows") || strings.Contains(raw, "refs/heads/dev") {
		t.Fatal("promotion must not request workflow writes or name dev as a ref to execute")
	}

	landed := workflow.Jobs["landed"]
	const condition = "github.event_name == 'pull_request_target' && github.event.pull_request.merged == true && github.event.pull_request.head.repo.full_name == github.repository && github.event.pull_request.base.ref == 'main' && startsWith(github.event.pull_request.head.ref, 'release/nightly-') && github.event.pull_request.user.id == 320493937"
	if strings.Join(strings.Fields(landed.If), " ") != condition {
		t.Fatalf("landing must require a merged same-repository promotion into main: %q", landed.If)
	}
	if !reflect.DeepEqual(landed.Permissions, map[string]string{"contents": "write"}) || len(landed.Steps) != 1 || landed.Steps[0].Uses != "" ||
		!reflect.DeepEqual(landed.Steps[0].Env, map[string]string{"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}", "HEAD_REF": "${{ github.event.pull_request.head.ref }}"}) {
		t.Fatal("landing must be a single checkout-free step with the workflow token and the branch name in env")
	}
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Run, "${{") {
				t.Fatalf("expression expanded inside a shell script: %q", step.Run)
			}
		}
	}
	for _, required := range []string{`^release/nightly-[0-9]{8}-[0-9a-f]{7}$`, "event_type=nightly-release", `git/refs/heads/$HEAD_REF`} {
		if !strings.Contains(landed.Steps[0].Run, required) {
			t.Fatalf("landing lost %q", required)
		}
	}
}

// Nightly builds exactly the commit that triggered it, from main's tooling, and
// no longer selects a source or splits its build.
func TestNightlyBuildsOnlyTheTriggeringMainCommit(t *testing.T) {
	workflow, raw := readPromoteWorkflow(t, "nightly.yml")
	for _, removed := range []string{"source_mode", "source-mode", "refs/heads/dev", "--mode dev", "nightly-main-bridge", "nightly-build", "--require-dev-policy", "workflow_dispatch"} {
		if strings.Contains(raw, removed) {
			t.Fatalf("nightly.yml still carries %q", removed)
		}
	}
	checkouts := 0
	for name, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/checkout@") {
				continue
			}
			checkouts++
			// The container job names the commit the publish job pinned to github.sha.
			if ref := step.With["ref"]; ref != "${{ github.sha }}" && !(name == "container" && ref == "${{ needs.publish.outputs.commit }}") {
				t.Fatalf("nightly.yml/%s checks out %q instead of its triggering commit", name, ref)
			}
		}
	}
	if checkouts == 0 || !strings.Contains(raw, `test "$commit" = "$GITHUB_SHA"`) {
		t.Fatal("nightly must pin its checkout to github.sha")
	}
}
