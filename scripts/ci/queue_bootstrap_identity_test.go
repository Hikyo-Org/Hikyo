package ci_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Reject unauthenticated identities immediately while retaining public metadata
// that distinguishes identity failures from Jobs API status propagation lag.
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

// Replay the actual running-job/queued-response race without real sleeps or
// network requests. A retry never grants a fresh setup clock or another job ID.
func TestQueueBootstrapStatusConsistency(t *testing.T) {
	const head = "c90eee1ce8d5607ee7ca5fdb0c77b4bb6003e8f1"
	const source = "6342ae361232fbb6d1b0dcef6f106a438a781718"
	started := time.Date(2026, 10, 16, 18, 57, 22, 0, time.UTC).Unix()
	for _, filename := range []string{"ci-queue-worker.yml", "ci-queue-gate.yml"} {
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", filename))
		if err != nil {
			t.Fatal(err)
		}
		var workflow queueWorkflowContract
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			name      string
			statuses  []string
			change    string
			secondNow int64
			wantOK    bool
			wantCalls int
		}{
			{"running fast path", []string{"in_progress"}, "", 0, true, 1},
			{"observed queued then running", []string{"queued", "in_progress"}, "", 0, true, 2},
			{"queued twice then running", []string{"queued", "queued", "in_progress"}, "", 0, true, 3},
			{"queued bounded", []string{"queued"}, "", 0, false, 5},
			{"terminal status", []string{"completed"}, "", 0, false, 1},
			{"changed job", []string{"queued", "in_progress"}, "id", 0, false, 2},
			{"changed clock", []string{"queued", "in_progress"}, "started_at", 0, false, 2},
			{"expired while retrying", []string{"queued", "in_progress"}, "", 60, false, 2},
			{"wrong queued run", []string{"queued"}, "run_id", 0, false, 1},
		} {
			t.Run(filename+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				for i, status := range tc.statuses {
					job := map[string]any{"id": 117143001761, "run_id": 38951286305, "run_attempt": 1, "name": "publish-" + head, "head_sha": source, "status": status, "started_at": "2026-10-16T18:57:22Z"}
					if i > 0 || tc.change == "run_id" {
						switch tc.change {
						case "id":
							job["id"] = 999
						case "started_at":
							job["started_at"] = "2026-10-16T18:57:23Z"
						case "run_id":
							job["run_id"] = 999
						}
					}
					body, err := json.Marshal(map[string]any{"total_count": 1, "jobs": []any{job}})
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("response-%d", i+1)), body, 0600); err != nil {
						t.Fatal(err)
					}
				}
				files := map[string]string{
					"gh": fmt.Sprintf(`#!/bin/sh
set -eu
test "$1" = api
test "$2" = 'repos/Hikyo-Org/Hikyo/actions/runs/38951286305/attempts/1/jobs?per_page=100'
n=0
if [ -f "$FIXTURE/calls" ]; then n=$(cat "$FIXTURE/calls"); fi
n=$((n+1))
echo "$n" > "$FIXTURE/calls"
if [ "$n" -gt %d ]; then n=%d; fi
cat "$FIXTURE/response-$n"
`, len(tc.statuses), len(tc.statuses)),
					"date": fmt.Sprintf(`#!/bin/sh
set -eu
test "$1" = +%%s
n=0
if [ -f "$FIXTURE/calls" ]; then n=$(cat "$FIXTURE/calls"); fi
if [ "$n" -gt 1 ] && [ %d -gt 0 ]; then echo %d; else echo %d; fi
`, tc.secondNow, started+tc.secondNow, started+4),
					"sleep": `#!/bin/sh
set -eu
test "$1" = 1
echo sleep >> "$FIXTURE/sleeps"
`,
					"environment": "",
				}
				for name, body := range files {
					if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0700); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "bash", "-c", workflow.Jobs["publish"].Steps[0].Run)
				cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "FIXTURE="+dir, "GH_REPO=Hikyo-Org/Hikyo", "GITHUB_RUN_ID=38951286305", "GITHUB_RUN_ATTEMPT=1", "GITHUB_EVENT_NAME=workflow_run", "GITHUB_SHA="+source, "HEAD="+head, "GITHUB_ENV="+filepath.Join(dir, "environment"))
				output, runErr := cmd.CombinedOutput()
				calls, err := os.ReadFile(filepath.Join(dir, "calls"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.TrimSpace(string(calls)) != strconv.Itoa(tc.wantCalls) {
					t.Fatalf("unexpected retries: %s, %v %s", calls, runErr, output)
				}
				written, err := os.ReadFile(filepath.Join(dir, "environment"))
				if err != nil {
					t.Fatal(err)
				}
				if !tc.wantOK {
					if runErr == nil || len(written) != 0 {
						t.Fatalf("unsafe identity accepted: %v %s %s", runErr, output, written)
					}
					return
				}
				want := fmt.Sprintf("PR_GATE_STARTED_AT=%d\nPR_GATE_DEADLINE=%d\n", started, started+60)
				if runErr != nil || string(written) != want {
					t.Fatalf("original clock changed: %v %s %s", runErr, output, written)
				}
				if tc.wantCalls == 1 {
					if _, err := os.Stat(filepath.Join(dir, "sleeps")); !os.IsNotExist(err) {
						t.Fatal("running fast path slept")
					}
				}
			})
		}
	}
}
