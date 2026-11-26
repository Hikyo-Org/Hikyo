package ci_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	// ci-lint-cache.yml writes one seed per ISO week for the module set and
	// runner ABI. test_core_serial and the advisory invariants job read the
	// newest seed by prefix, then fall back to any seed for the runner ABI.
	lintDriverModulePrefix   = "go-lintdriver-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-${{ hashFiles('go.mod', 'go.sum', 'scripts/ci/go-tool-modules.txt') }}-"
	lintDriverRunnerPrefix   = "go-lintdriver-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-"
	lintDriverGenerationID   = "lint-cache-generation"
	lintDriverGenerationRun  = `echo "value=$(date -u +%G-W%V)-nodwarf" >>"$GITHUB_OUTPUT"`
	lintDriverSeedKey        = lintDriverModulePrefix + "${{ steps." + lintDriverGenerationID + ".outputs.value }}"
	lintDriverRestoreName    = "Restore lint driver Go cache"
	lintDriverSeedLintTests  = "go test -count=1 -timeout=20m ./internal/lint\n"
	lintDriverSerialPackage  = "lint_package=$(go list ./internal/lint)\n"
	lintDriverSerialLintTest = `go test -count=1 -timeout=20m "$lint_package" || status=1`
)

var lintDriverSeedSpec = reuseSeedSpec{
	label:        "lint driver seed",
	group:        "ci-lint-cache-main",
	bootstrap:    "${{ runner.temp }}/lint-bootstrap",
	lookupID:     "lint-driver-seed",
	key:          lintDriverSeedKey,
	restoreKeys:  lintDriverRunnerPrefix,
	generationID: lintDriverGenerationID,
	generation:   lintDriverGenerationRun,
	compile: "cache=\"$HOME/.cache/go-build\"\n" +
		"test ! -e \"$cache\"\n" +
		"test \"$(go env GOCACHE)\" = \"$cache\"\n" +
		"./scripts/ci/prepare-go-cache-disk.sh\n" +
		"go run ./scripts/ci/prepare-lint-cache\n" +
		lintDriverSeedLintTests +
		"du -sh \"$cache\"\n" +
		"df -h \"$cache\"\n",
	maxTimeout: 45,
}

