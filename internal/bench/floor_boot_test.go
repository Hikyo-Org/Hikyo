//go:build linux || darwin

package bench

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func floorBootCommand(t *testing.T) (*exec.Cmd, *os.File) {
	t.Helper()
	log, err := os.OpenFile(filepath.Join(os.Getenv("HIKYO_FLOOR_EVIDENCE"), "dev-server.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/hikyo", "server", "--dev", "--listen", "127.0.0.1:49761", "--operational-listen", "127.0.0.1:49762")
	cmd.Dir = t.TempDir()
	cmd.Stdout = log
	cmd.Stderr = log
	return cmd, log
}

func TestFloorBootCommandFailureDiagnostics(t *testing.T) {
	if os.Getenv("HIKYO_FLOOR_LOG_HELPER") == "1" {
		fmt.Fprintln(os.Stdout, "public startup stdout fixture")
		fmt.Fprintln(os.Stderr, "public startup stderr fixture")
		os.Exit(17)
	}
	directory := t.TempDir()
	t.Setenv("HIKYO_FLOOR_EVIDENCE", directory)
	cmd, log := floorBootCommand(t)
	defer log.Close()
	cmd.Path = os.Args[0]
	cmd.Args = []string{os.Args[0], "-test.run=^TestFloorBootCommandFailureDiagnostics$"}
	cmd.Env = append(os.Environ(), "HIKYO_FLOOR_LOG_HELPER=1")
	var exit *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &exit) || exit.ExitCode() != 17 {
		t.Fatalf("startup failure was not preserved: %v", err)
	}
	info, err := log.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("startup diagnostics must be a private regular file: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(directory, "dev-server.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"public startup stdout fixture", "public startup stderr fixture"} {
		if !strings.Contains(string(raw), marker) {
			t.Fatalf("startup diagnostics did not preserve %q", marker)
		}
	}
}
