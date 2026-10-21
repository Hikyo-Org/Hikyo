package ci_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNightlyAppleSigningGatesPublication(t *testing.T) {
	var workflow struct {
		Jobs map[string]struct {
			Needs       yaml.Node            `yaml:"needs"`
			If          string               `yaml:"if"`
			RunsOn      string               `yaml:"runs-on"`
			Environment string               `yaml:"environment"`
			Permissions map[string]string    `yaml:"permissions"`
			Outputs     map[string]string    `yaml:"outputs"`
			Steps       []mainValidationStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	raw, err := os.ReadFile("../../.github/workflows/nightly.yml")
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	for name, expected := range map[string][]string{
		"build-artifacts": {"validation"},
		"apple-sign":      {"build-artifacts"},
		"publish":         {"validation", "build-artifacts", "apple-sign"},
		"container":       {"publish"},
	} {
		needs, err := workflowNeeds(workflow.Jobs[name].Needs)
		if err != nil || !reflect.DeepEqual(needs, expected) {
			t.Fatalf("%s dependencies = %v, expected %v: %v", name, needs, expected, err)
		}
	}
	apple := workflow.Jobs["apple-sign"]
	if apple.RunsOn != "macos-15" || !reflect.DeepEqual(apple.Permissions, map[string]string{"contents": "read"}) || apple.If != "github.ref == 'refs/heads/main' && needs.build-artifacts.outputs.skip != 'true'" {
		t.Fatal("nightly Apple signing requires main, macOS and read-only repository access; only an already published release may skip it")
	}
	publish := workflow.Jobs["publish"]
	if publish.Environment != "nightly-release" || publish.If != "always() && github.ref == 'refs/heads/main' && needs.validation.result == 'success' && needs.build-artifacts.result == 'success' && (needs.build-artifacts.outputs.skip == 'true' || needs.apple-sign.result == 'success')" {
		t.Fatal("nightly publication must fail closed on validation/build/Apple failure while permitting already-published container retries")
	}
	for _, key := range []string{"tag", "version", "commit"} {
		if publish.Outputs[key] != "${{ needs.build-artifacts.outputs."+key+" }}" {
			t.Fatalf("container retry loses immutable source output %s", key)
		}
	}
	builds, signatures := 0, 0
	for name, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Uses, "goreleaser/goreleaser-action@") {
				builds++
				if name != "build-artifacts" {
					t.Fatal("nightly binaries may only be built before Apple signing")
				}
			}
			for key, value := range step.Env {
				if strings.HasPrefix(key, "APPLE_") && strings.Contains(value, "secrets.") && name != "apple-sign" {
					t.Fatal("nightly Apple secrets escaped the signing job")
				}
			}
			if step.Run == "./scripts/release/sign-apple-artifacts.sh dist" {
				signatures++
				if name != "apple-sign" || step.If != "" || step.Env["APPLE_RELEASE_CHANNEL"] != "nightly" || step.Env["APPLE_RELEASE_VERSION"] != "${{ needs.build-artifacts.outputs.version }}" || step.Env["APPLE_RELEASE_COMMIT"] != "${{ needs.build-artifacts.outputs.commit }}" {
					t.Fatal("nightly Apple signing must bind the canonical build identity without an optional step")
				}
				for _, key := range []string{"APPLE_DEVELOPER_ID_P12_BASE64", "APPLE_DEVELOPER_ID_P12_PASSWORD", "APPLE_NOTARY_KEY_P8"} {
					if step.Env[key] != "${{ secrets."+key+" }}" {
						t.Fatalf("nightly Apple signing lacks %s", key)
					}
				}
			}
			if strings.Contains(step.Uses, "actions/download-artifact@") && (name == "apple-sign" || name == "publish") {
				upstream := "build-artifacts"
				if name == "publish" {
					upstream = "apple-sign"
				}
				if step.With["artifact-ids"] != "${{ needs."+upstream+".outputs.artifact_id }}" || step.With["merge-multiple"] != "true" || step.With["name"] != "" {
					t.Fatalf("%s must consume this run's immutable upstream artifact ID", name)
				}
			}
		}
	}
	if builds != 1 || signatures != 1 {
		t.Fatal("nightly requires exactly one canonical build and Apple signing stage")
	}
	var final strings.Builder
	for _, step := range publish.Steps {
		final.WriteString(step.Run)
		final.WriteByte('\n')
	}
	previous := -1
	for _, gate := range []string{"test \"$COMMIT\" = \"$GITHUB_SHA\"", "sha256sum --check --strict checksums.txt", "Darwin_$arch.dmg", "prepare-image-root.sh", "cmp nightly-context/nightly-sources.json", "dist/hikyo_*_Darwin_*.dmg", "create-nightly-manifest.sh", "cosign sign-blob", "nightly verify", "smoke-release-startup.sh", "TestPackagedNightlyReleaseUpgrade", "gh release create"} {
		position := strings.Index(final.String(), gate)
		if position <= previous {
			t.Fatalf("missing or reordered nightly publication gate %s", gate)
		}
		previous = position
	}
}
