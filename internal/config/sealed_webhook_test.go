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
	// WriteFile applies the caller's umask. Set the deliberately unsafe fixture
	// mode explicitly so a restrictive runner still exercises the refusal.
	if err := os.Chmod(path, 0o644); err != nil {
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

const sealedWebhookJSONV2 = `{
  "instance_id": "hikyo-prod",
  "signing_key_file": "signing.pem",
  "signing_key_file_mldsa65": "signing-mldsa65.pem",
  "targets": [{
    "id": "payments", "origin": "https://recv.example", "protocol": 2, "recipient_key": "age1pq1x", "ack_key": "ed25519:y",
    "ack_key_mldsa65": "mldsa65:z", "generation": 2, "timeout_seconds": 5, "max_response_bytes": 4096, "confirmed_fingerprint": "swh2-x"
  }]
}`

func TestParseSealedWebhookConfigProtocolTwo(t *testing.T) {
	var asked []string
	cfg, err := parseSealedWebhookConfig([]byte(sealedWebhookJSONV2), "/etc/hikyo", func(p string) ([]byte, error) {
		asked = append(asked, p)
		return []byte("PEM:" + filepath.Base(p)), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Targets[0]; got.Protocol != 2 || got.AckKeyMLDSA65 != "mldsa65:z" || string(cfg.SigningKeyMLDSA65PEM) != "PEM:signing-mldsa65.pem" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if len(asked) != 2 || asked[1] != "/etc/hikyo/signing-mldsa65.pem" {
		t.Fatalf("key files read = %v", asked)
	}
	// A version 1 file without any v2 member parses as protocol 1 and never
	// reads a second key file.
	v1, err := parseSealedWebhookConfig([]byte(sealedWebhookJSON), "/", func(p string) ([]byte, error) { return []byte("PEM"), nil })
	if err != nil || v1.Targets[0].Protocol != 1 || v1.SigningKeyMLDSA65PEM != nil {
		t.Fatalf("v1 file = %+v, %v", v1, err)
	}
}

func TestParseSealedWebhookConfigProtocolTwoFailsClosed(t *testing.T) {
	key := func(string) ([]byte, error) { return []byte("PEM"), nil }
	cases := map[string]string{
		"protocol 3":                        strings.Replace(sealedWebhookJSONV2, `"protocol": 2`, `"protocol": 3`, 1),
		"protocol 0":                        strings.Replace(sealedWebhookJSONV2, `"protocol": 2`, `"protocol": 0`, 1),
		"v2 without the ml-dsa ack key":     strings.Replace(sealedWebhookJSONV2, `"ack_key_mldsa65": "mldsa65:z", `, ``, 1),
		"v2 without the ml-dsa signing key": strings.Replace(sealedWebhookJSONV2, `"signing_key_file_mldsa65": "signing-mldsa65.pem",`, ``, 1),
		"ml-dsa signing key, no v2 target":  strings.Replace(strings.Replace(sealedWebhookJSONV2, `"protocol": 2, `, ``, 1), `"ack_key_mldsa65": "mldsa65:z", `, ``, 1),
		"v1 target with an ml-dsa ack key":  strings.Replace(sealedWebhookJSONV2, `"protocol": 2, `, ``, 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSealedWebhookConfig([]byte(raw), "/", key); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	if _, err := parseSealedWebhookConfig([]byte(sealedWebhookJSONV2), "/", func(p string) ([]byte, error) {
		if strings.Contains(p, "mldsa65") {
			return nil, errors.New("gone")
		}
		return []byte("PEM"), nil
	}); err == nil {
		t.Fatal("unreadable ML-DSA key accepted")
	}
}
