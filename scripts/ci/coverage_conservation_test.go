package ci_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// The frozen pre-split inventory catches removed commands even when the new
// registry and workflow would agree with each other after a coverage reduction.
func TestFullCandidateConservesBaselineCoverage(t *testing.T) {
	type check struct {
		Name string `json:"name" yaml:"name"`
		Run  string `json:"run" yaml:"run"`
		Uses string `json:"uses" yaml:"uses"`
		If   string `json:"if" yaml:"if"`
	}
	var baseline struct {
		Source  string            `json:"source_commit"`
		Domains []string          `json:"full_domains"`
		Jobs    map[string]string `json:"jobs"`
	}
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "scripts/ci/testdata/full-suite-baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &baseline); err != nil {
		t.Fatal(err)
	}
	if baseline.Source != "e4bf44e5ce66237f58f81bdb965c2e548a6285a1" {
		t.Fatal("coverage inventory lost its measured pre-split source")
	}
	var registry struct {
		Classes map[string][]string `json:"path_classes"`
	}
	data, err = os.ReadFile(filepath.Join(root, "scripts/ci/ci-job-registry.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(registry.Classes["full"], baseline.Domains) {
		t.Fatal("full candidate must preserve all baseline validation domains")
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []check `yaml:"steps"`
		} `yaml:"jobs"`
	}
	data, err = os.ReadFile(filepath.Join(root, ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	for name, expected := range baseline.Jobs {
		job, exists := workflow.Jobs[name]
		if !exists {
			t.Fatalf("baseline validation job %s was removed", name)
		}
		actual := make([]check, 0)
		for _, step := range job.Steps {
			if step.Name == "Validate the immutable caller revision" || step.Name == "Require every validation job" {
				continue
			}
			actual = append(actual, step)
		}
		encoded, err := json.Marshal(actual)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(encoded)
		if hex.EncodeToString(digest[:]) != expected {
			t.Fatalf("%s no longer conserves every baseline check and condition", name)
		}
	}
}
