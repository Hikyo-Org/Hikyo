// Package bench defines the required floor measurement evidence and its gate.
package bench

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	scanbench "github.com/Hikyo-Org/hikyo/internal/scanning/bench"
)

type CPUFactor struct {
	Factor            float64 `json:"factor"`
	CalibrationCommit *string `json:"calibration_commit"`
	CalibrationDate   *string `json:"calibration_date"`
}
type Derating struct {
	Schema         string               `json:"schema"`
	PolicyDate     string               `json:"policy_date"`
	Authority      string               `json:"authority"`
	MemoryFactor   float64              `json:"memory_factor"`
	CPU            map[string]CPUFactor `json:"cpu"`
	Interpretation string               `json:"interpretation"`
}
type Publish struct {
	ElapsedMS        float64 `json:"elapsed_ms"`
	Cells            int     `json:"cells"`
	Environments     int     `json:"environments"`
	Keys             int     `json:"keys"`
	Operation        string  `json:"operation"`
	ReadbackVerified bool    `json:"readback_verified"`
}
type Reencrypt struct {
	ElapsedMS            float64 `json:"elapsed_ms"`
	ValueRows            int     `json:"value_rows"`
	RowsMoved            int     `json:"rows_moved"`
	ChunkRows            int     `json:"chunk_rows"`
	CommittedProgression []int   `json:"committed_progression"`
	MinimumIntervalMS    float64 `json:"minimum_interchunk_interval_ms"`
	ConfiguredPauseMS    int     `json:"configured_pause_ms"`
	ReadbackVerified     bool    `json:"readback_verified"`
}
type Operator struct {
	Schema           string `json:"schema"`
	SourceCommit     string `json:"source_commit"`
	SourceDiffSHA256 string `json:"source_diff_sha256"`
	BinarySHA256     string `json:"operator_binary_sha256"`
	Architecture     string `json:"architecture"`
	OuterCPU         int    `json:"outer_cpu"`
	OuterMemory      int64  `json:"outer_memory_bytes"`
	CPU              int    `json:"operator_cpu_millicores"`
	MemoryLimit      int64  `json:"operator_memory_limit_bytes"`
	MemoryPeak       int64  `json:"operator_memory_peak_bytes"`
	RSSPeak          int64  `json:"operator_rss_peak_bytes"`
	RSSMeasurement   struct {
		Process struct {
			PID          uint64 `json:"node_pid"`
			StartTime    uint64 `json:"start_time_ticks"`
			ContainerID  string `json:"container_id"`
			BinarySHA256 string `json:"executable_sha256"`
			RSS          int64  `json:"rss_bytes"`
			PeakRSS      int64  `json:"rss_peak_bytes"`
		} `json:"process"`
	} `json:"rss_measurement"`
	Swap   int64 `json:"swap_bytes"`
	Passed bool  `json:"passed"`
}

// ScannerRun binds the scanner's direct process launch to the same image and
// floor limits. A separate launch avoids inherited pre-exec RSS from the KDF
// parent's large address space contaminating Getrusage's high-water mark.
type ScannerRun struct {
	Image      string   `json:"image"`
	Entrypoint []string `json:"entrypoint"`
	Status     string   `json:"status"`
	ExitCode   int      `json:"exit_code"`
	OOMKilled  bool     `json:"oom_killed"`
	NanoCPUs   int64    `json:"nano_cpus"`
	Memory     int64    `json:"memory"`
	MemorySwap int64    `json:"memory_swap"`
}

