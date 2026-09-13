package selfupdate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func TestVerifyCachedReleaseDerivesPayloadAuthorityOffline(t *testing.T) {
	for _, stable := range []bool{false, true} {
		t.Run(map[bool]string{false: "nightly", true: "stable"}[stable], func(t *testing.T) {
			installer, status, _, _, _, responses := preparedNightlyFixture(t, nil)
			if stable {
				installer, status, _, _, _, responses = preparedStableFixture(t)
			}
			prepared, err := installer.PrepareRelease(t.Context(), status)
			if err != nil {
				t.Fatal(err)
			}
			// Removing every HTTP response proves this is an offline path.
			clear(responses)
			altered := prepared
			altered.BinaryPath = filepath.Join(t.TempDir(), "unsigned-executable")
			altered.BinarySHA256 = releaseidentity.Hash([]byte("unsigned executable"))
			verified, err := installer.VerifyCachedRelease(t.Context(), altered, releaseidentity.SnapshotFloor{})
			if err != nil {
				t.Fatal(err)
			}
			if verified.BinaryPath != prepared.BinaryPath || verified.BinarySHA256 != prepared.BinarySHA256 || verified.Identity != prepared.Identity {
				t.Fatal("unsigned cached descriptor became executable authority")
			}
			archive := filepath.Join(prepared.Directory, mustArchiveName(t, prepared.Identity.Version))
			if err := os.WriteFile(archive, []byte("tampered cached archive"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := installer.VerifyCachedRelease(t.Context(), altered, releaseidentity.SnapshotFloor{}); err == nil {
				t.Fatal("accepted corrupted signed archive")
			}
		})
	}
}

func TestVerifyCachedReleaseRefusesChangedExtractedExecutable(t *testing.T) {
	installer, status, _, _, _, _ := preparedStableFixture(t)
	prepared, err := installer.PrepareRelease(t.Context(), status)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prepared.BinaryPath, []byte("changed executable"), 0700); err != nil {
		t.Fatal(err)
	}
	prepared.BinarySHA256 = releaseidentity.Hash([]byte("changed executable"))
	if _, err := installer.VerifyCachedRelease(t.Context(), prepared, releaseidentity.SnapshotFloor{}); err == nil {
		t.Fatal("accepted matching unsigned digest for changed executable")
	}
}
