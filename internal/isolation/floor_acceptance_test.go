//go:build flooracceptance

package isolation

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// This opt-in release gate refuses an unconstrained or emulated-target claim.
// It shares the full K2/K3 fixture with ordinary CI, then drives the shipped
// binary through the operator runbook against a separate disposable instance.
func TestFloorBackupRestoreAcceptance(t *testing.T) {
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm64" {
		t.Fatal("floor evidence requires native Linux arm64 execution")
	}
	read := func(path string) string {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(raw))
	}
	cpu := read("/sys/fs/cgroup/cpu.max")
	memory := read("/sys/fs/cgroup/memory.max")
	swap := read("/sys/fs/cgroup/memory.swap.max")
	if cpu != "400000 100000" || memory != "4294967296" || swap != "0" {
		t.Fatalf("floor limits differ: cpu=%q memory=%q swap=%q", cpu, memory, swap)
	}
	c := custody{backupStore: "/custody/backup", rootStore: "/custody/root"}
	for _, path := range []string{c.identityFile(), filepath.Join(c.backupStore, "recipient"), filepath.Join(c.rootStore, "rootkey")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatal("custody files must be regular mode-0600 files")
		}
	}
	if c.read(t, c.backupStore, "identity") == c.read(t, c.rootStore, "rootkey") {
		t.Fatal("custody inputs must be distinct")
	}
	started := time.Now().UTC()
	if !t.Run("K2_K3", func(t *testing.T) {
		runBackupRestoreDrill(t, sqliteTarget(t, t.TempDir(), c.recipient(t)), c)
	}) {
		return
	}
	if !t.Run("CLI_runbook", func(t *testing.T) { runFloorCLIRunbook(t, c) }) {
		return
	}
	elapsed := time.Since(started)
	if elapsed >= 30*time.Minute {
		t.Fatal("floor acceptance exceeded the 30-minute RTO")
	}
	events := read("/sys/fs/cgroup/memory.events")
	if !strings.Contains("\n"+events+"\n", "\noom_kill 0\n") {
		t.Fatal("floor cgroup suffered an OOM kill")
	}
	evidence := map[string]any{
		"schema_version": 1, "status": "pass", "scope": "K2/K3 and CLI backup/restore runbook",
		"architecture": runtime.GOARCH, "os": runtime.GOOS, "go_version": runtime.Version(),
		"cpu_max": cpu, "memory_max": memory, "memory_swap_max": swap,
		"memory_peak": read("/sys/fs/cgroup/memory.peak"), "memory_events": events,
		"started_at": started, "elapsed_ms": elapsed.Milliseconds(), "rto_target_ms": 1800000,
		"operator_fit": "not measured", "physical_pi_calibration": "not claimed",
	}
	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/evidence/result.json", append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func runFloorCLIRunbook(t *testing.T, c custody) {
	t.Helper()
	target := sqliteTarget(t, t.TempDir(), c.recipient(t))
	db, a := buildInstance(t, target, c)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "/hikyo", args...)
		cmd.Env = []string{"HIKYO_DB=sqlite:" + target.cfg.Store.Path, "HIKYO_BACKUP_RTO_TARGET=30m"}
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("CLI %s: %v\n%s", args[0:2], err, output)
		}
		assertNoPlaintext(t, "CLI output", output, a)
		return output
	}
	before := archiveFiles(t, target.cfg.BackupDir)
	run("backup", "export", "--out", target.cfg.BackupDir, "--recipient", c.recipient(t))
	var archive string
	for path := range archiveFiles(t, target.cfg.BackupDir) {
		if !before[path] {
			if archive != "" {
				t.Fatal("export wrote multiple files")
			}
			archive = path
		}
	}
	if archive == "" {
		t.Fatal("CLI export wrote no archive")
	}
	// Quarterly rehearsal uses both independent custody inputs and reports
	// successful decrypt, single-principal reconciliation and credential mint.
	report := run("restore", "drill", "--from", archive, "--identity-file", c.identityFile(),
		"--root-key-file", filepath.Join(c.rootStore, "rootkey"), "--principal", "usr_ident",
		"--project", "org_a/prj_a1", "--target-sqlite", filepath.Join(t.TempDir(), "rehearsal.db"), "--cleanup", "-o", "json")
	// The public report is JSON on stdout; stderr can contain operational logs,
	// so save only the parsed JSON object after selecting its output line.
	var parsed map[string]any
	for _, line := range strings.Split(string(report), "\n") {
		var candidate map[string]any
		if json.Unmarshal([]byte(line), &candidate) == nil && candidate["rto_met"] != nil {
			parsed = candidate
		}
	}
	if parsed == nil || parsed["rto_met"] != true || parsed["ok"] != true {
		t.Fatalf("CLI drill did not report RTO success: %s", report)
	}
	raw, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/evidence/cli-drill.json", append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	target.destroy(t)
	run("restore", "run", "--from", archive, "--identity-file", c.identityFile())
	status := run("restore", "status")
	if !strings.Contains(string(status), "usr_ident") {
		t.Fatal("restored human not pending reconciliation")
	}
	run("restore", "reconcile", "--principal", "usr_ident")
	restored := target.open(t)
	if got := queryInt(t, restored, "SELECT reconciled_epoch FROM principals WHERE id = 'usr_ident'"); got == 0 {
		t.Fatal("CLI did not reconcile chosen principal")
	}
	if got := queryInt(t, restored, "SELECT COUNT(*) FROM principals WHERE id <> 'usr_ident' AND reconciled_epoch = 0"); got == 0 {
		t.Fatal("CLI reconciliation did not leave other principals inert")
	}
	if got := queryInt(t, restored, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'restore.completed'"); got != 1 {
		t.Fatal("CLI restore completion was not audited")
	}
}
