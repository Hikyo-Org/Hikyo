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
			{"valid", "status", "in_progress"},
			{"queued propagation lag", "status", "queued"},
			{"pending propagation lag", "status", "pending"},
			{"invalid id type", "id", "123"},
			{"nonpositive id", "id", 0},
			{"fractional id", "id", 123.5},
			{"wrong run", "run_id", 999},
			{"wrong attempt", "run_attempt", 2},
			{"wrong source", "head_sha", head},
			{"completed status", "status", "completed"},
			{"unknown status", "status", "unknown"},
			{"missing start", "started_at", nil},
		} {
			t.Run(filename+"/"+test.name, func(t *testing.T) {
				job := map[string]any{"id": 116399951289, "run_id": 38728437258, "run_attempt": 1, "name": "publish-" + head, "head_sha": source, "status": "pending", "started_at": "2026-10-15T11:35:41Z"}
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
				if err := os.WriteFile(filepath.Join(dir, "sleep"), []byte("#!/bin/sh\necho unexpected publisher polling >&2\nexit 99\n"), 0700); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "bash", "-c", workflow.Jobs["publish"].Steps[0].Run)
				cmd.WaitDelay = time.Second
				event := "workflow_run"
				if filename == "ci-queue-worker.yml" {
					event = "workflow_dispatch"
				}
				cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "GH_REPO=Hikyo-Org/Hikyo", "GITHUB_RUN_ID=38728437258", "GITHUB_RUN_ATTEMPT=1", "GITHUB_EVENT_NAME="+event, "GITHUB_SHA="+source, "HEAD="+head, "GITHUB_ENV="+envPath, "RESPONSE_FILE="+responsePath)
				output, runErr := cmd.CombinedOutput()
				written, err := os.ReadFile(envPath)
				if err != nil {
					t.Fatal(err)
				}
				if test.name == "valid" || test.name == "queued propagation lag" || test.name == "pending propagation lag" {
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
				if len(captured.Actual) != 6 || len(captured.Expected) != 6 || captured.Expected["head_sha"] != source || captured.Expected["status"] != "pending, queued or in_progress" {
					t.Fatalf("incomplete identity diagnostic: %s", diagnostic)
				}
				if test.field != "status" && captured.Actual["status"] != "pending" {
					t.Fatalf("malformed pending identity was not exercised: %s", diagnostic)
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

// A running step needs its immutable identity and start clock, not a later
// Jobs API status transition. Execute both routes without sleeps or retries.
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
		routes := []struct{ event, jobHead string }{{"workflow_dispatch", source}}
		if filename == "ci-queue-gate.yml" {
			routes = []struct{ event, jobHead string }{{"pull_request_target", head}, {"workflow_run", source}}
		}
		for _, route := range routes {
			for _, tc := range []struct {
				name, status string
				elapsed      int64
				wantOK       bool
			}{
				{"running", "in_progress", 4, true},
				{"queued status lag", "queued", 4, true},
				{"pending status lag", "pending", 4, true},
				{"pending last work second", "pending", 44, true},
				{"pending work cutoff", "pending", 45, true},
				{"pending last setup second retains original cutoff", "pending", 59, true},
				{"pending expired original clock", "pending", 60, false},
				{"pending future original clock", "pending", -1, false},
				{"last setup second retains original cutoff", "queued", 59, true},
				{"expired original clock", "queued", 60, false},
				{"future original clock", "queued", -1, false},
				{"terminal job", "completed", 4, false},
			} {
				t.Run(filename+"/"+route.event+"/"+tc.name, func(t *testing.T) {
					dir := t.TempDir()
					job := map[string]any{"id": 117143001761, "run_id": 38951286305, "run_attempt": 1, "name": "publish-" + head, "head_sha": route.jobHead, "status": tc.status, "started_at": "2026-10-16T18:57:22Z"}
					body, err := json.Marshal(map[string]any{"total_count": 1, "jobs": []any{job}})
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "response"), body, 0600); err != nil {
						t.Fatal(err)
					}
					files := map[string]string{
						"gh": `#!/bin/sh
set -eu
test "$1" = api
test "$#" = 2
test "$2" = 'repos/Hikyo-Org/Hikyo/actions/runs/38951286305/attempts/1/jobs?per_page=100'
echo call >> "$FIXTURE/calls"
cat "$FIXTURE/response"
`,
						"date":        fmt.Sprintf("#!/bin/sh\nset -eu\ntest \"$1\" = +%%s\necho %d\n", started+tc.elapsed),
						"sleep":       "#!/bin/sh\necho unexpected publisher polling >> \"$FIXTURE/sleeps\"\nexit 99\n",
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
					cmd.WaitDelay = time.Second
					cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "FIXTURE="+dir, "GH_REPO=Hikyo-Org/Hikyo", "GITHUB_RUN_ID=38951286305", "GITHUB_RUN_ATTEMPT=1", "GITHUB_EVENT_NAME="+route.event, "GITHUB_SHA="+source, "HEAD="+head, "GITHUB_ENV="+filepath.Join(dir, "environment"))
					output, runErr := cmd.CombinedOutput()
					calls, err := os.ReadFile(filepath.Join(dir, "calls"))
					if err != nil {
						t.Fatal(err)
					}
					if string(calls) != "call\n" {
						t.Fatalf("bootstrap polled the Jobs API: %s", calls)
					}
					if sleeps, err := os.ReadFile(filepath.Join(dir, "sleeps")); !os.IsNotExist(err) {
						t.Fatalf("bootstrap waited for Jobs API status: %v %s", err, sleeps)
					}
					written, err := os.ReadFile(filepath.Join(dir, "environment"))
					if err != nil {
						t.Fatal(err)
					}
					if !tc.wantOK {
						if runErr == nil || len(written) != 0 {
							t.Fatalf("unsafe clock/status accepted: %v %s %s", runErr, output, written)
						}
						return
					}
					want := fmt.Sprintf("PR_GATE_STARTED_AT=%d\nPR_GATE_DEADLINE=%d\n", started, started+60)
					if runErr != nil || string(written) != want {
						t.Fatalf("original clock changed: %v %s %s", runErr, output, written)
					}
					// Replay the real compile step with a no-op helper. Setup status lag
					// must neither reset work time nor borrow the publication reserve.
					if err := os.MkdirAll(filepath.Join(dir, "scripts", "ci"), 0700); err != nil {
						t.Fatal(err)
					}
					helper := "#!/bin/sh\nset -eu\necho build >> \"$FIXTURE/builds\"\n"
					if err := os.WriteFile(filepath.Join(dir, "scripts", "ci", "build-queue-publisher.sh"), []byte(helper), 0700); err != nil {
						t.Fatal(err)
					}
					compileIndex := 2
					if filename == "ci-queue-worker.yml" {
						compileIndex = 3
					}
					compile := exec.CommandContext(ctx, "bash", "-c", workflow.Jobs["publish"].Steps[compileIndex].Run)
					compile.Dir = dir
					compile.Env = append(append([]string(nil), cmd.Env...), strings.Fields(string(written))...)
					compile.Env = append(compile.Env, "KIND=admission")
					compileOutput, compileErr := compile.CombinedOutput()
					builds, buildErr := os.ReadFile(filepath.Join(dir, "builds"))
					if tc.elapsed < 45 {
						if compileErr != nil || buildErr != nil || string(builds) != "build\n" {
							t.Fatalf("remaining original work budget refused: %v %v %s %s", compileErr, buildErr, compileOutput, builds)
						}
					} else if compileErr == nil || !os.IsNotExist(buildErr) {
						t.Fatalf("exhausted work borrowed publication reserve: %v %v %s %s", compileErr, buildErr, compileOutput, builds)
					}
				})
			}
		}
	}
}
