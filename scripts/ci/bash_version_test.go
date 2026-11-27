package ci_test

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// bash older than 4 ignores errexit for a failed [[ ]] or (( )), so a guard such as
// `[[ $id =~ ^[1-9][0-9]*$ ]]` under `set -e` lets the script continue: it fails open.
const bashGuard = `(( BASH_VERSINFO[0] >= 4 )) || { echo 'requires bash >= 4: errexit ignores a failed [[ ]] on older bash' >&2; exit 2; }`

// checkBash reports why the bash at path cannot run the workflow-shell guards faithfully.
func checkBash(path string) error {
	out, err := exec.Command(path, "-c", `printf %s "${BASH_VERSINFO[0]}"`).Output()
	if err != nil {
		return fmt.Errorf("cannot run %s: %w", path, err)
	}
	if major, err := strconv.Atoi(string(out)); err != nil || major < 4 {
		return fmt.Errorf("%s is bash %q: workflow-shell guards need bash >= 4 (older bash ignores errexit for [[ ]] and fails open); put a newer bash first on PATH", path, out)
	}
	return nil
}

func TestMain(m *testing.M) {
	if err := checkBash("bash"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func TestCheckBashRefusesOldBash(t *testing.T) {
	fake := func(major string) string {
		path := filepath.Join(t.TempDir(), "bash")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf "+major+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, old := range []string{"3", "2"} {
		if err := checkBash(fake(old)); err == nil || !strings.Contains(err.Error(), "bash >= 4") {
			t.Fatalf("bash %s: got %v, want refusal naming bash >= 4", old, err)
		}
	}
	for _, ok := range []string{"4", "5"} {
		if err := checkBash(fake(ok)); err != nil {
			t.Fatalf("bash %s: %v", ok, err)
		}
	}
	if err := checkBash(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing bash must be refused")
	}
}

// Skips only where no bash 3 exists (Linux CI); TestEveryBashGuardChecksTheVersion
// holds the line everywhere.
func TestGuardRefusesOnBash3(t *testing.T) {
	out, err := exec.Command("/bin/bash", "-c", `printf %s "${BASH_VERSINFO[0]}"`).Output()
	if err != nil || string(out) != "3" {
		t.Skipf("/bin/bash is not bash 3 (%q, %v)", out, err)
	}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows/ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow mainValidationWorkflow
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	// A dev ref with DEVELOPMENT=false must be refused; on bash 3 the unguarded
	// [[ ]] tag-pattern check used to let it through.
	cmd := exec.Command("/bin/bash", "-e", "-o", "pipefail", "-c", workflow.Jobs["changes"].Steps[0].Run)
	cmd.Env = append(os.Environ(), "GITHUB_REPOSITORY=Hikyo-Org/Hikyo", "EVENT_REPOSITORY=Hikyo-Org/Hikyo", "EVENT_REPOSITORY_ID=10",
		"GITHUB_EVENT_NAME=push", "GITHUB_REF=refs/heads/dev", "GITHUB_SHA="+sha, "SOURCE_REVISION="+sha,
		"DEVELOPMENT=false", "DEFAULT_BRANCH=main", "TAG_DELETED=false")
	output, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 || !strings.Contains(string(output), "requires bash >= 4") {
		t.Fatalf("guard on bash 3: err=%v output=%s, want exit 2 naming bash >= 4", err, output)
	}
}

// Every shell that relies on [[ ]] or (( )) under errexit, or on bash 4 features,
// must refuse old bash before doing anything else.
func TestEveryBashGuardChecksTheVersion(t *testing.T) {
	root := repositoryRoot(t)
	needs := regexp.MustCompile(`(?m)\[\[|^\s*\(\(|declare -A|mapfile|readarray|\$\{[A-Za-z_]+(,,|\^\^)`)
	workflows, err := filepath.Glob(filepath.Join(root, ".github/workflows/*.yml"))
	if err != nil || len(workflows) == 0 {
		t.Fatalf("workflows: %v", err)
	}
	for _, path := range workflows {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var workflow mainValidationWorkflow
		if err := yaml.Unmarshal(raw, &workflow); err != nil {
			t.Fatal(err)
		}
		for name, job := range workflow.Jobs {
			for _, step := range job.Steps {
				if needs.MatchString(step.Run) && !strings.Contains(step.Run, bashGuard) {
					t.Errorf("%s job %s step %q lacks the bash version guard", filepath.Base(path), name, step.Name)
				}
			}
		}
	}
	var scripts []string
	if err := filepath.WalkDir(filepath.Join(root, "scripts"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && d.Name() == "testdata" {
			return fs.SkipDir // fixtures are compared byte for byte
		}
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".sh") {
			scripts = append(scripts, path)
		}
		return err
	}); err != nil || len(scripts) == 0 {
		t.Fatalf("scripts: %v", err)
	}
	for _, path := range scripts {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if (strings.HasPrefix(text, "#!/usr/bin/env bash\n") || strings.HasPrefix(text, "#!/bin/bash\n")) && needs.MatchString(text) && !strings.Contains(text, bashGuard) {
			t.Errorf("%s lacks the bash version guard", path)
		}
	}
}
