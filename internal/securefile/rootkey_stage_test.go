package securefile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

func TestStageRootKeyCreatesOwnerOnlyRuntimeFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "source", "root-key")
	destination := filepath.Join(dir, "runtime", "root-key")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte{0x5a}, crypto.KeySize)
	if err := os.WriteFile(source, []byte(crypto.EncodeRootKey(want)), 0o440); err != nil {
		t.Fatal(err)
	}

	if err := StageRootKey(source, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o400 {
		t.Fatalf("staged root key mode = %04o, want 0400", got)
	}
	got, err := crypto.ReadRootKey(destination, "")
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(got)
	if !bytes.Equal(got, want) {
		t.Fatal("staged root key bytes differ from source")
	}
}

func TestStageRootKeyRefusesInvalidSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "runtime", "root-key")
	if err := os.WriteFile(source, []byte("not-a-root-key"), 0o440); err != nil {
		t.Fatal(err)
	}

	if err := StageRootKey(source, destination); err == nil {
		t.Fatal("invalid root-key source was staged")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("invalid source published destination: %v", err)
	}
}
