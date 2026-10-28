package benchmark

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAffectedWorkloads(t *testing.T) {
	for _, test := range []struct {
		path string
		want workloads
	}{
		{"web/src/routes/Matrix.tsx", browserMatrix},
		{"web/pnpm-lock.yaml", browserMatrix},
		{"clients/ts/src/generated/client.gen.ts", browserMatrix},
		{".nvmrc", browserMatrix},
		{"web/src/routes/Matrix.test.tsx", 0},
		{"web/src/routes/Matrix.stories.tsx", 0},
		{"docs/handoff/performance.md", 0},
		{"internal/service/values.go", workflowReadsWrites | deliveryPolling | routerAdmission},
		{"internal/service/service.go", workflowReadsWrites | deliveryPolling | routerAdmission},
		{"internal/scanning/rules.toml", workflowReadsWrites | secretScanning},
		{"internal/server/admission.go", routerAdmission},
		{"internal/crypto/envelope.go", allGoWorkloads},
		{"internal/store/queries/values.sql", allGoWorkloads},
		{"internal/buildcompat/development.json", allGoWorkloads},
		{"internal/new-shared-owner/input.json", allGoWorkloads},
		{"go.mod", allGoWorkloads},
		{"api/openapi.yaml", allPRWorkloads},
		{"scripts/ci/benchmark/selection.go", allPRWorkloads},
		{"scripts/ci/codspeed-verify/main.go", allPRWorkloads},
		{".github/workflows/matrix-performance.yml", browserMatrix},
		{".github/workflows/pr-benchmark.yml", allPRWorkloads},
	} {
		t.Run(test.path, func(t *testing.T) {
			if got := pathWorkloads(test.path); got != test.want {
				t.Fatalf("selected %s (%d), want %s (%d)", got.description(), got, test.want.description(), test.want)
			}
		})
	}
}

func TestChangedWorkloadsUnionsPagesAndRenames(t *testing.T) {
	fakeGH(t, map[string]string{
		"repos/o/r/pulls/7/files?per_page=100": `[[{"filename":"web/src/Matrix.tsx"}],[{"filename":"docs/deleted.txt","previous_filename":"internal/service/values.go"}]]`,
	})
	got, err := changedWorkloads("o/r", pull{Number: 7, ChangedFiles: 2})
	if err != nil || got != browserMatrix|workflowReadsWrites|deliveryPolling|routerAdmission {
		t.Fatalf("selection=%d error=%v", got, err)
	}
}

func TestWorkloadCommandsExcludeRoutineMicrobenchmarks(t *testing.T) {
	command := allPRWorkloads.command()
	for _, excluded := range []string{"BenchmarkEnvelope", "BenchmarkParse", "BenchmarkEncode", "BenchmarkAckSetMaximum", "BenchmarkCataloguePresence", "BenchmarkAdvisoryFanout", "-bench=."} {
		if strings.Contains(command, excluded) {
			t.Fatalf("routine PR command includes diagnostic %s", excluded)
		}
	}
	if browserMatrix.goMeasurement() || browserMatrix.command() != "" {
		t.Fatal("browser-only changes must not allocate a Go measurement")
	}
	if allGoWorkloads.browserMeasurement() {
		t.Fatal("Go-only workloads must not claim browser coverage")
	}
	if !strings.Contains(deliveryPolling.command(), "/^(typical|render-limit)$/^full$/") {
		t.Fatal("delivery PR selection lost its representative fixture bounds")
	}
	if !strings.Contains(workloads(0).command(), "-bench=.") || !workloads(0).browserMeasurement() {
		t.Fatal("pending legacy authorizations must preserve their admitted scope")
	}
}

func TestWorkloadAuthorizationRoundTrip(t *testing.T) {
	head := strings.Repeat("a", 40)
	for _, selected := range []workloads{0, browserMatrix, workflowReadsWrites | deliveryPolling, allPRWorkloads} {
		want := request{head: head, run: 10, attempt: 2, workloads: selected}
		body := commentBody(pull{Number: 7}, want, "Queued.")
		got, err := parseRequest(body)
		if err != nil || got != want {
			t.Fatalf("request=%+v error=%v, want %+v", got, err, want)
		}
	}
	legacy := fmt.Sprintf("<!-- hikyo-benchmark head=%s run=10 attempt=2 -->\n", head)
	if got, err := parseRequest(legacy); err != nil || got.workloads != 0 {
		t.Fatalf("legacy request=%+v error=%v", got, err)
	}
	for _, selection := range []string{"32", "255", "99999999999", "-1", "16\ncommand=evil"} {
		body := fmt.Sprintf("<!-- hikyo-benchmark head=%s run=10 attempt=2 workloads=%s -->\n", head, selection)
		if _, err := parseRequest(body); err == nil {
			t.Fatalf("accepted invalid workload selection %q", selection)
		}
	}
}

func TestVerifyEmitsOnlyAuthorizedWorkloads(t *testing.T) {
	head := strings.Repeat("a", 40)
	c := testComment(t, head, request{head: head, run: 10, attempt: 2, workloads: browserMatrix})
	fakeGH(t, map[string]string{
		"repos/o/r/pulls/7":                        `{"number":7,"state":"open","head":{"sha":"` + head + `"}}`,
		"repos/o/r/issues/7/comments?per_page=100": encode(t, [][]comment{{c}}),
	})
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_RUN_ID", "10")
	t.Setenv("GITHUB_RUN_ATTEMPT", "2")
	output := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(output, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_OUTPUT", output)
	e := controlEvent{}
	e.Pull.Number, e.Pull.Head.SHA = 7, head
	if err := verifyRequest("o/r", e); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "allowed=true\ngo=false\nbrowser=true\n") || strings.Contains(string(raw), "go test") {
		t.Fatalf("unexpected browser-only outputs: %s", raw)
	}
}

func TestCompletionRequiresEverySelectedWorkload(t *testing.T) {
	r := benchmarkRun{ID: 10, Attempt: 2}
	macro := job{ID: 1, Name: "Requested PR walltime", Labels: []string{macroLabel}, Status: "completed", Conclusion: "success", RunnerName: "macro"}
	browser := job{ID: 2, Name: "Requested PR browser performance / Large matrix browser performance", Labels: []string{"ubuntu-latest"}, Status: "completed", Conclusion: "success", RunnerName: "hosted"}
	for _, test := range []struct {
		name     string
		selected workloads
		jobs     []job
		ok       bool
	}{
		{"browser-only needs no Macro", browserMatrix, []job{browser}, true},
		{"Go-only needs no browser", workflowReadsWrites, []job{macro}, true},
		{"mixed requires both", allPRWorkloads, []job{macro, browser}, true},
		{"missing browser is not success", allPRWorkloads, []job{macro}, false},
		{"missing Go is not success", allPRWorkloads, []job{browser}, false},
		{"skipped is not measured", browserMatrix, nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			fakeGH(t, map[string]string{"repos/o/r/actions/runs/10/attempts/2/jobs?per_page=100": encode(t, []map[string][]job{{"jobs": test.jobs}})})
			if err := completedMeasurement("o/r", r, test.selected); (err == nil) != test.ok {
				t.Fatalf("completion error=%v, want success=%t", err, test.ok)
			}
		})
	}
}
