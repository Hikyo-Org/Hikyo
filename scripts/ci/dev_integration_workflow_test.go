package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDevIntegrationKeepsOneActiveAndNewestPending(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-dev.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On map[string]struct {
			Branches []string `yaml:"branches"`
		} `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel bool   `yaml:"cancel-in-progress"`
			Queue  string `yaml:"queue"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			Uses        string            `yaml:"uses"`
			If          string            `yaml:"if"`
			With        map[string]string `yaml:"with"`
			Permissions map[string]string `yaml:"permissions"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.On) != 1 || !reflect.DeepEqual(workflow.On["push"].Branches, []string{"dev"}) {
		t.Fatal("only a merged dev push may request development integration")
	}
	if workflow.Concurrency.Group != "ci-dev-${{ github.repository_id }}" || workflow.Concurrency.Cancel || workflow.Concurrency.Queue != "single" {
		t.Fatal("dev must finish its active immutable proof and retain only the newest pending run")
	}
	if !reflect.DeepEqual(workflow.Permissions, map[string]string{"actions": "read", "contents": "read"}) {
		t.Fatal("dev execution must not acquire privileged publication permissions")
	}
	validation := workflow.Jobs["validation"]
	if len(workflow.Jobs) != 1 || validation.Uses != "./.github/workflows/ci.yml" || validation.If != "github.ref == 'refs/heads/dev' && !github.event.deleted" ||
		!reflect.DeepEqual(validation.With, map[string]string{"revision": "${{ github.sha }}", "development": "true"}) ||
		!reflect.DeepEqual(validation.Permissions, map[string]string{"actions": "read", "contents": "read", "pull-requests": "read"}) {
		t.Fatal("dev must call the complete read-only graph with its exact push SHA")
	}
}

func TestDevSourceAuthorityRefusesOtherEventsAndRevisions(t *testing.T) {
	for _, filename := range []string{"ci.yml", "floor-bench.yml"} {
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", filename))
		if err != nil {
			t.Fatal(err)
		}
		var workflow mainValidationWorkflow
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatal(err)
		}
		job := "changes"
		if filename == "floor-bench.yml" {
			job = "measure"
		}
		guard := workflow.Jobs[job].Steps[0].Run
		for _, mutation := range []string{"valid", "wrong-source", "wrong-ref", "unadmitted-dev", "wrong-event", "wrong-default", "deleted"} {
			t.Run(filename+"/"+mutation, func(t *testing.T) {
				sha := strings.Repeat("a", 40)
				env := map[string]string{
					"GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID": "10",
					"GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/heads/dev", "GITHUB_SHA": sha,
					"SOURCE_REVISION": sha, "DEVELOPMENT": "true", "DEFAULT_BRANCH": "main", "TAG_DELETED": "false",
				}
				switch mutation {
				case "wrong-source":
					env["SOURCE_REVISION"] = strings.Repeat("b", 40)
				case "wrong-ref":
					env["GITHUB_REF"] = "refs/heads/feature"
				case "unadmitted-dev":
					env["DEVELOPMENT"] = "false"
				case "wrong-event":
					env["GITHUB_EVENT_NAME"] = "repository_dispatch"
				case "wrong-default":
					env["DEFAULT_BRANCH"] = "dev"
				case "deleted":
					env["TAG_DELETED"] = "true"
				}
				cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", guard)
				cmd.Env = os.Environ()
				for key, value := range env {
					cmd.Env = append(cmd.Env, key+"="+value)
				}
				output, err := cmd.CombinedOutput()
				want := mutation == "valid"
				if (err == nil) != want {
					t.Fatalf("development authority allowed=%v, want=%v: %v %s", err == nil, want, err, output)
				}
			})
		}
	}
}

// Checkout by SHA leaves no refs/heads/dev, so the guard has to run against real
// Git: a static or stubbed check passed while every dev run died on this line.
func TestDevPushGuardRunsAgainstDetachedCheckout(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	guard := ""
	const start, end = `if [ "$EVENT_NAME" = push ]; then`, `plan=$(./scripts/ci/classify-changed-paths.sh --all)`
	for _, step := range workflow.Jobs["changes"].Steps {
		if i := strings.Index(step.Run, start); i >= 0 {
			guard = step.Run[i:]
			guard = guard[:strings.Index(guard, end)] + "fi\n"
		}
	}
	if guard == "" {
		t.Fatal("dev push guard moved")
	}
	dir := t.TempDir()
	git := func(cwd string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = cwd
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	origin, clone := filepath.Join(dir, "origin"), filepath.Join(dir, "clone")
	git(dir, "init", "-q", "-b", "dev", origin)
	git(origin, "commit", "-q", "--allow-empty", "-m", "first")
	first := git(origin, "rev-parse", "HEAD")
	git(origin, "commit", "-q", "--allow-empty", "-m", "second")
	tip := git(origin, "rev-parse", "HEAD")
	git(origin, "switch", "-q", "--orphan", "other")
	git(origin, "commit", "-q", "--allow-empty", "-m", "unrelated")
	unrelated := git(origin, "rev-parse", "HEAD")
	git(origin, "switch", "-q", "dev")
	git(dir, "clone", "-q", origin, clone)
	git(clone, "checkout", "-q", "--detach", tip)
	git(clone, "branch", "-q", "-D", "dev")
	for name, tc := range map[string]struct {
		revision string
		want     bool
	}{"current tip": {tip, true}, "rerun after dev moved": {first, true}, "not on dev": {unrelated, false}} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", guard)
			cmd.Dir = clone
			cmd.Env = append(os.Environ(), "EVENT_NAME=push", "DEVELOPMENT=true", "GITHUB_REF=refs/heads/dev", "SOURCE_REVISION="+tc.revision)
			if out, err := cmd.CombinedOutput(); (err == nil) != tc.want {
				t.Fatalf("guard for %s: %v\n%s", tc.revision, err, out)
			}
		})
	}
}

// Skipping feedback is safe only on canonical heads a full graph has covered. A
// fork naming its branch release/ or main must not reach this path.
func TestFeedbackSkipsOnlyCanonicalPromotions(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-feedback.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	const want = "${{ github.event.pull_request.head.repo.full_name == github.repository && ((github.event.pull_request.base.ref == 'main' && startsWith(github.event.pull_request.head.ref, 'release/')) || (github.event.pull_request.base.ref == 'dev' && github.event.pull_request.head.ref == 'main')) }}"
	for _, step := range workflow.Jobs["feedback"].Steps {
		if step.Name == "Select fast feedback domains" {
			if step.Env["PROMOTION"] != want || !strings.Contains(step.Run, `if [ "$PROMOTION" = true ]; then base=$FAST_FEEDBACK_HEAD; fi`) {
				t.Fatalf("feedback skip condition changed: %q", step.Env["PROMOTION"])
			}
			return
		}
	}
	t.Fatal("feedback plan step moved")
}
