package ci_test

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

// Check the actual jobs: diagnostics must survive notarization failure without
// uploading the private Keychain or credential scratch directory.
func TestAppleNotarizationEvidenceRetainedOnFailure(t *testing.T) {
	for _, channel := range []string{"stable", "nightly"} {
		t.Run(channel, func(t *testing.T) {
			file := "release.yml"
			if channel == "nightly" {
				file = "nightly.yml"
			}
			raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", file))
			if err != nil {
				t.Fatal(err)
			}
			var workflow struct {
				Jobs map[string]struct {
					Steps []struct {
						Name string            `yaml:"name"`
						If   string            `yaml:"if"`
						Run  string            `yaml:"run"`
						Uses string            `yaml:"uses"`
						Env  map[string]string `yaml:"env"`
						With map[string]string `yaml:"with"`
					} `yaml:"steps"`
				} `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			signingIndex, uploadIndex := -1, -1
			for index, step := range workflow.Jobs["apple-notarize"].Steps {
				if step.Run == "./scripts/release/with-apple-ticket-network.sh ./scripts/release/sign-apple-artifacts.sh --notarize-only dist" {
					signingIndex = index
					if step.Env["APPLE_NOTARY_EVIDENCE_DIR"] != "${{ runner.temp }}/apple-notarization" {
						t.Fatal("notarization must write diagnostics outside credential scratch and release payloads")
					}
				}
				if step.Name != "Retain Apple notarization diagnostics" {
					continue
				}
				if uploadIndex >= 0 {
					t.Fatal("duplicate notarization diagnostics uploader")
				}
				uploadIndex = index
				if step.If != "${{ always() }}" || step.Uses != "actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9" {
					t.Fatal("diagnostics require the pinned uploader even after notarization failure")
				}
				if step.With["path"] != "${{ runner.temp }}/apple-notarization/*.json" || len(step.Env) != 0 {
					t.Fatal("diagnostics upload must select only public JSON without credential environment")
				}
				if step.With["name"] != "apple-notarization-"+channel+"-${{ github.run_id }}-${{ github.run_attempt }}" || step.With["if-no-files-found"] != "warn" {
					t.Fatal("diagnostics require a distinct run/attempt identity; pre-submission failures may have no receipt")
				}
			}
			if signingIndex < 0 || uploadIndex <= signingIndex {
				t.Fatal("missing or reordered signing and diagnostics upload")
			}
		})
	}
}
