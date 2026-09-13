//go:build linux

package app

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

func TestUnattendedCredentialSealedRepeatedChildReads(t *testing.T) {
	root := bytes.Repeat([]byte{91}, crypto.KeySize)
	if os.Getenv("HIKYO_TEST_UNATTENDED_ROOT_CHILD") == "true" {
		for range 2 {
			actual, err := crypto.ReadRootKey("/proc/self/fd/3", "")
			if err != nil || !bytes.Equal(actual, root) {
				t.Fatalf("child root read failed: %v", err)
			}
			crypto.Zero(actual)
		}
		return
	}
	credential, path, err := unattendedRootCredential(&config.Config{}, root)
	if err != nil {
		t.Fatal(err)
	}
	defer credential.Close()
	info, err := credential.Stat()
	if err != nil || info.Mode().Perm() != 0600 || path != "/proc/self/fd/3" {
		t.Fatalf("unexpected credential transport: %v", err)
	}
	if _, err := credential.WriteAt([]byte("x"), 0); err == nil {
		t.Fatal("sealed root credential remained writable")
	}
	command := exec.CommandContext(t.Context(), "/proc/self/exe", "-test.run=^TestUnattendedCredentialSealedRepeatedChildReads$")
	command.Env = append(os.Environ(), "HIKYO_TEST_UNATTENDED_ROOT_CHILD=true")
	command.ExtraFiles = []*os.File{credential}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("credential child failed: %v: %s", err, output)
	}
}
