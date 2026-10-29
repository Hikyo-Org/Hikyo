//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

func TestCodexConfigurationRoundTripsTOMLMetacharacters(t *testing.T) {
	endpoint := "https://fixture.example/mcp/codex?quoted=\"\\\n[model_providers.other]"
	raw, err := codexConfiguration(endpoint, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		MCP map[string]struct {
			URL     string            `toml:"url"`
			Headers map[string]string `toml:"http_headers"`
		} `toml:"mcp_servers"`
		Providers map[string]map[string]any `toml:"model_providers"`
	}
	if _, err := toml.Decode(string(raw), &config); err != nil {
		t.Fatal(err)
	}
	if len(config.MCP) != 1 || config.MCP["hikyo"].URL != endpoint || len(config.Providers) != 1 || config.MCP["hikyo"].Headers[probeHeader] != strings.Repeat("a", 64) {
		t.Fatal("TOML structure or value escaped its field")
	}
}

func TestTransportIdentityCannotBeReplacedByResponseBody(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Fixture-Process", "actual")
		w.Write([]byte(`{"Status":401,"Body":"ignored","Challenge":"fake","Process":"forged","result":{"structuredContent":null}}`))
	}))
	defer server.Close()
	p := &probe{client: server.Client(), target: target{URL: server.URL}}
	r, err := p.call(context.Background(), "tools/call", "fixture", nil, "")
	if err != nil || r.Status != 200 || r.Process != "actual" || r.Challenge != "" || !bytes.Contains(r.Body, []byte("forged")) || successful(r) {
		t.Fatal("JSON overrode transport or null content passed", err)
	}
}

func TestWireReceiptCannotCombineInvocationsOrHideRecorderFailure(t *testing.T) {
	first, second := strings.Repeat("a", 64), strings.Repeat("b", 64)
	document := wireReceipt{Schema: "hikyo.dev/mcp-wire/v1", Observations: []wireObservation{
		{Invocation: invocationHash(first), Method: "initialize", Status: 200, Requested: "2025-06-18", Negotiated: "2025-06-18"},
		{Invocation: invocationHash(second), Method: "tools/call", Status: 200, Header: "2025-06-18", BearerPresent: true},
	}}
	if validCodexWire(document, first) || validCodexWire(document, second) {
		t.Fatal("mixed invocations passed")
	}
	document.Observations[1].Invocation = invocationHash(first)
	if !validCodexWire(document, first) {
		t.Fatal("complete invocation control failed")
	}
	document.Failed = true
	if validCodexWire(document, first) {
		t.Fatal("failed recorder passed")
	}
	document.Failed = false
	document.Observations = append(document.Observations, wireObservation{ResponseSession: true})
	if validCodexWire(document, first) {
		t.Fatal("unrelated session observation hidden")
	}
}

func TestLiveSnapshotReportsFailureDespiteStalePersistedSuccess(t *testing.T) {
	dir, err := os.MkdirTemp("", "m651-wire-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := &wireRecorder{directory: dir}
	if err := writePrivate(filepath.Join(dir, "wire-receipt.json"), w.snapshot()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "wire-receipt.json.next"), 0700); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "https://fixture.example/mcp/codex", strings.NewReader(`{"method":"ping"}`))
	w.wrap(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { out.WriteHeader(200) })).ServeHTTP(httptest.NewRecorder(), request)
	closeServer, err := serveWireSnapshot(context.Background(), dir, w)
	if err != nil {
		t.Fatal(err)
	}
	defer closeServer()
	document, err := fetchWireSnapshot(context.Background(), dir)
	if err != nil || !document.Failed {
		t.Fatal("live snapshot hid persistence failure", err)
	}
	raw, _ := json.Marshal(document)
	if bytes.Contains(raw, []byte("Authorization")) {
		t.Fatal("snapshot contains secret-bearing field")
	}
	if err := os.Remove(filepath.Join(dir, "wire-receipt.json.next")); err != nil {
		t.Fatal(err)
	}
	capped := &wireRecorder{directory: dir, observations: make([]wireObservation, 1000)}
	capped.wrap(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { out.WriteHeader(200) })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "https://fixture.example/mcp/codex", strings.NewReader(`{"method":"ping"}`)))
	var persisted wireReceipt
	if err := readPrivate(filepath.Join(dir, "wire-receipt.json"), &persisted); err != nil || !persisted.Failed || len(persisted.Observations) != 1000 {
		t.Fatal("observation cap failure not persisted", err)
	}
}

