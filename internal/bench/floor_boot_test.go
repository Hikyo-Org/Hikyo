//go:build linux || darwin

package bench

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const (
	floorPublicAddress      = "127.0.0.1:19761"
	floorOperationalAddress = "127.0.0.1:19762"
)

// Polling an unbound loopback port inside the ephemeral range can self-connect
// and leave TIME_WAIT state that prevents the measured server from binding.
// Refuse overlapping host settings instead of retrying a contaminated boot.
func validateFloorBootListeners(addresses []string, localRange string) error {
	fields := strings.Fields(localRange)
	if len(fields) != 2 {
		return fmt.Errorf("floor boot requires a two-bound Linux ephemeral port range")
	}
	first, firstErr := strconv.Atoi(fields[0])
	last, lastErr := strconv.Atoi(fields[1])
	if firstErr != nil || lastErr != nil || first < 1 || last > 65535 || first > last {
		return fmt.Errorf("floor boot has an invalid Linux ephemeral port range")
	}
	for _, address := range addresses {
		_, rawPort, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("floor boot listener: %w", err)
		}
		port, err := strconv.Atoi(rawPort)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("floor boot listener has an invalid port")
		}
		if port >= first && port <= last {
			return fmt.Errorf("floor boot listener %s overlaps Linux ephemeral ports %d-%d", address, first, last)
		}
	}
	return nil
}

func floorBootCommand(t *testing.T) (*exec.Cmd, *os.File) {
	t.Helper()
	if runtime.GOOS == "linux" {
		localRange, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
		if err != nil {
			t.Fatal(err)
		}
		if err := validateFloorBootListeners([]string{floorPublicAddress, floorOperationalAddress}, string(localRange)); err != nil {
			t.Fatal(err)
		}
	}
	log, err := os.OpenFile(filepath.Join(os.Getenv("HIKYO_FLOOR_EVIDENCE"), "dev-server.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/hikyo", "server", "--dev", "--listen", floorPublicAddress, "--operational-listen", floorOperationalAddress)
	cmd.Dir = t.TempDir()
	cmd.Stdout = log
	cmd.Stderr = log
	return cmd, log
}

func TestFloorBootListenersRejectEphemeralCollision(t *testing.T) {
	for _, tc := range []struct {
		name, localRange string
		addresses        []string
		refused          bool
	}{
		{"current listeners", "32768\t60999\n", []string{floorPublicAddress, floorOperationalAddress}, false},
		{"original listeners", "32768 60999", []string{"127.0.0.1:49761", "127.0.0.1:49762"}, true},
		{"reproduced self-connect allocation", "49762 49762", []string{"127.0.0.1:49762"}, true},
		{"current listeners with reproduced allocation", "49762 49762", []string{floorPublicAddress, floorOperationalAddress}, false},
		{"public lower boundary", "19761 19761", []string{floorPublicAddress, floorOperationalAddress}, true},
		{"operational only upper boundary", "19762 19762", []string{floorPublicAddress, floorOperationalAddress}, true},
		{"missing bound", "32768", []string{floorPublicAddress}, true},
		{"extra bound", "32768 60999 65535", []string{floorPublicAddress}, true},
		{"invalid bound", "32768 invalid", []string{floorPublicAddress}, true},
		{"zero bound", "0 60999", []string{floorPublicAddress}, true},
		{"overflow bound", "32768 65536", []string{floorPublicAddress}, true},
		{"reversed bounds", "60999 32768", []string{floorPublicAddress}, true},
		{"malformed listener", "32768 60999", []string{"invalid"}, true},
		{"unassigned listener", "32768 60999", []string{"127.0.0.1:0"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateFloorBootListeners(tc.addresses, tc.localRange); (err != nil) != tc.refused {
				t.Fatalf("listener refusal = %v, want %v: %v", err != nil, tc.refused, err)
			}
		})
	}
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
