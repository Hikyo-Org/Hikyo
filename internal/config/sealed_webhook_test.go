package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const sealedWebhookJSON = `{
  "instance_id": "hikyo-prod",
  "signing_key_file": "signing.pem",
  "targets": [{
    "id": "payments", "origin": "https://recv.example", "recipient_key": "age1x", "ack_key": "ed25519:y",
    "generation": 1, "timeout_seconds": 5, "max_response_bytes": 4096, "confirmed_fingerprint": "sha256:z"
  }]
}`

func TestParseSealedWebhookConfigResolvesKeyRelativeToFile(t *testing.T) {
	var asked string
	cfg, err := parseSealedWebhookConfig([]byte(sealedWebhookJSON), "/etc/hikyo", func(p string) ([]byte, error) {
		asked = p
		return []byte("PEM"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if asked != "/etc/hikyo/signing.pem" || string(cfg.SigningKeyPEM) != "PEM" || cfg.InstanceID != "hikyo-prod" {
		t.Fatalf("cfg = %+v asked %q", cfg, asked)
	}
	if got := cfg.Targets[0]; got.Timeout != 5*time.Second || got.MaxResponseBytes != 4096 || got.ConfirmedFingerprint != "sha256:z" {
		t.Fatalf("target = %+v", got)
	}
}

func TestParseSealedWebhookConfigFailsClosed(t *testing.T) {
	key := func(string) ([]byte, error) { return []byte("PEM"), nil }
	cases := map[string]string{
		"unknown field":   strings.Replace(sealedWebhookJSON, `"generation"`, `"callback_url": "https://x", "generation"`, 1),
		"no fingerprint":  strings.Replace(sealedWebhookJSON, `"sha256:z"`, `""`, 1),
		"long timeout":    strings.Replace(sealedWebhookJSON, `"timeout_seconds": 5`, `"timeout_seconds": 11`, 1),
		"large response":  strings.Replace(sealedWebhookJSON, `"max_response_bytes": 4096`, `"max_response_bytes": 65537`, 1),
		"zero generation": strings.Replace(sealedWebhookJSON, `"generation": 1`, `"generation": 0`, 1),
		"no targets":      `{"instance_id":"i","signing_key_file":"k","targets":[]}`,
		"trailing":        sealedWebhookJSON + `{}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSealedWebhookConfig([]byte(raw), "/", key); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	dup := strings.Replace(sealedWebhookJSON, `}]`, `}, {"id": "payments", "origin": "https://other.example", "recipient_key": "a", "ack_key": "b", "generation": 1, "timeout_seconds": 1, "max_response_bytes": 1, "confirmed_fingerprint": "c"}]`, 1)
	if _, err := parseSealedWebhookConfig([]byte(dup), "/", key); err == nil {
		t.Fatal("duplicate id accepted")
	}
	if _, err := parseSealedWebhookConfig([]byte(sealedWebhookJSON), "/", func(string) ([]byte, error) { return nil, errors.New("gone") }); err == nil {
		t.Fatal("unreadable key accepted")
	}
}

func TestSigningKeyFileRefusesLooseModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes only")
	}
	path := filepath.Join(t.TempDir(), "k.pem")
	if err := os.WriteFile(path, []byte("PEM"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSigningKeyFile(path); err == nil || !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("loose mode accepted: %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readSigningKeyFile(path); err != nil || string(got) != "PEM" {
		t.Fatalf("0600 key refused: %v", err)
	}
}
