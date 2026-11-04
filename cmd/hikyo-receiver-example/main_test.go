package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
	"github.com/Hikyo-Org/hikyo/internal/sealedreceiver"
)

func TestKeygenFingerprintAndConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"keygen", "-dir", dir}, &out); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"identity.age", "ack.pem"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", name, info, err)
		}
	}
	if strings.Contains(out.String(), "AGE-SECRET-KEY") || strings.Contains(out.String(), "PRIVATE KEY") {
		t.Fatal("keygen printed private material")
	}
	var recipient, ack string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		k, v, _ := strings.Cut(line, ": ")
		switch k {
		case "recipient_key":
			recipient = v
		case "ack_key":
			ack = v
		}
	}
	out.Reset()
	if err := run([]string{"fingerprint", "-origin", "https://recv.example", "-recipient", recipient, "-ack-key", ack}, &out); err != nil {
		t.Fatal(err)
	}
	want, _ := sealedhook.Fingerprint("https://recv.example", recipient, ack, 1)
	if strings.TrimSpace(out.String()) != want {
		t.Fatalf("fingerprint = %q, want %q", out.String(), want)
	}
	_, sender, _ := sealedhook.GenerateSigningKey()
	cfg, _ := json.Marshal(map[string]any{
		"target_id": "payments", "instance_id": "hikyo", "generation": 1,
		"identity_file": "identity.age", "ack_key_file": "ack.pem",
		"senders":  []map[string]string{{"key": sender}, {"key": sender, "not_after": "2026-10-01T00:00:00Z"}},
		"bindings": map[string]string{"prod": "token"},
	})
	path := filepath.Join(dir, "receiver.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadReceiver(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Senders) != 2 || loaded.Senders[1].NotAfter.IsZero() || loaded.Bindings["prod"] != "token" {
		t.Fatalf("loaded = %+v", loaded)
	}
	if err := run([]string{"keygen", "-dir", dir}, &out); err == nil {
		t.Fatal("keygen overwrote existing keys")
	}
}

func kv(t *testing.T, out string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		k, v, ok := strings.Cut(line, ": ")
		if !ok {
			t.Fatalf("unexpected line %q", line)
		}
		m[k] = v
	}
	return m
}

