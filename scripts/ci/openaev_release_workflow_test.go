package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAEVPostReleaseCustody(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/openaev-release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On map[string]struct {
			Types     []string `yaml:"types"`
			Workflows []string `yaml:"workflows"`
		} `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		CacheMode   string            `yaml:"cache-mode"`
		Jobs        map[string]struct {
			If             string            `yaml:"if"`
			RunsOn         string            `yaml:"runs-on"`
			TimeoutMinutes int               `yaml:"timeout-minutes"`
			CacheMode      string            `yaml:"cache-mode"`
			Permissions    map[string]string `yaml:"permissions"`
			Steps          []struct {
				Uses string            `yaml:"uses"`
				Name string            `yaml:"name"`
				Env  map[string]string `yaml:"env"`
				If   string            `yaml:"if"`
				Run  string            `yaml:"run"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if strings.Join(workflow.On["release"].Types, ",") != "published" {
		t.Fatal("stable and prerelease publication must trigger validation")
	}
	if strings.Join(workflow.On["workflow_run"].Workflows, ",") != "nightly-release" || strings.Join(workflow.On["workflow_run"].Types, ",") != "completed" {
		t.Fatal("nightly must wait for completed signed-container producer")
	}
	for _, event := range []string{"push", "pull_request", "merge_group"} {
		if _, ok := workflow.On[event]; ok {
			t.Fatalf("validation must be outside release and merge gates: %s", event)
		}
	}
	if workflow.CacheMode != "none" {
		t.Fatal("hosted validation must deny GitHub cache capabilities")
	}
	if workflow.Permissions["contents"] != "read" {
		t.Fatal("default token must be read only")
	}
	for name, job := range workflow.Jobs {
		if job.CacheMode != "" && job.CacheMode != "none" {
			t.Fatalf("%s overrides cache deny", name)
		}
		wantTimeout := 5
		if name == "validate" {
			wantTimeout = 45
		}
		if job.TimeoutMinutes != wantTimeout {
			t.Fatalf("%s timeout differs", name)
		}
		if job.RunsOn != "ubuntu-24.04-arm" {
			t.Fatalf("%s is not GitHub-hosted ARM64", name)
		}
		expectedPermissions := map[string]string{"contents": "read"}
		if name == "identity" {
			expectedPermissions["checks"] = "read"
		}
		if name == "publish-pass" {
			expectedPermissions["checks"] = "write"
		}
		if len(job.Permissions) != len(expectedPermissions) {
			t.Fatalf("%s token permissions differ", name)
		}
		for key, value := range expectedPermissions {
			if job.Permissions[key] != value {
				t.Fatalf("%s token permission %s differs", name, key)
			}
		}
		for _, step := range job.Steps {
			if strings.HasPrefix(step.Uses, "actions/checkout@") {
				if step.With["persist-credentials"] != "false" {
					t.Fatalf("%s checkout persists credentials", name)
				}
				if strings.Contains(step.With["ref"], "source_sha") || strings.Contains(step.With["ref"], "tag") {
					t.Fatalf("%s executes candidate source", name)
				}
			}
		}
	}
	source := ""
	for _, step := range workflow.Jobs["identity"].Steps {
		if step.Name != "Pin trusted harness source" {
			continue
		}
		source = step.Run
		if len(step.Env) != 1 || step.Env["GH_TOKEN"] != "${{ github.token }}" {
			t.Fatal("harness source may not depend on event or branch inputs")
		}
	}
	if !strings.Contains(source, "sha=$(gh api repos/Hikyo-Org/Hikyo/commits/main --jq '.sha')") {
		t.Fatal("harness SHA must resolve from the literal authenticated trusted main endpoint")
	}
	for _, forbidden := range []string{"EVENT_NAME", "REHEARSAL_SHA", "DEFAULT_BRANCH", "github.sha", "github.event", "inputs.", "if ["} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("event-dependent harness source: %s", forbidden)
		}
	}
	for name, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/checkout@") {
				continue
			}
			expected := "${{ needs.identity.outputs.trusted_sha }}"
			if name == "identity" {
				expected = "${{ steps.source.outputs.sha }}"
			}
			if step.With["ref"] != expected {
				t.Fatalf("%s must checkout the one resolved main SHA", name)
			}
		}
	}
	publisher := workflow.Jobs["publish-pass"]
	if !strings.Contains(publisher.If, "github.event_name == 'release' || github.event_name == 'workflow_run'") || !strings.Contains(publisher.If, "needs.validate.result == 'success'") || !strings.Contains(publisher.If, "needs.validate.outputs.passed == 'true'") {
		t.Fatal("publisher must reject rehearsals and incomplete outcomes")
	}
	runtime := workflow.Jobs["validate"]
	cleanup, attest, upload := -1, -1, -1
	harness, startup := -1, -1
	for i, step := range runtime.Steps {
		if strings.Contains(step.Run, "scripts/openaev/check.sh") {
			harness = i
			for _, check := range []string{"requirements-check.lock", "GITHUB_PATH", "go test ./scripts/openaev/native-prepare", "go test ./scripts/ci -run '^TestOpenAEV' -count=1"} {
				if !strings.Contains(step.Run, check) {
					t.Fatalf("missing post-publication harness check: %s", check)
				}
			}
		}
		if strings.Contains(step.Run, "stack/start.sh") {
			startup = i
		}
		if strings.Contains(step.Run, "stack/stop.sh") {
			cleanup = i
			if step.If != "always()" {
				t.Fatal("cleanup is not unconditional")
			}
		}
		if strings.Contains(step.Run, "publish_pass.py") {
			attest = i
		}
		if strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
			upload = i
			if step.If != "" {
				t.Fatal("PASS evidence must require successful suite and cleanup")
			}
			path := step.With["path"]
			if !strings.Contains(path, "validated-pass.json") || strings.Contains(path, "*") {
				t.Fatal("raw evidence must never be uploaded")
			}
		}
	}
	if !(harness >= 0 && harness < startup && startup < cleanup) {
		t.Fatal("harness checks must succeed before disposable infrastructure starts")
	}
	if !(cleanup >= 0 && cleanup < attest && attest < upload) {
		t.Fatal("complete PASS must be validated only after successful teardown")
	}
}

