//go:build unix

package importer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A deadline that expires before the helper starts is a deadline, not a
// failure: exec refuses to start under a done context, and that refusal must
// map to the timeout exit. On a loaded runner the deadline can lapse between
// any earlier "still in time" check and Start, which is the race this pins.
func TestSubprocessDeadlineLapsedBeforeStartIsATimeout(t *testing.T) {
	spec := subprocessSpec{
		Command: "/bin/sh", Args: []string{"-c", "exit 0"},
		MaxBytes: 1024, RunDeadlineUnixNano: time.Now().Add(-time.Second).UnixNano(),
	}
	encoded, err := encodeSubprocessSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(subprocessSpecEnv, encoded)
	handled, code := RunInternalSubprocess([]string{internalSubprocessMode}, io.Discard)
	if !handled || code != subprocessExitTimeout {
		t.Fatalf("handled=%v code=%d, want timeout exit %d", handled, code, subprocessExitTimeout)
	}
}

// A helper that forks a grandchild and outlives the deadline must take the
// grandchild down with it: the timeout kills the whole process group, not
// just the direct child.
func TestSubprocessTimeoutKillsTheGrandchild(t *testing.T) {
	pidFile := subprocessGrandchildPIDFile(t)
	// Two seconds is far past a shell fork on a loaded runner and still keeps
	// the test short; the assertion is about who dies, not how fast.
	deadline := time.Now().Add(2 * time.Second)
	spec := subprocessSpec{
		Command:  "/bin/sh",
		Args:     []string{"-c", `sleep 30 & echo $! > "$1"; wait`, "_", pidFile},
		MaxBytes: 1024, RunDeadlineUnixNano: deadline.UnixNano(),
	}
	encoded, err := encodeSubprocessSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(subprocessSpecEnv, encoded)

	started := time.Now()
	handled, code := RunInternalSubprocess([]string{internalSubprocessMode}, io.Discard)
	pid := readSubprocessGrandchildPID(t, pidFile)
	if !handled || code != subprocessExitTimeout {
		t.Fatalf("handled=%v code=%d, want timeout exit %d", handled, code, subprocessExitTimeout)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("timeout path took %s: Wait hung on the inherited pipe", elapsed)
	}

	waitForSubprocessGrandchildExit(t, pid)
}

func TestSubprocessNonzeroExitKillsTheGrandchild(t *testing.T) {
	pidFile := subprocessGrandchildPIDFile(t)
	var output bytes.Buffer
	handled, code := runSubprocessWithGrandchild(t, pidFile, "7", 1024, &output)
	pid := readSubprocessGrandchildPID(t, pidFile)
	if !handled || code != 1 {
		t.Errorf("handled=%v code=%d, want failure exit 1", handled, code)
	}
	if output.Len() != 0 {
		t.Errorf("failed helper output was delivered: %q", output.String())
	}
	waitForSubprocessGrandchildExit(t, pid)
}

func TestSubprocessOutputWriteFailureKillsTheGrandchild(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		err  error
	}{
		{name: "write error", n: 0, err: errors.New("output unavailable")},
		{name: "partial write error", n: 3, err: errors.New("output unavailable")},
		{name: "complete write error", n: len("helper-output"), err: errors.New("output unavailable")},
		{name: "short write without error", n: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pidFile := subprocessGrandchildPIDFile(t)
			var attempted, delivered bytes.Buffer
			calls := 0
			writer := subprocessOutputWriter(func(p []byte) (int, error) {
				calls++
				attempted.Write(p)
				delivered.Write(p[:min(tc.n, len(p))])
				return tc.n, tc.err
			})
			handled, code := runSubprocessWithGrandchild(t, pidFile, "0", 1024, writer)
			pid := readSubprocessGrandchildPID(t, pidFile)
			if !handled || code != 1 {
				t.Errorf("handled=%v code=%d, want failure exit 1", handled, code)
			}
			if calls != 1 || attempted.String() != "helper-output" {
				t.Errorf("output writes=%d content=%q, want one write of helper-output", calls, attempted.String())
			}
			if got, want := delivered.String(), "helper-output"[:tc.n]; got != want {
				t.Errorf("delivered=%q, want %q", got, want)
			}
			waitForSubprocessGrandchildExit(t, pid)
		})
	}
}

func TestSubprocessOverflowKillsTheGrandchild(t *testing.T) {
	pidFile := subprocessGrandchildPIDFile(t)
	var output bytes.Buffer
	handled, code := runSubprocessWithGrandchild(t, pidFile, "0", 4, &output)
	pid := readSubprocessGrandchildPID(t, pidFile)
	if !handled || code != subprocessExitOverflow {
		t.Errorf("handled=%v code=%d, want overflow exit %d", handled, code, subprocessExitOverflow)
	}
	if output.Len() != 0 {
		t.Errorf("overflowed helper output was delivered: %q", output.String())
	}
	waitForSubprocessGrandchildExit(t, pid)
}

// Successful Unix helpers may intentionally leave background descendants.
// Failure cleanup must not change the Unix release policy on success.
func TestSubprocessSuccessLeavesTheGrandchildRunning(t *testing.T) {
	pidFile := subprocessGrandchildPIDFile(t)
	var output bytes.Buffer
	handled, code := runSubprocessWithGrandchild(t, pidFile, "0", 1024, &output)
	pid := readSubprocessGrandchildPID(t, pidFile)
	if !handled || code != 0 {
		t.Errorf("handled=%v code=%d, want success exit 0", handled, code)
	}
	if got := output.String(); got != "helper-output" {
		t.Errorf("output=%q, want helper-output", got)
	}
	// Allow an accidental kill at return to be delivered and reaped before
	// probing: kill(pid, 0) can briefly succeed for a dying process.
	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Errorf("successful helper's grandchild %d did not survive: %v", pid, err)
	}
}

