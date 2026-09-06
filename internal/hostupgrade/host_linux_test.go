//go:build linux

package hostupgrade

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
