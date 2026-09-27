package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
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
