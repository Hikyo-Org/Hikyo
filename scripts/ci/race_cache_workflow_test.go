package ci_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// These declarations model workflow YAML, not runtime stores. Reuse names keep
// invariant 12's cache declaration sweep reserved for registered runtime state.
const (
	reuseRestoreAction = "actions/cache/restore@55cc8345863c7cc4c66a329aec7e433d2d1c52a9"
	reuseSaveAction    = "actions/cache/save@55cc8345863c7cc4c66a329aec7e433d2d1c52a9"
	// The one key ci-race-cache.yml writes and the merge-gate race shards and
	// reliability race isolation read. It carries no shard or planner identity.
	sharedRaceReuseKey         = "go-race-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-${{ hashFiles('go.mod', 'go.sum') }}"
	sharedRaceReuseRestoreKeys = "go-race-v2-${{ runner.os }}-${{ runner.arch }}-${{ steps.runner-cache-abi.outputs.value }}-"
)

func readWorkflowFile(t *testing.T, name string) (map[string]any, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(t), ".github/workflows", name))
	if err != nil {
		t.Fatal(err)
	}
	var workflow map[string]any
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatal(err)
	}
	return workflow, raw
}

func workflowJobSteps(t *testing.T, workflow map[string]any, job string) []any {
	t.Helper()
	jobs, ok := workflow["jobs"].(map[string]any)
	if !ok {
		t.Fatal("workflow has no jobs")
	}
	body, ok := jobs[job].(map[string]any)
	if !ok {
		t.Fatalf("workflow has no %s job", job)
	}
	steps, ok := body["steps"].([]any)
	if !ok {
		t.Fatalf("%s has no steps", job)
	}
	return steps
}

