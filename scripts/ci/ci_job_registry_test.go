package ci_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

type registry struct {
	Version     int                 `json:"version"`
	Workflow    string              `json:"workflow"`
	PathClasses map[string][]string `json:"path_classes"`
	Jobs        map[string]jobRule  `json:"jobs"`
}

type jobRule struct {
	RequiredGate string   `json:"required_gate"`
	PlanJobs     []string `json:"plan_jobs,omitempty"`
}

type workflow struct {
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Uses     string           `yaml:"uses"`
	Needs    yaml.Node        `yaml:"needs"`
	If       string           `yaml:"if"`
	Strategy workflowStrategy `yaml:"strategy"`
	Steps    []struct {
		If string `yaml:"if"`
	} `yaml:"steps"`
}

type workflowStrategy struct {
	Matrix map[string]yaml.Node `yaml:"matrix"`
}

var planReference = regexp.MustCompile(`fromJSON\(needs\.changes\.outputs\.plan\)(?:\['([^']+)'\]|\.([A-Za-z0-9_-]+))`)

func TestCIJobRegistryMatchesWorkflow(t *testing.T) {
	repoRoot := repositoryRoot(t)
	registryPath := filepath.Join(repoRoot, "scripts", "ci", "ci-job-registry.json")

	registryData, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}

	var gotRegistry registry
	if err := json.Unmarshal(registryData, &gotRegistry); err != nil {
		t.Fatalf("parse registry: %v", err)
	}

	workflowData, err := os.ReadFile(filepath.Join(repoRoot, gotRegistry.Workflow))
	if err != nil {
		t.Fatal(err)
	}

	if err := validateRegistry(gotRegistry, workflowData); err != nil {
		t.Fatal(err)
	}
}

func TestCIJobRegistryRejectsWorkflowDrift(t *testing.T) {
	baseRegistry := registry{
		Version:     2,
		Workflow:    ".github/workflows/ci.yml",
		PathClasses: map[string][]string{"full": {"test"}},
		Jobs: map[string]jobRule{
			"changes":     {RequiredGate: "always"},
			"test":        {RequiredGate: "planned", PlanJobs: []string{"test"}},
			"ci-required": {RequiredGate: "aggregate"},
		},
	}
	baseWorkflow := []byte(`jobs:
  changes:
    runs-on: ubuntu-latest
  test:
    needs: changes
    if: ${{ fromJSON(needs.changes.outputs.plan).test }}
    runs-on: ubuntu-latest
  ci-required:
    if: always() && !cancelled()
    needs: [changes, test]
    runs-on: ubuntu-latest
`)
	if err := validateRegistry(baseRegistry, baseWorkflow); err != nil {
		t.Fatalf("valid base fixture was rejected: %v", err)
	}

	tests := map[string]struct {
		mutateRegistry func(registry) registry
		workflow       []byte
	}{
		"added workflow job": {
			workflow: []byte(string(baseWorkflow) + "  future-job:\n    runs-on: ubuntu-latest\n"),
		},
		"removed workflow job": {
			workflow: []byte(`jobs:
  changes:
    runs-on: ubuntu-latest
  ci-required:
    needs: [changes]
    runs-on: ubuntu-latest
`),
		},
		"renamed workflow job": {
			workflow: []byte(`jobs:
  changes:
    runs-on: ubuntu-latest
  tests:
    needs: changes
    runs-on: ubuntu-latest
  ci-required:
    needs: [changes, tests]
    runs-on: ubuntu-latest
`),
		},
		"unregistered gate job": {
			mutateRegistry: func(input registry) registry {
				delete(input.Jobs, "test")
				return input
			},
			workflow: baseWorkflow,
		},
		"plan key gating no required check": {
			mutateRegistry: func(input registry) registry {
				input.Jobs["test"] = jobRule{RequiredGate: "indirect", PlanJobs: []string{"test"}}
				return input
			},
			workflow: []byte(`jobs:
  changes:
    runs-on: ubuntu-latest
  test:
    needs: changes
    if: ${{ fromJSON(needs.changes.outputs.plan).test }}
    runs-on: ubuntu-latest
  ci-required:
    if: always() && !cancelled()
    needs: [changes]
    runs-on: ubuntu-latest
`),
		},
		"step references unknown plan key": {
			workflow: []byte(`jobs:
  changes:
    runs-on: ubuntu-latest
  test:
    needs: changes
    if: ${{ fromJSON(needs.changes.outputs.plan).test }}
    runs-on: ubuntu-latest
  checks:
    needs: changes
    runs-on: ubuntu-latest
    steps:
      - if: ${{ fromJSON(needs.changes.outputs.plan).tset }}
        run: "true"
  ci-required:
    if: always() && !cancelled()
    needs: [changes, checks, test]
    runs-on: ubuntu-latest
`),
			mutateRegistry: func(input registry) registry {
				input.Jobs["checks"] = jobRule{RequiredGate: "always"}
				return input
			},
		},
		"aggregate without cancellation guard": {
			workflow: []byte(`jobs:
  changes:
    runs-on: ubuntu-latest
  test:
    needs: changes
    if: ${{ fromJSON(needs.changes.outputs.plan).test }}
    runs-on: ubuntu-latest
  ci-required:
    if: always()
    needs: [changes, test]
    runs-on: ubuntu-latest
`),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			gotRegistry := cloneRegistry(t, baseRegistry)
			if test.mutateRegistry != nil {
				gotRegistry = test.mutateRegistry(gotRegistry)
			}
			if err := validateRegistry(gotRegistry, test.workflow); err == nil {
				t.Fatal("workflow drift was accepted")
			}
		})
	}
}

