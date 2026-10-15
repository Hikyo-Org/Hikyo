package ci_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestQueueReportWorkflowTrustAndWriterLease(t *testing.T) {
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
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci-queue-report.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	trigger := workflow.On["workflow_run"]
	if len(workflow.On) != 1 || !slices.Equal(trigger.Workflows, []string{"trusted-merge-ci"}) || !slices.Equal(trigger.Types, []string{"requested", "in_progress", "completed"}) {
		t.Fatal("report only trusted queue workflow progress and completion")
	}
	// All rebuilt groups, reruns and PRs in this repository share the status
	// writer. A branch/run/attempt key permits an old-base POST to overtake the
	// new-base writer after its final read, despite correct freshness checks.
	if workflow.Concurrency.Group != "queue-report-${{ github.repository_id }}" || workflow.Concurrency.Cancel || workflow.Concurrency.Queue != "max" {
		t.Fatal("queue reports require one non-cancelling repository writer across rebuilt groups")
	}
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		t.Fatal("default permissions must be read-only")
	}
	job, ok := workflow.Jobs["report"]
	if !ok || len(workflow.Jobs) != 1 || len(job.Permissions) != 4 || job.Permissions["statuses"] != "write" || job.Permissions["actions"] != "read" || job.Permissions["contents"] != "read" || job.Permissions["pull-requests"] != "read" {
		t.Fatal("reporter may write only advisory commit statuses")
	}
	if len(job.Steps) != 3 || !strings.HasPrefix(job.Steps[0].Uses, "actions/checkout@") || job.Steps[0].With["ref"] != "${{ github.event.repository.default_branch }}" || job.Steps[0].With["persist-credentials"] != "false" {
		t.Fatal("only trusted default-branch tooling may execute with status-write credentials")
	}
	invoke := job.Steps[2]
	if invoke.Run != "go run ./scripts/ci/pr-gate --queue-report" || len(invoke.Env) != 2 || invoke.Env["GH_TOKEN"] != "${{ github.token }}" || invoke.Env["GH_REPO"] != "${{ github.repository }}" {
		t.Fatal("reporter must use the isolated advisory role with authenticated callback metadata")
	}
	if strings.Contains(string(raw), "download-artifact") || strings.Contains(string(raw), "secrets.") {
		t.Fatal("reporting must not execute artifacts or use external secrets")
	}
}
