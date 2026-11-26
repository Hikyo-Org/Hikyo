package main

import "errors"

// ci-feedback runs the supply-chain fixtures as a separate job beside its
// matrix lanes, because the fixtures need minutes where a lane has seconds. A
// selection job decides from the base commit's classifier whether the fixtures
// run; the fixtures job is skipped when it selects nothing.
const (
	supplyChainSelectName   = "select supply-chain fixtures"
	supplyChainFixturesName = "supply-chain fixtures"
)

// splitSupplyChainJobs separates the two supply-chain jobs from the lane jobs so
// the lane and ignored-metadata logic judges only the jobs it was written for.
func splitSupplyChainJobs(all []job) (lanes []job, supply []job) {
	for _, j := range all {
		if j.Name == supplyChainSelectName || j.Name == supplyChainFixturesName {
			supply = append(supply, j)
		} else {
			lanes = append(lanes, j)
		}
	}
	return lanes, supply
}

// ignoredSupplyChain judges a metadata-only run, which never grants admission:
// whichever supply-chain jobs GitHub has materialized must be unique and skipped.
func ignoredSupplyChain(supply []job) error {
	seen := map[string]bool{}
	for _, j := range supply {
		if seen[j.Name] || j.Status != "completed" || j.Conclusion != "skipped" {
			return errors.New("invalid ignored metadata feedback")
		}
		seen[j.Name] = true
	}
	return nil
}

// judgeSupplyChain applies dev admission to a completed run. The selection job
// and the fixtures job must each appear exactly once at the exact head. The
// selection job must succeed. The fixtures job must succeed, or be skipped, and
// a skip counts only because the selection job succeeded and chose nothing: a
// skipped fixtures job under a failed, cancelled or missing selection is a
// failure. It returns whether the run must be treated as failed.
func judgeSupplyChain(supply []job, head string) (failed bool, err error) {
	var selection, fixtures []job
	for _, j := range supply {
		if j.HeadSHA != head {
			return false, errors.New("feedback job source mismatch")
		}
		if j.Name == supplyChainSelectName {
			selection = append(selection, j)
		} else {
			fixtures = append(fixtures, j)
		}
	}
	if len(selection) != 1 || len(fixtures) != 1 {
		return false, errors.New("missing or duplicated supply-chain fixture job")
	}
	selected := selection[0].Status == "completed" && selection[0].Conclusion == "success"
	f := fixtures[0]
	ran := f.Status == "completed" && f.Conclusion == "success"
	skipped := f.Status == "completed" && f.Conclusion == "skipped"
	return !selected || !(ran || skipped), nil
}
