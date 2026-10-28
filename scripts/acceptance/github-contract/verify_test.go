package contract_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func syntheticLog(t *testing.T, mutate func([]map[string]any) []map[string]any) []byte {
	t.Helper()
	var events []map[string]any
	events = append(events, map[string]any{"kind": "binding", "candidate_sha": strings.Repeat("b", 40), "workflow_sha": strings.Repeat("a", 40), "repository_id": 42, "organization_id": 43, "environment_id": 44, "authorization_ref": "synthetic", "workflow_sha256": strings.Repeat("c", 64)})
	for i := 1; i <= 18; i++ {
		scope := []string{"repository", "organization", "environment"}[(i-1)/6]
		events = append(events,
			map[string]any{"kind": "dispatch_intent", "nonce": i},
			map[string]any{"kind": "owned_run", "run_id": i},
			map[string]any{"kind": "owned_artifact", "artifact_id": 100 + i},
			map[string]any{"kind": "cleaned_artifact", "artifact_id": 100 + i},
			map[string]any{"kind": "consumption", "run_id": i, "artifact_id": 100 + i, "destination": scope, "workflow_sha": strings.Repeat("a", 40), "secret_sha256": strings.Repeat("d", 64), "variable_sha256": strings.Repeat("e", 64)},
			map[string]any{"kind": "cleaned_run", "run_id": i},
		)
	}
	events = append(events, map[string]any{"kind": "owned_name", "destination": "repository", "surface": "secret", "name": "SYNTHETIC"}, map[string]any{"kind": "cleaned_name", "destination": "repository", "surface": "secret", "name": "SYNTHETIC"}, map[string]any{"kind": "owned_environment", "name": "synthetic"}, map[string]any{"kind": "cleaned_environment", "name": "synthetic"})
	if mutate != nil {
		events = mutate(events)
	}
	var output bytes.Buffer
	encode := json.NewEncoder(&output)
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if err := encode.Encode(map[string]string{"Action": "output", "Output": "    file.go:1: GITHUB_CONTRACT_EVIDENCE " + string(data) + "\n"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"", "repository_missing_Variables_write_fails_first-sync_sentinel", "organization_missing_Variables_write_fails_first-sync_sentinel", "repository_minimum_token_and_workflow_consumption", "organization_minimum_token_and_workflow_consumption", "protected_environment_minimum_token_and_workflow_consumption", "administration_token_auto-create_identity_pin"} {
		testName := "TestGitHubDotComContract"
		if name != "" {
			testName += "/" + name
		}
		if err := encode.Encode(map[string]string{"Action": "pass", "Test": testName}); err != nil {
			t.Fatal(err)
		}
	}
	return output.Bytes()
}

func verify(t *testing.T, input []byte) bool {
	t.Helper()
	command := exec.CommandContext(t.Context(), "jq", "-se", "--arg", "candidate", strings.Repeat("b", 40), "--arg", "workflow", strings.Repeat("a", 40), "--argjson", "repository_id", "42", "--argjson", "organization_id", "43", "--argjson", "environment_id", "44", "--arg", "approval_ref", "synthetic", "-f", "verify.jq")
	command.Stdin = bytes.NewReader(input)
	output, err := command.CombinedOutput()
	if err == nil {
		return strings.TrimSpace(string(output)) == "true"
	}
	if strings.TrimSpace(string(output)) != "false" {
		t.Fatalf("validator execution error: %v: %s", err, output)
	}
	return false
}

func TestReceiptRejectsSkipsFailuresMissingCleanupAndWrongCandidate(t *testing.T) {
	if !verify(t, syntheticLog(t, nil)) {
		t.Fatal("complete synthetic receipt refused")
	}
	for _, kind := range []string{"skip", "fail"} {
		input := append(syntheticLog(t, nil), []byte(`{"Action":"`+kind+`","Test":"TestGitHubDotComContract"}`)...)
		if verify(t, input) {
			t.Fatalf("%s certified as provider proof", kind)
		}
	}
	for _, kind := range []string{"cleaned_artifact", "owned_artifact", "cleaned_run", "cleaned_name", "cleaned_environment", "consumption", "owned_run", "binding", "dispatch_intent"} {
		t.Run("missing/"+kind, func(t *testing.T) {
			input := syntheticLog(t, func(events []map[string]any) []map[string]any {
				for i, event := range events {
					if event["kind"] == kind {
						return append(events[:i], events[i+1:]...)
					}
				}
				return events
			})
			if verify(t, input) {
				t.Fatal("partial receipt certified")
			}
		})
	}
	for _, field := range []string{"candidate_sha", "workflow_sha", "repository_id", "organization_id", "environment_id", "authorization_ref"} {
		t.Run("binding/"+field, func(t *testing.T) {
			input := syntheticLog(t, func(events []map[string]any) []map[string]any { events[0][field] = "wrong"; return events })
			if verify(t, input) {
				t.Fatal("wrong binding certified")
			}
		})
	}
	for _, field := range []string{"run_id", "artifact_id", "destination", "workflow_sha", "secret_sha256", "variable_sha256"} {
		t.Run("consumption/"+field, func(t *testing.T) {
			input := syntheticLog(t, func(events []map[string]any) []map[string]any {
				for _, event := range events {
					if event["kind"] == "consumption" {
						event[field] = "wrong"
						break
					}
				}
				return events
			})
			if verify(t, input) {
				t.Fatal("wrong consumption certified")
			}
		})
	}
}

func TestRunnerPersistsHoldForSkippedFailedAndTruncatedOutput(t *testing.T) {
	script, err := filepath.Abs("run.sh")
	if err != nil {
		t.Fatal(err)
	}
	validator, err := os.ReadFile("verify.jq")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"pass", "skip", "fail", "truncated", "unsafe-credentials", "expired-authorization"} {
		t.Run(mode, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "synthetic checkout")
			bin := filepath.Join(root, "bin")
			if err := os.MkdirAll(bin, 0700); err != nil {
				t.Fatal(err)
			}
			write := func(path string, content []byte, permissions os.FileMode) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, path), content, permissions); err != nil {
					t.Fatal(err)
				}
			}
			candidate := strings.Repeat("b", 40)
			write("bin/git", []byte(`#!/bin/sh
case "$*" in
  'rev-parse --show-toplevel') printf '%s\n' "$SYNTHETIC_ROOT" ;;
  'rev-parse HEAD') printf '%s\n' "$SYNTHETIC_CANDIDATE" ;;
  'rev-parse HEAD^{tree}') printf '%s\n' synthetic-tree ;;
  'status --porcelain --untracked-files=normal'|'check-ignore -q '*) exit 0 ;;
  *) exit 99 ;;
esac
`), 0700)
			write("bin/go", []byte(fmt.Sprintf(`#!/bin/sh
if [ "$*" = 'env GOVERSION' ]; then printf '%%s\n' '%s'; exit 0; fi
cat "$SYNTHETIC_ROOT/input.jsonl"
exit "$SYNTHETIC_GO_EXIT"
`, runtime.Version())), 0700)
			write("go.mod", []byte("go "+strings.TrimPrefix(runtime.Version(), "go")+"\n"), 0600)
			if err := os.MkdirAll(filepath.Join(root, "scripts/acceptance/github-contract"), 0700); err != nil {
				t.Fatal(err)
			}
			write("scripts/acceptance/github-contract/verify.jq", validator, 0600)
			input := syntheticLog(t, nil)
			if mode == "skip" {
				input = []byte(`{"Action":"skip","Test":"TestGitHubDotComContract"}`)
			}
			if mode == "truncated" {
				input = append(input, []byte(`{"Action":`)...)
			}
			write("input.jsonl", input, 0600)
			expires := time.Now().UTC().Add(time.Hour)
			if mode == "expired-authorization" {
				expires = expires.Add(-2 * time.Hour)
			}
			authorization := map[string]any{"candidate_sha": candidate, "approved_by": "synthetic", "approval_ref": "synthetic", "expires_at": expires.Format(time.RFC3339), "synthetic_only": true, "no_new_spend": true, "maximum_workflow_dispatches": 18, "owner": "fixture", "repository": "repo", "environment": "contract", "ref": "main", "workflow_sha": strings.Repeat("a", 40), "repository_id": 42, "organization_id": 43, "environment_id": 44, "credential_grants_attested": true}
			data, err := json.Marshal(authorization)
			if err != nil {
				t.Fatal(err)
			}
			write("authorization.json", data, 0600)
			credentials := map[string]string{}
			for _, key := range []string{"REPOSITORY_TOKEN", "ORGANIZATION_TOKEN", "ENVIRONMENT_TOKEN", "REPOSITORY_DENIED_TOKEN", "ORGANIZATION_DENIED_TOKEN", "ENVIRONMENT_DENIED_TOKEN", "HARNESS_TOKEN"} {
				credentials[key] = "github_pat_synthetic_" + key
			}
			data, err = json.Marshal(credentials)
			if err != nil {
				t.Fatal(err)
			}
			permissions := os.FileMode(0600)
			if mode == "unsafe-credentials" {
				permissions = 0644
			}
			write("credentials.json", data, permissions)
			// The queue deliberately uses umask 077. Create the intended unsafe
			// fixture explicitly instead of letting that mask secure it for us.
			if err := os.Chmod(filepath.Join(root, "credentials.json"), permissions); err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(t.Context(), "bash", script, candidate, filepath.Join(root, "authorization.json"), filepath.Join(root, "credentials.json"), filepath.Join(root, "evidence"))
			command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "SYNTHETIC_ROOT="+root, "SYNTHETIC_CANDIDATE="+candidate, "SYNTHETIC_GO_EXIT=0")
			if mode == "fail" {
				command.Env = append(command.Env, "SYNTHETIC_GO_EXIT=1")
			}
			output, runErr := command.CombinedOutput()
			if (mode == "pass") != (runErr == nil) {
				t.Fatalf("runner outcome: %v: %s", runErr, output)
			}
			data, readErr := os.ReadFile(filepath.Join(root, "evidence/receipt.json"))
			if mode == "unsafe-credentials" || mode == "expired-authorization" {
				if !os.IsNotExist(readErr) {
					t.Fatal("preflight refusal started evidence/test execution")
				}
				return
			}
			if readErr != nil {
				t.Fatalf("missing failure receipt: %v: %s", readErr, output)
			}
			var receipt struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal(data, &receipt); err != nil {
				t.Fatal(err)
			}
			want := "HOLD"
			if mode == "pass" {
				want = "PASS"
			}
			if receipt.Status != want {
				t.Fatalf("receipt status %q, want %q", receipt.Status, want)
			}
		})
	}
}
