package configrollout

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The enrollment runbook generates keys with OpenSSL. This proves the exact
// commands it prints produce files the code accepts, and that the default
// OpenSSL ML-DSA private encoding (seed plus expanded key) is refused. It skips
// where OpenSSL is older than 3.5 or lacks the seed-only provider parameter.
func TestRunbookOpenSSLKeyFilesParse(t *testing.T) {
	openssl, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("openssl not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) []byte {
		t.Helper()
		out, err := exec.Command(openssl, args...).CombinedOutput()
		if err != nil {
			t.Skipf("openssl %v unsupported here: %s", args, bytes.TrimSpace(out))
		}
		return out
	}
	ed := filepath.Join(dir, "authority-ed25519.key")
	pq := filepath.Join(dir, "authority-mldsa65.key")
	expanded := filepath.Join(dir, "expanded.key")
	run("genpkey", "-algorithm", "ED25519", "-out", ed)
	run("genpkey", "-algorithm", "ML-DSA-65", "-provparam", "ml-dsa.output_formats=seed-only", "-out", pq)
	run("genpkey", "-algorithm", "ML-DSA-65", "-out", expanded)
	read := func(path string) []byte {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	dual := append(append([]byte{}, read(ed)...), read(pq)...)
	signers, err := ParseSigners(dual)
	if err != nil || signers.Ed25519 == nil || signers.MLDSA65 == nil {
		t.Fatalf("runbook signer file refused: %v", err)
	}
	if _, err := ParseSigners(read(expanded)); err == nil {
		t.Fatal("OpenSSL default expanded ML-DSA private key accepted")
	}
	edPub := run("pkey", "-in", ed, "-pubout")
	pqPub := run("pkey", "-in", pq, "-pubout")
	keys, err := ParsePublicKeys(append(append([]byte{}, edPub...), pqPub...))
	if err != nil {
		t.Fatalf("runbook public file refused: %v", err)
	}
	want, got := signers.Public().KeyIDs(), keys.KeyIDs()
	if len(got) != 2 || got[AlgEd25519] != want[AlgEd25519] || got[AlgMLDSA65] != want[AlgMLDSA65] {
		t.Fatal("public keys exported by OpenSSL do not match the private keys' ids", got, want)
	}
}