func TestProtocolTwoKeygenFingerprintAndPQRequiredReceiver(t *testing.T) {
	recvDir, sendDir := t.TempDir(), t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"keygen", "-protocol", "2", "-dir", recvDir}, &out); err != nil {
		t.Fatal(err)
	}
	pub := kv(t, out.String())
	for _, name := range []string{"identity-hybrid.age", "ack.pem", "ack-mldsa65.pem"} {
		if info, err := os.Stat(filepath.Join(recvDir, name)); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s: %v %v", name, info, err)
		}
	}
	if !strings.HasPrefix(pub["recipient_key"], "age1pq1") || !strings.HasPrefix(pub["ack_key_mldsa65"], "mldsa65:") {
		t.Fatalf("public output = %v", pub)
	}
	for _, secret := range []string{"AGE-SECRET-KEY", "PRIVATE KEY"} {
		if strings.Contains(out.String(), secret) {
			t.Fatalf("keygen printed %s material", secret)
		}
	}
	if err := run([]string{"keygen", "-protocol", "2", "-dir", recvDir}, &out); err == nil {
		t.Fatal("keygen overwrote existing keys")
	}
	if err := run([]string{"keygen", "-protocol", "3", "-dir", t.TempDir()}, &out); err == nil {
		t.Fatal("unknown protocol accepted")
	}

	out.Reset()
	if err := run([]string{"sender-keygen", "-dir", sendDir}, &out); err != nil {
		t.Fatal(err)
	}
	// By default only the ML-DSA key is minted: the instance keeps the Ed25519
	// key its protocol 1 receivers already pin.
	if strings.Contains(out.String(), "PRIVATE KEY") || !strings.Contains(out.String(), "public_key: mldsa65:") || strings.Contains(out.String(), "ed25519") {
		t.Fatalf("sender-keygen output = %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(sendDir, "signing-ed25519.pem")); err == nil {
		t.Fatal("sender-keygen replaced or created the Ed25519 key without being asked")
	}
	out.Reset()
	if err := run([]string{"sender-keygen", "-with-ed25519", "-dir", t.TempDir()}, &out); err != nil || !strings.Contains(out.String(), "public_key: ed25519:") || strings.Contains(out.String(), "ed25519: ed25519") {
		t.Fatalf("new installation output = %q, %v", out.String(), err)
	}
	edDir := t.TempDir()
	if err := run([]string{"sender-keygen", "-with-ed25519", "-dir", edDir}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(edDir, "signing-ed25519.pem"), filepath.Join(sendDir, "signing-ed25519.pem")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := run([]string{"pubkey", "-pem", filepath.Join(sendDir, "signing-mldsa65.pem")}, &out); err != nil {
		t.Fatal(err)
	}
	senderPQ := kv(t, out.String())
	if !strings.HasPrefix(senderPQ["public_key"], "mldsa65:") {
		t.Fatalf("pubkey = %v", senderPQ)
	}
	out.Reset()
	if err := run([]string{"pubkey", "-pem", filepath.Join(sendDir, "signing-ed25519.pem")}, &out); err != nil {
		t.Fatal(err)
	}
	senderEd := kv(t, out.String())
	if err := run([]string{"pubkey", "-pem", filepath.Join(recvDir, "identity-hybrid.age")}, &out); err == nil {
		t.Fatal("pubkey accepted an age identity")
	}

	out.Reset()
	if err := run([]string{"fingerprint", "-protocol", "2", "-origin", "https://recv.example", "-recipient", pub["recipient_key"], "-ack-key", pub["ack_key"], "-ack-key-mldsa65", pub["ack_key_mldsa65"], "-generation", "4"}, &out); err != nil {
		t.Fatal(err)
	}
	ed, _ := sealedhook.ParsePublicKey(pub["ack_key"])
	pq, _ := sealedhook.ParseMLDSAPublicKey(pub["ack_key_mldsa65"])
	want, err := sealedhook.FingerprintV2("https://recv.example", pub["recipient_key"], ed, pq, 4)
	if err != nil || strings.TrimSpace(out.String()) != want {
		t.Fatalf("fingerprint = %q, want %q (%v)", out.String(), want, err)
	}
	for _, bad := range [][]string{
		{"fingerprint", "-protocol", "2", "-origin", "https://recv.example", "-recipient", pub["recipient_key"], "-ack-key", pub["ack_key"]},
		{"fingerprint", "-protocol", "1", "-origin", "https://recv.example", "-recipient", pub["recipient_key"], "-ack-key", pub["ack_key"]},
		{"fingerprint", "-protocol", "1", "-origin", "https://recv.example", "-recipient", "x", "-ack-key", pub["ack_key"], "-ack-key-mldsa65", pub["ack_key_mldsa65"]},
		{"fingerprint", "-protocol", "3"},
	} {
		if err := run(bad, &bytes.Buffer{}); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}

	// A PQ-required receiver: no version 1 identity, only version 2 pins.
	cfg, _ := json.Marshal(map[string]any{
		"target_id": "payments", "instance_id": "hikyo", "generation": 4,
		"hybrid_identity_file": "identity-hybrid.age", "ack_key_file": "ack.pem", "ack_key_mldsa65_file": "ack-mldsa65.pem",
		"senders_v2": []map[string]string{{"key": senderEd["public_key"], "key_mldsa65": senderPQ["public_key"]}, {"key": senderEd["public_key"], "key_mldsa65": senderPQ["public_key"], "not_after": "2026-10-01T00:00:00Z"}},
		"bindings":   map[string]string{"prod": "token"},
	})
	path := filepath.Join(recvDir, "receiver.json")
	if err := os.WriteFile(path, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadReceiver(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Identity != "" || loaded.AckSigner != nil || loaded.AckSignerV2 == nil || len(loaded.SendersV2) != 2 || loaded.SendersV2[1].NotAfter.IsZero() {
		t.Fatalf("loaded = %+v", loaded)
	}
	receiver, err := sealedreceiver.New(loaded)
	if err != nil {
		t.Fatal(err)
	}
	// The version 1 route does not exist on a PQ-required receiver.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, sealedhook.SyncPath, strings.NewReader("{}"))
	req.Header.Set("Content-Type", sealedhook.ContentType)
	receiver.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("v1 route on a PQ-required receiver = %d", rec.Code)
	}
	// A config naming a half-configured v2 receiver is refused.
	broken, _ := json.Marshal(map[string]any{
		"target_id": "payments", "instance_id": "hikyo", "generation": 4, "hybrid_identity_file": "identity-hybrid.age",
		"ack_key_file": "ack.pem", "ack_key_mldsa65_file": "missing.pem", "senders_v2": []map[string]string{},
	})
	if err := os.WriteFile(path, broken, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadReceiver(path); err == nil {
		t.Fatal("missing ML-DSA ack key accepted")
	}
}

// TestPersistStateSyncsTheFileThenTheRenamedEntry pins the durability order:
// the temporary file is synced before the rename, and the parent directory is
// synced after it, so an acknowledged apply cannot be undone by power loss.
func TestPersistStateSyncsTheFileThenTheRenamedEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	var events []string
	oldFile, oldDir := syncFile, syncDir
	t.Cleanup(func() { syncFile, syncDir = oldFile, oldDir })
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	syncFile = func(f *os.File) error {
		if len(events) == 0 && (exists(path) || !exists(path+".tmp")) {
			t.Error("the file must be synced before the rename, while it is still the temporary file")
		}
		events = append(events, "file")
		return oldFile(f)
	}
	syncDir = func(d string) error {
		if d != dir || !exists(path) || exists(path+".tmp") {
			t.Errorf("the directory must be synced after the rename (dir %q, state present %v)", d, exists(path))
		}
		events = append(events, "dir")
		return oldDir(d)
	}
	if err := persistState(path, []byte(`{"values":{},"seen":{}}`)); err != nil {
		t.Fatal(err)
	}
	if strings.Join(events, ",") != "file,dir" {
		t.Fatalf("sync events = %v, want file then dir", events)
	}
	if raw, _ := os.ReadFile(path); string(raw) != `{"values":{},"seen":{}}` {
		t.Fatalf("state = %q", raw)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("state mode = %v", info.Mode().Perm())
	}
	// A failed sync is a failed apply: nothing is acknowledged.
	syncDir = func(string) error { return errors.New("sync refused") }
	if err := persistState(path, []byte("{}")); err == nil {
		t.Fatal("a failed directory sync was reported as a durable apply")
	}
}