func TestRaceSeedWorkflowTrustAndBounds(t *testing.T) {
	workflow, raw := readWorkflowFile(t, "ci-race-cache.yml")
	if err := requireRaceSeed(workflow); err != nil {
		t.Fatal(err)
	}
	seedStep := func(w map[string]any, i int) map[string]any {
		return w["jobs"].(map[string]any)["seed"].(map[string]any)["steps"].([]any)[i].(map[string]any)
	}
	job := func(w map[string]any) map[string]any { return w["jobs"].(map[string]any)["seed"].(map[string]any) }
	for name, mutation := range map[string]func(map[string]any){
		"PR trigger":       func(w map[string]any) { w["on"].(map[string]any)["pull_request"] = nil },
		"push trigger":     func(w map[string]any) { w["on"].(map[string]any)["push"] = nil },
		"missing schedule": func(w map[string]any) { delete(w["on"].(map[string]any), "schedule") },
		"write token":      func(w map[string]any) { w["permissions"] = map[string]any{"contents": "write"} },
		"job write token":  func(w map[string]any) { job(w)["permissions"] = map[string]any{"actions": "write"} },
		"cancelling group": func(w map[string]any) { w["concurrency"].(map[string]any)["cancel-in-progress"] = true },
		"shared group":     func(w map[string]any) { w["concurrency"].(map[string]any)["group"] = "ci-${{ github.ref }}" },
		"untrusted ref":    func(w map[string]any) { job(w)["if"] = "true" },
		"any ref":          func(w map[string]any) { job(w)["if"] = "github.event_name == 'schedule'" },
		"unbounded job":    func(w map[string]any) { job(w)["timeout-minutes"] = 360 },
		"job env redirects cache": func(w map[string]any) {
			job(w)["env"] = map[string]any{"GOCACHE": "${{ github.workspace }}/.ci-cache/go-race"}
		},
		"second job": func(w map[string]any) {
			w["jobs"].(map[string]any)["extra"] = map[string]any{"runs-on": "ubuntu-latest"}
		},
		"extra step": func(w map[string]any) {
			j := job(w)
			j["steps"] = append(j["steps"].([]any), map[string]any{"run": "go test -race ./..."})
		},
		"mutable checkout ref": func(w map[string]any) { seedStep(w, 0)["with"].(map[string]any)["ref"] = "${{ github.ref }}" },
		"checkout persists credentials": func(w map[string]any) {
			seedStep(w, 0)["with"].(map[string]any)["persist-credentials"] = true
		},
		"checkout default credentials": func(w map[string]any) { delete(seedStep(w, 0)["with"].(map[string]any), "persist-credentials") },
		"setup-go implicit cache":      func(w map[string]any) { seedStep(w, 1)["with"].(map[string]any)["cache"] = true },
		"bootstrap inherits real cache": func(w map[string]any) {
			delete(seedStep(w, 1), "env")
		},
		"bootstrap touches real cache": func(w map[string]any) {
			seedStep(w, 1)["env"] = map[string]any{"GOCACHE": "~/.cache/go-build"}
		},
		"module cache key changes": func(w map[string]any) { seedStep(w, 2)["with"].(map[string]any)["key"] = "go-mod-v2" },
		"lookup restores entries":  func(w map[string]any) { delete(seedStep(w, 4)["with"].(map[string]any), "lookup-only") },
		"lookup is skipped":        func(w map[string]any) { seedStep(w, 4)["if"] = "false" },
		"lookup key is per shard": func(w map[string]any) {
			seedStep(w, 4)["with"].(map[string]any)["key"] = strings.Replace(sharedRaceReuseKey, "go-race-v2-", "go-race-v2-shard-0-", 1)
		},
		"lookup key ignores go.sum": func(w map[string]any) {
			seedStep(w, 4)["with"].(map[string]any)["key"] = strings.Replace(sharedRaceReuseKey, "'go.mod', 'go.sum'", "'go.mod'", 1)
		},
		"lookup key is per run": func(w map[string]any) {
			seedStep(w, 4)["with"].(map[string]any)["key"] = sharedRaceReuseKey + "-${{ github.sha }}"
		},
		"lookup restore keys widen": func(w map[string]any) {
			seedStep(w, 4)["with"].(map[string]any)["restore-keys"] = "go-race-v2-"
		},
		"compile runs tests": func(w map[string]any) {
			c := seedStep(w, 5)
			c["run"] = strings.Replace(c["run"].(string), "go list -deps -test -export -race ./...", "go test -race ./...", 1)
		},
		"compile drops race detector": func(w map[string]any) {
			c := seedStep(w, 5)
			c["run"] = strings.Replace(c["run"].(string), " -race", "", 1)
		},
		"compile reuses existing cache": func(w map[string]any) {
			c := seedStep(w, 5)
			c["run"] = strings.Replace(c["run"].(string), "test ! -e \"$cache\"\n", "", 1)
		},
		"compile fail open":   func(w map[string]any) { seedStep(w, 5)["continue-on-error"] = true },
		"compile always runs": func(w map[string]any) { delete(seedStep(w, 5), "if") },
		"compile redirects":   func(w map[string]any) { seedStep(w, 5)["env"] = map[string]any{"GOCACHE": "/tmp/other"} },
		"save without exact miss": func(w map[string]any) {
			seedStep(w, 6)["if"] = "success() && (github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'"
		},
		"save from any ref": func(w map[string]any) {
			seedStep(w, 6)["if"] = "success() && steps.race-seed.outputs.cache-hit != 'true'"
		},
		"save after failure": func(w map[string]any) {
			seedStep(w, 6)["if"] = strings.Replace(seedStep(w, 6)["if"].(string), "success() && ", "always() && ", 1)
		},
		"save under another key": func(w map[string]any) { seedStep(w, 6)["with"].(map[string]any)["key"] = sharedRaceReuseKey },
		"save another path": func(w map[string]any) {
			seedStep(w, 6)["with"].(map[string]any)["path"] = "~/go/pkg/mod"
		},
		"save fail open": func(w map[string]any) { seedStep(w, 6)["continue-on-error"] = true },
	} {
		t.Run(name, func(t *testing.T) {
			var changed map[string]any
			if err := yaml.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			mutation(changed)
			if requireRaceSeed(changed) == nil {
				t.Fatal("accepted unsafe race seed publisher")
			}
		})
	}
}

