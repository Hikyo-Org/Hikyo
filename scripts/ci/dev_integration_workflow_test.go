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
		for _, mutation := range []string{"valid", "wrong-source", "wrong-ref", "unadmitted-dev", "wrong-event", "wrong-default", "deleted", "candidate"} {
			t.Run(filename+"/"+mutation, func(t *testing.T) {
				sha := strings.Repeat("a", 40)
				env := map[string]string{
					"GITHUB_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY": "Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID": "10",
					"GITHUB_EVENT_NAME": "push", "GITHUB_REF": "refs/heads/dev", "GITHUB_SHA": sha,
					"SOURCE_REVISION": sha, "DEVELOPMENT": "true", "FULL_CANDIDATE": "false", "DEFAULT_BRANCH": "main", "TAG_DELETED": "false",
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
				case "candidate":
					env["FULL_CANDIDATE"] = "true"
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

func TestNightlySeparatesSourceFromTrustedPublisher(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/nightly.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"validation", "build-artifacts", "apple-sign", "apple-notarize", "apple-acceptance", "publish", "container"} {
		found := false
		for _, step := range workflow.Jobs[name].Steps {
			if !strings.HasPrefix(step.Uses, "actions/checkout@") {
				continue
			}
			found = true
			want := "${{ github.sha }}"
			if name == "build-artifacts" {
				want = "${{ needs.validation.outputs.commit }}"
			}
			if step.With["ref"] != want || step.With["persist-credentials"] != "false" {
				t.Fatalf("%s must bind its source/tooling lane without credentials", name)
			}
		}
		if !found {
			t.Fatalf("%s has no immutable checkout", name)
		}
	}
	var recheck, stage mainValidationStep
	for _, step := range workflow.Jobs["publish"].Steps {
		if step.Name == "Revalidate the pinned development proof before signing" {
			recheck = step
		}
		if step.Name == "Stage the complete nightly payload inventory" {
			stage = step
		}
		if strings.Contains(step.Run, `test "$COMMIT" = "$GITHUB_SHA"`) {
			t.Fatal("development source must not be confused with the publisher OIDC commit")
		}
	}
	for _, required := range []string{`--mode "$SOURCE_MODE" --commit "$COMMIT" --authority-commit "$GITHUB_SHA"`, "cmp nightly-context/source-validation.json"} {
		if !strings.Contains(recheck.Run, required) {
			t.Fatalf("nightly must revalidate its pinned proof: %s", required)
		}
	}
	if stage.Env["HIKYO_NIGHTLY_PUBLISHER_COMMIT"] != "${{ needs.validation.outputs.source_mode == 'dev' && github.sha || '' }}" || stage.Env["HIKYO_NIGHTLY_SOURCE_PROOF"] != "${{ needs.validation.outputs.source_mode == 'dev' && format('{0}/nightly-context/source-validation.json', github.workspace) || '' }}" ||
		!strings.Contains(stage.Run, `cp "$HIKYO_NIGHTLY_SOURCE_PROOF" nightly-payloads/source-validation.json`) {
		t.Fatal("the signed nightly must bind the exact development receipt and publisher commit")
	}
	for _, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Run, `tar -xzf "$RUNNER_TEMP/`) {
				t.Fatal("dev-built transport archives must use the bounded payload-only reader")
			}
		}
	}
}

