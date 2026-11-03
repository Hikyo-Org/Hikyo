package ci_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The publisher holds check-write credentials. Pin its executable boundary,
// including absence of a skipped job that could satisfy the required check.
// The required API context must be produced by the independent trusted
// controller, never by an admission-skipped native PR job.
func TestRequiredJobCannotSkipOnPullRequests(t *testing.T) {
	root := repositoryRoot(t)
	for _, name := range []string{"ci-control.yml", "ci-fork.yml", "ci-gate-reconcile.yml", "ci-candidate.yml", "ci-pr-gate.yml"} {
		if _, err := os.Stat(filepath.Join(root, ".github/workflows", name)); !os.IsNotExist(err) {
			t.Fatalf("obsolete automatic producer %s must be retired", name)
		}
	}
	var policy struct {
		Rules []struct {
			Type       string `json:"type"`
			Parameters struct {
				Checks []struct {
					Context string `json:"context"`
				} `json:"required_status_checks"`
			} `json:"parameters"`
		} `json:"rules"`
	}
	raw, err := os.ReadFile(filepath.Join(root, "release/repository/main-ci-gate.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	var contexts []string
	for _, rule := range policy.Rules {
		if rule.Type == "required_status_checks" {
			for _, c := range rule.Parameters.Checks {
				contexts = append(contexts, c.Context)
			}
		}
	}
	if !slices.Equal(contexts, []string{"ci-queue-required"}) {
		t.Fatal("canonical policy must require the trusted queue producer")
	}
	queueRaw, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-queue-gate.yml"))
	if err != nil {
		t.Fatal(err)
	}
	// The canonical context is now published by direct expected-head jobs and
	// dispatched head workers. Reuse their actual-source trust contracts instead
	// of tying merge eligibility to the former unbounded go-run command.
	if err := queuePublisherContract(queueRaw); err != nil {
		t.Fatalf("canonical queue context must match trusted direct writers and read-only discovery: %v", err)
	}
	workerRaw, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-queue-worker.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := queueWorkerContract(workerRaw); err != nil {
		t.Fatalf("canonical queue context must match trusted expected-head workers: %v", err)
	}

	// GitHub required contexts have a name/app identity, not workflow identity.
	// This prevents accidental native collisions; actual hostile collisions are
	// refused with owned pending barriers by the controller's runtime tests.
	entries, err := os.ReadDir(filepath.Join(root, ".github/workflows"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(root, ".github/workflows", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var w struct {
			Jobs map[string]struct {
				Name string `yaml:"name"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(content, &w); err != nil {
			t.Fatal(err)
		}
		for id, j := range w.Jobs {
			if id == contexts[0] || j.Name == contexts[0] || id == "ci-required-next" || j.Name == "ci-required-next" {
				t.Fatalf("native workflow %s must not publish the required PR context", entry.Name())
			}
		}
	}
}