func requireRaceSeed(workflow map[string]any) error {
	triggers, ok := workflow["on"].(map[string]any)
	if !ok || len(triggers) != 2 {
		return fmt.Errorf("race seed must run only daily or manually")
	}
	if _, ok := triggers["workflow_dispatch"]; !ok {
		return fmt.Errorf("missing manual race seed")
	}
	if _, ok := triggers["schedule"]; !ok {
		return fmt.Errorf("missing daily race seed")
	}
	if !reflect.DeepEqual(workflow["permissions"], map[string]any{"contents": "read"}) {
		return fmt.Errorf("race seed must be read-only")
	}
	if !reflect.DeepEqual(workflow["concurrency"], map[string]any{"group": "ci-race-cache-main", "cancel-in-progress": false}) {
		return fmt.Errorf("race seed runs must queue behind each other, never cancel a publisher")
	}
	jobs, ok := workflow["jobs"].(map[string]any)
	if !ok || len(jobs) != 1 {
		return fmt.Errorf("race seed must have exactly one job")
	}
	job, ok := jobs["seed"].(map[string]any)
	if !ok {
		return fmt.Errorf("race seed job is missing")
	}
	if job["if"] != "(github.event_name == 'workflow_dispatch' || github.event_name == 'schedule') && github.ref == 'refs/heads/main'" {
		return fmt.Errorf("only trusted main may populate the race seed")
	}
	// No job-level env, permissions, container, or defaults: the cache location
	// and token authority stay the runner defaults the steps assert.
	keys := make([]string, 0, len(job))
	for key := range job {
		keys = append(keys, key)
	}
	if len(keys) != 4 || job["runs-on"] != "ubuntu-latest" || job["steps"] == nil {
		return fmt.Errorf("race seed job must set only its guard, runner, timeout, and steps (got %v)", keys)
	}
	if timeout, ok := job["timeout-minutes"].(int); !ok || timeout < 1 || timeout > 30 {
		return fmt.Errorf("race seed needs an explicit bounded timeout")
	}
	steps, ok := job["steps"].([]any)
	if !ok || len(steps) != 7 {
		return fmt.Errorf("unexpected race seed preparation or publication")
	}
	step := func(i int) map[string]any {
		s, _ := steps[i].(map[string]any)
		return s
	}
	checkout := step(0)
	if uses, _ := checkout["uses"].(string); !strings.HasPrefix(uses, "actions/checkout@") ||
		!reflect.DeepEqual(checkout["with"], map[string]any{"ref": "${{ github.sha }}", "persist-credentials": false}) {
		return fmt.Errorf("race seed must use the immutable main SHA without credentials")
	}
	setup := step(1)
	if uses, _ := setup["uses"].(string); !strings.HasPrefix(uses, "actions/setup-go@") ||
		!reflect.DeepEqual(setup["env"], map[string]any{"GOCACHE": "${{ runner.temp }}/race-bootstrap"}) {
		return fmt.Errorf("setup diagnostics must use a separate temporary bootstrap cache")
	}
	if !reflect.DeepEqual(setup["with"], map[string]any{"go-version-file": "go.mod", "cache": false}) {
		return fmt.Errorf("race seed must select pinned Go without implicit caches")
	}
	modules := step(2)
	if modules["uses"] != reuseRestoreAction || !reflect.DeepEqual(modules["with"], map[string]any{
		"path":         "~/go/pkg/mod",
		"key":          "go-mod-v2-${{ runner.os }}-${{ hashFiles('go.mod', 'go.sum', 'scripts/ci/go-tool-modules.txt') }}",
		"restore-keys": "go-mod-v2-${{ runner.os }}-",
	}) {
		return fmt.Errorf("race seed must read the shared module cache with the standard reader")
	}
	abi := step(3)
	if abi["id"] != "runner-cache-abi" || abi["run"] != "./scripts/ci/export-runner-cache-abi.sh" {
		return fmt.Errorf("race seed must select the runner cache ABI")
	}
	lookup := step(4)
	if lookup["id"] != "race-seed" || lookup["uses"] != reuseRestoreAction || lookup["if"] != nil || lookup["continue-on-error"] != nil ||
		!reflect.DeepEqual(lookup["with"], map[string]any{
			"path":         "~/.cache/go-build",
			"key":          sharedRaceReuseKey,
			"restore-keys": sharedRaceReuseRestoreKeys,
			"lookup-only":  true,
		}) {
		return fmt.Errorf("race seed lookup must use the shared key and never restore historical entries")
	}
	compile := step(5)
	wantCompile := "cache=\"$HOME/.cache/go-build\"\ntest ! -e \"$cache\"\ntest \"$(go env GOCACHE)\" = \"$cache\"\ngo list -deps -test -export -race ./... >/dev/null\ndu -sh \"$cache\"\n"
	if compile["run"] != wantCompile || compile["if"] != "steps.race-seed.outputs.cache-hit != 'true'" || compile["continue-on-error"] != nil || compile["env"] != nil {
		return fmt.Errorf("fresh race seed must compile every race dependency without executing tests")
	}
	save := step(6)
	if save["uses"] != reuseSaveAction || save["continue-on-error"] != nil ||
		save["if"] != "success() && steps.race-seed.outputs.cache-hit != 'true' && (github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'" ||
		!reflect.DeepEqual(save["with"], map[string]any{"path": "~/.cache/go-build", "key": "${{ steps.race-seed.outputs.cache-primary-key }}"}) {
		return fmt.Errorf("only a successful main seed may publish, once per exact key")
	}
	return nil
}

