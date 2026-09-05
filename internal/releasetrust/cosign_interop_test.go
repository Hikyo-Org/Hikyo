package releasetrust_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

// Release CI supplies the same pinned Cosign binary used by the offline
// ceremony. Normal unit tests retain maintained-library fixtures without CLI.
func TestCosignLegacyBundleInProcessInterop(t *testing.T) {
	cosign := os.Getenv("COSIGN_BIN")
	if cosign == "" {
		t.Skip("release fixture job supplies pinned COSIGN_BIN")
	}
	dir := t.TempDir()
	prefix := filepath.Join(dir, "synthetic")
	payloadPath := filepath.Join(dir, "statement.json")
	bundlePath := filepath.Join(dir, "statement.sigstore.json")
	payload := []byte(`{"fixture":"exact signed bytes"}`)
	if err := os.WriteFile(payloadPath, payload, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		command := exec.Command(cosign, args...)
		command.Env = append(os.Environ(), "COSIGN_PASSWORD=synthetic-fixture", "HTTPS_PROXY=http://127.0.0.1:9", "HTTP_PROXY=http://127.0.0.1:9", "ALL_PROXY=http://127.0.0.1:9", "NO_PROXY=")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("pinned Cosign fixture failed: %v: %s", err, output)
		}
	}
	run("generate-key-pair", "--output-key-prefix", prefix)
	run("sign-blob", "--yes", "--new-bundle-format=false", "--tlog-upload=false", "--use-signing-config=false", "--key", prefix+".key", "--bundle", bundlePath, payloadPath)
	public, err := os.ReadFile(prefix + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := releasetrust.VerifyOperatorSignature(public, bundle, payload); err != nil {
		t.Fatal(err)
	}
	if releasetrust.VerifyOperatorSignature(public, bundle, append(payload, ' ')) == nil {
		t.Fatal("changed Cosign artifact accepted")
	}
}
