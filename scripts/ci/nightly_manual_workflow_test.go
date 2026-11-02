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

// TestMainValidationWorkflowPolicyNightlyManual verifies the request boundary,
// dispatch failures and chain-restart-only exception without publishing a release.
func TestMainValidationWorkflowPolicyNightlyManual(t *testing.T) {
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
	if !ok || len(manual.On) != 1 || len(manual.Permissions) != 0 {
		t.Fatal("manual request must retain a single dispatch boundary and no default token permissions")
	}
	var inputs struct {
		Inputs map[string]struct {
			Type     string   `yaml:"type"`
			Required bool     `yaml:"required"`
			Default  string   `yaml:"default"`
			Options  []string `yaml:"options"`
		} `yaml:"inputs"`
	}
	if err := dispatch.Decode(&inputs); err != nil {
		t.Fatal(err)
	}
	mode := inputs.Inputs["source_mode"]
	if len(inputs.Inputs) != 1 || mode.Type != "choice" || !mode.Required || mode.Default != "dev" || !reflect.DeepEqual(mode.Options, []string{"dev", "main-bridge"}) {
		t.Fatal("manual request must default to dev and offer only the explicit v1 bridge")
	}
	job := manual.Jobs["request"]
	if len(manual.Jobs) != 1 || job.If != "github.ref == 'refs/heads/main'" || job.RunsOn != "ubuntu-latest" || job.Timeout.Value != "3" || !reflect.DeepEqual(job.Permissions, map[string]string{"contents": "write"}) || len(job.Steps) != 1 {
		t.Fatal("manual request must be a bounded main-only job with dispatch permissions")
	}
	step := job.Steps[0]
	if step.Uses != "" || !reflect.DeepEqual(step.Env, map[string]string{"GH_TOKEN": "${{ github.token }}", "GH_REPO": "${{ github.repository }}", "SOURCE_MODE": "${{ inputs.source_mode }}"}) {
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
	if !reflect.DeepEqual(events.Types, []string{"nightly-release", "nightly-chain-restart", "nightly-main-bridge"}) {
		t.Fatal("publication must accept ordinary manual requests and deliberate chain restarts")
	}
	predecessorGuards := 0
	for _, name := range []string{"build-artifacts", "publish"} {
		for _, step := range nightly.Jobs[name].Steps {
			if guard, ok := step.Env["ALLOW_MISSING_PREDECESSOR"]; ok {
				predecessorGuards++
				if guard != "${{ github.event_name == 'repository_dispatch' && github.event.action == 'nightly-chain-restart' && github.event.client_payload.allow_missing_predecessor == true }}" {
					t.Fatal("only the explicit chain-restart event may waive the predecessor requirement")
				}
			}
		}
	}
	if predecessorGuards != 3 {
		t.Fatal("predecessor resolution, verification and upgrade proof must retain their guards")
	}
	for _, scenario := range []struct {
		name, mode, event string
		apiFailure        bool
		wantFailure       bool
	}{
		{name: "dev", mode: "dev", event: "nightly-release"},
		{name: "main bridge", mode: "main-bridge", event: "nightly-main-bridge"},
		{name: "API failure", mode: "dev", event: "nightly-release", apiFailure: true, wantFailure: true},
		{name: "invalid mode", mode: "unknown", wantFailure: true},
		{name: "missing mode", wantFailure: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := t.TempDir()
			called := filepath.Join(dir, "called")
			fakeGH := "#!/bin/sh\n" +
				`printf called > "$DISPATCH_CALLED"` + "\n" +
				`test "$GH_TOKEN" = fixture-token && test "$*" = "api --method POST repos/Hikyo-Org/Hikyo/dispatches -f event_type=$EXPECTED_EVENT" || exit 2` + "\n" +
				`if [ "$REJECT_DISPATCH" = true ]; then exit 1; fi` + "\n"
			if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(fakeGH), 0o700); err != nil {
				t.Fatal(err)
			}
			summary := filepath.Join(dir, "summary.md")
			cmd := exec.Command("bash", "-e", "-o", "pipefail", "-c", step.Run)
			cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GH_TOKEN=fixture-token", "GH_REPO=Hikyo-Org/Hikyo", "GITHUB_REPOSITORY=Hikyo-Org/Hikyo", "SOURCE_MODE="+scenario.mode,
				"GITHUB_SERVER_URL=https://github.com", "GITHUB_STEP_SUMMARY="+summary,
				"DISPATCH_CALLED="+called, "EXPECTED_EVENT="+scenario.event,
				"REJECT_DISPATCH="+map[bool]string{false: "false", true: "true"}[scenario.apiFailure])
			out, err := cmd.CombinedOutput()
			if (err != nil) != scenario.wantFailure {
				t.Fatalf("dispatch result: %v %s", err, out)
			}
			_, err = os.Stat(called)
			if scenario.event == "" && !os.IsNotExist(err) {
				t.Fatal("invalid mode must be refused before contacting GitHub")
			}
			body, err := os.ReadFile(summary)
			if scenario.wantFailure {
				if !os.IsNotExist(err) {
					t.Fatal("rejected request must not report success")
				}
			} else if err != nil || !strings.Contains(string(body), "https://github.com/Hikyo-Org/Hikyo/actions/workflows/nightly.yml") {
				t.Fatalf("accepted request must link publication status: %v %s", err, body)
			}
		})
	}
}
