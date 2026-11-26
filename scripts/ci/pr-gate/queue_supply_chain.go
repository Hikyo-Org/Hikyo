package main

import (
	"context"
	"errors"
	"strings"
)

// ci-feedback runs the supply-chain fixtures as a separate job beside its
// matrix lanes, because the fixtures need minutes where a lane has seconds. A
// selection job decides whether the fixtures run and the fixtures job is
// skipped when it selects nothing. The selection job executes the PR's own
// merge commit, so a PR can make it select nothing: its verdict is not trusted
// for admission. A skipped fixtures job is accepted only after
// supplyChainRefusal recomputes the selection from the base commit's input list
// and the PR's changed files, in this privileged gate. The fixtures job itself
// checks out the PR head, and the feedback lanes run the merge commit's own
// tooling: that is the existing baseline, which this gate does not widen.
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
func judgeSupplyChain(supply []job, head string) (failed, fixturesSkipped bool, err error) {
	var selection, fixtures []job
	for _, j := range supply {
		if j.HeadSHA != head {
			return false, false, errors.New("feedback job source mismatch")
		}
		if j.Name == supplyChainSelectName {
			selection = append(selection, j)
		} else {
			fixtures = append(fixtures, j)
		}
	}
	if len(selection) != 1 || len(fixtures) != 1 {
		return false, false, errors.New("missing or duplicated supply-chain fixture job")
	}
	selected := selection[0].Status == "completed" && selection[0].Conclusion == "success"
	f := fixtures[0]
	ran := f.Status == "completed" && f.Conclusion == "success"
	skipped := f.Status == "completed" && f.Conclusion == "skipped"
	return !selected || (!ran && !skipped), skipped, nil
}

const (
	supplyChainListPath     = "scripts/ci/supply-chain-fixture-inputs.txt"
	supplyChainSelectorPath = "scripts/ci/select-supply-chain-fixtures.sh"
	supplyChainFeedbackPath = ".github/workflows/ci-feedback.yml"
	supplyChainWorkflowPath = ".github/workflows/ci.yml"
	supplyChainJobKey       = "  supply-chain-checks:"
)

// supplyChainJobBlock returns the lines of ci.yml's supply-chain-checks job, as
// select-supply-chain-fixtures.sh extracts them: up to the next line indented by
// exactly two spaces.
func supplyChainJobBlock(workflow []byte) string {
	var block []string
	in := false
	for _, line := range strings.Split(string(workflow), "\n") {
		switch {
		case line == supplyChainJobKey:
			in = true
		case in && len(line) > 2 && line[:2] == "  " && line[2] != ' ':
			return strings.Join(block, "\n")
		case in:
			block = append(block, line)
		}
	}
	return strings.Join(block, "\n")
}

// supplyChainSelects reports whether path is one of the fixtures' inputs: an
// exact entry, or under an entry ending in a slash.
func supplyChainSelects(entries []string, path string) bool {
	switch path {
	case "":
		return false
	case supplyChainListPath, supplyChainSelectorPath, supplyChainFeedbackPath:
		return true
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry, "/") && strings.HasPrefix(path, entry) || path == entry {
			return true
		}
	}
	return false
}

// supplyChainRefusal recomputes, from trusted inputs, whether the PR must have
// run the supply-chain fixtures: any changed file listed in the base commit's
// input list, the list, the selector or ci-feedback.yml themselves, or a change
// to the steps of ci.yml's job. The caller applies it when the fixtures job was
// skipped, whatever the PR's own selection job reported. It costs one contents
// read of the base list, plus two of ci.yml when ci.yml is in the file list.
func (q *queueController) supplyChainRefusal(ctx context.Context, p pull, files []pullFile) (string, error) {
	base := p.admission().SHA
	raw, err := q.contents(ctx, supplyChainListPath, base)
	if err != nil {
		return "", err
	}
	var entries []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			entries = append(entries, line)
		}
	}
	if len(entries) == 0 {
		return "", errors.New("the supply-chain fixture input list is empty at the base")
	}
	refusal := func(path string) string {
		return "This PR changes a supply-chain fixture input (" + path + "), so the supply-chain fixtures must run and pass; the fixtures job was skipped."
	}
	ciChanged := false
	for _, file := range files {
		for _, path := range []string{file.Name, file.Previous} {
			if supplyChainSelects(entries, path) {
				return refusal(path), nil
			}
			ciChanged = ciChanged || path == supplyChainWorkflowPath
		}
	}
	if !ciChanged {
		return "", nil
	}
	before, err := q.contents(ctx, supplyChainWorkflowPath, base)
	if err != nil {
		return "", err
	}
	after, err := q.contents(ctx, supplyChainWorkflowPath, p.Head.SHA)
	if err != nil {
		return "", err
	}
	if supplyChainJobBlock(before) != supplyChainJobBlock(after) {
		return refusal(supplyChainWorkflowPath + " supply-chain-checks"), nil
	}
	return "", nil
}
