package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestReporterWorkflowExecutesOnlyTrustedTooling(t *testing.T) {
	raw, err := os.ReadFile("../../../.github/workflows/ci-queue-report.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On map[string]struct {
			Workflows []string `yaml:"workflows"`
			Types     []string `yaml:"types"`
		} `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Concurrency struct {
			Group  string `yaml:"group"`
			Cancel bool   `yaml:"cancel-in-progress"`
			Queue  string `yaml:"queue"`
		} `yaml:"concurrency"`
		Jobs map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				With map[string]string `yaml:"with"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	trigger := workflow.On["workflow_run"]
	if len(workflow.On) != 1 || !slices.Equal(trigger.Workflows, []string{"trusted-merge-ci"}) || !slices.Equal(trigger.Types, []string{"requested", "in_progress", "completed"}) {
		t.Fatal("reporter must track only trusted merge workflow lifecycle")
	}
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		t.Fatal("global permission must remain contents read")
	}
	if workflow.Concurrency.Group != "merge-queue-report-${{ github.event.workflow_run.id }}-${{ github.event.workflow_run.run_attempt }}" || workflow.Concurrency.Cancel || workflow.Concurrency.Queue != "max" {
		t.Fatal("run/attempt publication must be serialized without dropping events")
	}
	if len(workflow.Jobs) != 1 {
		t.Fatal("unexpected executable reporter jobs")
	}
	j := workflow.Jobs["report"]
	if len(j.Permissions) != 4 || j.Permissions["actions"] != "read" || j.Permissions["checks"] != "write" || j.Permissions["contents"] != "read" || j.Permissions["pull-requests"] != "read" {
		t.Fatal("reporter permission boundary changed")
	}
	if len(j.Steps) != 3 {
		t.Fatal("unexpected executable steps in write-token job")
	}
	checkout, setup, publish := j.Steps[0], j.Steps[1], j.Steps[2]
	if !strings.HasPrefix(checkout.Uses, "actions/checkout@") || checkout.With["ref"] != "${{ github.event.repository.default_branch }}" || checkout.With["persist-credentials"] != "false" {
		t.Fatal("reporter must check out only trusted default branch without persisting credentials")
	}
	if !strings.HasPrefix(setup.Uses, "actions/setup-go@") || setup.With["cache"] != "false" || setup.With["go-version-file"] != "go.mod" {
		t.Fatal("reporter must use trusted Go toolchain without cache")
	}
	if publish.Run != "go run ./scripts/ci/queue-report" || publish.Uses != "" || publish.Env["GH_TOKEN"] != "${{ github.token }}" || publish.Env["GH_REPO"] != "${{ github.repository }}" {
		t.Fatal("reporter must run only the trusted Go publisher")
	}
	for _, forbidden := range []string{"download-artifact", "secrets.", "statuses: write", "go run ./scripts/ci/pr-gate --queue-report"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("reporter must retain one trusted check publisher without external inputs: %s", forbidden)
		}
	}
}
