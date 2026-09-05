// Command ops-floor runs the explicitly bounded O2 release checks inside the
// native Linux cgroup created by scripts/ci/ops-floor.sh.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/storagehealth"
)

type limits struct {
	Architecture string `json:"architecture"`
	CPU          string `json:"cpu_max"`
	Memory       string `json:"memory_max"`
	Swap         string `json:"memory_swap_max"`
}

func (l limits) validate(expected, operatingSystem string) error {
	wantCPU := map[string]string{"arm64": "400000 100000", "amd64": "200000 100000"}[expected]
	if operatingSystem != "linux" || wantCPU == "" || l.Architecture != expected || l.CPU != wantCPU || l.Memory != "4294967296" || l.Swap != "0" {
		return fmt.Errorf("native floor limits mismatch: architecture=%s cpu=%s memory=%s swap=%s", l.Architecture, l.CPU, l.Memory, l.Swap)
	}
	return nil
}

type check struct {
	Name      string   `json:"name"`
	ElapsedMS int64    `json:"elapsed_ms"`
	Tests     []string `json:"tests"`
}

var doctorStates = []string{"healthy", "prune-warning", "backup-error", "drill-warning", "adapter-warning", "provider-error", "storage-warning", "recovered"}

type doctorFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
}
type doctorState struct {
	Status   string                 `json:"status"`
	Volume   storagehealth.Capacity `json:"measured_volume"`
	ExitCode int                    `json:"exit_code"`
	Findings []doctorFinding        `json:"findings"`
}

var expectedFindings = map[string]doctorFinding{
	"prune-warning":   {"retention-prune", "warn"},
	"backup-error":    {"backup-rpo", "error"},
	"drill-warning":   {"restore-drill", "warn"},
	"adapter-warning": {"adapter-targets", "warn"},
	"provider-error":  {"metadata_expired", "error"},
	"storage-warning": {"project-storage", "warn"},
}

func verifyDoctor(data []byte) error {
	var states map[string]doctorState
	if err := json.Unmarshal(data, &states); err != nil {
		return err
	}
	if len(states) != len(doctorStates) {
		return errors.New("doctor checklist state count differs")
	}
	for _, name := range doctorStates {
		state, ok := states[name]
		if !ok {
			return fmt.Errorf("doctor state %s missing", name)
		}
		expected, negative := expectedFindings[name]
		wantStatus := "ok"
		if negative {
			wantStatus = "warning"
			if expected.Severity == "error" {
				wantStatus = "error"
			}
		}
		volume := storagehealth.FromCapacity(state.Volume)
		if !volume.Known {
			return fmt.Errorf("doctor state %s lacks independently measured volume", name)
		}
		volumeSeverity := "ok"
		if volume.UsedPercent >= 90 {
			volumeSeverity = "error"
			wantStatus = "error"
		} else if volume.UsedPercent >= 80 {
			volumeSeverity = "warn"
			if wantStatus == "ok" {
				wantStatus = "warning"
			}
		}
		wantExit := 0
		if wantStatus == "error" {
			wantExit = 4
		}
		if state.ExitCode != wantExit {
			return fmt.Errorf("doctor state %s exit differs", name)
		}
		if state.Status != wantStatus {
			return fmt.Errorf("doctor state %s verdict differs", name)
		}
		codes := map[string]string{}
		for _, f := range state.Findings {
			if _, duplicate := codes[f.Code]; duplicate {
				return fmt.Errorf("doctor state %s repeats %s", name, f.Code)
			}
			codes[f.Code] = f.Severity
			want := "ok"
			if negative && f.Code == expected.Code {
				want = expected.Severity
			}
			if f.Code == "data-volume" {
				want = volumeSeverity
			}
			if f.Severity != want {
				return fmt.Errorf("doctor state %s finding %s severity differs", name, f.Code)
			}
		}
		for _, code := range []string{"retention-prune", "project-storage", "backup-rpo", "restore-drill", "adapter-targets", "data-volume", "root-escrow", "pin-expiry", "root-rotation", "reencrypt", "database-durability", "argon2-floor"} {
			if codes[code] == "" {
				return fmt.Errorf("doctor state %s omitted %s", name, code)
			}
		}
		if negative && codes[expected.Code] != expected.Severity {
			return fmt.Errorf("doctor state %s omitted expected %s severity", name, expected.Code)
		}
	}
	return nil
}

