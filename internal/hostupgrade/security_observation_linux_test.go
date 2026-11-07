//go:build linux

package hostupgrade

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLinuxCheckRunningProvesConfiguredAndRunningBytesWithoutMutation(t *testing.T) {
	h := rootTestHost(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(executable)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	h.config.Binary = filepath.Join(h.config.StateDirectory, "installed-binary")
	out, err := os.OpenFile(h.config.Binary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(out, in)
	if err := out.Close(); err != nil || copyErr != nil {
		t.Fatal(copyErr, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	h.client, h.config.HealthURL = server.Client(), server.URL+"/readyz"
	h.run = func(_ context.Context, c command) ([]byte, error) {
		if c.args[2] != "show" {
			t.Fatal("running observation mutated systemd", c.args)
		}
		return []byte("ActiveState=active\nMainPID=" + strconv.Itoa(os.Getpid()) + "\n"), nil
	}
	digest, err := fileDigest(executable)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.CheckRunning(t.Context(), digest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.config.Binary, []byte("another binary"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := h.CheckRunning(t.Context(), digest); err == nil {
		t.Fatal("accepted another installed binary behind the same process")
	}
	if active, err := h.Maintenance(); err != nil || active {
		t.Fatal("observation introduced maintenance", active, err)
	}
}

func TestLinuxSecurityCacheRequiresPrivateRootOwnedStorage(t *testing.T) {
	h := rootTestHost(t)
	cache := filepath.Join(filepath.Dir(h.config.StateDirectory), "security", "downloads")
	if err := h.PrepareCacheDirectory(cache); err != nil {
		t.Fatal(err)
	}
	for _, protected := range []string{h.config.WorkingDirectory, h.config.PublicDirectory, h.config.InstallationDirectory, h.config.CustodyDirectory} {
		if err := h.PrepareCacheDirectory(filepath.Join(protected, "security-downloads")); err == nil {
			t.Fatal("accepted a cache overlapping runtime data or custody", protected)
		}
	}
	unsafe := filepath.Join(filepath.Dir(h.config.StateDirectory), "unsafe")
	if err := os.Mkdir(unsafe, 0777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unsafe, 0777); err != nil {
		t.Fatal(err)
	}
	if err := h.PrepareCacheDirectory(filepath.Join(unsafe, "downloads")); err == nil {
		t.Fatal("created executable cache under a writable ancestor")
	}
	if _, err := os.Lstat(filepath.Join(unsafe, "downloads")); !os.IsNotExist(err) {
		t.Fatal("wrote cache before validating ancestor ownership", err)
	}
}

func TestLinuxLocalBundleLocatorUsesExistingRootOwnedRuntimeEvidence(t *testing.T) {
	h := rootTestHost(t)
	if err := os.Mkdir(filepath.Join(h.config.StateDirectory, "downloads"), 0700); err != nil {
		t.Fatal(err)
	}
	first, current := filepath.Join(h.config.PublicDirectory, "bundle-original"), filepath.Join(h.config.PublicDirectory, "bundle-current")
	h.env = map[string]string{"HIKYO_UPGRADE_BUNDLE": first}
	if bundle, err := h.LocalBundleDirectory(); err != nil || bundle != first {
		t.Fatal("lost primary local bundle locator", bundle, err)
	}
	runtimeFile := filepath.Join(h.config.StateDirectory, "runtime.env")
	if err := os.WriteFile(runtimeFile, []byte(fmt.Sprintf("HIKYO_UPGRADE_BUNDLE=%s\n", current)), 0600); err != nil {
		t.Fatal(err)
	}
	if bundle, err := h.LocalBundleDirectory(); err != nil || bundle != current {
		t.Fatal("did not use the installed runtime's current evidence", bundle, err)
	}
	if h.env["HIKYO_UPGRADE_BUNDLE"] != first {
		t.Fatal("bundle inspection rewrote base runtime configuration")
	}
	if err := os.WriteFile(runtimeFile, []byte("HIKYO_UPGRADE_BUNDLE=/tmp/unowned-bundle\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.LocalBundleDirectory(); err == nil {
		t.Fatal("accepted an unrelated bundle locator")
	}
	if err := os.WriteFile(runtimeFile, []byte(strings.Repeat("x", (1<<20)+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := h.LocalBundleDirectory(); err == nil {
		t.Fatal("read unbounded runtime evidence configuration")
	}
}
