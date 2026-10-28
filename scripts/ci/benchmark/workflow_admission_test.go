package benchmark

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestWorkflowAdmissionRefusals(t *testing.T) {
	for _, automatic := range []bool{true, false} {
		for _, workflow := range []string{"pr-benchmark.yml", "matrix-performance.yml"} {
			for _, failure := range []string{"mismatch", "missing trusted SHA", "missing candidate SHA", "API failure", "invalid JSON"} {
				t.Run(fmt.Sprintf("automatic=%t/%s/%s", automatic, workflow, failure), func(t *testing.T) {
					head := strings.Repeat("a", 40)
					pr := pull{Number: 7, State: "open", ChangedFiles: 1}
					pr.Head.SHA = head
					pr.Head.Repo.FullName = "o/r"
					c := testComment(t, head, request{head: head})
					if !automatic {
						c.Body = strings.Replace(c.Body, "- [ ] Run benchmark", checkbox, 1)
					}
					r := benchmarkRun{ID: 10, Head: head, Title: "pr-benchmark #7", Event: "pull_request", Status: "completed", Conclusion: "success", Attempt: 1}
					e := controlEvent{Action: "edited", Comment: c, WorkflowRun: r}
					e.Issue.Number = pr.Number
					e.Issue.Pull = &struct {
						URL string `json:"url"`
					}{URL: "https://api.github.com/repos/o/r/pulls/7"}
					e.Sender.Login = "maintainer"
					e.Changes.Body = &struct {
						From string `json:"from"`
					}{From: strings.Replace(c.Body, checkbox, "- [ ] Run benchmark", 1)}
					responses := map[string]string{
						"repos/o/r/collaborators/maintainer/permission": `{"permission":"write"}`,
						"repos/o/r/actions/runs/10":                     encode(t, r),
						"repos/o/r/pulls/7":                             encode(t, pr),
						"repos/o/r/pulls/7/files?per_page=100":          `[[{"filename":"internal/service/values.go"}]]`,
						"repos/o/r/issues/7/comments?per_page=100":      encode(t, [][]comment{{c}}),
						"repos/o/r/issues/comments/20":                  `{}`,
					}
					for _, name := range []string{"pr-benchmark.yml", "matrix-performance.yml"} {
						path := "repos/o/r/contents/.github/workflows/" + name + "?ref="
						responses[path+"main"] = `{"sha":"trusted"}`
						responses[path+head] = `{"sha":"trusted"}`
						if name == workflow {
							switch failure {
							case "mismatch":
								responses[path+head] = `{"sha":"different"}`
							case "missing trusted SHA":
								responses[path+"main"] = `{}`
							case "missing candidate SHA":
								responses[path+head] = `{}`
							case "API failure":
								delete(responses, path+head)
							case "invalid JSON":
								responses[path+head] = `{`
							}
						}
					}
					log := fakeGH(t, responses)
					err := runBenchmark("o/r", e, automatic)
					wantSkip := automatic && failure == "mismatch"
					if (err == nil) != wantSkip {
						t.Fatalf("expected skip=%t, got error=%v", wantSkip, err)
					}
					raw, err := os.ReadFile(log)
					if err != nil {
						t.Fatal(err)
					}
					calls := string(raw)
					if strings.Contains(calls, "--method POST") || strings.Contains(calls, "workflows/codspeed.yml/runs") {
						t.Fatal("refused workflow must not reserve budget or trigger a measurement")
					}
					status := "Benchmark declined:"
					if wantSkip {
						status = "Automatic benchmark **not run**:"
					}
					if !strings.Contains(calls, "--method PATCH") || !strings.Contains(calls, status) || !strings.Contains(calls, workflow) {
						t.Fatalf("missing workflow refusal status:\n%s", calls)
					}
				})
			}
		}
	}
}
