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
