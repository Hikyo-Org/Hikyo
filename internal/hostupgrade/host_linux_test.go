//go:build linux

package hostupgrade

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func rootTestHost(t *testing.T) *Host {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root filesystem acceptance requires Linux root")
	}
	base, err := os.MkdirTemp("/root", "hikyo-host-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	c := DefaultConfig()
	c.StateDirectory = filepath.Join(base, "state")
	c.CustodyDirectory = filepath.Join(base, "keys")
	c.CandidateDirectory = filepath.Join(base, "candidates")
	c.PublicDirectory = filepath.Join(base, "public")
	c.InstallationDirectory = filepath.Join(base, "installation")
	h := &Host{config: c, uid: 65534, gid: 65534, systemdDirectory: filepath.Join(base, "systemd"), runtimeDirectory: filepath.Join(base, "run")}
	if err := os.Mkdir(h.systemdDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := h.PrepareDirectories(); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestLinuxStopFailureLeavesDurableFence(t *testing.T) {
	h := rootTestHost(t)
	var operations []string
	h.run = func(_ context.Context, c command) ([]byte, error) {
		op := c.args[2]
		operations = append(operations, op)
		if op == "stop" {
			return nil, errors.New("stop failed")
		}
		return nil, nil
	}
	if err := h.FenceAndStop(context.Background()); err == nil {
		t.Fatal("ignored stop failure")
	}
	if active, err := h.Maintenance(); err != nil || !active {
		t.Fatalf("lost fence: %v", err)
	}
	b, err := os.ReadFile(h.dropin("90-hikyo-upgrade-fence.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "ConditionPathExists=|!"+h.maintenanceFile()) || !strings.Contains(string(b), "ConditionPathExists=|"+h.startPermission()) {
		t.Fatalf("bad reboot fence: %s", b)
	}
	if strings.Join(operations, ",") != "daemon-reload,stop" {
		t.Fatalf("unexpected operations: %v", operations)
	}
}

// A fence that cannot be installed or refreshed must still stop the writer it was meant to hold.
func TestLinuxFenceFailureStillStopsTheWriter(t *testing.T) {
	h := rootTestHost(t)
	if err := os.Mkdir(filepath.Dir(h.startPermission()), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.startPermission(), []byte("temporary candidate start\n"), 0600); err != nil {
		t.Fatal(err)
	}
	reload := errors.New("daemon-reload failed")
	var operations []string
	h.run = func(_ context.Context, c command) ([]byte, error) {
		op := c.args[2]
		operations = append(operations, op)
		switch op {
		case "daemon-reload":
			return nil, reload
		case "show":
			return []byte("ActiveState=inactive\nMainPID=0\nControlGroup=\n"), nil
		}
		return nil, nil
	}
	if err := h.FenceAndStop(context.Background()); !errors.Is(err, reload) {
		t.Fatalf("fence failure not reported: %v", err)
	}
	if strings.Join(operations, ",") != "daemon-reload,stop,show" {
		t.Fatalf("writer not stopped and verified: %v", operations)
	}
	if _, err := os.Lstat(h.startPermission()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temporary start permission survived a failed fence")
	}
	if active, err := h.Maintenance(); err != nil || active {
		t.Fatalf("maintenance published without its boot condition: %v", err)
	}
}

// A new installation migrates with the configured binary before the boot proves its
// identity, so only the authenticated release may reach migrate.
func TestLinuxNewInstallationRefusesAnotherBinaryBeforeMigrating(t *testing.T) {
	if err := trustedFile("/usr/bin/systemctl"); err != nil {
		t.Skip("preflight needs a root-owned /usr/bin/systemctl:", err)
	}
	h := rootTestHost(t)
	base := filepath.Dir(h.config.StateDirectory)
	h.config.WorkingDirectory = filepath.Join(base, "work")
	h.config.EnvironmentFile = filepath.Join(base, "hikyo.env")
	h.config.RootKeyFile = filepath.Join(base, "root.key")
	h.config.Binary = filepath.Join(base, "hikyo")
	fragment := filepath.Join(base, "hikyo.service")
	for path, data := range map[string]string{h.config.EnvironmentFile: "HIKYO_DB=sqlite:hikyo.db\n", h.config.RootKeyFile: strings.Repeat("a", 64), h.config.Binary: "release bytes", fragment: "[Service]\n"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(h.config.WorkingDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(h.config.WorkingDirectory, int(h.uid), int(h.gid)); err != nil {
		t.Fatal(err)
	}
	start := "{ path=" + h.config.Binary + " ; argv[]=" + h.config.Binary + " server --root-key-file=/run/credentials/hikyo.service/hikyo-root-key ; ignore_errors=no }"
	unit := "User=hikyo\nGroup=hikyo\nWorkingDirectory=" + h.config.WorkingDirectory + "\nEnvironmentFiles=" + h.config.EnvironmentFile + " (ignore_errors=no)\nExecStart=" + start +
		"\nExecStartPre=\nExecStartPost=\nExecStop=\nExecStopPost=\nRootDirectory=\nRootImage=\nKillMode=control-group\nControlGroup=\n"
	migrated := errors.New("migrate ran")
	h.run = func(_ context.Context, c command) ([]byte, error) {
		if c.path == h.config.Binary {
			return nil, migrated
		}
		if c.args[2] != "show" {
			t.Fatalf("unexpected systemd operation %v", c.args)
		}
		if strings.Contains(c.args[4], "FragmentPath") {
			return []byte("FragmentPath=" + fragment + "\nDropInPaths=\n"), nil
		}
		return []byte(unit), nil
	}
	if err := h.BootNewInstallation(t.Context(), strings.Repeat("0", 64), true, time.Second, io.Discard, io.Discard); err == nil || errors.Is(err, migrated) {
		t.Fatalf("another binary reached migrate: %v", err)
	}
	digest, err := fileDigest(h.config.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.BootNewInstallation(t.Context(), digest, true, time.Second, io.Discard, io.Discard); !errors.Is(err, migrated) {
		t.Fatalf("the authenticated binary did not reach migrate: %v", err)
	}
}

func TestLinuxCancelledStartUsesFreshStopContext(t *testing.T) {
	h := rootTestHost(t)
	h.run = func(ctx context.Context, c command) ([]byte, error) {
		switch c.args[2] {
		case "show":
			return []byte("ActiveState=inactive\nMainPID=0\nControlGroup=\n"), nil
		case "start":
			return nil, ctx.Err()
		case "stop":
			if ctx.Err() != nil {
				t.Fatal("cleanup inherited the canceled context")
			}
		}
		return nil, nil
	}
	if err := h.FenceAndStop(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.StartCandidate(ctx, strings.Repeat("a", 64), true, time.Second); err == nil {
		t.Fatal("accepted canceled start")
	}
	if active, err := h.Maintenance(); err != nil || !active {
		t.Fatal("lost durable maintenance")
	}
	if _, err := os.Lstat(h.startPermission()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temporary restart permission survived failure")
	}
}

func TestLinuxHealthyCandidateWaitsForCoordinatorCompletion(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(strconv.FormatBool(ready), func(t *testing.T) {
			h := rootTestHost(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/readyz" && !ready {
					w.WriteHeader(http.StatusServiceUnavailable)
				}
			}))
			defer server.Close()
			h.config.HealthURL = server.URL + "/readyz"
			h.client = server.Client()
			active := false
			h.run = func(_ context.Context, c command) ([]byte, error) {
				switch c.args[2] {
				case "start":
					active = true
				case "stop":
					active = false
				case "show":
					if active {
						return []byte("ActiveState=active\nMainPID=" + strconv.Itoa(os.Getpid()) + "\nControlGroup=\n"), nil
					}
					return []byte("ActiveState=inactive\nMainPID=0\nControlGroup=\n"), nil
				}
				return nil, nil
			}
			if err := h.FenceAndStop(context.Background()); err != nil {
				t.Fatal(err)
			}
			digest, err := fileDigest("/proc/self/exe")
			if err != nil {
				t.Fatal(err)
			}
			if err = h.StartCandidate(context.Background(), digest, ready, time.Second); err != nil {
				t.Fatal(err)
			}
			if maintained, err := h.Maintenance(); err != nil || !maintained {
				t.Fatal("candidate cleared maintenance before coordinator DB check")
			}
			if err = h.Complete(context.Background()); err != nil {
				t.Fatal(err)
			}
			if maintained, _ := h.Maintenance(); maintained {
				t.Fatal("completion did not clear maintenance")
			}
		})
	}
}

func TestLinuxCandidateDigestMismatchFencesAndStops(t *testing.T) {
	h := rootTestHost(t)
	active := false
	h.run = func(_ context.Context, c command) ([]byte, error) {
		switch c.args[2] {
		case "start":
			active = true
		case "stop":
			active = false
		case "show":
			if active {
				return []byte("ActiveState=active\nMainPID=" + strconv.Itoa(os.Getpid()) + "\nControlGroup=\n"), nil
			}
			return []byte("ActiveState=inactive\nMainPID=0\nControlGroup=\n"), nil
		}
		return nil, nil
	}
	if err := h.FenceAndStop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.StartCandidate(context.Background(), strings.Repeat("b", 64), true, 20*time.Millisecond); err == nil {
		t.Fatal("accepted a different executable")
	}
	if active {
		t.Fatal("wrong executable still running")
	}
	if maintained, _ := h.Maintenance(); !maintained {
		t.Fatal("lost durable fence")
	}
}

func TestLinuxPublicFilesNeverFollowRuntimeSymlinks(t *testing.T) {
	h := rootTestHost(t)
	secret := filepath.Join(h.config.StateDirectory, "secret")
	if err := os.WriteFile(secret, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(h.config.PublicDirectory, "receipt.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.PublishPublicEvidence("receipt.json", []byte("replacement")); err == nil {
		t.Fatal("followed evidence symlink")
	}
	b, err := os.ReadFile(secret)
	if err != nil || string(b) != "untouched" {
		t.Fatal("modified private file")
	}
	source := filepath.Join(h.config.StateDirectory, "source")
	if err = os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(secret, filepath.Join(source, "payload")); err != nil {
		t.Fatal(err)
	}
	if _, err = h.StagePublicBundle(source); err == nil {
		t.Fatal("followed source bundle symlink")
	}
}

func TestLinuxRuntimeCredentialReadableOnlyByRuntimeUser(t *testing.T) {
	h := rootTestHost(t)
	keyPath := filepath.Join(h.config.StateDirectory, "root.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("a", 64)), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := runtimeCredential(keyPath, h.uid, h.gid)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if fileOwner(info) != int(h.uid) || info.Mode().Perm() != 0600 {
		t.Fatal("credential is not runtime-owned0600")
	}
	// Opening through /proc/self/fd exercises exactly the path ReadRootKey
	// will use after credentials change, without copying a key onto disk.
	b, err := os.ReadFile("/proc/self/fd/" + strconv.Itoa(int(f.Fd())))
	if err != nil || len(b) != 64 {
		t.Fatalf("credential fd not readable: %v", err)
	}
	child, err := runCommand(context.Background(), command{path: "/bin/cat", args: []string{"/proc/self/fd/3"}, env: []string{"PATH=/usr/bin:/bin"}, runtime: true, uid: h.uid, gid: h.gid, rootKey: keyPath})
	if err != nil || string(child) != strings.Repeat("a", 64) {
		t.Fatalf("runtime UID cannot reopen credential fd: %v", err)
	}
}

func TestLinuxExistingConditionsCannotBypassFence(t *testing.T) {
	h := rootTestHost(t)
	unit := filepath.Join(h.systemdDirectory, h.config.Unit)
	h.run = func(context.Context, command) ([]byte, error) {
		return []byte("FragmentPath=" + unit + "\nDropInPaths=\n"), nil
	}
	for _, condition := range []string{"ConditionPathExists=", "ConditionPathExists=/somewhere", "ConditionUser=|root", "ConditionUser=\\\n |root"} {
		if err := os.WriteFile(unit, []byte("[Unit]\n"+condition+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := h.validateUnitFiles(context.Background()); err == nil {
			t.Fatalf("accepted fence bypass %q", condition)
		}
	}
	if err := os.WriteFile(unit, []byte("[Unit]\nConditionUser=root\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := h.validateUnitFiles(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxPrunePublicKeepsOnlyReferencedEvidence(t *testing.T) {
	h := rootTestHost(t)
	public := h.config.PublicDirectory
	mk := func(name string) string {
		path := filepath.Join(public, name)
		if err := os.MkdirAll(filepath.Join(path, "inner"), 0755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	keptBundle, keptEvidence, keptBackup := mk("bundle-current"), mk("evidence-current"), mk("backup-current")
	for _, stale := range []string{"bundle-old", "evidence-old", "backup-old"} {
		mk(stale)
	}
	// A runtime-owned symlink inside a stale backup must never be followed.
	secret := filepath.Join(h.config.StateDirectory, "secret")
	if err := os.WriteFile(secret, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(h.config.StateDirectory, filepath.Join(public, "backup-old", "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.PublishPublicEvidence("operator.pub", []byte("public key")); err != nil {
		t.Fatal(err)
	}
	if err := h.PrunePublic(RuntimeEvidence{BundleDirectory: keptBundle, EvidenceDirectory: keptEvidence, CiphertextPath: filepath.Join(keptBackup, "backup.age")}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(public)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	if strings.Join(names, ",") != "backup-current,bundle-current,evidence-current,operator.pub" {
		t.Fatalf("unexpected public directory after prune: %v", names)
	}
	if raw, err := os.ReadFile(secret); err != nil || string(raw) != "untouched" {
		t.Fatal("prune followed a symlink out of the public directory", err)
	}
	if err := h.PrunePublic(RuntimeEvidence{BundleDirectory: filepath.Join(h.config.StateDirectory, "bundle-x")}); err == nil {
		t.Fatal("accepted retained evidence outside the public directory")
	}
}

// A host enrolled from nothing has no journal, so the bundle its service boots from is
// named only by runtime.env. Pruning an attempt's leftovers must not remove it.
func TestLinuxPrunePublicKeepsTheBundleRuntimeEnvironmentNames(t *testing.T) {
	h := rootTestHost(t)
	public := h.config.PublicDirectory
	for _, name := range []string{"bundle-source", "bundle-attempt", "evidence-attempt", "backup-attempt"} {
		if err := os.MkdirAll(filepath.Join(public, name, "inner"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(h.config.StateDirectory, "downloads"), 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(public, "bundle-source")
	if err := os.WriteFile(filepath.Join(h.config.StateDirectory, "runtime.env"), []byte("HIKYO_UPGRADE_BUNDLE="+source+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	bundle, err := h.LocalBundleDirectory()
	if err != nil || bundle != source {
		t.Fatal(bundle, err)
	}
	if err := h.PrunePublic(RuntimeEvidence{BundleDirectory: bundle}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(public)
	if err != nil || len(entries) != 1 || entries[0].Name() != "bundle-source" {
		t.Fatal("the source bundle was pruned or an attempt's artifacts remain", entries, err)
	}
}

func TestLinuxPublicBundleIgnoresCallerUmask(t *testing.T) {
	h := rootTestHost(t)
	previous := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(previous) })
	resetUmask()
	source := filepath.Join(h.config.StateDirectory, "source")
	if err := os.MkdirAll(filepath.Join(source, "releases"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "releases", "index.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	staged, err := h.StagePublicBundle(source)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{staged: 0755, filepath.Join(staged, "releases"): 0755, filepath.Join(staged, "releases", "index.json"): 0644} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%s mode %o, want %o: runtime user could not read the bundle", path, info.Mode().Perm(), want)
		}
	}
	output, err := h.PreparePublicOutput("backup-umask")
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(output); err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("runtime-owned backup output must stay private", err)
	}
	evidence, err := h.PublishPublicEvidence("operator.pub", []byte("public key"))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(evidence); err != nil || info.Mode().Perm() != 0644 {
		t.Fatal("public evidence must be world-readable", err)
	}
}

func TestLinuxRuntimeFailureSavesErrorOutputForTheOperator(t *testing.T) {
	h := rootTestHost(t)
	h.run = func(_ context.Context, c command) ([]byte, error) {
		if c.path == "/usr/bin/systemctl" {
			if c.args[2] == "show" {
				return []byte("ActiveState=inactive\nMainPID=0\nControlGroup=\n"), nil
			}
			return nil, nil
		}
		if c.stderr == nil {
			t.Fatal("runtime child ran without an error sink")
		}
		if _, err := c.stderr.Write([]byte("hikyo backup: offline bundle member unavailable\n")); err != nil {
			t.Fatal(err)
		}
		return nil, errors.New("exit status 1")
	}
	if err := h.FenceAndStop(context.Background()); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(h.config.CandidateDirectory, "hikyo-candidate")
	if err := os.WriteFile(candidate, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	evidence := RuntimeEvidence{BundleDirectory: filepath.Join(h.config.PublicDirectory, "bundle"), OperatorPublicKey: filepath.Join(h.config.PublicDirectory, "operator.pub")}
	_, err := h.Migrate(context.Background(), candidate, evidence)
	if err == nil || !strings.Contains(err.Error(), "runtime migrate failed") || !strings.Contains(err.Error(), "error output is in ") {
		t.Fatalf("runtime failure hid its cause: %v", err)
	}
	logPath := err.Error()[strings.LastIndex(err.Error(), " ")+1:]
	info, err := os.Lstat(logPath)
	if err != nil || info.Mode().Perm() != 0600 || fileOwner(info) != 0 {
		t.Fatal("runtime error log must be a root-only file", err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(raw), "offline bundle member unavailable") {
		t.Fatal("runtime error log lost the child's message", err)
	}
	if strings.Contains(fmt.Sprint(err), "offline bundle member unavailable") {
		t.Fatal("child error output must not reach the operator error string")
	}
}

func TestLinuxPruneCandidatesRemovesStagedBinaries(t *testing.T) {
	h := rootTestHost(t)
	candidates := h.config.CandidateDirectory
	for _, name := range []string{"hikyo-" + strings.Repeat("a", 64), "hikyo-" + strings.Repeat("b", 64), ".hikyo-binary-123"} {
		if err := os.WriteFile(filepath.Join(candidates, name), []byte("binary"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"unrelated", "hikyo-operator-notes"} {
		if err := os.WriteFile(filepath.Join(candidates, name), []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.PruneCandidates(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Name() != "hikyo-operator-notes" || entries[1].Name() != "unrelated" {
		t.Fatalf("candidates after prune: %v", entries)
	}
}
