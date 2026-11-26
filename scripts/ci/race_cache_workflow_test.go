package ci_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
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
		"top-level env": func(w map[string]any) { w["env"] = map[string]any{"GOFLAGS": "-race=false"} },
		"top-level defaults": func(w map[string]any) {
			w["defaults"] = map[string]any{"run": map[string]any{"shell": "sh"}}
		},
		"PR trigger":       func(w map[string]any) { w["on"].(map[string]any)["pull_request"] = nil },
		"push trigger":     func(w map[string]any) { w["on"].(map[string]any)["push"] = nil },
		"missing schedule": func(w map[string]any) { delete(w["on"].(map[string]any), "schedule") },
		"write token":      func(w map[string]any) { w["permissions"] = map[string]any{"contents": "write"} },
		"job write token":  func(w map[string]any) { job(w)["permissions"] = map[string]any{"actions": "write"} },
		"cancelling group": func(w map[string]any) { w["concurrency"].(map[string]any)["cancel-in-progress"] = true },
		"own group":        func(w map[string]any) { w["concurrency"].(map[string]any)["group"] = "ci-race-cache-main" },
		"ref group":        func(w map[string]any) { w["concurrency"].(map[string]any)["group"] = "ci-${{ github.ref }}" },
		"untrusted ref":    func(w map[string]any) { job(w)["if"] = "true" },
		"any ref":          func(w map[string]any) { job(w)["if"] = "github.event_name == 'schedule'" },
		"unbounded job":    func(w map[string]any) { job(w)["timeout-minutes"] = 360 },
		"job env redirects cache": func(w map[string]any) {
			job(w)["env"] = map[string]any{"GOCACHE": "${{ github.workspace }}/.ci-cache/go-race"}
		},
		"no prune after the save": func(w map[string]any) { delete(w["jobs"].(map[string]any), "prune") },
		"prune widened": func(w map[string]any) {
			w["jobs"].(map[string]any)["prune"].(map[string]any)["permissions"].(map[string]any)["contents"] = "write"
		},
		"prune without needs": func(w map[string]any) { delete(w["jobs"].(map[string]any)["prune"].(map[string]any), "needs") },
		"prune of another workflow": func(w map[string]any) {
			w["jobs"].(map[string]any)["prune"].(map[string]any)["uses"] = "./.github/workflows/ci.yml"
		},
		"prune runs on a cancelled seed": func(w map[string]any) {
			w["jobs"].(map[string]any)["prune"].(map[string]any)["if"] = "${{ always() }}"
		},
		"seed job holds the token": func(w map[string]any) { job(w)["permissions"] = map[string]any{"actions": "write"} },
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
		"module save loses the main guard": func(w map[string]any) {
			seedStep(w, 8)["if"] = "success() && steps.go-module-cache.outputs.cache-hit != 'true'"
		},
		"module save on a hit": func(w map[string]any) {
			seedStep(w, 8)["if"] = strings.Replace(seedStep(w, 8)["if"].(string), " && steps.go-module-cache.outputs.cache-hit != 'true'", "", 1)
		},
		"module save fail open":  func(w map[string]any) { seedStep(w, 8)["continue-on-error"] = true },
		"module save wrong path": func(w map[string]any) { seedStep(w, 8)["with"].(map[string]any)["path"] = "~/go/pkg/mod" },
		"module save wrong key": func(w map[string]any) {
			seedStep(w, 8)["with"].(map[string]any)["key"] = "${{ steps.race-seed.outputs.cache-primary-key }}"
		},
		"module populate is a private copy": func(w map[string]any) { seedStep(w, 7)["run"] = "go mod download\n" },
		"module restore unnamed":            func(w map[string]any) { delete(seedStep(w, 2), "id") },
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

// reuseSeedSpec names what differs between the trusted-main build cache seeds;
// requireReuseSeed holds the trust rules they share.
type reuseSeedSpec struct {
	label, group, bootstrap, lookupID, key, restoreKeys, compile string
	// generationID and generation, when set, pin a step between the ABI and
	// the lookup whose output rotates the key.
	generationID, generation string
	maxTimeout               int
	// moduleWriter pins the daily trusted-main save of the shared module cache
	// after the build-cache save, so an evicted go-mod-v2 refills within a day.
	moduleWriter bool
}

var raceSeedSpec = reuseSeedSpec{
	label:       "race seed",
	group:       "ci-cache-seed-writers",
	bootstrap:   "${{ runner.temp }}/race-bootstrap",
	lookupID:    "race-seed",
	key:         sharedRaceReuseKey,
	restoreKeys: sharedRaceReuseRestoreKeys,
	compile:     "cache=\"$HOME/.cache/go-build\"\ntest ! -e \"$cache\"\ntest \"$(go env GOCACHE)\" = \"$cache\"\ngo list -deps -test -export -race ./... >/dev/null\ndu -sh \"$cache\"\n",
	maxTimeout:  30,

	moduleWriter: true,
}

