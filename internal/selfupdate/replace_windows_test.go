//go:build windows

package selfupdate

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

const replacementChild = "HIKYO_REPLACEMENT_TEST_CHILD"

func TestReplaceBinaryPublishesWhileAnotherProcessMapsTarget(t *testing.T) {
	if os.Getenv(replacementChild) == "1" {
		if err := os.WriteFile(os.Getenv("HIKYO_REPLACEMENT_READY"), []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		for {
			if _, err := os.Stat(os.Getenv("HIKYO_REPLACEMENT_STOP")); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}

	dir := t.TempDir()
	target := filepath.Join(dir, "hikyo.exe")
	copyTestExecutable(t, target)
	ready := filepath.Join(dir, "ready")
	stop := filepath.Join(dir, "stop")
	command := exec.Command(target, "-test.run=^TestReplaceBinaryPublishesWhileAnotherProcessMapsTarget$")
	command.Env = append(os.Environ(),
		replacementChild+"=1",
		"HIKYO_REPLACEMENT_READY="+ready,
		"HIKYO_REPLACEMENT_STOP="+stop,
	)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	waitForFile(t, ready)

	want := []byte("verified replacement")
	if err := replaceBinary(t.Context(), target, want, 0); err != nil {
		t.Fatalf("replace a mapped executable: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("published executable = %q, want %q", got, want)
	}
	backupPattern := filepath.Join(dir, previousPattern(target))
	if err := cleanupPrevious(target); err != nil {
		t.Fatal(err)
	}
	if backups := matches(t, backupPattern); len(backups) != 1 {
		t.Fatalf("mapped previous executable backups = %v, want one retained backup", backups)
	}

	if err := os.WriteFile(stop, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := cleanupPrevious(target); err != nil {
		t.Fatal(err)
	}
	if backups := matches(t, backupPattern); len(backups) != 0 {
		t.Fatalf("previous executable backups remain: %v", backups)
	}
}

func copyTestExecutable(t *testing.T, target string) {
	t.Helper()
	sourcePath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	destination, err := os.Create(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()
		t.Fatal(err)
	}
	if err := destination.Close(); err != nil {
		t.Fatal(err)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func matches(t *testing.T, pattern string) []string {
	t.Helper()
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	return paths
}
