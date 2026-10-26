package ci_test

import (
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Evaluate the workflow's actual boolean expression across upstream outcomes.
// GitHub and Go share these literal equality and boolean operators.
func TestNightlyPublicationRequiresSigningButAllowsNotarizationFailure(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/nightly.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			If      string            `yaml:"if"`
			Outputs map[string]string `yaml:"outputs"`
			Steps   []struct {
				ID   string `yaml:"id"`
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
				Uses string `yaml:"uses"`
				If   string `yaml:"if"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	notary := workflow.Jobs["apple-notarize"]
	findStep := func(t *testing.T, nameOrID string) int {
		t.Helper()
		for index, step := range notary.Steps {
			if step.Name == nameOrID || step.ID == nameOrID {
				return index
			}
		}
		t.Fatalf("missing notarization step %q", nameOrID)
		return -1
	}
	helperIndex := findStep(t, "notarize")
	diagnosticsIndex := findStep(t, "Retain Apple notarization diagnostics")
	if notary.Steps[helperIndex].Run != "./scripts/release/with-apple-ticket-network.sh ./scripts/release/sign-apple-artifacts.sh --notarize-only dist" {
		t.Fatal("notarize outcome must belong to the actual notarization helper")
	}
	t.Run("helper outcome output binding", func(t *testing.T) {
		if got := notary.Outputs["notarize_outcome"]; got != "${{ steps.notarize.outcome }}" {
			t.Fatalf("notarization outcome output = %q, wanted the helper step's raw outcome", got)
		}
	})
	t.Run("diagnostics outcome output binding", func(t *testing.T) {
		step := notary.Steps[diagnosticsIndex]
		if diagnosticsIndex <= helperIndex || step.ID == "" || !strings.HasPrefix(step.Uses, "actions/upload-artifact@") || step.If != "${{ always() }}" {
			t.Fatal("diagnostics outcome must belong to the always-run diagnostics upload after the helper")
		}
		if got := notary.Outputs["diagnostics_outcome"]; got != "${{ steps."+step.ID+".outcome }}" {
			t.Fatalf("diagnostics outcome output = %q, wanted the diagnostics upload step's raw outcome", got)
		}
	})
	for _, tc := range []struct {
		name, validation, build, sign, notary, helper, skip, ref, verified, failedStep, diagnostics string
		cancelled, want                                                                             bool
	}{
		{"accepted", "success", "success", "success", "success", "success", "false", "refs/heads/main", "true", "", "success", false, true},
		{"provider helper failure with retained diagnostics", "success", "success", "success", "failure", "failure", "false", "refs/heads/main", "true", "", "success", false, true},
		{"helper and diagnostics upload failure", "success", "success", "success", "failure", "failure", "false", "refs/heads/main", "true", "Retain Apple notarization diagnostics", "success", false, false},
		{"helper failure with missing diagnostics outcome", "success", "success", "success", "failure", "failure", "false", "refs/heads/main", "true", "", "", false, false},
		{"helper failure with skipped diagnostics", "success", "success", "success", "failure", "failure", "false", "refs/heads/main", "true", "", "skipped", false, false},
		{"helper failure with cancelled diagnostics", "success", "success", "success", "failure", "failure", "false", "refs/heads/main", "true", "", "cancelled", false, false},
		{"helper failure with unknown diagnostics outcome", "success", "success", "success", "failure", "failure", "false", "refs/heads/main", "true", "", "unknown", false, false},
		{"job success with missing diagnostics outcome", "success", "success", "success", "success", "success", "false", "refs/heads/main", "true", "", "", false, true},
		{"diagnostics upload failure after helper success", "success", "success", "success", "failure", "success", "false", "refs/heads/main", "true", "Retain Apple notarization diagnostics", "success", false, false},
		{"finalized packaging failure after helper success", "success", "success", "success", "failure", "success", "false", "refs/heads/main", "true", "Retain finalized nightly artifacts and predecessor context", "success", false, false},
		{"final artifact upload failure after helper success", "success", "success", "success", "failure", "success", "false", "refs/heads/main", "true", "upload-inputs", "success", false, false},
		{"invalid notarization inputs", "success", "success", "success", "failure", "failure", "false", "refs/heads/main", "false", "", "success", false, false},
		{"missing input verification", "success", "success", "success", "failure", "failure", "false", "refs/heads/main", "", "", "success", false, false},
		{"signing failure", "success", "success", "failure", "skipped", "skipped", "false", "refs/heads/main", "true", "", "success", false, false},
		{"validation failure", "failure", "success", "success", "failure", "failure", "false", "refs/heads/main", "true", "", "success", false, false},
		{"build failure", "success", "failure", "success", "failure", "failure", "false", "refs/heads/main", "true", "", "success", false, false},
		{"notarization cancelled", "success", "success", "success", "cancelled", "failure", "false", "refs/heads/main", "true", "", "success", false, false},
		{"notarization skipped", "success", "success", "success", "skipped", "failure", "false", "refs/heads/main", "true", "", "success", false, false},
		{"notarization unknown", "success", "success", "success", "unknown", "failure", "false", "refs/heads/main", "true", "", "success", false, false},
		{"helper cancelled", "success", "success", "success", "failure", "cancelled", "false", "refs/heads/main", "true", "", "success", false, false},
		{"helper skipped", "success", "success", "success", "failure", "skipped", "false", "refs/heads/main", "true", "", "success", false, false},
		{"helper unknown", "success", "success", "success", "failure", "unknown", "false", "refs/heads/main", "true", "", "success", false, false},
		{"missing helper outcome", "success", "success", "success", "failure", "", "false", "refs/heads/main", "true", "", "success", false, false},
		{"job success with failed helper", "success", "success", "success", "success", "failure", "false", "refs/heads/main", "true", "", "success", false, false},
		{"job success with missing helper outcome", "success", "success", "success", "success", "", "false", "refs/heads/main", "true", "", "success", false, false},
		{"operator cancellation", "success", "success", "success", "failure", "failure", "false", "refs/heads/main", "true", "", "success", true, false},
		{"other branch", "success", "success", "success", "failure", "failure", "false", "refs/heads/feature", "true", "", "success", false, false},
		{"published container retry", "success", "success", "skipped", "skipped", "skipped", "true", "refs/heads/main", "", "", "", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stepOutcomes := make([]string, len(notary.Steps))
			for index := range stepOutcomes {
				stepOutcomes[index] = "success"
			}
			stepOutcomes[helperIndex] = tc.helper
			stepOutcomes[diagnosticsIndex] = tc.diagnostics
			if tc.failedStep != "" {
				index := findStep(t, tc.failedStep)
				if index <= helperIndex {
					t.Fatalf("late failure step %q must follow the helper", tc.failedStep)
				}
				stepOutcomes[index] = "failure"
			}
			// Resolve the actual job output from actual step IDs and outcomes,
			// keeping helper and diagnostics outcomes independent of job failures.
			outputs := make(map[string]string)
			for index, step := range notary.Steps {
				if step.ID != "" {
					outputs["${{ steps."+step.ID+".outcome }}"] = stepOutcomes[index]
				}
			}
			expression := strings.ReplaceAll(workflow.Jobs["publish"].If, "'", "\"")
			for name, value := range map[string]string{"needs.apple-acceptance.result": "success", "needs.apple-notarize.outputs.recovery_pending": "false", "github.ref": tc.ref, "needs.validation.result": tc.validation, "needs.build-artifacts.result": tc.build, "needs.apple-sign.result": tc.sign, "needs.apple-notarize.result": tc.notary, "needs.build-artifacts.outputs.skip": tc.skip, "needs.apple-notarize.outputs.inputs_verified": tc.verified, "needs.apple-notarize.outputs.notarize_outcome": outputs[notary.Outputs["notarize_outcome"]], "needs.apple-notarize.outputs.diagnostics_outcome": outputs[notary.Outputs["diagnostics_outcome"]]} {
				expression = strings.ReplaceAll(expression, name, strconv.Quote(value))
			}
			expression = strings.ReplaceAll(expression, "always()", "true")
			expression = strings.ReplaceAll(expression, "cancelled()", strconv.FormatBool(tc.cancelled))
			value, err := types.Eval(token.NewFileSet(), nil, token.NoPos, expression)
			if err != nil {
				t.Fatal(err)
			}
			if value.Value == nil || value.Value.Kind() != constant.Bool || constant.BoolVal(value.Value) != tc.want {
				t.Fatalf("publication condition %s = %v, wanted %t", expression, value.Value, tc.want)
			}
		})
	}
}

// Execute each workflow's transfer guard against altered signed payloads,
// including a self-consistent checksum rewrite after the signing job.
func TestAppleSigningPhaseTransferBindsSignedBytes(t *testing.T) {
	for _, file := range []string{"nightly.yml", "release.yml"} {
		t.Run(file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", file))
			if err != nil {
				t.Fatal(err)
			}
			var workflow mainValidationWorkflow
			if err := yaml.Unmarshal(raw, &workflow); err != nil {
				t.Fatal(err)
			}
			find := func(job, name string) mainValidationStep {
				t.Helper()
				for _, step := range workflow.Jobs[job].Steps {
					if step.Name == name {
						return step
					}
				}
				t.Fatalf("missing %s/%s", job, name)
				return mainValidationStep{}
			}
			bind := find("apple-sign", "Bind signed macOS artifact bytes")
			verify := find("apple-notarize", "Verify exact signed macOS input bytes")
			var fallback mainValidationStep
			if file == "nightly.yml" {
				fallback = find("publish", "Verify signed macOS fallback input bytes")
				if fallback.Env["SIGNED_DARWIN_SHA256"] != "${{ needs.apple-sign.outputs.darwin_sha256 }}" || fallback.If != "needs.build-artifacts.outputs.skip != 'true' && needs.apple-notarize.result == 'failure'" {
					t.Fatal("fallback must independently recheck the original signed macOS digest")
				}
			}
			if verify.Env["SIGNED_DARWIN_SHA256"] != "${{ needs.apple-sign.outputs.darwin_sha256 }}" {
				t.Fatal("notarization must bind the signing job's independent digest")
			}
			for _, mutation := range []string{"none", "archive", "DMG", "missing DMG", "symlink DMG"} {
				t.Run(mutation, func(t *testing.T) {
					dir := t.TempDir()
					if err := os.Mkdir(filepath.Join(dir, "dist"), 0700); err != nil {
						t.Fatal(err)
					}
					// Use the real digest helper from the workflow, retaining a
					// separate disposable payload directory for each mutation.
					if err := os.MkdirAll(filepath.Join(dir, "scripts", "release"), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(filepath.Join(repositoryRoot(t), "scripts", "release", "apple-artifact-digest.sh"), filepath.Join(dir, "scripts", "release", "apple-artifact-digest.sh")); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(filepath.Join(repositoryRoot(t), "scripts", "lib"), filepath.Join(dir, "scripts", "lib")); err != nil {
						t.Fatal(err)
					}
					version := "1.2.3"
					for _, arch := range []string{"arm64", "x86_64"} {
						for _, ext := range []string{"tar.gz", "dmg"} {
							name := "hikyo_" + version + "_Darwin_" + arch + "." + ext
							if err := os.WriteFile(filepath.Join(dir, "dist", name), []byte("signed "+name), 0600); err != nil {
								t.Fatal(err)
							}
						}
					}
					outputFile := filepath.Join(dir, "outputs")
					run := func(script string, extra ...string) error {
						cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", script)
						cmd.Dir = dir
						cmd.Env = append(os.Environ(), "VERSION="+version, "GITHUB_OUTPUT="+outputFile)
						cmd.Env = append(cmd.Env, extra...)
						return cmd.Run()
					}
					if err := run(bind.Run); err != nil {
						t.Fatal(err)
					}
					bound, err := os.ReadFile(outputFile)
					if err != nil {
						t.Fatal(err)
					}
					digest, ok := strings.CutPrefix(strings.TrimSpace(string(bound)), "sha256=")
					if !ok || len(digest) != 64 {
						t.Fatalf("invalid signing output %q", bound)
					}
					name := filepath.Join(dir, "dist", "hikyo_1.2.3_Darwin_arm64.dmg")
					switch mutation {
					case "archive":
						name = filepath.Join(dir, "dist", "hikyo_1.2.3_Darwin_arm64.tar.gz")
						fallthrough
					case "DMG":
						if err := os.WriteFile(name, []byte("substituted"), 0600); err != nil {
							t.Fatal(err)
						}
					case "missing DMG":
						if err := os.Remove(name); err != nil {
							t.Fatal(err)
						}
					case "symlink DMG":
						if err := os.Remove(name); err != nil {
							t.Fatal(err)
						}
						if err := os.Symlink("hikyo_1.2.3_Darwin_x86_64.dmg", name); err != nil {
							t.Fatal(err)
						}
					}
					if err := run("cd dist && shasum -a 256 hikyo_*.tar.gz > checksums.txt"); err != nil {
						t.Fatal(err)
					}
					if err := run(verify.Run, "SIGNED_DARWIN_SHA256="+digest); (err == nil) != (mutation == "none") {
						t.Fatalf("mutation %s: guard returned %v", mutation, err)
					}
					if fallback.Run != "" {
						if err := run(fallback.Run, "SIGNED_DARWIN_SHA256="+digest); (err == nil) != (mutation == "none") {
							t.Fatalf("fallback mutation %s: guard returned %v", mutation, err)
						}
					}
				})
			}
		})
	}
}

// Run the actual staging script with signed DMGs present in the fallback input:
// a failed notarization job must never leak them into the authenticated release.
func TestNightlyNotarizationOutcomeControlsPublishedDMGs(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/nightly.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	var stage mainValidationStep
	for _, step := range workflow.Jobs["publish"].Steps {
		if step.Name == "Stage the complete nightly payload inventory" {
			stage = step
		}
	}
	if stage.Run == "" || stage.Env["APPLE_NOTARIZATION_RESULT"] != "${{ needs.apple-notarize.result }}" {
		t.Fatal("missing authenticated notarization outcome")
	}
	for _, outcome := range []string{"success", "failure", "cancelled", "skipped", "unknown"} {
		for _, missing := range []bool{false, true} {
			t.Run(outcome+map[bool]string{false: "", true: "/missing DMG"}[missing], func(t *testing.T) {
				dir := t.TempDir()
				write := func(name, content string) {
					t.Helper()
					path := filepath.Join(dir, name)
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(content), 0600); err != nil {
						t.Fatal(err)
					}
				}
				version := "0.0.1-nightly.20261022.1.gaaaaaaaa"
				for _, suffix := range []string{"Darwin_arm64.tar.gz", "Darwin_x86_64.tar.gz", "Linux_arm64.tar.gz", "Linux_x86_64.tar.gz", "Windows_arm64.zip", "Windows_x86_64.zip", "arm64.deb", "arm64.apk"} {
					write("dist/hikyo_"+version+"_"+suffix, "signed archive")
				}
				for _, suffix := range []string{"a.rpm", "a.pkg.tar.zst"} {
					write("dist/hikyo-"+suffix, "native package")
				}
				for _, name := range []string{"checksums.txt", "binary-provenance.json"} {
					write("dist/"+name, "canonical bytes")
				}
				for _, arch := range []string{"arm64", "x86_64"} {
					if !missing || arch != "x86_64" {
						write("dist/hikyo_"+version+"_Darwin_"+arch+".dmg", "signed DMG")
					}
				}
				write("temp/upgrade-compatibility.json", "compatibility")
				write("release/trust/nightly/policy.json", "policy")
				write("release/trust/nightly/trusted-root.json", "trust root")
				// Stop at the manifest-builder seam while preserving all actual
				// workflow selection, inventory copying and release-note logic.
				write("scripts/release/create-nightly-manifest.sh", "#!/bin/sh\nset -eu\ntest -f \"$4/NIGHTLY-BUILD.txt\"\n")
				if err := os.Chmod(filepath.Join(dir, "scripts/release/create-nightly-manifest.sh"), 0700); err != nil {
					t.Fatal(err)
				}
				// The evidence binder has its own byte-binding tests. Keep this
				// inventory-selection fixture stopped at that explicit seam.
				write("scripts/release/apple_notary_state.py", "import sys\nassert sys.argv[1] == 'append-evidence'\n")
				cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", stage.Run)
				cmd.Dir = dir
				cmd.Env = append(os.Environ(), "VERSION="+version, "COMMIT="+strings.Repeat("a", 40), "GITHUB_RUN_NUMBER=1", "RUNNER_TEMP="+filepath.Join(dir, "temp"), "APPLE_NOTARIZATION_RESULT="+outcome)
				output, err := cmd.CombinedOutput()
				wantSuccess := outcome == "failure" || outcome == "success" && !missing
				if (err == nil) != wantSuccess {
					t.Fatalf("staging %s missing=%t: %v\n%s", outcome, missing, err, output)
				}
				if !wantSuccess {
					return
				}
				dmgs, err := filepath.Glob(filepath.Join(dir, "nightly-payloads", "*.dmg"))
				if err != nil {
					t.Fatal(err)
				}
				wantDMGs := 0
				if outcome == "success" {
					wantDMGs = 2
				}
				if len(dmgs) != wantDMGs {
					t.Fatalf("published %d DMGs, expected %d", len(dmgs), wantDMGs)
				}
				notes, err := os.ReadFile(filepath.Join(dir, "nightly-payloads", "NIGHTLY-BUILD.txt"))
				if err != nil {
					t.Fatal(err)
				}
				if outcome == "failure" && (!strings.Contains(string(notes), "NOT verified") || strings.Contains(string(notes), "signed and notarized")) {
					t.Fatalf("misleading macOS fallback notes: %s", notes)
				}
			})
		}
	}
}
