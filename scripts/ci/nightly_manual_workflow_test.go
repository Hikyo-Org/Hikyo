package ci_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestNightlyManualWorkflow(t *testing.T) {
	read := func(name string) queueWorkflowContract {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", name))
		if err != nil {
			t.Fatal(err)
		}
		var workflow queueWorkflowContract
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatal(err)
		}
		return workflow
	}
	manual, nightly := read("nightly-manual.yml"), read("nightly.yml")
	dispatch, ok := manual.On["workflow_dispatch"]
	if !ok || len(manual.On) != 1 || dispatch.Tag != "!!null" || len(manual.Permissions) != 0 {
		t.Fatal("manual request must have no inputs and no default token permissions")
	}
	job := manual.Jobs["request"]
	if len(manual.Jobs) != 1 || job.If != "github.ref == 'refs/heads/main'" || job.RunsOn != "ubuntu-latest" || job.Timeout.Value != "3" || !reflect.DeepEqual(job.Permissions, map[string]string{"contents": "write"}) || len(job.Steps) != 1 {
		t.Fatal("manual request must be a bounded main-only job with dispatch permissions")
	}
	step := job.Steps[0]
	if step.Uses != "" || !reflect.DeepEqual(step.Env, map[string]string{"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}"}) {
		t.Fatal("manual request must use only the built-in token without checkout or release credentials")
	}
	if _, ok := nightly.On["workflow_dispatch"]; ok {
		t.Fatal("publication must not expose a branch-selected manual workflow")
	}
	var events struct {
		Types []string `yaml:"types"`
	}
	repositoryDispatch := nightly.On["repository_dispatch"]
	if err := repositoryDispatch.Decode(&events); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events.Types, []string{"nightly-release", "nightly-chain-restart"}) {
		t.Fatal("publication must accept ordinary manual requests and deliberate chain restarts")
	}
	predecessorGuards := 0
	for _, step := range nightly.Jobs["publish"].Steps {
		if guard, ok := step.Env["ALLOW_MISSING_PREDECESSOR"]; ok {
			predecessorGuards++
			if guard != "${{ github.event_name == 'repository_dispatch' && github.event.action == 'nightly-chain-restart' && github.event.client_payload.allow_missing_predecessor == true }}" {
				t.Fatal("only the explicit chain-restart event may waive the predecessor requirement")
			}
		}
	}
	if predecessorGuards != 3 {
		t.Fatal("predecessor resolution, verification and upgrade proof must retain their guards")
	}
	for _, rejected := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "API failure"}[rejected], func(t *testing.T) {
			dir := t.TempDir()
			fakeGH := "#!/bin/sh\n" +
				`test "$GH_TOKEN" = fixture-token && test "$*" = 'api --method POST repos/Hikyo-Org/Hikyo/dispatches -f event_type=nightly-release' || exit 2` + "\n" +
				`if [ "$REJECT_DISPATCH" = true ]; then exit 1; fi` + "\n"
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(fakeGH), 0o700); err != nil {
				t.Fatal(err)
			}
			summary := filepath.Join(dir, "summary.md")
			cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", step.Run)
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GH_TOKEN=fixture-token", "GH_REPO=Hikyo-Org/Hikyo", "GITHUB_REPOSITORY=Hikyo-Org/Hikyo",
				"GITHUB_SERVER_URL=https://github.com", "GITHUB_STEP_SUMMARY="+summary,
				"REJECT_DISPATCH="+map[bool]string{false: "false", true: "true"}[rejected])
			out, err := cmd.CombinedOutput()
			if (err != nil) != rejected {
				t.Fatalf("dispatch result: %v %s", err, out)
			}
			body, err := os.ReadFile(summary)
			if rejected {
				if !os.IsNotExist(err) {
					t.Fatal("rejected request must not report success")
				}
			} else if err != nil || !strings.Contains(string(body), "https://github.com/Hikyo-Org/Hikyo/actions/workflows/nightly.yml") {
				t.Fatalf("accepted request must link publication status: %v %s", err, body)
			}
		})
	}
}
