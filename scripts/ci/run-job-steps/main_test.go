package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunnableRefusesWhatAReplayCannotHonor(t *testing.T) {
	for name, s := range map[string]step{
		"expression":        {Name: "x", Run: "echo ${{ inputs.revision }}"},
		"env expression":    {Name: "x", Run: "true", Env: map[string]string{"A": "${{ github.sha }}"}},
		"condition":         {Name: "x", Run: "true", If: "false"},
		"fail open":         {Name: "x", Run: "true", ContinueOnError: true},
		"working dir":       {Name: "x", Run: "true", WorkingDir: "sub"},
		"unsupported shell": {Name: "x", Run: "true", Shell: "pwsh"},
	} {
		if _, err := runnable([]step{s}); err == nil {
			t.Errorf("%s: replay accepted a step it cannot honor", name)
		}
	}
	got, err := runnable([]step{
		{Name: validateRevisionStep, Run: "echo ${{ inputs.revision }}"},
		{Name: "checkout", Uses: "actions/checkout@v1"},
		{Name: "keep", Run: "true"},
	})
	if err != nil || len(got) != 1 || got[0].Name != "keep" {
		t.Fatalf("got %v, %v; want only the run step", got, err)
	}
}

// Env must reach the step (a dropped HIKYO_REQUIRE_COSIGN_TEST would silently
// skip tests), a failing step must stop the replay, and a GITHUB_PATH append
// must be visible to later steps only.
func TestExecuteCarriesEnvAndPathAndStopsOnFailure(t *testing.T) {
	dir := t.TempDir()
	tool := filepath.Join(dir, "bin")
	if err := os.MkdirAll(tool, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tool, "fixture-tool"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "marker")
	steps := []step{
		{Name: "before", Run: "! command -v fixture-tool"},
		{Name: "add", Run: `echo "` + tool + `" >>"$GITHUB_PATH"`},
		{Name: "after", Run: `fixture-tool && test "$REQUIRED" = 1 && touch "` + marker + `"`, Env: map[string]string{"REQUIRED": "1"}},
	}
	if err := execute(steps); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the last step did not run with its env and path")
	}
	err := execute([]step{{Name: "fail", Run: "exit 3"}, {Name: "never", Run: "touch " + marker + ".never"}})
	if err == nil || !strings.Contains(err.Error(), `step "fail"`) {
		t.Fatalf("got %v, want the failing step named", err)
	}
	if _, err := os.Stat(marker + ".never"); err == nil {
		t.Fatal("a step ran after a failure")
	}
}

// The replay must accept the job it exists for, with the Cosign tests' env.
func TestSupplyChainJobIsReplayable(t *testing.T) {
	steps, err := load("../../../.github/workflows/ci.yml", "supply-chain-checks")
	if err != nil {
		t.Fatal(err)
	}
	required := false
	for _, s := range steps {
		required = required || s.Env["HIKYO_REQUIRE_COSIGN_TEST"] == "1"
	}
	if len(steps) < 30 || !required {
		t.Fatalf("replay lost steps or the required-Cosign env: %d steps", len(steps))
	}
}

// A step's timeout-minutes must stop it, as it does under Actions.
func TestExecuteEnforcesStepTimeout(t *testing.T) {
	old := stepTimeoutUnit
	stepTimeoutUnit = 100 * time.Millisecond
	defer func() { stepTimeoutUnit = old }()
	start := time.Now()
	err := execute([]step{{Name: "slow", Run: "sleep 30", Timeout: 3}})
	if err == nil || !strings.Contains(err.Error(), "timeout") || time.Since(start) > 10*time.Second {
		t.Fatalf("got %v after %v, want a timeout error", err, time.Since(start))
	}
	if err := execute([]step{{Name: "quick", Run: "true", Timeout: 3}}); err != nil {
		t.Fatal(err)
	}
}

func TestRunnableSkipsCachePreparationByName(t *testing.T) {
	got, err := runnable([]step{
		{Name: "Select runner cache ABI", Run: "./scripts/ci/export-runner-cache-abi.sh"},
		{Name: "Reserve disk for shared Go cache restore", Run: "./scripts/ci/prepare-go-cache-disk.sh", Timeout: 3},
		{Name: "keep", Run: "true"},
	})
	if err != nil || len(got) != 1 || got[0].Name != "keep" {
		t.Fatalf("got %v, %v; want only the fixture step", got, err)
	}
}
