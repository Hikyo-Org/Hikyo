package badstablecopies // want "stablecopy: scripts/ci/pr-gate/main.go stableJSON options \\[json.Deterministic\\(true\\)\\] differ" "stablecopy: .*analysis-shards-go/main.go: no var stableJSON"

// The anchor the stablecopies analyzer runs on; the files it compares sit under
// the fixture root, mirroring the repository layout.
