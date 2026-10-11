package ci_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// workflow_run has write-capable credentials, so its executable source and
// inputs must remain on the trusted default branch, never the triggering PR.
func TestGateReconciliationWorkflowTrust(t *testing.T) {
	var workflow struct {
		On          map[string]yaml.Node `yaml:"on"`
		Permissions map[string]string    `yaml:"permissions"`
		Jobs        map[string]struct {
			If          string            `yaml:"if"`
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
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "ci-pr-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	trigger, ok := workflow.On["workflow_run"]
	var callback struct {
		Workflows []string `yaml:"workflows"`
		Types     []string `yaml:"types"`
	}
	if !ok {
		t.Fatal("missing completion-driven callback")
	}
	if err := trigger.Decode(&callback); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(callback.Workflows, []string{"pr-candidate"}) || !slices.Equal(callback.Types, []string{"requested", "in_progress", "completed"}) {
		t.Fatal("callback must invalidate starts/reruns and reassess completed full candidates")
	}
	if len(workflow.Permissions) != 1 || workflow.Permissions["contents"] != "read" {
		t.Fatal("workflow default must remain contents-read only")
	}
	job, ok := workflow.Jobs["publish"]
	if job.If != "github.event_name != 'workflow_run' || !startsWith(github.event.workflow_run.display_title, 'pr-metadata #')" {
		t.Fatal("only metadata-only callbacks may skip reconciliation; useful lifecycle events must run")
	}
	if !ok || len(workflow.Jobs) != 1 || job.Timeout <= 0 || job.Timeout > 10 {
		t.Fatal("callback requires one bounded reconciliation job")
	}
	if len(job.Permissions) != 4 || job.Permissions["contents"] != "read" || job.Permissions["actions"] != "read" || job.Permissions["checks"] != "write" || job.Permissions["pull-requests"] != "read" {
		t.Fatal("only check publication may write; Actions, PR metadata and contents remain read-only")
	}
	if len(job.Steps) != 3 {
		t.Fatal("callback may only check out trusted tooling, prepare Go and invoke reconciliation")
	}
	checkout := job.Steps[0]
	if !strings.HasPrefix(checkout.Uses, "actions/checkout@") || checkout.With["ref"] != "${{ github.event.repository.default_branch }}" || checkout.With["persist-credentials"] != "false" {
		t.Fatal("callback must check out the trusted default branch without retained credentials")
	}
	reconcile := job.Steps[2]
	if reconcile.Run != "go run ./scripts/ci/pr-gate" || reconcile.Uses != "" {
		t.Fatal("callback must execute only checked-in trusted reconciliation tooling")
	}
	for key, expected := range map[string]string{
		"GH_TOKEN":           "${{ github.token }}",
		"GH_REPO":            "${{ github.repository }}",
		"PR_GATE_CHECK_NAME": "ci-required-next",
	} {
		if reconcile.Env[key] != expected {
			t.Fatalf("%s must use the GitHub-owned callback identity", key)
		}
	}
	if len(reconcile.Env) != 3 || strings.Contains(string(raw), "secrets.") || strings.Contains(string(raw), "download-artifact") {
		t.Fatal("callback must not consume PR artifacts, secrets or additional executable inputs")
	}
}