func validateRegistry(gotRegistry registry, workflowData []byte) error {
	if gotRegistry.Version != 2 {
		return fmt.Errorf("registry version: got %d, want 2", gotRegistry.Version)
	}
	if gotRegistry.Workflow == "" || filepath.IsAbs(gotRegistry.Workflow) {
		return errors.New("registry workflow must be a repository-relative path")
	}

	var gotWorkflow workflow
	if err := yaml.Unmarshal(workflowData, &gotWorkflow); err != nil {
		return fmt.Errorf("parse workflow YAML: %w", err)
	}

	registryJobs := sortedKeys(gotRegistry.Jobs)
	workflowJobs := sortedKeys(gotWorkflow.Jobs)
	if !reflect.DeepEqual(registryJobs, workflowJobs) {
		return fmt.Errorf("registry/workflow job mismatch: registry=%v workflow=%v", registryJobs, workflowJobs)
	}

	fullJobs, ok := gotRegistry.PathClasses["full"]
	if !ok || len(fullJobs) == 0 {
		return errors.New("registry full path class is empty")
	}
	selectable := make(map[string]bool, len(fullJobs))
	for _, key := range fullJobs {
		selectable[key] = true
	}
	for class, keys := range gotRegistry.PathClasses {
		if len(keys) == 0 {
			return fmt.Errorf("path class %q is empty", class)
		}
		for _, key := range keys {
			if !selectable[key] {
				return fmt.Errorf("path class %q selects plan key %q outside the full plan", class, key)
			}
		}
	}

	for job, rule := range gotRegistry.Jobs {
		condition := gotWorkflow.Jobs[job].If
		switch rule.RequiredGate {
		case "always":
			if len(rule.PlanJobs) != 0 {
				return fmt.Errorf("job %q gate %q cannot name plan jobs", job, rule.RequiredGate)
			}
			if condition != "" {
				return fmt.Errorf("always job %q has condition %q", job, condition)
			}
		case "pull-request":
			if len(rule.PlanJobs) != 0 {
				return fmt.Errorf("job %q gate %q cannot name plan jobs", job, rule.RequiredGate)
			}
			if condition != "github.event_name != 'push'" {
				return fmt.Errorf("pull-request job %q has condition %q", job, condition)
			}
		case "aggregate":
			if len(rule.PlanJobs) != 0 {
				return fmt.Errorf("job %q gate %q cannot name plan jobs", job, rule.RequiredGate)
			}
			if condition != "always() && !cancelled()" {
				return fmt.Errorf("aggregate job %q has condition %q", job, condition)
			}
		case "planned":
			if len(rule.PlanJobs) == 0 {
				return fmt.Errorf("planned job %q has no plan jobs", job)
			}
			for _, planJob := range rule.PlanJobs {
				if !selectable[planJob] {
					return fmt.Errorf("job %q references non-selectable plan job %q", job, planJob)
				}
			}
		case "indirect":
			if len(rule.PlanJobs) == 0 {
				return fmt.Errorf("indirect job %q has no plan jobs", job)
			}
			for _, planJob := range rule.PlanJobs {
				if !selectable[planJob] {
					return fmt.Errorf("job %q references non-selectable plan job %q", job, planJob)
				}
			}
		default:
			return fmt.Errorf("job %q has unknown required gate %q", job, rule.RequiredGate)
		}

		planJobs := workflowPlanJobs(condition)
		wantPlanJobs := make([]string, len(rule.PlanJobs))
		copy(wantPlanJobs, rule.PlanJobs)
		sort.Strings(wantPlanJobs)
		if !reflect.DeepEqual(planJobs, wantPlanJobs) {
			return fmt.Errorf("job %q plan condition mismatch: workflow=%v registry=%v", job, planJobs, wantPlanJobs)
		}
	}

	// Plan keys name validation domains, not jobs. Each must select a check
	// ci-required gates, as a planned job or a step of a directly required job,
	// and a step condition cannot name an unknown key: fromJSON would yield
	// null and skip that check silently.
	gated := make(map[string]bool, len(fullJobs))
	for job, rule := range gotRegistry.Jobs {
		direct := directGate(rule.RequiredGate)
		if direct {
			for _, key := range workflowPlanJobs(gotWorkflow.Jobs[job].If) {
				gated[key] = true
			}
		}
		for _, step := range gotWorkflow.Jobs[job].Steps {
			for _, key := range workflowPlanJobs(step.If) {
				if !selectable[key] {
					return fmt.Errorf("job %q step references non-selectable plan key %q", job, key)
				}
				gated[key] = gated[key] || direct
			}
		}
	}
	for _, key := range fullJobs {
		if !gated[key] {
			return fmt.Errorf("plan key %q selects no check that ci-required gates", key)
		}
	}

	directNeeds, err := workflowNeeds(gotWorkflow.Jobs["ci-required"].Needs)
	if err != nil {
		return err
	}
	wantNeeds := make([]string, 0, len(gotRegistry.Jobs))
	for job, rule := range gotRegistry.Jobs {
		if directGate(rule.RequiredGate) {
			wantNeeds = append(wantNeeds, job)
		}
	}
	sort.Strings(directNeeds)
	sort.Strings(wantNeeds)
	if !reflect.DeepEqual(directNeeds, wantNeeds) {
		return fmt.Errorf("ci-required needs mismatch: workflow=%v registry=%v", directNeeds, wantNeeds)
	}

	for job, shardMatrix := range map[string]string{
		"isolation_shard": "${{ fromJSON(needs.analysis_shards.outputs.shards) }}",
		"race_shard":      "${{ fromJSON(needs.analysis_shards.outputs.race_shards) }}",
		"fuzz_shard":      "${{ fromJSON(needs.analysis_shards.outputs.shards) }}",
	} {
		if _, registered := gotRegistry.Jobs[job]; !registered {
			continue
		}
		shard := gotWorkflow.Jobs[job].Strategy.Matrix["shard"]
		if shard.Kind != yaml.ScalarNode || shard.Value != shardMatrix {
			return fmt.Errorf("job %q shard matrix must be %q", job, shardMatrix)
		}
	}

	return nil
}

