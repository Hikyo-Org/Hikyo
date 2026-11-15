package ci_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type wiringWorkflow struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string            `yaml:"name"`
			Run  string            `yaml:"run"`
			Env  map[string]string `yaml:"env"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

var (
	wiringBuild = regexp.MustCompile(`go build [^\n]*-o "\$RUNNER_TEMP/([A-Za-z0-9_-]+)"[^\n]*\./scripts/ci/pr-gate`)
	wiringSpawn = regexp.MustCompile(`go run [^\n]*\./scripts/ci/pr-gate`)
)

// The gate refuses the stable-release App's pull requests only when it knows the
// App's bot id. A binary that runs without it silently admits what the App's
// contents: write could add, so every step that runs pr-gate must receive both
// repository variables; the client id lets the gate fail closed when the bot id
// is missing.
func TestEveryPRGateInvocationReceivesTheStableReleaseAppConfiguration(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(repositoryRoot(t), ".github", "workflows", "*.yml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflows: %v", err)
	}
	invocations := 0
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var workflow wiringWorkflow
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for jobName, job := range workflow.Jobs {
			// The binaries a job compiles: from go build -o, and the shared publisher build script.
			binaries := map[string]bool{}
			for _, step := range job.Steps {
				for _, m := range wiringBuild.FindAllStringSubmatch(step.Run, -1) {
					binaries[m[1]] = true
				}
				if strings.Contains(step.Run, "build-queue-publisher.sh") {
					binaries["queue-publisher"] = true
				}
			}
			for _, step := range job.Steps {
				runs := wiringSpawn.MatchString(step.Run)
				for name := range binaries {
					if strings.Contains(step.Run, `"$RUNNER_TEMP/`+name+`"`) && !strings.Contains(step.Run, "go build") && !strings.Contains(step.Run, "build-queue-publisher.sh") {
						runs = true
					}
				}
				if !runs {
					continue
				}
				invocations++
				where := filepath.Base(file) + " " + jobName + " " + step.Name
				if step.Env["PR_GATE_STABLE_RELEASE_APP_BOT_ID"] != "${{ vars.STABLE_RELEASE_APP_BOT_ID }}" {
					t.Errorf("%s runs pr-gate without the stable-release App bot id", where)
				}
				if step.Env["PR_GATE_STABLE_RELEASE_APP_CLIENT_ID"] != "${{ vars.STABLE_RELEASE_APP_CLIENT_ID }}" {
					t.Errorf("%s runs pr-gate without the stable-release App client id, so a missing bot id would not fail closed", where)
				}
			}
		}
	}
	// Retirement discovery, two retirement runners, three queue steps, the worker and the dev gate.
	if invocations < 8 {
		t.Fatalf("found only %d pr-gate invocations; the scan no longer sees the workflows", invocations)
	}
}
