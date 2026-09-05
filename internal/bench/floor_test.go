//go:build floorbench && linux

package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/scanning"
)

// TestFloorBench is compiled outside the cgroup, then run in the same native
// constrained container as its CLI, scanner and signed datastore fixtures.
func TestFloorBench(t *testing.T) {
	directory := os.Getenv("HIKYO_FLOOR_EVIDENCE")
	if directory == "" {
		t.Fatal("use scripts/bench/floor.sh: evidence directory is required")
	}
	e := Evidence{Schema: "hikyo.dev/floor-bench/v1", Status: "failed", StartedAt: time.Now().UTC(), Architecture: runtime.GOARCH, OS: runtime.GOOS, Raw: os.Getenv("HIKYO_FLOOR_RAW") == "1", Interpretation: "Native ARM CPU timings multiplied by committed factors are conservative estimates, not measured Raspberry Pi performance. Memory is measured without derating. The admission work budget is not a total RSS cap. Reencrypt elapsed time includes mandatory pauses and has no throughput deadline."}
	decodeFile(t, filepath.Join(directory, "provenance.json"), &e.Provenance)
	decodeFile(t, "/derate.json", &e.Derating)
	defer func() {
		raw, err := json.MarshalIndent(e, "", "  ")
		if err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(directory, "floor-"+e.Provenance.SourceCommit+".json"), append(raw, '\n'), 0600); err != nil {
			t.Error(err)
		}
	}()
	if runtime.GOARCH != "arm64" || readText(t, "/sys/fs/cgroup/cpu.max") != "400000 100000" || readText(t, "/sys/fs/cgroup/memory.max") != "4294967296" || readText(t, "/sys/fs/cgroup/memory.swap.max") != "0" {
		t.Fatal("native Linux ARM64, 4 CPU, 4 GiB and zero swap are required")
	}
	e.CPU = 4
	e.MemoryLimit = 4 << 30
	measureBoot(t, &e)
	measureArgon(t, &e)
	for _, scenario := range []struct {
		name, file string
		target     any
	}{{"TestFloorBenchPublish", "publish.json", &e.Publish}, {"TestFloorBenchReencrypt", "reencrypt.json", &e.Reencrypt}} {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
		cmd := exec.CommandContext(ctx, "/isolation.test", "-test.run=^"+scenario.name+"$", "-test.v", "-test.timeout=4m")
		out, err := cmd.CombinedOutput()
		cancel()
		if writeErr := os.WriteFile(filepath.Join(directory, scenario.name+".log"), out, 0600); writeErr != nil {
			t.Fatal(writeErr)
		}
		if err != nil {
			t.Fatalf("%s failed: %v; inspect its fixture log", scenario.name, err)
		}
		if !strings.Contains(string(out), "--- PASS: "+scenario.name+" (") || strings.Contains(string(out), "--- SKIP:") {
			t.Fatalf("%s did not execute completely", scenario.name)
		}
		decodeFile(t, filepath.Join(directory, scenario.file), scenario.target)
	}
	decodeFile(t, filepath.Join(directory, "scanner.json"), &e.Scanner)
	decodeFile(t, filepath.Join(directory, "scanner-run.json"), &e.ScannerRun)
	rules, err := scanning.Load()
	if err != nil {
		t.Fatal(err)
	}
	if e.Scanner.SnapshotVersion != rules.SnapshotVersion() {
		t.Fatal("scanner artifact does not match compiled rules")
	}
	e.MemoryPeak = readInteger(t, "/sys/fs/cgroup/memory.peak")
	e.OOMEvents, e.OOMKills, err = memoryOOMCounts(readText(t, "/sys/fs/cgroup/memory.events"))
	if err != nil {
		t.Fatal(err)
	}
	decodeFile(t, filepath.Join(directory, "operator", "result.json"), &e.Operator)
	if e.Raw {
		e.Status = "raw-measurement-only"
		return
	}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	e.Status = "pass"
}