// A shard or isolation job that reads another key, or writes the cache, would
// either miss the seed again or reintroduce eviction pressure from many writers.
func requireSharedRaceReader(steps []any) error {
	restores := 0
	for _, value := range steps {
		step, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("malformed step")
		}
		if uses, _ := step["uses"].(string); strings.HasPrefix(uses, "actions/cache/save@") || strings.HasPrefix(uses, "actions/cache@") {
			return fmt.Errorf("only ci-race-cache.yml may write the race cache, found %v", uses)
		}
		if step["name"] != "Restore race Go cache" {
			continue
		}
		restores++
		with, _ := step["with"].(map[string]any)
		restoreKeys, _ := with["restore-keys"].(string)
		if step["uses"] != reuseRestoreAction || with["path"] != "~/.cache/go-build" || with["key"] != sharedRaceReuseKey ||
			strings.TrimSpace(restoreKeys) != sharedRaceReuseRestoreKeys || step["if"] != nil || step["continue-on-error"] != nil {
			return fmt.Errorf("race cache reader must use exactly the seed key and prefix")
		}
	}
	if restores != 1 {
		return fmt.Errorf("want exactly one race cache reader, got %d", restores)
	}
	return nil
}

func TestRaceReuseReadersShareTheSeedKey(t *testing.T) {
	seed, _ := readWorkflowFile(t, "ci-race-cache.yml")
	lookup := workflowJobSteps(t, seed, "seed")[4].(map[string]any)["with"].(map[string]any)
	if lookup["key"] != sharedRaceReuseKey || lookup["restore-keys"] != sharedRaceReuseRestoreKeys {
		t.Fatal("seed lookup drifted from the shared race cache key")
	}
	for workflowName, job := range map[string]string{"ci.yml": "race_shard", "reliability.yml": "race-isolation"} {
		workflow, raw := readWorkflowFile(t, workflowName)
		steps := workflowJobSteps(t, workflow, job)
		if err := requireSharedRaceReader(steps); err != nil {
			t.Fatalf("%s %s: %v", workflowName, job, err)
		}
		if strings.Contains(string(raw), "go-race-v2-shard-") {
			t.Fatalf("%s still names a per-shard race cache", workflowName)
		}
	}
}

