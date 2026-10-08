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