func TestLocalLauncherRefusesSymlinkAndNoncanonicalParentsBeforeCreation(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	for _, runtime := range []string{dir + "/alias/child", dir + "/real/../child", dir + "/missing/child"} {
		if exec.Command("sh", "local.sh", runtime).Run() == nil {
			t.Fatal("ambiguous parent accepted")
		}
		if _, err := os.Lstat(runtime); !os.IsNotExist(err) {
			t.Fatal("launcher created a resource before canonical-parent refusal")
		}
	}
}

func TestDockerCreateWithoutStartIsCleanedByExactNameAndLabel(t *testing.T) {
	name := "hikyo-mcp-651-unit"
	id := strings.Repeat("a", 12)
	removed := false
	clean := cleanupDocker(name, "", "", func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "ps "):
			if !strings.Contains(joined, "name=^/"+name+"$") || !strings.Contains(joined, "label=hikyo.acceptance=651") {
				t.Fatal("fallback lookup too broad")
			}
			return id, nil
		case strings.HasPrefix(joined, "network ls"):
			return "", nil
		case joined == "rm -f "+id:
			removed = true
			return "", nil
		case joined == "inspect "+id:
			return "", errors.New("absent")
		default:
			t.Fatal("unexpected cleanup command", joined)
			return "", errors.New("unexpected")
		}
	})
	if !clean || !removed {
		t.Fatal("created orphan not removed")
	}
	if cleanupDocker(name, "", "", func(...string) (string, error) { return "", errors.New("daemon unavailable") }) {
		t.Fatal("failed lookup reported clean")
	}
}

func TestUnresponsiveOwnedProcessGetsKillFallback(t *testing.T) {
	cmd := exec.Command("sh", "-c", "trap '' TERM; exec sleep 30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &serverProcess{cmd: cmd, done: make(chan error, 1)}
	go func() { p.done <- cmd.Wait(); close(p.done) }()
	defer func() { cmd.Process.Kill(); <-p.done }()
	// Give sh time to install its ignored signal disposition.
	time.Sleep(30 * time.Millisecond)
	start := time.Now()
	stopServer(p, 30*time.Millisecond)
	if time.Since(start) > time.Second {
		t.Fatal("owned process stop exceeded fallback budget")
	}
}

func TestMissingClientIdentityRefusesBeforeAnyResourceCommand(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(dir, "runtime")
	if err := os.Mkdir(runtime, 0700); err != nil {
		t.Fatal(err)
	}
	server := filepath.Join(dir, "server")
	if err := os.WriteFile(server, []byte("synthetic nonexecuted fixture input"), 0700); err != nil {
		t.Fatal(err)
	}
	inspector := filepath.Join(dir, "inspector")
	if err := os.WriteFile(inspector, []byte("synthetic inspector inventory"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"git": "printf aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "docker": "printf reached > \"$TEST_RESOURCE_MARKER\"; exit 1"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MCP651_CODEX", filepath.Join(dir, "absent-codex"))
	t.Setenv("MCP651_INSPECTOR_ENTRY", inspector)
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	t.Setenv("TEST_RESOURCE_MARKER", filepath.Join(dir, "resource-called"))
	if local(context.Background(), runtime, server) == nil {
		t.Fatal("missing client identity accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "resource-called")); !os.IsNotExist(err) {
		t.Fatal("allocated resources before rejecting missing artifact identity")
	}
}

func TestScratchEnumerationFailureOverridesEarlierPassingOutcome(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(file, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	r := newReceipt()
	r.Checks["outcome"] = "PASS"
	if finishLocalScratch(file, r, nil) == nil || r.Checks["private_scratch_cleanup"] != "FAIL" || !strings.HasPrefix(r.Checks["outcome"], "FAIL") {
		t.Fatal("failed enumeration retained passing cleanup/outcome")
	}
}

func TestScratchRemovalFailureOverridesEarlierPassingOutcome(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permission failure")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "private-synthetic"), []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	r := newReceipt()
	r.Checks["outcome"] = "PASS"
	if finishLocalScratch(dir, r, nil) == nil || r.Checks["private_scratch_cleanup"] != "FAIL" || !strings.HasPrefix(r.Checks["outcome"], "FAIL") {
		t.Fatal("failed removal retained passing cleanup/outcome")
	}
}
