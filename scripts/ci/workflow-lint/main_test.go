package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestConcurrencyQueueValidation(t *testing.T) {
	for _, test := range []struct {
		name, block string
		bad         bool
	}{
		{"queue max", "  group: measurements\n  cancel-in-progress: false\n  queue: max", false},
		{"queue single", "  group: measurements\n  cancel-in-progress: true\n  queue: single", false},
		{"unknown queue", "  group: measurements\n  queue: all", true},
		{"invalid type", "  group: measurements\n  queue: true", true},
		{"conflicting cancellation", "  group: measurements\n  queue: max\n  cancel-in-progress: true", true},
		{"conditional cancellation rejected", "  group: measurements\n  queue: max\n  cancel-in-progress: '${{ github.ref }}'", true},
		{"duplicate queue", "  group: measurements\n  queue: max\n  queue: single", true},
		{"flow cannot hide another key", "  {group: measurements, queue: max, typo: true}", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte("on: push\nconcurrency:\n" + test.block + "\njobs: {}\n")
			normalized, err := validateQueues(raw)
			if (err != nil) != test.bad {
				t.Fatalf("error=%v bad=%t", err, test.bad)
			}
			if err == nil && strings.Count(string(normalized), "\n") != strings.Count(string(raw), "\n") {
				t.Fatal("line numbers must be preserved")
			}
		})
	}
}

func TestQueueExtensionPreservesOtherLintInput(t *testing.T) {
	raw := []byte("on: push\njobs:\n  test:\n    concurrency:\n      group: test\n      queue: max\n      typo: true\n    runs-on: invalid-runner\n    steps:\n      - run: echo queue:max\n")
	normalized, err := validateQueues(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(normalized), "queue: max") {
		t.Fatal("validated unsupported key should be removed for old parser")
	}
	for _, s := range []string{"typo: true", "invalid-runner", "echo queue:max"} {
		if !strings.Contains(string(normalized), s) {
			t.Fatalf("other checks lost %q", s)
		}
	}
}

func TestAlertsPermissionValidation(t *testing.T) {
	for _, test := range []struct {
		name, doc string
		bad       bool
	}{
		{"workflow read", "on: push\npermissions:\n  contents: read\n  vulnerability-alerts: read\njobs: {}\n", false},
		{"job none", "on: push\njobs:\n  a:\n    permissions:\n      vulnerability-alerts: none\n      contents: read\n", false},
		{"job read with comment", "on: push\njobs:\n  a:\n    permissions:\n      vulnerability-alerts: read # alerts\n      contents: read\n", false},
		{"write is refused", "on: push\npermissions:\n  vulnerability-alerts: write\njobs: {}\n", true},
		{"job write is refused", "on: push\njobs:\n  a:\n    permissions:\n      vulnerability-alerts: write\n", true},
		{"an expression is refused", "on: push\npermissions:\n  vulnerability-alerts: ${{ inputs.level }}\njobs: {}\n", true},
		{"a quoted value is refused", "on: push\npermissions:\n  vulnerability-alerts: 'read'\njobs: {}\n", true},
		{"an empty value is refused", "on: push\npermissions:\n  vulnerability-alerts:\njobs: {}\n", true},
		{"duplicate is refused", "on: push\npermissions:\n  vulnerability-alerts: read\n  vulnerability-alerts: write\njobs: {}\n", true},
		{"flow cannot hide another scope", "on: push\npermissions: {contents: write, vulnerability-alerts: read}\njobs: {}\n", true},
		{"an anchored block is refused", "on: push\npermissions: &p\n  vulnerability-alerts: read\njobs:\n  a:\n    outputs: *p\n", true},
		{"an anchored job block is refused", "on: push\njobs:\n  a:\n    permissions: &p\n      contents: read\n      vulnerability-alerts: read\n", true},
		{"other places are not permissions", "on: push\nenv:\n  vulnerability-alerts: write\njobs: {}\n", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw := []byte(test.doc)
			normalized, err := validateAlertsPermission(raw)
			if (err != nil) != test.bad {
				t.Fatalf("error=%v bad=%t", err, test.bad)
			}
			if err != nil {
				return
			}
			if strings.Count(string(normalized), "\n") != strings.Count(string(raw), "\n") {
				t.Fatal("line numbers must be preserved")
			}
			if strings.Contains(string(normalized), "vulnerability-alerts: read") || strings.Contains(string(normalized), "vulnerability-alerts: none") {
				t.Fatal("the validated scope must be removed for the old parser")
			}
			var node yaml.Node
			if err := yaml.Unmarshal(normalized, &node); err != nil {
				t.Fatalf("the normalized workflow must still parse: %v\n%s", err, normalized)
			}
			// Every accepted case here keeps another key beside the scope, so no empty mapping may appear.
			if strings.Count(string(normalized), "{}") != strings.Count(string(raw), "{}") {
				t.Fatalf("a block with other keys must not gain an empty mapping: %q", normalized)
			}
		})
	}
}