func TestNightlyAppleRevalidatesSourceBeforeCredentials(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/nightly.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.Jobs["build-artifacts"].Outputs["source_mode"] != "${{ needs.validation.outputs.source_mode }}" {
		t.Fatal("Apple phases must inherit the trusted selector's source mode")
	}
	var guard string
	for _, name := range []string{"apple-sign", "apple-notarize", "apple-acceptance", "publish"} {
		job := workflow.Jobs[name]
		if job.Env["APPLE_RELEASE_SOURCE_MODE"] != "${{ needs.build-artifacts.outputs.source_mode }}" || job.Env["APPLE_RELEASE_COMMIT"] != "${{ needs.build-artifacts.outputs.commit }}" {
			t.Fatalf("%s lost the exact selected source for its receipts", name)
		}
		if name == "publish" || name == "apple-acceptance" {
			continue
		}
		authenticated := false
		for _, step := range job.Steps {
			if step.Name == "Authenticate the exact Apple source before loading credentials" {
				if guard != "" && guard != step.Run {
					t.Fatal("Apple phases use different source authentication")
				}
				guard, authenticated = step.Run, true
			}
			for _, value := range step.Env {
				if strings.Contains(value, "secrets.") && !authenticated {
					t.Fatalf("%s exposes credentials before source authentication", name)
				}
			}
		}
		if !authenticated {
			t.Fatalf("%s has no trusted source authentication", name)
		}
	}
	for _, scenario := range []string{"dev", "main", "stale receipt", "failed CI", "wrong bridge", "unknown mode"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "nightly-context"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "nightly-context/source-validation.json"), []byte("current receipt\n"), 0600); err != nil {
				t.Fatal(err)
			}
			fakeGo := `#!/bin/bash
set -euo pipefail
case "$*" in
  "run ./scripts/ci/source-proof --repository Hikyo-Org/Hikyo --mode dev --commit $FIXTURE_SOURCE --authority-commit $FIXTURE_PUBLISHER --output $RUNNER_TEMP/apple-source-current.json")
    test "$FIXTURE_CASE" != 'failed CI'
    if [ "$FIXTURE_CASE" = 'stale receipt' ]; then printf stale; else printf 'current receipt\n'; fi >"$RUNNER_TEMP/apple-source-current.json" ;;
  "run ./scripts/release/nightly source-validation --source-proof nightly-context/source-validation.json --source-commit $FIXTURE_SOURCE --publisher-commit $FIXTURE_PUBLISHER --out $RUNNER_TEMP/apple-source-validation.json")
    printf 'validated descriptor\n' >"$RUNNER_TEMP/apple-source-validation.json" ;;
  'run ./scripts/release/nightly preflight --require-main-policy') test "$FIXTURE_CASE" = main ;;
  *) exit 91 ;;
esac
`
			if err := os.WriteFile(filepath.Join(dir, "go"), []byte(fakeGo), 0700); err != nil {
				t.Fatal(err)
			}
			source, publisher, mode := strings.Repeat("a", 40), strings.Repeat("b", 40), "dev"
			if scenario == "main" {
				source, mode = publisher, "main"
			} else if scenario == "wrong bridge" {
				mode = "main"
			} else if scenario == "unknown mode" {
				mode = "feature"
			}
			environment := filepath.Join(dir, "environment")
			cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", guard)
			cmd.Dir = dir
			cmd.Env = []string{
				"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"),
				"GITHUB_REPOSITORY=Hikyo-Org/Hikyo", "GITHUB_SHA=" + publisher,
				"APPLE_RELEASE_COMMIT=" + source, "APPLE_RELEASE_SOURCE_MODE=" + mode,
				"RUNNER_TEMP=" + dir, "GITHUB_ENV=" + environment,
				"FIXTURE_CASE=" + scenario, "FIXTURE_SOURCE=" + source, "FIXTURE_PUBLISHER=" + publisher,
			}
			out, err := cmd.CombinedOutput()
			wantSuccess := scenario == "dev" || scenario == "main"
			if (err == nil) != wantSuccess {
				t.Fatalf("source authentication: %v\n%s", err, out)
			}
			body, err := os.ReadFile(environment)
			if scenario == "dev" {
				if err != nil || !strings.Contains(string(body), "APPLE_RELEASE_SOURCE_VALIDATION_SHA256=") {
					t.Fatalf("authenticated dev descriptor was not exported: %v %s", err, body)
				}
			} else if !os.IsNotExist(err) {
				t.Fatal("refused or legacy source exported dev authority")
			}
		})
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