// A successful process is insufficient: the filter must have executed every
// selected top-level test and must contain no skip, including nested skips.
func verifyTests(output string, expected []string) error {
	if strings.Contains(output, "--- SKIP:") || strings.Contains(output, "--- FAIL:") {
		return errors.New("selected tests failed or skipped")
	}
	for _, name := range expected {
		if !strings.Contains(output, "=== RUN   "+name+"\n") || !strings.Contains(output, "--- PASS: "+name+" (") {
			return fmt.Errorf("selected test %s did not pass", name)
		}
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ops-floor:", err)
		os.Exit(1)
	}
}

func run() error {
	read := func(name string) (string, error) {
		b, e := os.ReadFile(filepath.Join("/sys/fs/cgroup", name))
		return strings.TrimSpace(string(b)), e
	}
	cpu, err := read("cpu.max")
	if err != nil {
		return err
	}
	memory, err := read("memory.max")
	if err != nil {
		return err
	}
	swap, err := read("memory.swap.max")
	if err != nil {
		return err
	}
	l := limits{runtime.GOARCH, cpu, memory, swap}
	if err := l.validate(os.Getenv("HIKYO_OPS_FLOOR_ARCH"), runtime.GOOS); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	started := time.Now().UTC()
	var checks []check
	suites := []struct {
		name, path string
		tests      []string
	}{
		{"doctor-and-storage", "/isolation.test", []string{"TestOpsFloorDoctor", "TestOpsFloorStorageRefusal"}},
		{"admission", "/admission.test", []string{"TestDerivedConcurrency", "TestBootRefusesABudgetOneVerificationCannotFit", "TestSemaphoreBoundsConcurrentVerifications", "TestQueueDepthIsBounded", "TestPerIPSlidingWindow", "TestPerAccountBackoffTransitions", "TestLimiterStateIsBounded"}},
	}
	for _, suite := range suites {
		at := time.Now()
		cmd := exec.CommandContext(ctx, suite.path, "-test.v", "-test.count=1", "-test.timeout=15m", "-test.run=^("+strings.Join(suite.tests, "|")+")$")
		cmd.Env = []string{"HIKYO_OPS_FLOOR_CLI=/hikyo", "HIKYO_OPS_FLOOR_OUTPUT=/evidence"}
		output, err := cmd.CombinedOutput()
		if writeErr := os.WriteFile("/evidence/"+suite.name+".log", output, 0600); writeErr != nil {
			return writeErr
		}
		if err != nil {
			return fmt.Errorf("%s failed: inspect its log: %w", suite.name, err)
		}
		if err := verifyTests(string(output), suite.tests); err != nil {
			return err
		}
		checks = append(checks, check{suite.name, time.Since(at).Milliseconds(), suite.tests})
	}
	doctor, err := os.ReadFile("/evidence/doctor.json")
	if err != nil {
		return err
	}
	if err := verifyDoctor(doctor); err != nil {
		return err
	}
	peak, err := read("memory.peak")
	if err != nil {
		return err
	}
	events, err := read("memory.events")
	if err != nil {
		return err
	}
	if !strings.Contains("\n"+events+"\n", "\noom_kill 0\n") || !strings.Contains("\n"+events+"\n", "\noom 0\n") {
		return errors.New("floor cgroup reported OOM")
	}
	peakBytes, err := strconv.ParseInt(peak, 10, 64)
	if err != nil || peakBytes <= 0 || peakBytes > 4294967296 {
		return errors.New("invalid floor peak memory")
	}
	var recorded map[string]doctorState
	if err := json.Unmarshal(doctor, &recorded); err != nil {
		return err
	}
	healthyCapacity := true
	for _, state := range recorded {
		if storagehealth.FromCapacity(state.Volume).UsedPercent >= 80 {
			healthyCapacity = false
		}
	}
	result := struct {
		Schema          string    `json:"schema"`
		Status          string    `json:"status"`
		Limits          limits    `json:"limits"`
		Started         time.Time `json:"started_at"`
		ElapsedMS       int64     `json:"elapsed_ms"`
		Peak            int64     `json:"memory_peak_bytes"`
		Checks          []check   `json:"checks"`
		DoctorStates    []string  `json:"doctor_states"`
		Trust           string    `json:"trust_domain"`
		Scope           string    `json:"scope"`
		HealthyCapacity bool      `json:"healthy_capacity"`
	}{"hikyo.dev/ops-floor/v1", "pass", l, started, time.Since(started).Milliseconds(), peakBytes, checks, doctorStates, "local-development", "complete doctor checklist, independently measured real SQLite volume, storage refusal and selected admission bounds; degraded host capacity retains its actual verdict and is not healthy capacity acceptance; other registry fixtures remain ordinary CI evidence", healthyCapacity}
	raw, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("/evidence/result.json", append(raw, '\n'), 0600)
}