func TestAlertsPermissionKeepsOtherLintInput(t *testing.T) {
	raw := []byte("on: push\npermissions:\n  contents: write\n  vulnerability-alerts: read\n  typo: read\njobs:\n  a:\n    runs-on: invalid-runner\n    steps:\n      - run: echo vulnerability-alerts read\n")
	normalized, err := validateAlertsPermission(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"contents: write", "typo: read", "invalid-runner", "echo vulnerability-alerts read"} {
		if !strings.Contains(string(normalized), s) {
			t.Fatalf("other checks lost %q", s)
		}
	}
	if strings.Contains(string(normalized), "{}") {
		t.Fatalf("a block with other keys must not gain an empty mapping: %q", normalized)
	}
}

func TestNormalizeAppliesBothExtensions(t *testing.T) {
	raw := []byte("on: push\npermissions:\n  vulnerability-alerts: read\nconcurrency:\n  group: g\n  queue: max\njobs: {}\n")
	normalized, err := normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(normalized), "queue: max") || strings.Contains(string(normalized), "vulnerability-alerts") {
		t.Fatalf("both unsupported keys must be stripped: %q", normalized)
	}
	if _, err := normalize([]byte("on: push\npermissions:\n  vulnerability-alerts: write\njobs: {}\n")); err == nil {
		t.Fatal("a write scope must be refused")
	}
	if _, err := normalize([]byte("on: push\nconcurrency:\n  group: g\n  queue: all\njobs: {}\n")); err == nil {
		t.Fatal("a bad queue must still be refused")
	}
}

// A permissions block whose only key is the scope must not become empty: actionlint
// rejects an empty value, so the line becomes an empty flow mapping.
func TestSoleAlertsPermissionBecomesAnEmptyMapping(t *testing.T) {
	for _, test := range []struct{ name, doc string }{
		{"workflow", "on: push\npermissions:\n  vulnerability-alerts: read\njobs: {}\n"},
		{"job", "on: push\njobs:\n  a:\n    permissions:\n      vulnerability-alerts: none\n    runs-on: ubuntu-latest\n"},
		{"with a comment", "on: push\npermissions:\n  vulnerability-alerts: read # alerts\njobs: {}\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := validateAlertsPermission([]byte(test.doc))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(normalized), "\n") != strings.Count(test.doc, "\n") {
				t.Fatal("line numbers must be preserved")
			}
			var parsed struct {
				Permissions map[string]string `yaml:"permissions"`
				Jobs        map[string]struct {
					Permissions map[string]string `yaml:"permissions"`
				} `yaml:"jobs"`
			}
			if err := yaml.Unmarshal(normalized, &parsed); err != nil {
				t.Fatalf("the normalized workflow must still parse: %v\n%s", err, normalized)
			}
			if strings.Contains(string(normalized), "vulnerability-alerts") {
				t.Fatalf("the scope must be gone: %q", normalized)
			}
			// Assert on the scope's own line: the fixtures' "jobs: {}" would satisfy a whole-file search.
			lines := strings.Split(test.doc, "\n")
			for i, line := range lines {
				if trimmed := strings.TrimLeft(line, " "); strings.HasPrefix(trimmed, "vulnerability-alerts:") {
					want := line[:len(line)-len(trimmed)] + "{}"
					if got := strings.Split(string(normalized), "\n")[i]; got != want {
						t.Fatalf("a sole key must become an empty mapping on its own line: got %q, want %q", got, want)
					}
				}
			}
		})
	}
}
