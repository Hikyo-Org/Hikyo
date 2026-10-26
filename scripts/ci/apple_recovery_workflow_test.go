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

func TestNightlyPendingRecoveryAndNativeAcceptanceBlockPublication(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/nightly.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, notary, helper, pending, native string
		want                                  bool
	}{
		{"complete native pair", "success", "success", "false", "success", true},
		{"native failure", "success", "success", "false", "failure", false},
		{"native skipped", "success", "success", "false", "skipped", false},
		{"native cancelled", "success", "success", "false", "cancelled", false},
		{"pending submission", "failure", "failure", "true", "skipped", false},
		{"qualified interruption", "failure", "skipped", "true", "skipped", false},
		{"terminal fallback preserved", "failure", "failure", "false", "skipped", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expression := strings.ReplaceAll(workflow.Jobs["publish"].If, "'", "\"")
			for name, value := range map[string]string{
				"github.ref": "refs/heads/main", "needs.validation.result": "success", "needs.build-artifacts.result": "success",
				"needs.apple-sign.result": "success", "needs.apple-notarize.result": tc.notary,
				"needs.build-artifacts.outputs.skip": "false", "needs.apple-notarize.outputs.inputs_verified": "true",
				"needs.apple-notarize.outputs.notarize_outcome": tc.helper, "needs.apple-notarize.outputs.diagnostics_outcome": "success",
				"needs.apple-notarize.outputs.recovery_pending": tc.pending, "needs.apple-acceptance.result": tc.native,
			} {
				expression = strings.ReplaceAll(expression, name, strconv.Quote(value))
			}
			expression = strings.ReplaceAll(strings.ReplaceAll(expression, "always()", "true"), "cancelled()", "false")
			value, err := types.Eval(token.NewFileSet(), nil, token.NoPos, expression)
			if err != nil || value.Value == nil || value.Value.Kind() != constant.Bool || constant.BoolVal(value.Value) != tc.want {
				t.Fatalf("publication condition = %v, want %t: %v", value.Value, tc.want, err)
			}
		})
	}
}

func applePhasePermissions(name string) map[string]string {
	permissions := map[string]string{"contents": "read"}
	if name == "apple-notarize" {
		permissions["actions"] = "read"
	}
	return permissions
}

func TestAppleCheckpointFixtures(t *testing.T) {
	cmd := exec.Command("python3", "-B", "-m", "unittest", "discover", "-s", "../release", "-p", "test_apple_notary_state.py")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Apple checkpoint failure contracts: %v\n%s", err, output)
	}
}

func TestAppleNativeInstallationFixtures(t *testing.T) {
	cmd := exec.Command("sh", "../release/verify-apple-install_test.sh")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Apple installation failure contracts: %v\n%s", err, output)
	}
}

func TestAppleCheckpointOrderAndAuthority(t *testing.T) {
	for _, name := range []string{"nightly.yml", "release.yml"} {
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		var workflow mainValidationWorkflow
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatal(err)
		}
		var phases []string
		for _, step := range workflow.Jobs["apple-notarize"].Steps {
			if strings.Contains(step.Run, "apple_notary_state.py restore") || strings.Contains(step.Run, "apple_notary_state.py snapshot") {
				expectedChannel := "nightly"
				if name == "release.yml" {
					expectedChannel = "stable"
				}
				if step.Env["APPLE_RELEASE_CHANNEL"] != expectedChannel {
					t.Fatalf("%s checkpoint lost explicit %s channel binding", name, expectedChannel)
				}
			}
			if strings.Contains(step.Run, "sign-apple-artifacts.sh --prepare-only") {
				phases = append(phases, "prepare")
				for key, value := range step.Env {
					if strings.Contains(value, "secrets.") {
						t.Fatalf("credential-free prepare reads secret %s", key)
					}
				}
			}
			if strings.Contains(step.With["name"], "-prepared") {
				phases = append(phases, "persist-intent")
			}
			if strings.Contains(step.Run, "sign-apple-artifacts.sh --submit-only") {
				phases = append(phases, "submit")
			}
			if strings.Contains(step.With["name"], "-submitted") {
				phases = append(phases, "persist-UUID")
			}
			if strings.Contains(step.Run, "sign-apple-artifacts.sh --notarize-only") {
				phases = append(phases, "wait-and-finalize")
			}
		}
		if strings.Join(phases, ",") != "prepare,persist-intent,submit,persist-UUID,wait-and-finalize" {
			t.Fatalf("%s loses submit-once checkpoint order: %v", name, phases)
		}
	}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/apple-release-recovery.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secrets.", "id-token: write", "contents: write", "environment:", "workflow_dispatch:", "pull_request"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("recovery worker gained unrelated authority: %s", forbidden)
		}
	}
	if !strings.Contains(string(raw), "actions: write") || !strings.Contains(string(raw), "apple_notary_state.py recover") {
		t.Fatal("recovery must use only the bound original-run continuation worker")
	}
}