func measureArgon(t *testing.T, e *Evidence) {
	t.Helper()
	parameters := crypto.PasswordFloor
	limiter, err := admission.New(admission.Config{BudgetMiB: 272, ArgonMemoryKiB: parameters.MemoryKiB})
	if err != nil {
		t.Fatal(err)
	}
	if limiter.Concurrency() != 4 {
		t.Fatal("272 MiB floor did not derive four verification slots")
	}
	e.AdmissionMiB = 272
	e.ArgonMemoryKiB = parameters.MemoryKiB
	e.ArgonTime = parameters.Time
	e.ArgonParallelism = parameters.Parallelism
	ready := make(chan struct{}, 4)
	start := make(chan struct{})
	results := make(chan error, 4)
	var workers sync.WaitGroup
	salts := make([][]byte, 4)
	for i := range salts {
		salts[i], err = crypto.NewSalt()
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		workers.Go(func() {
			release, err := limiter.Enter(t.Context(), fmt.Sprintf("192.0.2.%d", i+1))
			if err != nil {
				results <- err
				ready <- struct{}{}
				return
			}
			defer release()
			ready <- struct{}{}
			<-start
			verifier, err := crypto.DeriveVerifier([]byte("public floor fixture passphrase"), salts[i], parameters)
			if err == nil && len(verifier) == 0 {
				err = fmt.Errorf("empty verifier")
			}
			results <- err
		})
	}
	for i := 0; i < 4; i++ {
		select {
		case <-ready:
		case <-t.Context().Done():
			t.Fatal("Argon admission did not fill")
		}
	}
	e.ArgonConcurrent = limiter.Snapshot().InFlight
	started := time.Now()
	close(start)
	workers.Wait()
	e.ArgonMS = float64(time.Since(started)) / float64(time.Millisecond)
	for i := 0; i < 4; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
		e.ArgonCompleted++
	}
}

func measureBoot(t *testing.T, e *Evidence) {
	t.Helper()
	cmd := exec.Command("/hikyo", "server", "--dev", "--listen", "127.0.0.1:49761", "--operational-listen", "127.0.0.1:49762")
	cmd.Dir = t.TempDir()
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	started := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("default dev server shutdown: %v", err)
			}
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Error("default dev server did not shut down within 10 seconds")
		}
	}()
	transport := &http.Transport{}
	client := &http.Client{Transport: transport, Timeout: time.Second}
	defer transport.CloseIdleConnections()
	deadline := time.NewTimer(time.Minute)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			t.Fatal("default dev server did not become ready within one minute")
		case err := <-done:
			done <- err
			t.Fatalf("default dev server exited before readiness: %v", err)
		case <-ticker.C:
			response, err := client.Get("http://127.0.0.1:49762/readyz")
			if err != nil {
				continue
			}
			_, readErr := io.Copy(io.Discard, response.Body)
			closeErr := response.Body.Close()
			if readErr != nil || closeErr != nil || response.StatusCode != 200 {
				continue
			}
			e.BootMS = float64(time.Since(started)) / float64(time.Millisecond)
			// Observe five seconds of a running idle server, not the parent test's RSS.
			for i := 0; i < 5; i++ {
				select {
				case err := <-done:
					done <- err
					t.Fatalf("idle server exited: %v", err)
				case <-time.After(time.Second):
				}
				e.IdleRSS = max(e.IdleRSS, processRSS(t, cmd.Process.Pid))
			}
			e.IdleSampleSeconds = 5
			return
		}
	}
}
func processRSS(t *testing.T, pid int) int64 {
	t.Helper()
	for _, line := range strings.Split(readText(t, fmt.Sprintf("/proc/%d/status", pid)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "VmRSS:" && fields[2] == "kB" {
			n, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return n * 1024
		}
	}
	t.Fatal("server RSS unavailable")
	return 0
}
func decodeFile(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, value); err != nil {
		t.Fatal(err)
	}
}
func readText(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(raw))
}
func readInteger(t *testing.T, path string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(readText(t, path), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
