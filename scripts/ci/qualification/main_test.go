package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanningEnvironmentReplacesAmbientPlatform(t *testing.T) {
	actual := withEnvironment([]string{"PATH=/tools", "GOOS=darwin", "GOARCH=arm64", "CGO_ENABLED=0"}, map[string]string{"GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "1"})
	joined := strings.Join(actual, ",")
	for _, expected := range []string{"PATH=/tools", "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=1"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing planner environment %s: %s", expected, joined)
		}
	}
	if strings.Count(joined, "GOOS=") != 1 || strings.Contains(joined, "darwin") || strings.Contains(joined, "arm64") {
		t.Fatalf("ambient platform leaked into planning: %s", joined)
	}
}

func TestRegistryRefusesMissingAndUnregisteredJobs(t *testing.T) {
	registry := map[string]json.RawMessage{"test": nil, "race": nil}
	for _, graph := range []map[string]job{
		{"test": {}},
		{"test": {}, "race": {}, "new-unmapped-job": {}},
		{"test": {}, "renamed-race": {}},
	} {
		if err := checkRegistry(registry, graph); err == nil {
			t.Fatalf("workflow drift accepted: %+v", graph)
		}
	}
}

func TestPlanBindingRequiresSeparateIsolationRace(t *testing.T) {
	i := &inventory{Criteria: []criterion{{ID: "C-ACC", Tests: []testReference{{Name: "TestAccessLifecycle", Package: "./internal/isolation"}}}}, Plans: map[string]json.RawMessage{
		"ci-main":          json.RawMessage(`{"isolation_plan":[["TestAccessLifecycle"]],"race_plan":[["./api"]]}`),
		"reliability-race": json.RawMessage(`[]`),
	}}
	if err := bindPlans(i); err == nil {
		t.Fatal("ordinary isolation accepted as full race proof")
	}
	i.Criteria[0].Tests[0].ExecutionJobs = nil
	i.Plans["reliability-race"] = json.RawMessage(`[{"job":"race-isolation (0)","targets":["TestAccessLifecycle"]}]`)
	if err := bindPlans(i); err != nil {
		t.Fatal(err)
	}
	if i.Criteria[0].Tests[0].IsolationRaceJob == "" {
		t.Fatal("race job binding lost")
	}
}

func TestPlanBindingHonorsSplitPackageFilters(t *testing.T) {
	i := &inventory{Criteria: []criterion{{ID: "O1", Tests: []testReference{{Name: "TestSelected", Package: "./internal/app"}}}}, Plans: map[string]json.RawMessage{
		"ci-main":          json.RawMessage(`{"isolation_plan":[],"race_plan":[["github.com/Hikyo-Org/hikyo/internal/app\t^(TestOther)$"],["github.com/Hikyo-Org/hikyo/internal/app\t^(TestSelected)$"]]}`),
		"reliability-race": json.RawMessage(`[]`),
	}}
	if err := bindPlans(i); err != nil {
		t.Fatal(err)
	}
	jobs := strings.Join(i.Criteria[0].Tests[0].ExecutionJobs, ",")
	if jobs != "test_core,race shard (1)" {
		t.Fatalf("filtered package ownership wrong: %s", jobs)
	}
}

func TestPlanBindingKeepsTaggedOperatorEvidenceSeparate(t *testing.T) {
	r := reference("TestK8sOperator", "internal/isolation/k8s_operator_e2e_test.go", []byte("//go:build k8se2e\n"))
	i := &inventory{Criteria: []criterion{{Tests: []testReference{r}}}, Plans: map[string]json.RawMessage{"ci-main": json.RawMessage(`{}`), "reliability-race": json.RawMessage(`[]`)}}
	if err := bindPlans(i); err != nil {
		t.Fatal(err)
	}
	actual := i.Criteria[0].Tests[0]
	if strings.Join(actual.ExecutionJobs, ",") != "k8s-e2e" || actual.IsolationRaceJob != "" || actual.DefaultRaceExclusion == "" {
		t.Fatalf("tagged evidence misattributed: %+v", actual)
	}
}

func TestRepositoryPreparation(t *testing.T) {
	root := "../../.."
	i, err := prepare(root)
	if err != nil {
		t.Fatal(err)
	}
	if i.Phase != "PREPARATION" || i.Verdict != "HOLD" {
		t.Fatalf("preparation promoted: %s/%s", i.Phase, i.Verdict)
	}
	if i.APIRevision < 12 {
		t.Fatalf("source API inventory is stale: %d", i.APIRevision)
	}
	for _, engine := range []string{"sqlite", "postgres"} {
		m := i.Migrations[engine]
		if m.Head < "00079_" || len(m.Files) == 0 {
			t.Fatalf("missing current %s migration inventory: %+v", engine, m)
		}
	}
	acc := false
	for _, c := range i.Criteria {
		if c.Status != "NOTRUN" {
			t.Fatalf("historical result imported: %+v", c)
		}
		if c.ID == "C-ACC" {
			acc = true
			if len(c.Tests) < 3 {
				t.Fatal("C-ACC missing executable references")
			}
		}
	}
	if !acc {
		t.Fatal("C-ACC missing")
	}
	for _, g := range i.Specification.Gates {
		if g.Status != "NOTRUN" {
			t.Fatalf("gate promoted: %+v", g)
		}
	}
	if len(i.Workflows[".github/workflows/ci.yml"]) < 20 {
		t.Fatal("full graph omitted")
	}
	if len(i.Workflows[".github/workflows/release-acceptance.yml"]["floor-backup-restore"].Artifacts) == 0 {
		t.Fatal("recovery artifact requirements omitted")
	}
	if len(i.Inputs["internal/store/migrations/sqlite/00079_remote_pairing.sql"]) != 64 {
		t.Fatal("migration bytes not fingerprinted")
	}
	var output bytes.Buffer
	if err := run([]string{"--root", root}, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "INTERNAL-PASS") || strings.Contains(output.String(), "working_tree_path") {
		t.Fatal("historical result or machine path leaked")
	}
}

// The source closure check must fail on the same missing named criterion that
// previously escaped the historical ledger's numeric-only extraction.
func TestCriteriaRefusesMissingNamedCriterion(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/release/acceptance-1.0.md")
	if err != nil {
		t.Fatal(err)
	}
	var edited []string
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "| C-ACC |") {
			edited = append(edited, line)
		}
	}
	_, err = criteria(overriddenRead("docs/release/acceptance-1.0.md", []byte(strings.Join(edited, "\n"))))
	if err == nil || !strings.Contains(err.Error(), "missing owning criterion C-ACC") {
		t.Fatalf("missing C-ACC accepted: %v", err)
	}
}

func TestCriteriaRefusesStaleTestReference(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/release/acceptance-1.0.md")
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "[TestAccessLifecycle]", "[TestAccessLifecycleDeleted]", 1)
	_, err = criteria(overriddenRead("docs/release/acceptance-1.0.md", []byte(edited)))
	if err == nil || !strings.Contains(err.Error(), "missing test TestAccessLifecycleDeleted") {
		t.Fatalf("stale symbol accepted: %v", err)
	}
}

func TestCriteriaRefusesDuplicate(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/release/acceptance-1.0.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "| C-ACC |") {
			raw = append(raw, []byte("\n"+line+"\n")...)
			break
		}
	}
	_, err = criteria(overriddenRead("docs/release/acceptance-1.0.md", raw))
	if err == nil || !strings.Contains(err.Error(), "duplicate/unowned") {
		t.Fatalf("duplicate accepted: %v", err)
	}
}

func overriddenRead(path string, replacement []byte) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if p == path {
			return replacement, nil
		}
		return os.ReadFile(filepath.Join("../../..", p))
	}
}
