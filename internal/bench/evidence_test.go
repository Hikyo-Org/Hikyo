package bench

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	scanbench "github.com/Hikyo-Org/hikyo/internal/scanning/bench"
)

func validEvidence(t *testing.T) Evidence {
	t.Helper()
	var policy Derating
	raw, err := os.ReadFile("../../docs/release/measurements/derate.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	digest := strings.Repeat("b", 64)
	e := Evidence{Schema: "hikyo.dev/floor-bench/v1", Architecture: "arm64", OS: "linux", CPU: 4, MemoryLimit: 4 << 30, MemoryPeak: 500 << 20,
		Provenance: Provenance{SourceCommit: commit, SourceDiffSHA256: digest, RunURL: "https://example.test/run", Image: "sha256:" + digest, BinarySHA256: map[string]string{"hikyo": digest, "floor.test": digest, "isolation.test": digest, "bench-scan": digest}},
		BootMS:     100, IdleRSS: 30 << 20, IdleSampleSeconds: 5, ArgonMS: 100, AdmissionMiB: 272, ArgonMemoryKiB: 65536, ArgonTime: 3, ArgonParallelism: 2, ArgonConcurrent: 4, ArgonCompleted: 4,
		Publish:    Publish{ElapsedMS: 1000, Cells: 100000, Environments: 10, Keys: 10000, ReadbackVerified: true},
		Reencrypt:  Reencrypt{ElapsedMS: 250, ValueRows: 250, RowsMoved: 251, ChunkRows: 100, ConfiguredPauseMS: 100, MinimumIntervalMS: 101, CommittedProgression: []int{0, 100, 200}, ReadbackVerified: true},
		ScannerRun: ScannerRun{Image: "sha256:" + digest, Entrypoint: []string{"/bench-scan"}, Status: "exited", NanoCPUs: 4000000000, Memory: 4 << 30, MemorySwap: 4 << 30},
		Scanner:    scanbench.Result{HarnessVersion: scanbench.HarnessVersion, SnapshotVersion: "fixture", MachineModel: "synthetic validation fixture", Items: 38, ItemBytes: 65536, BootCompileMillis: 2, P99Millis: 1, BootPeakRSSBytes: 8 << 20},
		Operator:   Operator{Schema: "hikyo.dev/operator-floor-evidence/v1", SourceCommit: commit, SourceDiffSHA256: digest, BinarySHA256: digest, Architecture: "arm64", OuterCPU: 4, OuterMemory: 4 << 30, CPU: 200, MemoryLimit: 128 << 20, MemoryPeak: 64 << 20, RSSPeak: 32 << 20, Passed: true}, Derating: policy}
	e.Operator.RSSMeasurement.Process.PID = 123
	e.Operator.RSSMeasurement.Process.StartTime = 456
	e.Operator.RSSMeasurement.Process.ContainerID = digest
	e.Operator.RSSMeasurement.Process.BinarySHA256 = digest
	e.Operator.RSSMeasurement.Process.RSS = 31 << 20
	e.Operator.RSSMeasurement.Process.PeakRSS = 32 << 20
	return e
}

func TestFloorGateRefusesIncompleteOrOverBudgetEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Evidence)
	}{
		{"missing CPU factor", func(e *Evidence) { delete(e.Derating.CPU, "publish_ms") }},
		{"zero CPU factor", func(e *Evidence) { e.Derating.CPU["publish_ms"] = CPUFactor{Factor: 0} }},
		{"uncalibrated optimistic factor", func(e *Evidence) { e.Derating.CPU["publish_ms"] = CPUFactor{Factor: 1} }},
		{"NaN measurement", func(e *Evidence) { e.Publish.ElapsedMS = math.NaN() }},
		{"derated publish exceeds 30 seconds", func(e *Evidence) { e.Publish.ElapsedMS = 7500.001 }},
		{"derated scanner exceeds 5ms", func(e *Evidence) { e.Scanner.P99Millis = 1.251 }},
		{"derated compile exceeds 2 seconds", func(e *Evidence) { e.Scanner.BootCompileMillis = 501 }},
		{"missing scanner execution", func(e *Evidence) { e.ScannerRun = ScannerRun{} }},
		{"scanner wrong image", func(e *Evidence) { e.ScannerRun.Image = "other" }},
		{"scanner unconstrained", func(e *Evidence) { e.ScannerRun.Memory = 0 }},
		{"scanner failed", func(e *Evidence) { e.ScannerRun.ExitCode = 1 }},
		{"missing scanner memory", func(e *Evidence) { e.Scanner.BootPeakRSSBytes = 0 }},
		{"scanner memory cannot be derated", func(e *Evidence) { e.Derating.MemoryFactor = 0.25 }},
		{"missing operator evidence", func(e *Evidence) { e.Operator = Operator{} }},
		{"stale operator source", func(e *Evidence) { e.Operator.SourceCommit = strings.Repeat("c", 40) }},
		{"different operator source diff", func(e *Evidence) { e.Operator.SourceDiffSHA256 = strings.Repeat("c", 64) }},
		{"different operator binary", func(e *Evidence) { e.Operator.BinarySHA256 = strings.Repeat("c", 64) }},
		{"operator RSS at 128MiB", func(e *Evidence) { e.Operator.RSSPeak = 128 << 20 }},
		{"missing operator RSS", func(e *Evidence) { e.Operator.RSSPeak = 0 }},
		{"missing operator process identity", func(e *Evidence) { e.Operator.RSSMeasurement.Process.PID = 0 }},
		{"wrong measured operator executable", func(e *Evidence) { e.Operator.RSSMeasurement.Process.BinarySHA256 = strings.Repeat("c", 64) }},
		{"missing binary identity", func(e *Evidence) { delete(e.Provenance.BinarySHA256, "isolation.test") }},
		{"three verifications", func(e *Evidence) { e.ArgonCompleted = 3 }},
		{"sequential verifications", func(e *Evidence) { e.ArgonConcurrent = 1 }},
		{"less costly KDF", func(e *Evidence) { e.ArgonTime = 2 }},
		{"uncommitted publish", func(e *Evidence) { e.Publish.ReadbackVerified = false }},
		{"99k cells", func(e *Evidence) { e.Publish.Cells = 99000 }},
		{"rewrap oversize chunk", func(e *Evidence) { e.Reencrypt.CommittedProgression[1] = 150 }},
		{"rewrap removed pause", func(e *Evidence) { e.Reencrypt.MinimumIntervalMS = 99 }},
		{"rewrap incomplete", func(e *Evidence) { e.Reencrypt.ValueRows = 200 }},
		{"OOM allocation refusal without kill", func(e *Evidence) { e.OOMEvents = 1 }},
		{"OOM kill", func(e *Evidence) { e.OOMKills = 1 }},
		{"swap enabled", func(e *Evidence) { e.Swap = 1 }},
		{"wrong architecture", func(e *Evidence) { e.Architecture = "amd64" }},
		{"raw calibration cannot pass required CI", func(e *Evidence) { e.Raw = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := validEvidence(t)
			tc.mutate(&e)
			if err := e.Validate(); err == nil {
				t.Fatal("invalid floor evidence accepted")
			}
		})
	}
}