func directGate(gate string) bool {
	return gate == "always" || gate == "pull-request" || gate == "planned"
}

func workflowPlanJobs(condition string) []string {
	matches := planReference.FindAllStringSubmatch(condition, -1)
	jobs := make([]string, 0, len(matches))
	for _, match := range matches {
		job := match[1]
		if job == "" {
			job = match[2]
		}
		jobs = append(jobs, job)
	}
	sort.Strings(jobs)
	return jobs
}

func workflowNeeds(node yaml.Node) ([]string, error) {
	switch node.Kind {
	case yaml.SequenceNode:
		needs := make([]string, 0, len(node.Content))
		for _, child := range node.Content {
			needs = append(needs, child.Value)
		}
		return needs, nil
	case yaml.ScalarNode:
		if node.Value == "" {
			return nil, errors.New("ci-required has no needs")
		}
		return []string{node.Value}, nil
	default:
		return nil, fmt.Errorf("ci-required needs has unsupported YAML kind %d", node.Kind)
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func cloneRegistry(t *testing.T, input registry) registry {
	t.Helper()
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var cloned registry
	if err := json.Unmarshal(data, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}

func sortedKeys[Value any](input map[string]Value) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// A tag-side benchmark running independently is useful evidence, but cannot
// gate publication. The release graph must depend on the actual measured job.
func TestReleaseFloorBenchBlocksArtifactConstruction(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var release workflow
	if err := yaml.Unmarshal(raw, &release); err != nil {
		t.Fatal(err)
	}
	if release.Jobs["floor-bench"].Uses != "./.github/workflows/floor-bench.yml" {
		t.Fatal("release must call the actual floor measurement workflow")
	}
	var needs []string
	draft := release.Jobs["build-signed-draft"]
	if err := draft.Needs.Decode(&needs); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, job := range needs {
		if job == "floor-bench" {
			found = true
		}
	}
	if !found {
		t.Fatal("signed draft construction does not wait for the floor gate")
	}
}
