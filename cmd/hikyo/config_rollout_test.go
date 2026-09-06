package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestStageRolloutAuthorityUsesPrivateRuntimeFile(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	raw := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded})
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "runtime")
	if err := os.WriteFile(source, raw, 0440); err != nil {
		t.Fatal(err)
	}
	if err := stageRolloutAuthority(source, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil || info.Mode().Perm() != 0400 {
		t.Fatal("runtime signer permissions", err)
	}
	stored, err := os.ReadFile(destination)
	if err != nil || string(stored) != string(raw) {
		t.Fatal("runtime signer changed", err)
	}
	invalidSource := filepath.Join(dir, "invalid-source")
	if err := os.WriteFile(invalidSource, []byte("not a signer"), 0440); err != nil {
		t.Fatal(err)
	}
	if err := stageRolloutAuthority(invalidSource, destination); err == nil {
		t.Fatal("invalid signer accepted")
	}
	stored, err = os.ReadFile(destination)
	if err != nil || string(stored) != string(raw) {
		t.Fatal("failed stage replaced working signer", err)
	}
}