const moduleWriterPopulate = "./scripts/ci/populate-go-module-cache.sh"

func requireRaceSeed(workflow map[string]any) error { return requireReuseSeed(workflow, raceSeedSpec) }

func requireReuseSeed(workflow map[string]any, spec reuseSeedSpec) error {
	// No top-level env or defaults: either would change what the seed compiles
	// without touching the reviewed steps.
	top := make([]string, 0, len(workflow))
	for key := range workflow {
		top = append(top, key)
	}
	sort.Strings(top)
	if !reflect.DeepEqual(top, []string{"concurrency", "jobs", "name", "on", "permissions"}) {
		return fmt.Errorf("%s must set only name, triggers, permissions, concurrency and jobs (got %v)", spec.label, top)
	}
	triggers, ok := workflow["on"].(map[string]any)
	if !ok || len(triggers) != 2 {
		return fmt.Errorf("%s must run only daily or manually", spec.label)
	}
	if _, ok := triggers["workflow_dispatch"]; !ok {
		return fmt.Errorf("missing manual %s", spec.label)
	}
	if _, ok := triggers["schedule"]; !ok {
		return fmt.Errorf("missing daily %s", spec.label)
	}
	if !reflect.DeepEqual(workflow["permissions"], map[string]any{"contents": "read"}) {
		return fmt.Errorf("%s must be read-only", spec.label)
	}
	if !reflect.DeepEqual(workflow["concurrency"], map[string]any{"group": spec.group, "cancel-in-progress": false}) {
		return fmt.Errorf("%s runs must queue behind each other, never cancel a publisher", spec.label)
	}
	jobs, ok := workflow["jobs"].(map[string]any)
	if !ok || len(jobs) != 2 {
		return fmt.Errorf("%s must have exactly its seed job and its prune job", spec.label)
	}
	// The prune is the shared ci-cache-prune.yml run as a called workflow right after the save, so a
	// rotation never coexists with its predecessor for longer than a job takes to start. Its token is
	// actions: write and nothing else, in a job of its own; the seed job holds none.
	if !reflect.DeepEqual(jobs["prune"], map[string]any{
		"needs":       "seed",
		"if":          "${{ !cancelled() && needs.seed.result != 'skipped' }}",
		"permissions": map[string]any{"actions": "write"},
		"uses":        "./.github/workflows/ci-cache-prune.yml",
	}) {
		return fmt.Errorf("%s must prune right after its save through the shared prune workflow, with actions: write only", spec.label)
	}
	job, ok := jobs["seed"].(map[string]any)
	if !ok {
		return fmt.Errorf("%s job is missing", spec.label)
	}
	if job["if"] != "(github.event_name == 'workflow_dispatch' || github.event_name == 'schedule') && github.ref == 'refs/heads/main'" {
		return fmt.Errorf("only trusted main may populate the %s", spec.label)
	}
	// No job-level env, permissions, container, or defaults: the cache location
	// and token authority stay the runner defaults the steps assert.
	keys := make([]string, 0, len(job))
	for key := range job {
		keys = append(keys, key)
	}
	if len(keys) != 4 || job["runs-on"] != "ubuntu-latest" || job["steps"] == nil {
		return fmt.Errorf("%s job must set only its guard, runner, timeout, and steps (got %v)", spec.label, keys)
	}
	if timeout, ok := job["timeout-minutes"].(int); !ok || timeout < 1 || timeout > spec.maxTimeout {
		return fmt.Errorf("%s needs an explicit bounded timeout", spec.label)
	}
	next := 4
	if spec.generation != "" {
		next = 5
	}
	steps, ok := job["steps"].([]any)
	want := next + 3
	if spec.moduleWriter {
		want += 2
	}
	if !ok || len(steps) != want {
		return fmt.Errorf("unexpected %s preparation or publication", spec.label)
	}
	step := func(i int) map[string]any {
		s, _ := steps[i].(map[string]any)
		return s
	}
	checkout := step(0)
	if uses, _ := checkout["uses"].(string); !strings.HasPrefix(uses, "actions/checkout@") ||
		!reflect.DeepEqual(checkout["with"], map[string]any{"ref": "${{ github.sha }}", "persist-credentials": false}) {
		return fmt.Errorf("%s must use the immutable main SHA without credentials", spec.label)
	}
	setup := step(1)
	if uses, _ := setup["uses"].(string); !strings.HasPrefix(uses, "actions/setup-go@") ||
		!reflect.DeepEqual(setup["env"], map[string]any{"GOCACHE": spec.bootstrap}) {
		return fmt.Errorf("setup diagnostics must use a separate temporary bootstrap cache")
	}
	if !reflect.DeepEqual(setup["with"], map[string]any{"go-version-file": "go.mod", "cache": false}) {
		return fmt.Errorf("%s must select pinned Go without implicit caches", spec.label)
	}
	modules := step(2)
	if spec.moduleWriter && modules["id"] != "go-module-cache" {
		return fmt.Errorf("%s must name its module restore so it can save a miss", spec.label)
	}
	if modules["uses"] != reuseRestoreAction || !reflect.DeepEqual(modules["with"], map[string]any{
		"path":         moduleDownloadCachePath,
		"key":          "go-mod-v2-${{ runner.os }}-${{ hashFiles('go.mod', 'go.sum', 'scripts/ci/go-tool-modules.txt') }}",
		"restore-keys": "go-mod-v2-${{ runner.os }}-",
	}) {
		return fmt.Errorf("%s must read the shared module cache with the standard reader", spec.label)
	}
	abi := step(3)
	if abi["id"] != "runner-cache-abi" || abi["run"] != "./scripts/ci/export-runner-cache-abi.sh" {
		return fmt.Errorf("%s must select the runner cache ABI", spec.label)
	}
	if spec.generation != "" {
		generation := step(4)
		if len(generation) != 3 || generation["name"] == nil || generation["id"] != spec.generationID || generation["run"] != spec.generation {
			return fmt.Errorf("%s must rotate its key with exactly the reviewed generation step", spec.label)
		}
	}
	lookup := step(next)
	if lookup["id"] != spec.lookupID || lookup["uses"] != reuseRestoreAction || lookup["if"] != nil || lookup["continue-on-error"] != nil ||
		!reflect.DeepEqual(lookup["with"], map[string]any{
			"path":         "~/.cache/go-build",
			"key":          spec.key,
			"restore-keys": spec.restoreKeys,
			"lookup-only":  true,
		}) {
		return fmt.Errorf("%s lookup must use the shared key and never restore historical entries", spec.label)
	}
	miss := "steps." + spec.lookupID + ".outputs.cache-hit != 'true'"
	compile := step(next + 1)
	if compile["run"] != spec.compile || compile["if"] != miss || compile["continue-on-error"] != nil || compile["env"] != nil {
		return fmt.Errorf("fresh %s must run exactly its reviewed compile into an empty default cache", spec.label)
	}
	save := step(next + 2)
	if save["uses"] != reuseSaveAction || save["continue-on-error"] != nil ||
		save["if"] != "success() && "+miss+" && (github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'" ||
		!reflect.DeepEqual(save["with"], map[string]any{"path": "~/.cache/go-build", "key": "${{ steps." + spec.lookupID + ".outputs.cache-primary-key }}"}) {
		return fmt.Errorf("only a successful main seed may publish, once per exact key")
	}
	if spec.moduleWriter {
		moduleMiss := "steps.go-module-cache.outputs.cache-hit != 'true'"
		populate := step(next + 3)
		if populate["run"] != moduleWriterPopulate || populate["if"] != moduleMiss || populate["continue-on-error"] != nil || populate["env"] != nil {
			return fmt.Errorf("%s must fully populate a missed module cache before saving it", spec.label)
		}
		moduleSave := step(next + 4)
		if moduleSave["uses"] != reuseSaveAction || moduleSave["continue-on-error"] != nil ||
			moduleSave["if"] != "success() && "+moduleMiss+" && (github.event_name == 'workflow_dispatch' || github.event_name == 'schedule' || github.event_name == 'repository_dispatch') && github.ref == 'refs/heads/main'" ||
			!reflect.DeepEqual(moduleSave["with"], map[string]any{"path": moduleDownloadCachePath, "key": "${{ steps.go-module-cache.outputs.cache-primary-key }}"}) {
			return fmt.Errorf("only a successful main seed may publish the module cache, once per exact key")
		}
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
	for _, reader := range []struct{ workflow, job string }{
		{"ci.yml", "race_shard"}, {"reliability.yml", "race-isolation"}, {"reliability.yml", "race-analyzers"},
	} {
		workflowName, job := reader.workflow, reader.job
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
	// The lint seed guards before its disk script, which creates the directory.
	for file, guardEnd := range map[string]string{"ci-race-cache.yml": "go list -deps", "ci-lint-cache.yml": "./scripts/ci/prepare-go-cache-disk.sh"} {
		t.Run(file, func(t *testing.T) { testSeedGuardRequiresFreshDefaultDirectory(t, file, guardEnd) })
	}
}

func testSeedGuardRequiresFreshDefaultDirectory(t *testing.T, file, guardEnd string) {
	workflow, _ := readWorkflowFile(t, file)
	steps := workflowJobSteps(t, workflow, "seed")
	// The compile step is the one run step holding the guard's end marker.
	var prefix string
	found := false
	for _, raw := range steps {
		if run, ok := raw.(map[string]any)["run"].(string); ok {
			if before, _, ok := strings.Cut(run, guardEnd); ok {
				prefix, found = before, true
			}
		}
	}
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
