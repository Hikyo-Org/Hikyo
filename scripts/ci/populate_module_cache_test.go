package ci_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The two trusted writers of go-mod-v2 must save the same contents, so both populate the cache with
// the one script instead of private copies that can drift.
func TestModuleCachePopulationIsOneScript(t *testing.T) {
	const call = "./scripts/ci/populate-go-module-cache.sh"
	for _, file := range []string{"ci.yml", "ci-race-cache.yml"} {
		workflow, _ := readWorkflowFile(t, file)
		found := 0
		for _, job := range workflow["jobs"].(map[string]any) {
			steps, _ := job.(map[string]any)["steps"].([]any)
			for _, raw := range steps {
				step, _ := raw.(map[string]any)
				if step["name"] != "Populate shared Go module cache" {
					continue
				}
				found++
				if step["run"] != call {
					t.Errorf("%s populates the module cache with %q, want the shared script %s", file, step["run"], call)
				}
			}
		}
		if found != 1 {
			t.Errorf("%s has %d populate steps, want 1", file, found)
		}
	}
}

// Run the script with a fake go to see exactly what it asks for: the repository's modules, then each
// pinned tool module added to a scratch module and everything downloaded.
func TestModuleCachePopulationScriptDownloadsModulesAndPinnedTools(t *testing.T) {
	repo := repositoryRoot(t)
	bin, temp := t.TempDir(), t.TempDir()
	log := filepath.Join(t.TempDir(), "calls")
	fake := "#!/bin/sh\nprintf '%s|%s\\n' \"$(pwd -P)\" \"$*\" >>\"" + log + "\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(context.Background(), filepath.Join(repo, "scripts/ci/populate-go-module-cache.sh"))
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "RUNNER_TEMP="+temp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script failed: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	modules, err := os.ReadFile(filepath.Join(repo, "scripts/ci/go-tool-modules.txt"))
	if err != nil {
		t.Fatal(err)
	}
	realRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	realTemp, err := filepath.EvalSymlinks(temp)
	if err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(realTemp, "go-tool-modules")
	want := []string{realRepo + "|mod download", scratch + "|mod init ci-tools"}
	for _, module := range strings.Fields(string(modules)) {
		want = append(want, scratch+"|mod edit -require="+module)
	}
	want = append(want, scratch+"|mod download all")
	if got := strings.Split(strings.TrimSpace(string(raw)), "\n"); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("go calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
