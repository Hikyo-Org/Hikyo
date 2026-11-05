package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/mldsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/configrollout"
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

func rolloutKeyFiles(t *testing.T) (dir string, edPEM, pqPEM, edPub, pqPub []byte) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pq, err := mldsa.GenerateKey(mldsa.MLDSA65())
	if err != nil {
		t.Fatal(err)
	}
	edDER, _ := x509.MarshalPKCS8PrivateKey(private)
	pqDER, _ := x509.MarshalPKCS8PrivateKey(pq)
	edPubDER, _ := x509.MarshalPKIXPublicKey(private.Public())
	pqPubDER, _ := x509.MarshalPKIXPublicKey(pq.PublicKey())
	block := func(kind string, der []byte) []byte { return pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}) }
	return t.TempDir(), block("PRIVATE KEY", edDER), block("PRIVATE KEY", pqDER), block("PUBLIC KEY", edPubDER), block("PUBLIC KEY", pqPubDER)
}

func TestStageRolloutAuthorityAcceptsExactlyTheEnrolledShapes(t *testing.T) {
	dir, edPEM, pqPEM, _, _ := rolloutKeyFiles(t)
	for name, tc := range map[string]struct {
		raw []byte
		ok  bool
	}{
		"ed25519":       {edPEM, true},
		"dual":          {append(append([]byte{}, edPEM...), pqPEM...), true},
		"ml-dsa only":   {pqPEM, true},
		"duplicate":     {append(append([]byte{}, edPEM...), edPEM...), false},
		"trailing junk": {append(append([]byte{}, edPEM...), "junk"...), false},
	} {
		source := filepath.Join(dir, "source-"+name)
		destination := filepath.Join(dir, "runtime-"+name)
		if err := os.WriteFile(source, tc.raw, 0440); err != nil {
			t.Fatal(err)
		}
		err := stageRolloutAuthority(source, destination)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err=%v", name, err)
		}
		if tc.ok {
			if stored, _ := os.ReadFile(destination); string(stored) != string(tc.raw) {
				t.Errorf("%s: staged bytes changed", name)
			}
		} else if _, err := os.Stat(destination); err == nil {
			t.Errorf("%s: refused signer was staged", name)
		}
	}
}

func TestPrintAuthorityIDsMatchesTheLibraryIdentity(t *testing.T) {
	dir, _, _, edPub, pqPub := rolloutKeyFiles(t)
	path := filepath.Join(dir, "authority.pub")
	if err := os.WriteFile(path, append(append([]byte{}, edPub...), pqPub...), 0444); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = write
	code := runConfigRollout(t.Context(), []string{"--authority-public-key", path, "--print-authority-ids"}, &stderr)
	os.Stdout = stdout
	_ = write.Close()
	out, _ := io.ReadAll(read)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	keys, err := configrollout.ParsePublicKeys(append(append([]byte{}, edPub...), pqPub...))
	if err != nil {
		t.Fatal(err)
	}
	ids := keys.KeyIDs()
	want := "ed25519 " + ids["ed25519"] + "\nmldsa65 " + ids["mldsa65"] + "\n"
	if string(out) != want {
		t.Fatalf("got %q want %q", out, want)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("junk"), 0600); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if runConfigRollout(t.Context(), []string{"--authority-public-key", path, "--print-authority-ids"}, &stderr) == 0 {
		t.Fatal("junk key file accepted")
	}
}

func TestConfigRolloutRefusesKeyFilesThatAreNotTheEnrolledKeys(t *testing.T) {
	dir, _, _, edPub, pqPub := rolloutKeyFiles(t)
	_, _, _, otherEd, otherPQ := rolloutKeyFiles(t)
	keys, err := configrollout.ParsePublicKeys(append(append([]byte{}, edPub...), pqPub...))
	if err != nil {
		t.Fatal(err)
	}
	ids := keys.KeyIDs()
	enrollment := configrollout.Enrollment{
		ID: "enrollment-1", OwnerInstanceID: "owner", Incarnation: "incarnation",
		Target:        configrollout.Target{Namespace: "hikyo", Deployment: "hikyo", DeploymentUID: "d-1", StableNodeID: "node", Container: "hikyo", ConfigSecret: "config", RollbackSecret: "rollback", RequestSecret: "plan", ReceiptSecret: "receipt"},
		CommandSecret: "command", CommandSecretUID: "c-1", ResponseSecret: "response", ResponseSecretUID: "r-1", JournalSecret: "journal", JournalSecretUID: "j-1", LeaseName: "lease", LeaseUID: "l-1", ExecutorPod: "executor-0",
		Authority: &configrollout.Authority{Profile: "pqc-auth/1", Generation: 1, Required: []string{"ed25519", "mldsa65"}, KeyIDs: []string{ids["ed25519"], ids["mldsa65"]}},
	}
	raw, err := json.Marshal(enrollment)
	if err != nil {
		t.Fatal(err)
	}
	enrollmentPath := filepath.Join(dir, "enrollment.json")
	if err := os.WriteFile(enrollmentPath, raw, 0444); err != nil {
		t.Fatal(err)
	}
	for name, files := range map[string][]byte{
		"ed25519 only":   edPub,
		"swapped ML-DSA": append(append([]byte{}, edPub...), otherPQ...),
		"swapped both":   append(append([]byte{}, otherEd...), otherPQ...),
	} {
		path := filepath.Join(dir, "authority-"+name+".pub")
		if err := os.WriteFile(path, files, 0444); err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		// Refused before any cluster contact: there is none to contact here.
		if code := runConfigRollout(t.Context(), []string{"--enrollment-file", enrollmentPath, "--authority-public-key", path}, &stderr); code != 1 || !strings.Contains(stderr.String(), "does not match the enrollment") {
			t.Errorf("%s: exit %d: %q", name, code, stderr.String())
		}
	}
}
