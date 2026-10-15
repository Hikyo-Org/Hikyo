package ci_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// The two historical worker failures did not log the rejected API identity.
// Keep rejection strict, but retain enough public metadata to diagnose the next
// failure without guessing which field disagreed with the running workflow.
func TestQueueBootstrapIdentityValidationAndDiagnostics(t *testing.T) {
	const head = "5979d249e3da2415da169b9b19b77232dd2c93eb"
	const source = "568e9a1d981bbc512655f4a8c006d12e65c30d03"
	for _, filename := range []string{"ci-queue-worker.yml", "ci-queue-gate.yml"} {
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", filename))
		if err != nil {
			t.Fatal(err)
		}
		var workflow queueWorkflowContract
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			name, field string
			value       any
		}{
			{name: "valid"},
			{"invalid id type", "id", "123"},
			{"nonpositive id", "id", 0},
			{"fractional id", "id", 123.5},
			{"wrong run", "run_id", 999},
			{"wrong attempt", "run_attempt", 2},
			{"wrong source", "head_sha", head},
			{"queued status", "status", "queued"},
			{"completed status", "status", "completed"},
			{"missing start", "started_at", nil},
		} {
			t.Run(filename+"/"+test.name, func(t *testing.T) {
				job := map[string]any{"id": 116399951289, "run_id": 38728437258, "run_attempt": 1, "name": "publish-" + head, "head_sha": source, "status": "in_progress", "started_at": "2026-10-15T11:35:41Z"}
				if test.field != "" {
					job[test.field] = test.value
				}
				body, err := json.Marshal(map[string]any{"total_count": 1, "jobs": []any{job}})
				if err != nil {
					t.Fatal(err)
				}
				dir := t.TempDir()
				responsePath, envPath := filepath.Join(dir, "response.json"), filepath.Join(dir, "environment")
				if err := os.WriteFile(responsePath, body, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(envPath, nil, 0600); err != nil {
					t.Fatal(err)
				}
				shim := "#!/bin/sh\nset -eu\ntest \"$#\" = 2\ntest \"$1\" = api\ntest \"$2\" = 'repos/Hikyo-Org/Hikyo/actions/runs/38728437258/attempts/1/jobs?per_page=100'\ncat \"$RESPONSE_FILE\"\n"
				if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(shim), 0700); err != nil {
					t.Fatal(err)
				}
				started := time.Date(2026, 10, 15, 11, 35, 41, 0, time.UTC).Unix()
				clock := "#!/bin/sh\nset -eu\ntest \"$#\" = 1\ntest \"$1\" = +%s\necho " + strconv.FormatInt(started+2, 10) + "\n"
				if err := os.WriteFile(filepath.Join(dir, "date"), []byte(clock), 0700); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "bash", "-c", workflow.Jobs["publish"].Steps[0].Run)
				cmd.WaitDelay = time.Second
				cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_REPO=Hikyo-Org/Hikyo", "GITHUB_RUN_ID=38728437258", "GITHUB_RUN_ATTEMPT=1", "GITHUB_EVENT_NAME=workflow_run", "GITHUB_SHA="+source, "HEAD="+head, "GITHUB_ENV="+envPath, "RESPONSE_FILE="+responsePath)
				output, runErr := cmd.CombinedOutput()
				written, err := os.ReadFile(envPath)
				if err != nil {
					t.Fatal(err)
				}
				if test.name == "valid" {
					want := "PR_GATE_STARTED_AT=" + strconv.FormatInt(started, 10) + "\nPR_GATE_DEADLINE=" + strconv.FormatInt(started+60, 10) + "\n"
					if runErr != nil || string(written) != want {
						t.Fatalf("valid identity lost authenticated setup clock: %v %s %s", runErr, output, written)
					}
					return
				}
				if runErr == nil || len(written) != 0 {
					t.Fatalf("invalid identity reached setup: %v %s %s", runErr, output, written)
				}
				const prefix = "invalid publisher job identity: "
				_, diagnostic, found := strings.Cut(string(output), prefix)
				if !found {
					t.Fatalf("identity failure omitted actual and expected metadata: %s", output)
				}
				var captured struct {
					Actual   map[string]any `json:"actual"`
					Expected map[string]any `json:"expected"`
				}
				if err := json.Unmarshal([]byte(strings.TrimSpace(diagnostic)), &captured); err != nil {
					t.Fatalf("invalid identity diagnostic: %v: %s", err, diagnostic)
				}
				if len(captured.Actual) != 6 || len(captured.Expected) != 6 || captured.Expected["head_sha"] != source || captured.Expected["status"] != "in_progress" {
					t.Fatalf("incomplete identity diagnostic: %s", diagnostic)
				}
				want, err := json.Marshal(test.value)
				if err != nil {
					t.Fatal(err)
				}
				got, err := json.Marshal(captured.Actual[test.field])
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != string(want) {
					t.Fatalf("rejected %s omitted: got %s, want %s", test.field, got, want)
				}
			})
		}
	}
}