// Re-exec is essential here: Go can terminate on SIGPIPE when writing to
// os.Stdout before Write returns an error to the process-tree cleanup path.
func TestSubprocessReexecStdoutPipe(t *testing.T) {
	for _, closedReader := range []bool{true, false} {
		name := "open_reader"
		if closedReader {
			name = "closed_reader"
		}
		t.Run(name, func(t *testing.T) {
			pidFile := subprocessGrandchildPIDFile(t)
			spec := subprocessSpec{
				Command:  "/bin/sh",
				Args:     []string{"-c", `sleep 30 </dev/null >/dev/null 2>&1 & echo $! > "$1"; printf helper-output`, "_", pidFile},
				MaxBytes: 1024,
			}
			encoded, err := encodeSubprocessSpec(spec)
			if err != nil {
				t.Fatal(err)
			}
			reader, writer, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reader.Close() })
			t.Cleanup(func() { _ = writer.Close() })
			if closedReader {
				if err := reader.Close(); err != nil {
					t.Fatal(err)
				}
			}

			// Bound the whole re-exec without spending the helper's own
			// deadline on test-binary startup under a loaded runner.
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], internalSubprocessMode)
			cmd.Env = replaceEnv(os.Environ(), subprocessSpecEnv, encoded)
			cmd.Stdout = writer // A real fd 1, not an io.Writer adapter.
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			started := time.Now()
			runErr := cmd.Run() // Always waits for and reaps the wrapper.
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			pid := readSubprocessGrandchildPID(t, pidFile)
			if ctx.Err() != nil || time.Since(started) > 10*time.Second {
				t.Errorf("re-exec exceeded its bound: context=%v error=%v", ctx.Err(), runErr)
			}
			if cmd.ProcessState == nil {
				t.Fatalf("wrapper did not start: %v", runErr)
			}
			if stderr.Len() != 0 {
				t.Errorf("wrapper failure output was delivered on stderr: %q", stderr.String())
			}
			if closedReader {
				var exitErr *exec.ExitError
				if !errors.As(runErr, &exitErr) || cmd.ProcessState.ExitCode() != 1 {
					t.Errorf("wrapper exit=%d error=%v, want ordinary exit 1", cmd.ProcessState.ExitCode(), runErr)
				}
				waitForSubprocessGrandchildExit(t, pid)
				return
			}
			if runErr != nil || cmd.ProcessState.ExitCode() != 0 {
				t.Errorf("wrapper exit=%d error=%v, want success exit 0", cmd.ProcessState.ExitCode(), runErr)
			}
			if err := reader.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			output, err := io.ReadAll(reader)
			if err != nil || string(output) != "helper-output" {
				t.Errorf("wrapper output=%q error=%v, want helper-output", output, err)
			}
			// Give an accidental kill time to be delivered and reaped.
			time.Sleep(100 * time.Millisecond)
			if err := syscall.Kill(pid, 0); err != nil {
				t.Errorf("successful helper's grandchild %d did not survive: %v", pid, err)
			}
		})
	}
}

type subprocessOutputWriter func([]byte) (int, error)

func (w subprocessOutputWriter) Write(p []byte) (int, error) { return w(p) }

func runSubprocessWithGrandchild(t *testing.T, pidFile, exitCode string, maxBytes int, output io.Writer) (bool, int) {
	t.Helper()
	spec := subprocessSpec{
		Command: "/bin/sh",
		// Redirect all background stdio so the runner sees EOF before the
		// grandchild exits. Otherwise a deadline can mask ordinary failures.
		Args:     []string{"-c", `sleep 30 </dev/null >/dev/null 2>&1 & echo $! > "$1"; printf helper-output; exit "$2"`, "_", pidFile, exitCode},
		MaxBytes: maxBytes, RunDeadlineUnixNano: time.Now().Add(2 * time.Second).UnixNano(),
	}
	encoded, err := encodeSubprocessSpec(spec)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(subprocessSpecEnv, encoded)
	started := time.Now()
	handled, code := RunInternalSubprocess([]string{internalSubprocessMode}, output)
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Errorf("subprocess took %s: cleanup exceeded its bound", elapsed)
	}
	return handled, code
}

// Register cleanup before starting the shell, so even a failed exit-code or
// output assertion cannot leave the test's background process behind.
func subprocessGrandchildPIDFile(t *testing.T) string {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	t.Cleanup(func() {
		if _, err := os.Stat(pidFile); errors.Is(err, os.ErrNotExist) {
			return // The helper might have failed to start.
		}
		pid := readSubprocessGrandchildPID(t, pidFile)
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			t.Errorf("cleaning up grandchild %d: %v", pid, err)
		}
		waitForSubprocessGrandchildExit(t, pid)
	})
	return pidFile
}

func readSubprocessGrandchildPID(t *testing.T, pidFile string) int {
	t.Helper()
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("grandchild pid was not recorded before the deadline: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		t.Fatalf("grandchild pid %q: %v", raw, err)
	}
	return pid
}

func waitForSubprocessGrandchildExit(t *testing.T, pid int) {
	t.Helper()
	// The killed grandchild is reparented and reaped by init; give that a
	// bounded moment rather than asserting the instant after the kill.
	gone := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(gone) {
			t.Fatalf("grandchild %d survived cleanup (kill 0 err=%v)", pid, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