type Provenance struct {
	SourceCommit     string            `json:"source_commit"`
	SourceDirty      bool              `json:"source_dirty"`
	SourceDiffSHA256 string            `json:"source_diff_sha256"`
	RunURL           string            `json:"run_url"`
	BinarySHA256     map[string]string `json:"binary_sha256"`
	Image            string            `json:"image"`
}
type Evidence struct {
	Schema            string             `json:"schema"`
	Status            string             `json:"status"`
	StartedAt         time.Time          `json:"started_at"`
	Architecture      string             `json:"architecture"`
	OS                string             `json:"os"`
	CPU               int                `json:"cpu"`
	MemoryLimit       int64              `json:"memory_limit_bytes"`
	Swap              int64              `json:"swap_bytes"`
	MemoryPeak        int64              `json:"memory_peak_bytes"`
	OOMEvents         int                `json:"oom_events"`
	OOMKills          int                `json:"oom_kills"`
	PhysicalPi        bool               `json:"physical_pi"`
	Raw               bool               `json:"raw"`
	Provenance        Provenance         `json:"provenance"`
	BootMS            float64            `json:"boot_ms"`
	IdleRSS           int64              `json:"idle_rss_bytes"`
	IdleSampleSeconds int                `json:"idle_sample_seconds"`
	ArgonMS           float64            `json:"argon2_four_ms"`
	AdmissionMiB      int                `json:"admission_mib"`
	ArgonMemoryKiB    uint32             `json:"argon_memory_kib"`
	ArgonTime         uint32             `json:"argon_time"`
	ArgonParallelism  uint8              `json:"argon_parallelism"`
	ArgonConcurrent   int                `json:"argon_concurrent"`
	ArgonCompleted    int                `json:"argon_completed"`
	Publish           Publish            `json:"publish"`
	Reencrypt         Reencrypt          `json:"reencrypt"`
	Scanner           scanbench.Result   `json:"scanner"`
	ScannerRun        ScannerRun         `json:"scanner_run"`
	Operator          Operator           `json:"operator"`
	Derating          Derating           `json:"derating"`
	Estimates         map[string]float64 `json:"cpu_estimates_ms"`
	Interpretation    string             `json:"interpretation"`
}

