package main

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCompletePlanMatchesIndividualModes(t *testing.T) {
	packages, err := listPackages("../../..")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planAll(packages, 12, 3)
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range []struct {
		kind string
		plan [][]string
	}{{"race", plan.RacePlan}, {"fuzz", plan.FuzzPlan}, {"isolation", plan.IsolationPlan}} {
		for index, want := range family.plan {
			var output bytes.Buffer
			opts := options{shard: index, shardCount: len(family.plan)}
			switch family.kind {
			case "race":
				err = writeRaceShard(&output, packages, opts)
			case "fuzz":
				err = writeFuzzShard(&output, packages, opts)
			case "isolation":
				err = writeIsolationShard(&output, packages, opts, false)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(want, "\n") + "\n"; got != output.String() {
				t.Fatalf("%s shard %d changed assignments, order or filters", family.kind, index)
			}
		}
	}
	for index, got := range plan.RaceShards {
		if got != index {
			t.Fatal("race indices must cover every emitted shard in order")
		}
	}
	if !reflect.DeepEqual(plan.Shards, []int{0, 1, 2}) {
		t.Fatal("fuzz and isolation indices must cover every emitted shard")
	}
}

// The real command must inventory once; a missing family or bad target must
// not leave a partial object that downstream matrices can mistake for a plan.
func TestAllCommandInventoryAndRefusals(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"extra", "internal/isolation"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"extra/extra_test.go":                  "package extra\nimport \"testing\"\nfunc FuzzExtra(f *testing.F) {}\n",
		"internal/isolation/isolation_test.go": "package isolation\nimport \"testing\"\nfunc TestOne(t *testing.T) {}\n",
	}
	for path, contents := range files {
		if err := os.WriteFile(filepath.Join(root, path), []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
	}
	inventory := filepath.Join(root, "inventory.json")
	var data bytes.Buffer
	for _, dir := range []string{"extra", "internal/isolation"} {
		pkg := packageInfo{ImportPath: "example.com/" + dir, Dir: filepath.Join(root, dir), TestGoFiles: []string{filepath.Base(dir) + "_test.go"}}
		pkg.Module = &struct{ Dir string }{Dir: root}
		if err := json.MarshalEncode(jsontext.NewEncoder(&data, stableJSON), pkg); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(inventory, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	calls := filepath.Join(root, "calls")
	stub := filepath.Join(root, "go")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nset -eu\ntest \"$*\" = 'list -json ./...'\nprintf 'list\\n' >>\"$PLANNER_CALLS\"\ncat \"$PLANNER_INVENTORY\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GO_BIN", stub)
	t.Setenv("PLANNER_CALLS", calls)
	t.Setenv("PLANNER_INVENTORY", inventory)
	args := []string{"all", "--root", root, "--race-shards", "1", "--shards", "1"}
	var output bytes.Buffer
	if err := run(args, &output); err != nil {
		t.Fatal(err)
	}
	var plan completePlan
	if err := json.Unmarshal(output.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.FuzzPlan, [][]string{{"example.com/extra\tFuzzExtra"}}) || !reflect.DeepEqual(plan.IsolationPlan, [][]string{{"TestOne"}}) {
		t.Fatal("command lost discovered targets")
	}
	seen, err := os.ReadFile(calls)
	if err != nil || string(seen) != "list\n" {
		t.Fatalf("complete command must inventory exactly once: %q, %v", seen, err)
	}
	for name, bad := range map[string][]string{
		"missing counts": {"all"}, "negative": {"all", "--race-shards", "-1", "--shards", "1"},
		"unbounded":      {"all", "--race-shards", "257", "--shards", "1"},
		"extra argument": append(append([]string(nil), args...), "extra"),
		"empty shard":    {"all", "--root", root, "--race-shards", "2", "--shards", "1"},
	} {
		t.Run(name, func(t *testing.T) {
			var partial bytes.Buffer
			if err := run(bad, &partial); err == nil || partial.Len() != 0 {
				t.Fatalf("invalid complete plan must fail without output: %v, %q", err, partial.String())
			}
		})
	}
	if err := os.WriteFile(filepath.Join(root, "internal/isolation/isolation_test.go"), []byte("package isolation\nfunc TestBroken(\n"), 0600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := run(args, &output); err == nil || output.Len() != 0 {
		t.Fatal("target discovery errors must not publish partial output")
	}
}

func TestAllCapabilityDoesNotInventory(t *testing.T) {
	t.Setenv("GO_BIN", filepath.Join(t.TempDir(), "must-not-execute"))
	var output bytes.Buffer
	if err := run([]string{"--supports-all-v1"}, &output); err != nil || output.String() != "analysis-shards-all-v1\n" {
		t.Fatalf("capability probe: %v, %q", err, output.String())
	}
}

// TestRacePlanBalancesRepository plans this repository's twelve race shards and
// logs each predicted duration; run with -v after regenerating the weights.
func TestRacePlanBalancesRepository(t *testing.T) {
	packages, err := listPackages("../../..")
	if err != nil {
		t.Fatal(err)
	}
	shards, err := planRace(packages, 12)
	if err != nil {
		t.Fatal(err)
	}
	limit := raceShardJobSeconds(t)
	if timeout := raceGoTestTimeoutSeconds(t); raceInvocationBudgetSeconds > timeout/2 {
		t.Fatalf("race invocation budget %.0fs exceeds half of the %.0fs go test -timeout in test-race-packages.sh", raceInvocationBudgetSeconds, timeout)
	}
	total, longest := 0.0, 0.0
	for index, shard := range shards {
		t.Logf("shard %d: predicted %.0fs (sequential %.0fs, pool %.0fs, longest pool entry %.0fs)",
			index, shard.cost(), shard.sequential, shard.pool, shard.longest)
		total += shard.cost()
		longest = max(longest, shard.cost())
		if planned := shard.cost() + raceJobOverheadSeconds; planned > limit {
			t.Errorf("shard %d plans %.0fs with job overhead, over the %.0fs race_shard job timeout", index, planned, limit)
		}
		// The budget fallback is silent; fail here so a recalibration that
		// saturates it splits the package or adds race shards instead.
		for path, seconds := range shard.seconds {
			if seconds+raceProcessSetupSeconds > raceInvocationBudgetSeconds {
				t.Errorf("shard %d plans %.0fs for %s, over the %.0fs invocation budget", index, seconds+raceProcessSetupSeconds, path, raceInvocationBudgetSeconds)
			}
		}
	}
	if mean := total / float64(len(shards)); longest > mean*1.1 {
		t.Fatalf("longest shard predicts %.0fs, over 110%% of the %.0fs mean", longest, mean)
	}
}

// raceJobOverheadSeconds allows for race_shard steps outside test-race-packages.sh
// (checkout, toolchain, cache restore, PostgreSQL, artifact upload): 61 to 104 s
// per job in ci-dev run 44901528773.
const raceJobOverheadSeconds = 120.0

// raceShardJobSeconds reads the race_shard job's timeout-minutes from ci.yml.
func raceShardJobSeconds(t *testing.T) float64 {
	t.Helper()
	workflow, err := os.ReadFile("../../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?m)^  race_shard:\n(?:    .*\n|\n)*?    timeout-minutes: (\d+)\n`).FindSubmatch(workflow)
	if match == nil {
		t.Fatal("ci.yml race_shard job has no timeout-minutes")
	}
	minutes, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatal(err)
	}
	return float64(minutes) * 60
}

// raceGoTestTimeoutSeconds reads the race go test -timeout from
// test-race-packages.sh, which every planned invocation must finish within.
func raceGoTestTimeoutSeconds(t *testing.T) float64 {
	t.Helper()
	script, err := os.ReadFile("../../../scripts/ci/test-race-packages.sh")
	if err != nil {
		t.Fatal(err)
	}
	matches := regexp.MustCompile(`go test -json -race [^\n]*-timeout=(\S+)`).FindAllSubmatch(script, -1)
	if len(matches) != 1 {
		t.Fatal("test-race-packages.sh must run exactly one go test -race with -timeout")
	}
	timeout, err := time.ParseDuration(string(matches[0][1]))
	if err != nil {
		t.Fatal(err)
	}
	return timeout.Seconds()
}

// Split targets share startup, while an additional process consumes overhead.
func TestRacePlanChargesSetupOncePerPackage(t *testing.T) {
	pkg := &packageInfo{ImportPath: "example/internal/app", relativePath: "internal/app"}
	shard := raceShard{seconds: map[string]float64{}}
	first := raceUnit{pkg: pkg, seconds: 100}
	shard = shard.with(first)
	shard.seconds[pkg.ImportPath] = first.seconds
	shard = shard.with(raceUnit{pkg: pkg, seconds: 50})
	if want := 150 + raceProcessSetupSeconds; shard.cost() != want {
		t.Fatalf("split suite cost = %v, want %v", shard.cost(), want)
	}
	peer := &packageInfo{ImportPath: "example/internal/service", relativePath: "internal/service"}
	shard = shard.with(raceUnit{pkg: peer, seconds: 10})
	if want := 160 + 2*raceProcessSetupSeconds; shard.cost() != want {
		t.Fatalf("additional package cost = %v, want %v", shard.cost(), want)
	}
}

func TestRacePlanChargesSetupInPool(t *testing.T) {
	pkg := &packageInfo{ImportPath: "example/pool", relativePath: "pool"}
	shard := raceShard{seconds: map[string]float64{}}
	shard = shard.with(raceUnit{pkg: pkg, seconds: 100})
	shard.seconds[pkg.ImportPath] = 100
	shard = shard.with(raceUnit{pkg: pkg, seconds: 50})
	if want := 150 + raceProcessSetupSeconds; shard.cost() != want || shard.pool != want {
		t.Fatalf("pooled split suite = %+v, want cost/pool %v", shard, want)
	}
	peer := &packageInfo{ImportPath: "example/peer", relativePath: "peer"}
	shard = shard.with(raceUnit{pkg: peer, seconds: 100})
	if want := 250 + 2*raceProcessSetupSeconds; shard.pool != want {
		t.Fatalf("pooled work = %v, want %v", shard.pool, want)
	}
}

// A split target leaves the cheapest shard when it would push that shard's
// invocation past the budget, and falls back to cost when no shard fits.
func TestRacePlanKeepsInvocationsWithinBudget(t *testing.T) {
	app := &packageInfo{ImportPath: "example/internal/app", relativePath: "internal/app"}
	peer := &packageInfo{ImportPath: "example/peer", relativePath: "peer"}
	// Place units the way planRace does: totals first, then the package seconds.
	place := func(pkg *packageInfo, seconds float64) raceShard {
		shard := raceShard{seconds: map[string]float64{}}.with(raceUnit{pkg: pkg, seconds: seconds})
		shard.seconds[pkg.ImportPath] += seconds
		return shard
	}
	shards := []raceShard{place(app, raceInvocationBudgetSeconds-50), place(peer, 2000)}
	if got := bestRaceShard(shards, raceUnit{pkg: app, seconds: 100}); got != 1 {
		t.Fatalf("over-budget target went to shard %d, want 1", got)
	}
	if got := bestRaceShard(shards, raceUnit{pkg: app, seconds: 10}); got != 0 {
		t.Fatalf("in-budget target went to shard %d, want 0", got)
	}
	// Setup counts: 550 + 40 fits without it, but 550 + 40 + 20 does not.
	if got := bestRaceShard(shards, raceUnit{pkg: app, seconds: 40}); got != 1 {
		t.Fatalf("target over budget only with setup went to shard %d, want 1", got)
	}
	// 550 + 30 + 20 lands exactly on the budget, which still fits.
	if got := bestRaceShard(shards, raceUnit{pkg: app, seconds: 30}); got != 0 {
		t.Fatalf("target exactly at the budget went to shard %d, want 0", got)
	}
	if got := bestRaceShard(shards, raceUnit{pkg: peer, seconds: raceInvocationBudgetSeconds}); got != 0 {
		t.Fatalf("oversized unit went to shard %d, want the cheapest shard 0", got)
	}
	// A fitting shard keeps the unit even when a later shard is cheaper.
	reversed := []raceShard{place(peer, 2000), place(app, raceInvocationBudgetSeconds-50)}
	if got := bestRaceShard(reversed, raceUnit{pkg: app, seconds: 100}); got != 0 {
		t.Fatalf("cheaper over-budget shard took the target from fitting shard 0: got %d", got)
	}
}
