//go:build linux || darwin

package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEscrowFileRequiresDistinctPrivateCustody(t *testing.T) {
	dir := t.TempDir()
	primary := filepath.Join(dir, "primary")
	copyPath := filepath.Join(dir, "escrow")
	key, err := GenerateRootKey()
	if err != nil {
		t.Fatal(err)
	}
	defer Zero(key)
	for _, path := range []string{primary, copyPath} {
		if err := os.WriteFile(path, []byte(EncodeRootKey(key)), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := ReadEscrowRootKey(copyPath, primary); err != nil {
		t.Fatal(err)
	} else {
		Zero(got)
	}
	for name, makeAlias := range map[string]func(string, string) error{"hardlink": os.Link, "symlink": os.Symlink} {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(dir, name)
			if err := makeAlias(primary, p); err != nil {
				t.Fatal(err)
			}
			if got, err := ReadEscrowRootKey(p, primary); err == nil {
				Zero(got)
				t.Fatal("same custody accepted")
			}
		})
	}
	if got, err := ReadEscrowRootKey(primary, primary); err == nil {
		Zero(got)
		t.Fatal("server root accepted as separate escrow")
	}
	if err := os.Chmod(copyPath, 0644); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadEscrowRootKey(copyPath, primary); err == nil {
		Zero(got)
		t.Fatal("public escrow file accepted")
	}
}
