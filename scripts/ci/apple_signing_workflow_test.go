package ci_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAppleSigningFixtures(t *testing.T) {
	cmd := exec.Command("sh", "../release/sign-apple-artifacts_test.sh")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Apple signing fixtures: %v\n%s", err, output)
	}
}

func TestAppleSigningGatesFinalReleaseBytes(t *testing.T) {
	var workflow struct {
		Jobs map[string]struct {
			Needs       yaml.Node         `yaml:"needs"`
			RunsOn      string            `yaml:"runs-on"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Name string            `yaml:"name"`
				Run  string            `yaml:"run"`
				Uses string            `yaml:"uses"`
				If   string            `yaml:"if"`
				Env  map[string]string `yaml:"env"`
				With map[string]string `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	raw, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string][]string{
		"build-artifacts":    {"validation", "docs", "floor-bench"},
		"apple-sign":         {"build-artifacts"},
		"build-signed-draft": {"validation", "docs", "floor-bench", "build-artifacts", "apple-sign"},
	} {
		needs, err := workflowNeeds(workflow.Jobs[name].Needs)
		if err != nil {
			t.Fatal(err)
		}
		for _, required := range expected {
			if !contains(needs, required) {
				t.Fatalf("%s can run without %s", name, required)
			}
		}
	}
	apple := workflow.Jobs["apple-sign"]
	if apple.RunsOn != "macos-15" || len(apple.Permissions) != 1 || apple.Permissions["contents"] != "read" {
		t.Fatal("Apple signing must use macOS without repository mutation or OIDC permissions")
	}
	builds, appleSteps := 0, 0
	for name, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Uses, "goreleaser/goreleaser-action@") {
				builds++
				if name != "build-artifacts" {
					t.Fatal("release bytes rebuilt after Apple signing")
				}
			}
			for key, value := range step.Env {
				if strings.HasPrefix(key, "APPLE_") && strings.Contains(value, "secrets.") && name != "apple-sign" {
					t.Fatal("Apple credentials escaped the isolated signing job")
				}
			}
			if step.Run == "./scripts/release/sign-apple-artifacts.sh dist" {
				appleSteps++
				if name != "apple-sign" || step.If != "" {
					t.Fatal("Apple signing must be unconditional in the macOS job")
				}
				for _, key := range []string{"APPLE_DEVELOPER_ID_P12_BASE64", "APPLE_DEVELOPER_ID_P12_PASSWORD", "APPLE_NOTARY_KEY_P8"} {
					if step.Env[key] != "${{ secrets."+key+" }}" {
						t.Fatalf("missing secret %s", key)
					}
				}
			}
		}
	}
	if builds != 1 || appleSteps != 1 {
		t.Fatal("release requires one build and one mandatory Apple signing stage")
	}
	var final strings.Builder
	for _, step := range workflow.Jobs["build-signed-draft"].Steps {
		final.WriteString(step.Run)
		final.WriteByte('\n')
		if strings.Contains(step.Uses, "actions/download-artifact@") && step.With["artifact-ids"] != "${{ needs.apple-sign.outputs.artifact_id }}" {
			t.Fatal("final signing may consume only this run's finalized Apple artifacts")
		}
	}
	previous := -1
	for _, gate := range []string{"check-candidate.sh", "sha256sum --check --strict checksums.txt", "Darwin_$arch.dmg", "create-build-provenance.sh", "create-manifest.sh", "sign-stable-draft.sh", "gh release create"} {
		position := strings.Index(final.String(), gate)
		if position <= previous {
			t.Fatalf("missing or reordered release gate: %s", gate)
		}
		previous = position
	}
}
