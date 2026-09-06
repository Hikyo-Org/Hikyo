package backupreceipt

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestEvidencePreflightReadsExactArtifactWithoutCopyOrAuthority(t *testing.T) {
	f := newSignedEvidenceFixture(t, false)
	directory := t.TempDir()
	path := filepath.Join(directory, "backup.age")
	raw, err := os.ReadFile(f.ciphertext.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckEvidenceArtifacts(t.Context(), f.pin, f.plan, path, f.material, f.now); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadDir(directory)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("preflight created files")
	}
	for _, tc := range []struct {
		name     string
		material EvidenceMaterial
		now      time.Time
	}{
		{"missing signature", EvidenceMaterial{Receipt: f.material.Receipt, Attestation: f.material.Attestation}, f.now},
		{"substituted receipt", EvidenceMaterial{Receipt: append(append([]byte{}, f.material.Receipt...), ' '), Attestation: f.material.Attestation, Signature: f.material.Signature}, f.now},
		{"expired", f.material, f.now.Add(48 * time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckEvidenceArtifacts(t.Context(), f.pin, f.plan, path, tc.material, tc.now); err == nil {
				t.Fatal("accepted invalid evidence")
			}
		})
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := CheckEvidenceArtifacts(cancelled, f.pin, f.plan, path, f.material, f.now); err == nil {
		t.Fatal("ignored cancellation")
	}
	raw[0] ^= 1
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := CheckEvidenceArtifacts(t.Context(), f.pin, f.plan, path, f.material, f.now); err == nil {
		t.Fatal("accepted replaced ciphertext")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.ciphertext.path, path); err != nil {
		t.Fatal(err)
	}
	if err := CheckEvidenceArtifacts(t.Context(), f.pin, f.plan, path, f.material, f.now); err == nil {
		t.Fatal("followed substituted symlink")
	}
}