func TestFloorGateUsesDeclaredBoundsOnly(t *testing.T) {
	e := validEvidence(t)
	e.Publish.ElapsedMS = 7500
	e.Scanner.P99Millis = 1.25
	e.Scanner.BootCompileMillis = 500
	// Boot, KDF and reencrypt have no declared latency deadline in this gate.
	// Their measurements still require factors and appear as estimates.
	e.BootMS = 10000
	e.ArgonMS = 10000
	e.Reencrypt.ElapsedMS = 10000
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	if e.Estimates["publish_ms"] != 30000 || e.Estimates["reencrypt_ms"] != 40000 {
		t.Fatalf("derived estimates: %v", e.Estimates)
	}
}

func TestFloorMemoryCountersMustBePresent(t *testing.T) {
	for _, events := range []string{
		"", "oom 0\n", "oom_kill 0\n", "oom nope\noom_kill 0\n",
		"oom 0\noom_kill nope\n", "oom -1\noom_kill 0\n", "oom 0\noom_kill -1\n",
		"oom\noom_kill 0\n", "oom 0\noom_kill\n", "oom 0 0\noom_kill 0\n",
		"oom 0\noom 0\noom_kill 0\n", "oom 0\noom_kill 0\noom_kill 0\n",
		"oom 0\noom 1\noom_kill 0\n",
	} {
		if _, _, err := memoryOOMCounts(events); err == nil {
			t.Errorf("accepted malformed events %q", events)
		}
	}
	for _, tc := range []struct {
		events     string
		oom, kills int
	}{
		{"low 0\nmax 42\noom 0\noom_kill 0\n", 0, 0},
		{"oom 1\noom_kill 0\n", 1, 0},
		{"oom 3\noom_kill 2\n", 3, 2},
	} {
		oom, kills, err := memoryOOMCounts(tc.events)
		if err != nil || oom != tc.oom || kills != tc.kills {
			t.Fatalf("counter parse: oom=%d kills=%d err=%v", oom, kills, err)
		}
	}
}

func TestFloorOperatorRSSIsDistinctFromCgroupPageCache(t *testing.T) {
	e := validEvidence(t)
	e.Operator.MemoryPeak = 134295552 // actual observed page-cache-inclusive charge
	if err := e.Validate(); err != nil {
		t.Fatalf("valid bounded RSS refused because of diagnostic cgroup charge: %v", err)
	}
}
