package main

import (
	"strings"
	"testing"
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

func TestCacheModeValidation(t *testing.T) {
	for _, fixture := range []struct {
		name, fields string
		bad          bool
	}{
		{"deny", "cache-mode: none", false},
		{"read", "cache-mode: read", false},
		{"write", "cache-mode: write", false},
		{"write-only", "cache-mode: write-only", false},
		{"quoted enum", "cache-mode: 'none'", false},
		{"boolean", "cache-mode: false", true},
		{"null", "cache-mode: null", true},
		{"unknown mode", "cache-mode: disabled", true},
		{"expression", "cache-mode: '${{ github.ref }}'", true},
		{"duplicate", "cache-mode: none\ncache-mode: write", true},
		{"flow cannot hide a sibling", "{cache-mode: none, typo: true}", true},
		{"anchor cannot hide content", "cache-mode: &mode none", true},
	} {
		for _, scope := range []string{"workflow", "job"} {
			t.Run(fixture.name+"/"+scope, func(t *testing.T) {
				raw := "on: push\n" + fixture.fields + "\njobs: {}\n"
				if scope == "job" {
					raw = "on: push\njobs:\n  test:\n    " + strings.ReplaceAll(fixture.fields, "\n", "\n    ") + "\n    runs-on: ubuntu-latest\n"
				}
				normalized, err := validateCacheModes([]byte(raw))
				if (err != nil) != fixture.bad {
					t.Fatalf("error=%v bad=%t", err, fixture.bad)
				}
				if err == nil && strings.Count(string(normalized), "\n") != strings.Count(raw, "\n") {
					t.Fatal("line numbers must be preserved")
				}
			})
		}
	}
}

func TestCacheModeExtensionPreservesUnrelatedLintInput(t *testing.T) {
	raw := []byte("on: push\ncache-mode: none\nunknown-root-key: true\njobs:\n  test:\n    cache-mode: read\n    runs-on: invalid-runner\n    typo: true\n    steps:\n      - run: echo cache-mode:none\n")
	normalized, err := validateCacheModes(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(normalized), "cache-mode: none") || strings.Contains(string(normalized), "cache-mode: read") {
		t.Fatal("validated cache field still reaches old parser")
	}
	for _, expected := range []string{"unknown-root-key: true", "invalid-runner", "typo: true", "echo cache-mode:none"} {
		if !strings.Contains(string(normalized), expected) {
			t.Fatalf("unrelated lint input lost %s", expected)
		}
	}
}