var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Validate checks measurements, provenance and declared bounds. There is no
// passing path for an absent metric, a raw calibration run or stale operator
// evidence. CPU estimates with no declared latency bound remain informational.
func (e *Evidence) Validate() error {
	if e.Schema != "hikyo.dev/floor-bench/v1" || e.Raw || e.PhysicalPi {
		return errors.New("required gate needs a CI floor estimate, not raw or physical Pi acceptance")
	}
	if e.Architecture != "arm64" || e.OS != "linux" || e.CPU != 4 || e.MemoryLimit != 4<<30 || e.Swap != 0 || e.MemoryPeak <= 0 || e.MemoryPeak > e.MemoryLimit || e.OOMEvents != 0 || e.OOMKills != 0 {
		return errors.New("native floor limits or memory evidence differ")
	}
	p := e.Provenance
	if !commitPattern.MatchString(p.SourceCommit) || !digestPattern.MatchString(p.SourceDiffSHA256) || p.RunURL == "" || p.Image == "" {
		return errors.New("source/run provenance missing")
	}
	for _, name := range []string{"hikyo", "isolation.test", "floor.test", "bench-scan"} {
		if !digestPattern.MatchString(p.BinarySHA256[name]) {
			return fmt.Errorf("missing binary identity: %s", name)
		}
	}
	o := e.Operator
	if o.Schema != "hikyo.dev/operator-floor-evidence/v1" || !o.Passed || o.SourceCommit != p.SourceCommit || o.SourceDiffSHA256 != p.SourceDiffSHA256 || !digestPattern.MatchString(o.BinarySHA256) || o.BinarySHA256 != p.BinarySHA256["hikyo"] || o.Architecture != "arm64" || o.OuterCPU != 4 || o.OuterMemory != 4<<30 || o.CPU != 200 || o.MemoryLimit != 128<<20 || o.MemoryPeak <= 0 || o.RSSPeak <= 0 || o.RSSPeak >= 128<<20 || o.Swap != 0 {
		return errors.New("same-source real operator floor evidence missing or failed")
	}
	op := o.RSSMeasurement.Process
	if op.PID == 0 || op.StartTime == 0 || !digestPattern.MatchString(op.ContainerID) || op.BinarySHA256 != o.BinarySHA256 || op.RSS <= 0 || op.RSS > op.PeakRSS || op.PeakRSS != o.RSSPeak {
		return errors.New("operator process RSS identity or high-water measurement missing")
	}
	if e.BootMS <= 0 || e.IdleRSS <= 0 || e.IdleRSS >= 4<<30 || e.IdleSampleSeconds < 5 {
		return errors.New("boot/idle measurements missing")
	}
	if e.AdmissionMiB != 272 || e.ArgonMemoryKiB != 65536 || e.ArgonTime != 3 || e.ArgonParallelism != 2 || e.ArgonConcurrent != 4 || e.ArgonCompleted != 4 {
		return errors.New("four real floor Argon2 operations under the production admission limiter were not measured")
	}
	if e.Publish.Cells != 100000 || e.Publish.Environments*e.Publish.Keys != 100000 || !e.Publish.ReadbackVerified {
		return errors.New("100000 committed readable cells not measured")
	}
	r := e.Reencrypt
	if r.ValueRows != 250 || r.RowsMoved < 250 || r.ChunkRows != 100 || r.ConfiguredPauseMS != 100 || r.MinimumIntervalMS < 100 || !r.ReadbackVerified || len(r.CommittedProgression) != 3 || r.CommittedProgression[0] != 0 || r.CommittedProgression[1] != 100 || r.CommittedProgression[2] != 200 {
		return errors.New("actual bounded reencrypt chunks and readback missing")
	}
	sr := e.ScannerRun
	if sr.Image != p.Image || len(sr.Entrypoint) != 1 || sr.Entrypoint[0] != "/bench-scan" || sr.Status != "exited" || sr.ExitCode != 0 || sr.OOMKilled || sr.NanoCPUs != 4000000000 || sr.Memory != 4<<30 || sr.MemorySwap != 4<<30 {
		return errors.New("direct scanner process under identical native floor limits not verified")
	}
	s := e.Scanner
	if s.HarnessVersion != scanbench.HarnessVersion || s.SnapshotVersion == "" || s.MachineModel == "" || s.Items < 1 || s.ItemBytes != 65536 || s.BootPeakRSSBytes <= 0 || s.BootPeakRSSBytes > 32<<20 {
		return errors.New("scanner corpus or boot memory evidence missing or outside bound")
	}
	if e.Derating.Schema != "hikyo.dev/floor-derating/v1" || e.Derating.MemoryFactor != 1 {
		return errors.New("committed derating policy is missing or memory was derated")
	}
	e.Estimates = map[string]float64{}
	for name, value := range map[string]float64{"boot_ms": e.BootMS, "argon2_four_ms": e.ArgonMS, "publish_ms": e.Publish.ElapsedMS, "reencrypt_ms": r.ElapsedMS, "scanner_p99_ms": s.P99Millis, "scanner_boot_ms": s.BootCompileMillis} {
		f, ok := e.Derating.CPU[name]
		if !ok || !positiveFinite(f.Factor) || !positiveFinite(value) {
			return fmt.Errorf("missing or invalid CPU measurement/factor: %s", name)
		}
		if f.CalibrationCommit == nil || f.CalibrationDate == nil {
			if f.Factor != 4 || f.CalibrationCommit != nil || f.CalibrationDate != nil {
				return fmt.Errorf("uncalibrated factor must be 4.0: %s", name)
			}
		} else {
			if !commitPattern.MatchString(*f.CalibrationCommit) {
				return fmt.Errorf("invalid calibration commit: %s", name)
			}
			if _, err := time.Parse("2006-01-02", *f.CalibrationDate); err != nil {
				return fmt.Errorf("invalid calibration date: %s", name)
			}
		}
		e.Estimates[name] = value * f.Factor
	}
	for name, bound := range map[string]float64{"publish_ms": 30000, "scanner_p99_ms": 5, "scanner_boot_ms": 2000} {
		if e.Estimates[name] > bound {
			return fmt.Errorf("%s: measured × factor = %.3f ms exceeds %.3f ms", name, e.Estimates[name], bound)
		}
	}
	return nil
}
func positiveFinite(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// memoryOOMCounts requires exactly one well-formed value for both counters.
// An allocation OOM that did not kill a process is still a failed floor run.
func memoryOOMCounts(events string) (oom int, kills int, err error) {
	counts := map[string]int{}
	for _, line := range strings.Split(events, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || (fields[0] != "oom" && fields[0] != "oom_kill") {
			continue
		}
		if len(fields) != 2 {
			return 0, 0, errors.New("malformed OOM counter")
		}
		if _, seen := counts[fields[0]]; seen {
			return 0, 0, errors.New("duplicate OOM counter")
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil || n < 0 {
			return 0, 0, errors.New("invalid OOM counter")
		}
		counts[fields[0]] = n
	}
	if len(counts) != 2 {
		return 0, 0, errors.New("oom and oom_kill counters are both required")
	}
	return counts["oom"], counts["oom_kill"], nil
}
