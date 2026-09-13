package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// GitHub's full-commit-SHA policy is stricter than Actionlint's syntax check.
// Decode actual YAML so quoting, flow mappings and anchors cannot hide a pin.
func workflowActionPinErrors(raw []byte) []error {
	var workflow struct {
		Jobs map[string]struct {
			Uses  string `yaml:"uses"`
			Steps []struct {
				Uses string `yaml:"uses"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		return []error{err}
	}
	remote := regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(/[A-Za-z0-9_./-]+)?@[0-9a-fA-F]{40}$`)
	container := regexp.MustCompile(`^docker://[^\s@]+@sha256:[0-9a-f]{64}$`)
	var problems []error
	check := func(location, ref string) {
		if ref == "" || strings.HasPrefix(ref, "./") {
			return
		}
		if remote.MatchString(ref) || container.MatchString(ref) {
			return
		}
		problems = append(problems, fmt.Errorf("%s: uses %q must pin a remote action or workflow to exactly 40 hexadecimal commit characters (container actions require a SHA-256 digest)", location, ref))
	}
	for name, job := range workflow.Jobs {
		check("job "+name, job.Uses)
		for i, step := range job.Steps {
			check(fmt.Sprintf("job %s step %d", name, i+1), step.Uses)
		}
	}
	return problems
}

func TestWorkflowActionPins(t *testing.T) {
	entries, err := os.ReadDir("../../.github/workflows")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		ext := filepath.Ext(entry.Name())
		if entry.IsDir() || (ext != ".yml" && ext != ".yaml") {
			continue
		}
		count++
		t.Run(entry.Name(), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("../../.github/workflows", entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			for _, problem := range workflowActionPinErrors(raw) {
				t.Error(problem)
			}
		})
	}
	if count == 0 {
		t.Fatal("no workflows checked")
	}
}

func TestWorkflowActionPinsFixtures(t *testing.T) {
	sha := "dbcb813823bdd20940b903addbd779551569679f"
	for _, fixture := range []struct {
		name string
		yaml string
		bad  bool
	}{
		{"full SHA", "jobs: {build: {steps: [{uses: 'docker/login-action@" + sha + "'}]}}", false},
		{"41 character incident", "jobs: {build: {steps: [{uses: docker/login-action@" + sha + "d}]}}", true},
		{"39 characters", "jobs: {build: {steps: [{uses: docker/login-action@" + sha[:39] + "}]}}", true},
		{"nonhex", "jobs: {build: {steps: [{uses: docker/login-action@" + sha[:39] + "z}]}}", true},
		{"mutable version", "jobs: {build: {steps: [{uses: docker/login-action@v4.6.0}]}}", true},
		{"mutable branch", "jobs: {build: {steps: [{uses: docker/login-action@main}]}}", true},
		{"remote reusable workflow", "jobs: {build: {uses: org/repo/.github/workflows/build.yml@main}}", true},
		{"pinned reusable workflow", "jobs: {build: {uses: org/repo/.github/workflows/build.yml@" + sha + "}}", false},
		{"local workflow", "jobs: {build: {uses: ./.github/workflows/build.yml}}", false},
		{"local action", "jobs: {build: {steps: [{uses: ./.github/actions/build}]}}", false},
		{"folded scalar", "jobs:\n  build:\n    steps:\n      - uses: >-\n          docker/login-action@" + sha + "\n", false},
		{"alias mutable ref", "ref: &ref docker/login-action@main\njobs: {build: {steps: [{uses: *ref}]}}", true},
		{"alias pinned ref", "ref: &ref docker/login-action@" + sha + "\njobs: {build: {steps: [{uses: *ref}]}}", false},
		{"expression", "jobs: {build: {steps: [{uses: 'org/repo@${{ inputs.ref }}'}]}}", true},
		{"container tag", "jobs: {build: {steps: [{uses: 'docker://alpine:3'}]}}", true},
		{"container digest", "jobs: {build: {steps: [{uses: 'docker://alpine@sha256:" + strings.Repeat("a", 64) + "'}]}}", false},
		{"run content ignored", "jobs: {build: {steps: [{run: 'echo uses: org/repo@main'}]}}", false},
		{"invalid YAML", "jobs: [", true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			problems := workflowActionPinErrors([]byte(fixture.yaml))
			if got := len(problems) > 0; got != fixture.bad {
				t.Fatalf("rejected = %t, want %t: %v", got, fixture.bad, problems)
			}
		})
	}
}
