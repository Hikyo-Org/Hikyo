package ci_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// workflow_run has write-capable credentials, so its executable source and
// inputs must remain on the trusted default branch, never the triggering PR.
func TestGateReconciliationWorkflowTrust(t *testing.T) {
	var workflow struct {
		On map[string]struct {
			Workflows []string `yaml:"workflows"`
			Types     []string `yaml:"types"`
		} `yaml:"on"`
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Timeout     int               `yaml:"timeout-minutes"`
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				With map[string]string `yaml:"with"`
				Env  map[string]string `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci-gate-reconcile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	trigger, ok := workflow.On["workflow_run"]
	if !ok || len(workflow.On) != 1 || len(trigger.Workflows) != 1 || trigger.Workflows[0] != "fork-ci" || len(trigger.Types) != 1 || trigger.Types[0] != "completed" {
		t.Fatal("callback must only run after completed fork-ci validation")
	}
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		t.Fatal("workflow default must remain contents-read only")
	}
	job, ok := workflow.Jobs["reconcile"]
	if !ok || len(workflow.Jobs) != 1 || job.Timeout <= 0 || job.Timeout > 5 {
		t.Fatal("callback requires one bounded reconciliation job")
	}
	if len(job.Permissions) != 3 || job.Permissions["contents"] != "read" || job.Permissions["actions"] != "write" || job.Permissions["pull-requests"] != "read" {
		t.Fatal("only reconciliation may write Actions; PR and contents access stays read-only")
	}
	if len(job.Steps) != 2 {
		t.Fatal("callback may only check out trusted tooling and invoke reconciliation")
	}
	checkout := job.Steps[0]
	if !strings.HasPrefix(checkout.Uses, "actions/checkout@") || checkout.With["ref"] != "${{ github.event.repository.default_branch }}" || checkout.With["persist-credentials"] != "false" {
		t.Fatal("callback must check out the trusted default branch without retained credentials")
	}
	reconcile := job.Steps[1]
	if reconcile.Run != "./scripts/ci/reconcile-fork-gate.sh" || reconcile.Uses != "" {
		t.Fatal("callback must execute only checked-in trusted reconciliation tooling")
	}
	for key, expected := range map[string]string{
		"GH_TOKEN":           "${{ github.token }}",
		"GH_REPO":            "${{ github.repository }}",
		"SOURCE_RUN_ID":      "${{ github.event.workflow_run.id }}",
		"SOURCE_RUN_ATTEMPT": "${{ github.event.workflow_run.run_attempt }}",
	} {
		if reconcile.Env[key] != expected {
			t.Fatalf("%s must use the GitHub-owned callback identity", key)
		}
	}
	if len(reconcile.Env) != 4 || strings.Contains(string(raw), "secrets.") || strings.Contains(string(raw), "download-artifact") {
		t.Fatal("callback must not consume PR artifacts, secrets or additional executable inputs")
	}
}
