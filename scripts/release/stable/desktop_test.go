package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const desktopTestCommit = "a" // repeated to 40 characters by commit()

func commit() string { return strings.Repeat(desktopTestCommit, 40) }

// desktopDraft builds a signed desktop release directory for version 1.0.0 and
// returns the trust directory, the directory and the signer for re-signing.
func desktopDraft(t *testing.T) (string, string, func([]byte, string, string) []byte) {
	return desktopDraftAt(t, "1.0.0")
}

// desktopDraftAt builds a complete, correctly named and signed draft for any
// version, so a refusal can only come from the version rules themselves.
func desktopDraftAt(t *testing.T, version string) (string, string, func([]byte, string, string) []byte) {
	t.Helper()
	trust, _, _, sign := fullDraftFixture(t)
	dir := t.TempDir()
	inventory := desktopInventory{Schema: desktopInventorySchema, Version: version, SourceCommit: commit(), Tag: "desktop-v" + version, CLITag: "v" + version}
	for _, name := range desktopImageNames(version) {
		payload := []byte("synthetic disk image " + name)
		sum := sha256.Sum256(payload)
		if err := os.WriteFile(filepath.Join(dir, name), payload, 0o600); err != nil {
			t.Fatal(err)
		}
		inventory.Files = append(inventory.Files, desktopFile{Name: name, Size: int64(len(payload)), SHA256: hex.EncodeToString(sum[:])})
	}
	writeInventory(t, dir, inventory, sign, "v"+version)
	return trust, dir, sign
}

