package app

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

func TestConcurrentDevelopmentBootsKeepOneRoot(t *testing.T) {
	cfg := devConfig(t)
	keys := make(chan []byte, 16)
	errors := make(chan error, 16)
	var workers sync.WaitGroup
	for range 16 {
		workers.Go(func() {
			key, err := resolveRootKey(cfg, testLogger())
			keys <- key
			errors <- err
		})
	}
	workers.Wait()
	close(keys)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}
	var first []byte
	defer func() { crypto.Zero(first) }()
	for key := range keys {
		if first == nil {
			first = bytes.Clone(key)
		}
		if len(key) != crypto.KeySize || !bytes.Equal(first, key) {
			t.Error("concurrent startup observed a missing, partial or replaced root key")
		}
		crypto.Zero(key)
	}
}

func TestDevelopmentRootRefusesSymlinkAndPreservesTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "operator-owned")
	if err := os.WriteFile(target, []byte("must remain unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, devRootKeyName)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := ensureDevRootKey(path); err == nil {
		t.Fatal("symlink accepted as development custody")
	}
	raw, err := os.ReadFile(target)
	if err != nil || string(raw) != "must remain unchanged" {
		t.Fatal("refusal changed existing target")
	}
}