func TestLintDriverSeedWorkflowTrustAndBounds(t *testing.T) {
	workflow, raw := readWorkflowFile(t, "ci-lint-cache.yml")
	if err := requireReuseSeed(workflow, lintDriverSeedSpec); err != nil {
		t.Fatal(err)
	}
	job := func(w map[string]any) map[string]any { return w["jobs"].(map[string]any)["seed"].(map[string]any) }
	seedStep := func(w map[string]any, i int) map[string]any { return job(w)["steps"].([]any)[i].(map[string]any) }
	setKey := func(w map[string]any, key string) { seedStep(w, 5)["with"].(map[string]any)["key"] = key }
	editCompile := func(old, replacement string) func(map[string]any) {
		return func(w map[string]any) {
			c := seedStep(w, 6)
			c["run"] = strings.Replace(c["run"].(string), old, replacement, 1)
		}
	}
	for name, mutation := range map[string]func(map[string]any){
		"top-level env":      func(w map[string]any) { w["env"] = map[string]any{"GOFLAGS": "-tags=ui"} },
		"top-level defaults": func(w map[string]any) { w["defaults"] = map[string]any{"run": map[string]any{"shell": "sh"}} },
		"PR trigger":         func(w map[string]any) { w["on"].(map[string]any)["pull_request"] = nil },
		"push trigger":       func(w map[string]any) { w["on"].(map[string]any)["push"] = nil },
		"missing schedule":   func(w map[string]any) { delete(w["on"].(map[string]any), "schedule") },
		"write token":        func(w map[string]any) { w["permissions"] = map[string]any{"contents": "write"} },
		"job write token":    func(w map[string]any) { job(w)["permissions"] = map[string]any{"actions": "write"} },
		"cancelling group":   func(w map[string]any) { w["concurrency"].(map[string]any)["cancel-in-progress"] = true },
		"shared group":       func(w map[string]any) { w["concurrency"].(map[string]any)["group"] = "ci-race-cache-main" },
		"untrusted ref":      func(w map[string]any) { job(w)["if"] = "true" },
		"any ref":            func(w map[string]any) { job(w)["if"] = "github.event_name == 'schedule'" },
		"unbounded job":      func(w map[string]any) { job(w)["timeout-minutes"] = 360 },
		"no timeout":         func(w map[string]any) { delete(job(w), "timeout-minutes") },
		"second job": func(w map[string]any) {
			w["jobs"].(map[string]any)["extra"] = map[string]any{"runs-on": "ubuntu-latest"}
		},
		"extra writer": func(w map[string]any) {
			j := job(w)
			j["steps"] = append(j["steps"].([]any), map[string]any{
				"uses": reuseSaveAction, "with": map[string]any{"path": "~/.cache/go-build", "key": lintDriverSeedKey},
			})
		},
		"mutable checkout ref":          func(w map[string]any) { seedStep(w, 0)["with"].(map[string]any)["ref"] = "${{ github.ref }}" },
		"checkout persists credentials": func(w map[string]any) { seedStep(w, 0)["with"].(map[string]any)["persist-credentials"] = true },
		"setup-go implicit cache":       func(w map[string]any) { seedStep(w, 1)["with"].(map[string]any)["cache"] = true },
		"bootstrap touches real cache":  func(w map[string]any) { seedStep(w, 1)["env"] = map[string]any{"GOCACHE": "~/.cache/go-build"} },
		"lookup restores entries":       func(w map[string]any) { delete(seedStep(w, 5)["with"].(map[string]any), "lookup-only") },
		"lookup is skipped":             func(w map[string]any) { seedStep(w, 5)["if"] = "false" },
		"daily generation":              func(w map[string]any) { seedStep(w, 4)["run"] = `echo "value=$(date -u +%F)" >>"$GITHUB_OUTPUT"` },
		"fixed generation":              func(w map[string]any) { seedStep(w, 4)["run"] = `echo "value=2026-W48" >>"$GITHUB_OUTPUT"` },
		"skipped generation":            func(w map[string]any) { seedStep(w, 4)["if"] = "false" },
		"removed generation": func(w map[string]any) {
			j := job(w)
			steps := j["steps"].([]any)
			j["steps"] = append(steps[:4:4], steps[5:]...)
		},
		"key ignores go.sum": func(w map[string]any) {
			setKey(w, strings.Replace(lintDriverSeedKey, "'go.mod', 'go.sum', ", "'go.mod', ", 1))
		},
		"key without generation": func(w map[string]any) { setKey(w, lintDriverModulePrefix) },
		"key without ABI": func(w map[string]any) {
			setKey(w, strings.Replace(lintDriverSeedKey, "${{ steps.runner-cache-abi.outputs.value }}-", "", 1))
		},
		"key is per run":       func(w map[string]any) { setKey(w, lintDriverSeedKey+"-${{ github.run_id }}") },
		"key is per commit":    func(w map[string]any) { setKey(w, lintDriverSeedKey+"-${{ github.sha }}") },
		"restore keys widen":   func(w map[string]any) { seedStep(w, 5)["with"].(map[string]any)["restore-keys"] = "go-lintdriver-v2-" },
		"compile reuses cache": editCompile("test ! -e \"$cache\"\n", ""),
		"compile redirected":   editCompile("test \"$(go env GOCACHE)\" = \"$cache\"\n", ""),
		"skips prepare":        editCompile("go run ./scripts/ci/prepare-lint-cache\n", ""),
		"skips lint tests":     editCompile("go test -count=1 -timeout=20m ./internal/lint\n", ""),
		"narrowed lint tests":  editCompile("-timeout=20m ./internal/lint", "-timeout=20m -run TestRawSQL ./internal/lint"),
		"unbounded lint tests": editCompile(" -timeout=20m", ""),
		"guard after disk": editCompile("test ! -e \"$cache\"\ntest \"$(go env GOCACHE)\" = \"$cache\"\n./scripts/ci/prepare-go-cache-disk.sh\n",
			"./scripts/ci/prepare-go-cache-disk.sh\ntest ! -e \"$cache\"\ntest \"$(go env GOCACHE)\" = \"$cache\"\n"),
		"compile fail open":   func(w map[string]any) { seedStep(w, 6)["continue-on-error"] = true },
		"compile always runs": func(w map[string]any) { delete(seedStep(w, 6), "if") },
		"compile env":         func(w map[string]any) { seedStep(w, 6)["env"] = map[string]any{"GOCACHE": "/tmp/other"} },
		"save without exact miss": func(w map[string]any) {
			seedStep(w, 7)["if"] = "success() && (github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'"
		},
		"save from any ref": func(w map[string]any) {
			seedStep(w, 7)["if"] = "success() && steps.lint-driver-seed.outputs.cache-hit != 'true'"
		},
		"save after failure": func(w map[string]any) {
			seedStep(w, 7)["if"] = strings.Replace(seedStep(w, 7)["if"].(string), "success() && ", "always() && ", 1)
		},
		"save under another key": func(w map[string]any) { seedStep(w, 7)["with"].(map[string]any)["key"] = lintDriverSeedKey },
		"save another path":      func(w map[string]any) { seedStep(w, 7)["with"].(map[string]any)["path"] = "~/go/pkg/mod" },
	} {
		t.Run(name, func(t *testing.T) {
			var changed map[string]any
			if err := yaml.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			mutation(changed)
			if requireReuseSeed(changed, lintDriverSeedSpec) == nil {
				t.Fatal("accepted unsafe lint driver seed publisher")
			}
		})
	}
}