func writeInventory(t *testing.T, dir string, inventory desktopInventory, sign func([]byte, string, string) []byte, tag string) {
	t.Helper()
	raw, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{desktopInventoryName: raw, desktopBundleName: sign(raw, commit(), tag)} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func verifyDesktopArgs(trust, dir, version, sha string) []string {
	return []string{"verify-desktop", "--trust", trust, "--directory", dir, "--version", version, "--commit", sha}
}

func TestVerifyDesktopAcceptsExactSignedRelease(t *testing.T) {
	trust, dir, _ := desktopDraft(t)
	var out bytes.Buffer
	if err := run(verifyDesktopArgs(trust, dir, "1.0.0", commit()), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Hikyo-Desktop-1.0.0-arm64.dmg") {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func TestVerifyDesktopRejectsHostileReleases(t *testing.T) {
	read := func(t *testing.T, dir string) desktopInventory {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(dir, desktopInventoryName))
		if err != nil {
			t.Fatal(err)
		}
		var inventory desktopInventory
		if err := json.Unmarshal(raw, &inventory); err != nil {
			t.Fatal(err)
		}
		return inventory
	}
	arm := "Hikyo-Desktop-1.0.0-arm64.dmg"
	cases := map[string]func(t *testing.T, dir string, sign func([]byte, string, string) []byte){
		"extra file": func(t *testing.T, dir string, _ func([]byte, string, string) []byte) {
			if err := os.WriteFile(filepath.Join(dir, "unreviewed.txt"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"missing image": func(t *testing.T, dir string, _ func([]byte, string, string) []byte) {
			if err := os.Remove(filepath.Join(dir, arm)); err != nil {
				t.Fatal(err)
			}
		},
		"symlinked image": func(t *testing.T, dir string, _ func([]byte, string, string) []byte) {
			path := filepath.Join(dir, arm)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(desktopInventoryName, path); err != nil {
				t.Fatal(err)
			}
		},
		"digest mismatch": func(t *testing.T, dir string, _ func([]byte, string, string) []byte) {
			if err := os.WriteFile(filepath.Join(dir, arm), []byte("synthetic disk image changed!"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"same size different bytes": func(t *testing.T, dir string, _ func([]byte, string, string) []byte) {
			payload := []byte("synthetic disk image " + arm)
			payload[0] ^= 1
			if err := os.WriteFile(filepath.Join(dir, arm), payload, 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"tampered inventory": func(t *testing.T, dir string, _ func([]byte, string, string) []byte) {
			raw, err := os.ReadFile(filepath.Join(dir, desktopInventoryName))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, desktopInventoryName), append(raw, ' '), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"signed for the CLI tag of another version": func(t *testing.T, dir string, sign func([]byte, string, string) []byte) {
			writeInventory(t, dir, read(t, dir), sign, "v1.0.1")
		},
		"signed by the desktop tag identity": func(t *testing.T, dir string, sign func([]byte, string, string) []byte) {
			writeInventory(t, dir, read(t, dir), sign, "desktop-v1.0.0")
		},
		"signed for another commit": func(t *testing.T, dir string, sign func([]byte, string, string) []byte) {
			inventory := read(t, dir)
			raw, err := json.Marshal(inventory)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, desktopBundleName), sign(raw, strings.Repeat("b", 40), "v1.0.0"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"inventory commit mismatch": func(t *testing.T, dir string, sign func([]byte, string, string) []byte) {
			inventory := read(t, dir)
			inventory.SourceCommit = strings.Repeat("b", 40)
			writeInventory(t, dir, inventory, sign, "v1.0.0")
		},
		"inventory wrong tag": func(t *testing.T, dir string, sign func([]byte, string, string) []byte) {
			inventory := read(t, dir)
			inventory.Tag = "v1.0.0"
			writeInventory(t, dir, inventory, sign, "v1.0.0")
		},
		"inventory wrong cli tag": func(t *testing.T, dir string, sign func([]byte, string, string) []byte) {
			inventory := read(t, dir)
			inventory.CLITag = "desktop-v1.0.0"
			writeInventory(t, dir, inventory, sign, "v1.0.0")
		},
		"inventory wrong schema": func(t *testing.T, dir string, sign func([]byte, string, string) []byte) {
			inventory := read(t, dir)
			inventory.Schema = "hikyo.dev/desktop-inventory/v2"
			writeInventory(t, dir, inventory, sign, "v1.0.0")
		},
		"inventory lists extra file": func(t *testing.T, dir string, sign func([]byte, string, string) []byte) {
			inventory := read(t, dir)
			inventory.Files = append(inventory.Files, desktopFile{Name: "extra.dmg", Size: 1, SHA256: strings.Repeat("c", 64)})
			writeInventory(t, dir, inventory, sign, "v1.0.0")
		},
		"inventory repeats an image": func(t *testing.T, dir string, sign func([]byte, string, string) []byte) {
			inventory := read(t, dir)
			inventory.Files[1] = inventory.Files[0]
			writeInventory(t, dir, inventory, sign, "v1.0.0")
		},
		"inventory size mismatch": func(t *testing.T, dir string, sign func([]byte, string, string) []byte) {
			inventory := read(t, dir)
			inventory.Files[0].Size++
			writeInventory(t, dir, inventory, sign, "v1.0.0")
		},
		"missing bundle": func(t *testing.T, dir string, _ func([]byte, string, string) []byte) {
			if err := os.Remove(filepath.Join(dir, desktopBundleName)); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			trust, dir, sign := desktopDraft(t)
			mutate(t, dir, sign)
			if err := run(verifyDesktopArgs(trust, dir, "1.0.0", commit()), &bytes.Buffer{}); err == nil {
				t.Fatal("accepted hostile desktop release")
			}
		})
	}
	t.Run("expected commit differs", func(t *testing.T) {
		trust, dir, _ := desktopDraft(t)
		if err := run(verifyDesktopArgs(trust, dir, "1.0.0", strings.Repeat("b", 40)), &bytes.Buffer{}); err == nil {
			t.Fatal("accepted another expected commit")
		}
	})
	t.Run("expected version differs", func(t *testing.T) {
		trust, dir, _ := desktopDraft(t)
		if err := run(verifyDesktopArgs(trust, dir, "1.0.1", commit()), &bytes.Buffer{}); err == nil {
			t.Fatal("accepted another expected version")
		}
	})
	for _, version := range []string{"1.0.0-rc.1", "0.9.0", "1.0.0-nightly.1", "1.0.0+build"} {
		t.Run("refuses "+version, func(t *testing.T) {
			trust, dir, _ := desktopDraftAt(t, version)
			err := run(verifyDesktopArgs(trust, dir, version, commit()), &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "stable versions") {
				t.Fatalf("a complete signed draft at %s must fail the stable gate, got %v", version, err)
			}
		})
	}
}
