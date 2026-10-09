package main

import "testing"

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
	total, longest := 0.0, 0.0
	for index, shard := range shards {
		t.Logf("shard %d: predicted %.0fs (sequential %.0fs, pool %.0fs, longest pool entry %.0fs)",
			index, shard.cost(), shard.sequential, shard.pool, shard.longest)
		total += shard.cost()
		longest = max(longest, shard.cost())
	}
	if mean := total / float64(len(shards)); longest > mean*1.1 {
		t.Fatalf("longest shard predicts %.0fs, over 110%% of the %.0fs mean", longest, mean)
	}
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