// Readers restore the newest seed exactly once, right after the go-test-v2
// restore into the same directory, and never write any cache.
// actions/cache prefix-matches key before restore-keys, so a prefix key finds
// the current week's seed without a generation step in every reader.
func requireLintDriverReader(steps []any) error {
	restores, previous := 0, ""
	for _, value := range steps {
		step, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("malformed step")
		}
		uses, _ := step["uses"].(string)
		if strings.HasPrefix(uses, "actions/cache/save@") || strings.HasPrefix(uses, "actions/cache@") {
			return fmt.Errorf("only ci-lint-cache.yml may write the lint driver cache, found %v", uses)
		}
		with, _ := step["with"].(map[string]any)
		key, _ := with["key"].(string)
		if step["name"] == lintDriverRestoreName || strings.Contains(key, "go-lintdriver") {
			restores++
			if previous != "go-test-v2" || step["name"] != lintDriverRestoreName || uses != reuseRestoreAction ||
				len(step) != 3 || len(with) != 3 || with["path"] != "~/.cache/go-build" || key != lintDriverModulePrefix ||
				with["restore-keys"] != lintDriverRunnerPrefix {
				return fmt.Errorf("lint driver reader must restore the seed's module prefix, then its runner prefix, after the test cache")
			}
		}
		previous = ""
		if strings.HasPrefix(key, "go-test-v2-") && strings.HasPrefix(uses, "actions/cache/restore@") {
			previous = "go-test-v2"
		}
	}
	if restores != 1 {
		return fmt.Errorf("want exactly one lint driver cache reader, got %d", restores)
	}
	return nil
}

func TestLintDriverReadersShareTheSeedKey(t *testing.T) {
	for _, reader := range []struct{ workflow, job string }{
		{"ci.yml", "test_core_serial"}, {"ci-invariants.yml", "invariants"},
	} {
		t.Run(reader.workflow, func(t *testing.T) {
			workflow, raw := readWorkflowFile(t, reader.workflow)
			if err := requireLintDriverReader(workflowJobSteps(t, workflow, reader.job)); err != nil {
				t.Fatal(err)
			}
			restoreStep := func(steps []any) (int, map[string]any) {
				for i, value := range steps {
					if step := value.(map[string]any); step["name"] == lintDriverRestoreName {
						return i, step
					}
				}
				t.Fatal("lint driver restore step not found")
				return 0, nil
			}
			for name, mutation := range map[string]func([]any) []any{
				"reader saves": func(steps []any) []any {
					return append(steps, map[string]any{
						"uses": reuseSaveAction,
						"with": map[string]any{"path": "~/.cache/go-build", "key": lintDriverSeedKey},
					})
				},
				"combined cache action": func(steps []any) []any {
					_, step := restoreStep(steps)
					step["uses"] = strings.Replace(reuseRestoreAction, "cache/restore", "cache", 1)
					return steps
				},
				"pinned week": func(steps []any) []any {
					_, step := restoreStep(steps)
					step["with"].(map[string]any)["key"] = lintDriverModulePrefix + "2026-W48"
					return steps
				},
				"runner prefix as key": func(steps []any) []any {
					_, step := restoreStep(steps)
					step["with"].(map[string]any)["key"] = lintDriverRunnerPrefix
					return steps
				},
				"missing runner fallback": func(steps []any) []any {
					_, step := restoreStep(steps)
					delete(step["with"].(map[string]any), "restore-keys")
					return steps
				},
				"skipped reader": func(steps []any) []any {
					_, step := restoreStep(steps)
					step["if"] = "false"
					return steps
				},
				"removed reader": func(steps []any) []any {
					i, _ := restoreStep(steps)
					return append(steps[:i], steps[i+1:]...)
				},
				"duplicated reader": func(steps []any) []any {
					i, step := restoreStep(steps)
					return append(steps[:i+1], append([]any{step}, steps[i+1:]...)...)
				},
				"reader before test cache": func(steps []any) []any {
					i, _ := restoreStep(steps)
					steps[i-1], steps[i] = steps[i], steps[i-1]
					return steps
				},
			} {
				t.Run(name, func(t *testing.T) {
					var changed map[string]any
					if err := yaml.Unmarshal(raw, &changed); err != nil {
						t.Fatal(err)
					}
					job := changed["jobs"].(map[string]any)[reader.job].(map[string]any)
					if requireLintDriverReader(mutation(job["steps"].([]any))) == nil {
						t.Fatal("accepted a lint driver reader that writes or drifts from the seed")
					}
				})
			}
		})
	}
}

// The seed compiles the lint package's tests the way test_core_serial does. A
// build flag or environment prefix on either side would build different
// actions and miss the seed, so the serial line must match whole.
func TestLintDriverSeedMatchesSerialLintTests(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), "scripts/ci/test-core-packages.sh"))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	serial := 0
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(line, "$lint_package") && strings.Contains(line, "go test") {
			if strings.TrimLeft(line, " \t") != lintDriverSerialLintTest {
				t.Fatalf("serial lint test line %q must be exactly %q", line, lintDriverSerialLintTest)
			}
			serial++
		}
	}
	if !strings.Contains(lintDriverSeedSpec.compile, lintDriverSeedLintTests) ||
		!strings.Contains(script, lintDriverSerialPackage) || serial != 1 {
		t.Fatalf("test-core-packages.sh must run %q for ./internal/lint, as the lint driver seed does", lintDriverSerialLintTest)
	}
}