func TestOpenAEVAuthenticatesPrereleaseWithItsActualSigner(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/openaev-release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	var authentication string
	for _, step := range workflow.Jobs["validate"].Steps {
		if step.Name == "Authenticate immutable signed container" {
			authentication = step.Run
		}
	}
	if authentication == "" {
		t.Fatal("missing candidate authentication")
	}
	for _, tag := range []string{"v1.2.3", "v1.0.0-foo-nightly.1", "v0.0.1-nightly.20261012.75.gaaaaaaaa"} {
		t.Run(tag, func(t *testing.T) {
			dir := t.TempDir()
			capture := filepath.Join(dir, "cosign")
			// Exercise the workflow's actual shell with fake read-only registry/signature
			// clients. A prerelease containing nightly must retain its stable signer.
			script := `docker() { printf '%s\n' '{"digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}'; }
cosign() { printf '%s\n' "$*" >"$CAPTURE"; }
` + authentication
			command := exec.Command("bash", "-e", "-o", "pipefail", "-c", script)
			command.Env = append(os.Environ(), "REPOSITORY=Hikyo-Org/Hikyo", "RELEASE_TAG="+tag, "RELEASE_COMMIT="+strings.Repeat("a", 40), "GITHUB_OUTPUT="+filepath.Join(dir, "output"), "CAPTURE="+capture)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("authentication shell failed: %v: %s", err, output)
			}
			signed, err := os.ReadFile(capture)
			if err != nil {
				t.Fatal(err)
			}
			signer := "https://github.com/Hikyo-Org/Hikyo/.github/workflows/release.yml@refs/tags/" + tag
			if tag == "v0.0.1-nightly.20261012.75.gaaaaaaaa" {
				signer = "https://github.com/Hikyo-Org/Hikyo/.github/workflows/nightly.yml@refs/heads/main"
			}
			if !strings.Contains(string(signed), "--certificate-identity "+signer+" ") {
				t.Fatalf("wrong signer for %s: %s", tag, signed)
			}
		})
	}
}

func TestOpenAEVRehearsalDeniesCacheCapabilities(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/openaev-rehearsal.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		CacheMode string `yaml:"cache-mode"`
		Jobs      map[string]struct {
			CacheMode string `yaml:"cache-mode"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.CacheMode != "none" {
		t.Fatal("rehearsal must deny GitHub cache capabilities")
	}
	for name, job := range workflow.Jobs {
		if job.CacheMode != "" && job.CacheMode != "none" {
			t.Fatalf("%s overrides cache deny", name)
		}
	}
}