func TestRaceReuseReaderRejectsPerShardKeysAndWriters(t *testing.T) {
	workflow, raw := readWorkflowFile(t, "ci.yml")
	if err := requireSharedRaceReader(workflowJobSteps(t, workflow, "race_shard")); err != nil {
		t.Fatal(err)
	}
	restoreStep := func(steps []any) map[string]any {
		for _, value := range steps {
			if step := value.(map[string]any); step["name"] == "Restore race Go cache" {
				return step
			}
		}
		panic("race restore step not found")
	}
	for name, mutation := range map[string]func([]any) []any{
		"per-shard key": func(steps []any) []any {
			restoreStep(steps)["with"].(map[string]any)["key"] = strings.Replace(sharedRaceReuseKey, "go-race-v2-", "go-race-v2-shard-${{ matrix.shard }}-", 1)
			return steps
		},
		"planner-hash key": func(steps []any) []any {
			restoreStep(steps)["with"].(map[string]any)["key"] = sharedRaceReuseKey + "-${{ hashFiles('scripts/ci/analysis-shards-go/main.go') }}"
			return steps
		},
		"per-shard prefix": func(steps []any) []any {
			restoreStep(steps)["with"].(map[string]any)["restore-keys"] = "go-race-v2-shard-${{ matrix.shard }}-"
			return steps
		},
		"wrong path": func(steps []any) []any {
			restoreStep(steps)["with"].(map[string]any)["path"] = "~/go/pkg/mod"
			return steps
		},
		"skipped reader": func(steps []any) []any {
			restoreStep(steps)["if"] = "false"
			return steps
		},
		"removed reader": func(steps []any) []any {
			for i, value := range steps {
				if value.(map[string]any)["name"] == "Restore race Go cache" {
					return append(steps[:i], steps[i+1:]...)
				}
			}
			return steps
		},
		"re-added shard writer": func(steps []any) []any {
			return append(steps, map[string]any{
				"name": "Save race Go cache (trusted main validation only)",
				"uses": reuseSaveAction,
				"with": map[string]any{"path": "~/.cache/go-build", "key": sharedRaceReuseKey},
			})
		},
		"combined cache action": func(steps []any) []any {
			return append(steps, map[string]any{"uses": "actions/cache@55cc8345863c7cc4c66a329aec7e433d2d1c52a9"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			var changed map[string]any
			if err := yaml.Unmarshal(raw, &changed); err != nil {
				t.Fatal(err)
			}
			job := changed["jobs"].(map[string]any)["race_shard"].(map[string]any)
			job["steps"] = mutation(job["steps"].([]any))
			if requireSharedRaceReader(job["steps"].([]any)) == nil {
				t.Fatal("accepted a race cache reader that does not share the seed")
			}
		})
	}
}

// setup-go's diagnostics initialize GOCACHE, and `go env` creates the directory
// it reports. Run the real compile guard with the workflow's environment scopes
// to prove the bootstrap cache never occupies the directory the seed will save.
func TestRaceSeedGuardRequiresFreshDefaultDirectory(t *testing.T) {
	workflow, _ := readWorkflowFile(t, "ci-race-cache.yml")
	steps := workflowJobSteps(t, workflow, "seed")
	compile := steps[5].(map[string]any)["run"].(string)
	prefix, _, found := strings.Cut(compile, "go list -deps")
	if !found {
		t.Fatal("actual compile guard missing")
	}
	for name, tc := range map[string]struct {
		existing  bool
		redirect  bool
		wantError bool
	}{
		"fresh":              {},
		"existing cache":     {existing: true, wantError: true},
		"redirected GOCACHE": {redirect: true, wantError: true},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			home := filepath.Join(dir, "home")
			runnerTemp := filepath.Join(dir, "runner-temp")
			for _, p := range []string{home, runnerTemp} {
				if err := os.MkdirAll(p, 0700); err != nil {
					t.Fatal(err)
				}
			}
			defaultCache := filepath.Join(home, ".cache", "go-build")
			bootstrap := strings.Replace(steps[1].(map[string]any)["env"].(map[string]any)["GOCACHE"].(string), "${{ runner.temp }}", runnerTemp, 1)
			sentinel := filepath.Join(defaultCache, "existing-owned-entry")
			if tc.existing {
				if err := os.MkdirAll(defaultCache, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			environment := append(os.Environ(), "HOME="+home, "GOENV=off", "GOTOOLCHAIN=local", "XDG_CACHE_HOME=")
			diagnostics := exec.CommandContext(ctx, "go", "env")
			diagnostics.Dir = home
			diagnostics.Env = append(environment, "GOCACHE="+bootstrap)
			if err := diagnostics.Run(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(bootstrap); err != nil {
				t.Fatalf("real setup diagnostic did not initialize the bootstrap cache: %v", err)
			}
			// The default GOCACHE is platform specific; pin it to the runner's
			// Linux default so the guard's equality check sees the same value.
			guardCache := defaultCache
			if tc.redirect {
				guardCache = filepath.Join(home, "elsewhere")
			}
			guard := exec.CommandContext(ctx, "bash", "-e", "-c", prefix)
			guard.Dir = home
			guard.Env = append(environment, "GOCACHE="+guardCache)
			output, err := guard.CombinedOutput()
			if (err != nil) != tc.wantError {
				t.Fatalf("guard: err=%v output=%s", err, output)
			}
			if tc.existing {
				data, err := os.ReadFile(sentinel)
				if err != nil || string(data) != "preserve" {
					t.Fatalf("existing cache modified: %q %v", data, err)
				}
			}
		})
	}
}
