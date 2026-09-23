package main

import "testing"

// TestRacePlanBalancesRepository plans this repository's six race shards and
// logs each predicted duration; run with -v after regenerating the weights.
func TestRacePlanBalancesRepository(t *testing.T) {
	packages, err := listPackages("../../..")
	if err != nil {
		t.Fatal(err)
	}
	shards, err := planRace(packages, 6)
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
