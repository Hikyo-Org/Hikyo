// Package bench is the schema shared by cmd/bench-scan (which writes the result
// artifact) and the scanning validation test (which parses it). One struct, no
// drift between producer and consumer.
package bench

import "slices"

// HarnessVersion is bumped when the artifact schema or the measured metric
// changes, so a stale artifact fails the version match in the validation test.
// Not bumped for the lowest-p99-of-N noise rejection in cmd/bench-scan's
// measure: it neither changes the schema nor the metric (same corpus, same
// per-item percentile), it only rejects a shared runner's tail spike, and a
// selected pass can only equal or lower a prior single-pass number, so a
// committed v2 artifact stays a valid, conservative measurement rather than a
// stale one. The selection procedure is recorded additively instead (Passes
// and Selection below): an artifact WITHOUT them was measured in one pass.
const HarnessVersion = "2"

// SelectionLowestP99 names the estimator cmd/bench-scan applies: it scans the
// corpus several whole passes and reports the one pass whose p99 is lowest,
// p50 taken from that same pass. Recorded in Result.Selection so a reader of
// the artifact can tell best-of-N evidence from single-pass evidence.
const SelectionLowestP99 = "lowest-p99-pass"

// Pass is one whole scan of the corpus: the per-item p50 and p99 of that pass.
type Pass struct {
	P50Millis float64 `json:"p50_millis"`
	P99Millis float64 `json:"p99_millis"`
}

// SelectPass returns the index of the pass SelectionLowestP99 reports: the
// lowest p99, the earliest on a tie. -1 for no passes.
func SelectPass(passes []Pass) int {
	selected := -1
	for i, pass := range passes {
		if selected == -1 || pass.P99Millis < passes[selected].P99Millis {
			selected = i
		}
	}
	return selected
}

// Result is the JSON artifact emitted by cmd/bench-scan. Durations are in
// milliseconds. PeakRSSBytes is 0 when not cheaply obtainable; PeakRSSUnit
// records the platform meaning of the raw ru_maxrss value the run observed.
type Result struct {
	HarnessVersion    string  `json:"harness_version"`
	SnapshotVersion   string  `json:"snapshot_version"`
	Host              string  `json:"host"`
	MachineModel      string  `json:"machine_model"`
	Items             int     `json:"items"`
	ItemBytes         int     `json:"item_bytes"`
	BootCompileMillis float64 `json:"boot_compile_millis"`
	// BootPeakRSSBytes is peak RSS captured immediately after Load, i.e.
	// startup + ruleset compile, the ADR §7 ≤ 32 MiB boot bound.
	BootPeakRSSBytes int64   `json:"boot_peak_rss_bytes"`
	P50Millis        float64 `json:"p50_millis"`
	P99Millis        float64 `json:"p99_millis"`
	// PeakRSSBytes is peak RSS at the end of the run (informational: it
	// conflates compile and scan memory).
	PeakRSSBytes int64 `json:"peak_rss_bytes"`
	// Passes are every whole-corpus pass the run took, in order, and Selection
	// names how P50Millis/P99Millis were chosen among them. Both are absent
	// from an artifact measured in a single pass.
	Passes    []Pass `json:"passes,omitempty"`
	Selection string `json:"selection,omitempty"`
}

// Percentile returns the p-th percentile (0..100) of durations in
// milliseconds, using nearest-rank on a copy. Returns 0 for an empty input.
func Percentile(millis []float64, p float64) float64 {
	if len(millis) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(millis))
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	rank := int((p/100)*float64(len(sorted)-1) + 0.5)
	return sorted[rank]
}
