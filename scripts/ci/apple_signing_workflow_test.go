package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
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

// TestAppleTicketNetworkFixtures preserves hosted-only networking and cleanup failure gates.
func TestAppleTicketNetworkFixtures(t *testing.T) {
	cmd := exec.Command("sh", "../release/with-apple-ticket-network_test.sh")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Apple ticket network fixtures: %v\n%s", err, output)
	}
}

// TestHomebrewNightlyPublisherFixtures requires authenticated local and matching public DMGs before tap writes.
func TestHomebrewNightlyPublisherFixtures(t *testing.T) {
	cmd := exec.Command("sh", "../release/publish-homebrew-nightly_test.sh")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Homebrew nightly publisher fixtures: %v\n%s", err, output)
	}
}

// Stable publication always consumes both accepted notarized disk images.
func TestAppleSigningGatesFinalReleaseBytes(t *testing.T) {
	var workflow struct {
		Jobs map[string]struct {
			Needs       yaml.Node         `yaml:"needs"`
			RunsOn      string            `yaml:"runs-on"`
			Environment string            `yaml:"environment"`
			Env         map[string]string `yaml:"env"`
			Outputs     map[string]string `yaml:"outputs"`
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
		"apple-notarize":     {"build-artifacts", "apple-sign"},
		"build-signed-draft": {"validation", "docs", "floor-bench", "build-artifacts", "apple-sign", "apple-notarize"},
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
	for _, name := range []string{"apple-sign", "apple-notarize"} {
		apple := workflow.Jobs[name]
		if apple.Env["DEVELOPER_DIR"] != "/Applications/Xcode_26.3.app/Contents/Developer" {
			t.Fatal("Apple notarization must select the tested Xcode 26.3 client instead of the runner default")
		}
		if apple.RunsOn != "macos-15" || apple.Environment != "apple-signing" || len(apple.Permissions) != 1 || apple.Permissions["contents"] != "read" {
			t.Fatal("Apple signing must use macOS without repository mutation or OIDC permissions")
		}
	}
	builds, appleSteps, notarySteps := 0, 0, 0
	for name, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Uses, "goreleaser/goreleaser-action@") {
				builds++
				if name != "build-artifacts" {
					t.Fatal("release bytes rebuilt after Apple signing")
				}
			}
			for key, value := range step.Env {
				if strings.HasPrefix(key, "APPLE_") && strings.Contains(value, "secrets.") {
					owner := "apple-sign"
					if key == "APPLE_NOTARY_KEY_P8" {
						owner = "apple-notarize"
					}
					if name != owner {
						t.Fatal("Apple credentials escaped their isolated phase")
					}
				}
			}
			if step.Run == "./scripts/release/sign-apple-artifacts.sh --sign-only dist" {
				appleSteps++
				if name != "apple-sign" || step.If != "" {
					t.Fatal("Apple signing must be unconditional in the macOS job")
				}
				for _, key := range []string{"APPLE_DEVELOPER_ID_P12_BASE64", "APPLE_DEVELOPER_ID_P12_PASSWORD"} {
					if step.Env[key] != "${{ secrets."+key+" }}" {
						t.Fatalf("missing secret %s", key)
					}
				}
			}
			if step.Run == "./scripts/release/with-apple-ticket-network.sh ./scripts/release/sign-apple-artifacts.sh --notarize-only dist" {
				notarySteps++
				if name != "apple-notarize" || step.If != "" || step.Env["APPLE_NOTARY_KEY_P8"] != "${{ secrets.APPLE_NOTARY_KEY_P8 }}" {
					t.Fatal("stable notarization must be unconditional in its separate credential-isolated job")
				}
			}
		}
	}
	if builds != 1 || appleSteps != 1 || notarySteps != 1 {
		t.Fatal("release requires one build and separate mandatory Apple signing and notarization stages")
	}
	if workflow.Jobs["build-artifacts"].Outputs["non_darwin_sha256"] != "${{ steps.input-digests.outputs.non_darwin_sha256 }}" {
		t.Fatal("original Linux/Windows hashes must be bound outside the Apple job")
	}
	var integrityScript string
	var final strings.Builder
	for _, step := range workflow.Jobs["build-signed-draft"].Steps {
		if step.Name == "Verify unchanged Linux and Windows archive bytes" {
			if step.Env["NON_DARWIN_SHA256"] != "${{ needs.build-artifacts.outputs.non_darwin_sha256 }}" {
				t.Fatal("final platform archive verification must use original build digest")
			}
			integrityScript = step.Run
		}
		final.WriteString(step.Run)
		final.WriteByte('\n')
		if strings.Contains(step.Uses, "actions/download-artifact@") && step.With["artifact-ids"] != "${{ needs.apple-notarize.outputs.artifact_id }}" {
			t.Fatal("final signing may consume only this run's finalized Apple artifacts")
		}
	}
	testApplePlatformIntegrityGuard(t, integrityScript)
	previous := -1
	for _, gate := range []string{"check-candidate.sh", "sha256sum --check --strict checksums.txt", "Darwin_$arch.dmg", "create-build-provenance.sh", "create-manifest.sh", "sign-stable-draft.sh", "gh release create"} {
		position := strings.Index(final.String(), gate)
		if position <= previous {
			t.Fatalf("missing or reordered release gate: %s", gate)
		}
		previous = position
	}
}

// Execute the workflow guard itself against changed bytes and a self-consistent
// checksum rewrite. A rewritten post-Apple checksum cannot authorize new bytes.
func testApplePlatformIntegrityGuard(t *testing.T, script string) {
	t.Helper()
	if script == "" {
		t.Fatal("missing non-Darwin integrity guard")
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "dist"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"hikyo_1.2.3_Linux_arm64.tar.gz", "hikyo_1.2.3_Linux_x86_64.tar.gz", "hikyo_1.2.3_Windows_arm64.zip", "hikyo_1.2.3_Windows_x86_64.zip"} {
		if err := os.WriteFile(filepath.Join(dir, "dist", name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Record the original digest using the guard's hashing command.
	command, _, ok := strings.Cut(script, "\n")
	if !ok {
		t.Fatal("invalid archive integrity guard")
	}
	cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", command+"\nprintf '%s' \"$digest\"")
	cmd.Dir = dir
	digest, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	run := func(wantSuccess bool) {
		t.Helper()
		cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", script)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "NON_DARWIN_SHA256="+string(digest))
		output, err := cmd.CombinedOutput()
		if (err == nil) != wantSuccess {
			t.Fatalf("archive integrity guard: err=%v output=%s", err, output)
		}
	}
	run(true)
	if err := os.WriteFile(filepath.Join(dir, "dist", "hikyo_1.2.3_Linux_arm64.tar.gz"), []byte("changed Linux executable"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("bash", "-e", "-o", "pipefail", "-c", "cd dist && sha256sum hikyo_* >checksums.txt")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rewrite checksums: %v %s", err, output)
	}
	run(false)
}
