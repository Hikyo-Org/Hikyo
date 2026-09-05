package app

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestUpgradeCLISignsActualDrill(t *testing.T) {
	executable := os.Getenv("HIKYO_TEST_COSIGN")
	if executable == "" {
		if os.Getenv("HIKYO_REQUIRE_COSIGN_TEST") == "1" {
			t.Fatal("required cosign executable missing")
		}
		t.Skip("dedicated supply-chain lane supplies HIKYO_TEST_COSIGN")
	}
	t.Setenv("COSIGN_PASSWORD", "synthetic-operator-test-passphrase")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:9")
	t.Setenv("NO_PROXY", "")
	custody := t.TempDir()
	prefix := filepath.Join(custody, "operator")
	keygen := exec.CommandContext(t.Context(), executable, "generate-key-pair", "--output-key-prefix", prefix)
	keygen.Stdout = io.Discard
	keygen.Stderr = io.Discard
	if err := keygen.Run(); err != nil {
		t.Fatal("real cosign fixture key generation failed")
	}
	f := newUpgradeDrillFixture(t, store.EngineSQLite, true, true)
	public, err := os.ReadFile(prefix + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	pin, err := backupreceipt.PinOperator(f.source.InstanceID, public)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, raw []byte) string {
		path := filepath.Join(custody, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	receiptPath := write("receipt.json", f.request.Receipt)
	identityPath := write("backup-identity.txt", []byte(f.request.Unlock.Identity))
	rootPath := write("root.txt", []byte(crypto.EncodeRootKey(f.root)))
	destination := t.TempDir()
	scratch := filepath.Join(t.TempDir(), "restored.db")
	trust := TrustContext{Pinned: f.bundle.Pinned, Target: f.bundle.Target, Floor: releaseidentity.SnapshotFloor{}, OperatorPin: pin}
	args := []string{"upgrade-drill", "--bundle", f.bundle.Directory, "--from", f.archive, "--receipt", receiptPath, "--identity-file", identityPath, "--root-key-file", rootPath, "--target-sqlite", scratch, "--principal", "usr_drill", "--project", "org_drill/prj_drill", "--out", destination, "--cosign", executable, "--signing-key", prefix + ".key", "--valid-for", "1h"}
	var output bytes.Buffer
	if err := RunUpgradeBackup(t.Context(), &config.Config{}, args, &output, trust); err != nil {
		t.Fatal("actual signed CLI drill failed", err)
	}
	for _, private := range []string{f.request.Unlock.Identity, crypto.EncodeRootKey(f.root), "synthetic-never-output-secret", "synthetic-operator-test-passphrase"} {
		if strings.Contains(output.String(), private) {
			t.Fatal("CLI output leaked private custody or recovered plaintext")
		}
	}
	statements, err := filepath.Glob(filepath.Join(destination, "upgrade-attestation-*.json"))
	if err != nil || len(statements) != 2 {
		t.Fatal("public signature pair incomplete", err)
	}
	var statement, signature []byte
	for _, path := range statements {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(path, ".sigstore.json") {
			signature = raw
		} else {
			statement = raw
		}
	}
	if err := backupreceipt.CheckOperatorSignature(pin, signature, statement); err != nil {
		t.Fatal(err)
	}
	material := backupreceipt.EvidenceMaterial{Receipt: f.request.Receipt, Attestation: statement, Signature: signature}
	verified, err := backupreceipt.VerifyLegacyEvidence(t.Context(), pin, f.request.Plan, f.request.Ciphertext, material, backupreceipt.LegacyInspection{InstanceID: f.source.InstanceID, Engine: releaseidentity.SQLite, SchemaSHA256: f.source.SchemaDigest, MigrationSHA256: f.source.MigrationDigest, RestoreEpoch: f.source.RestoreEpoch}, f.proposal, time.Now())
	if err != nil || !verified.Valid() {
		t.Fatal("real cosign CLI output failed public-only gate", err)
	}
	if _, _, err := publishUpgradeAttestation(destination, statement, signature); err == nil {
		t.Fatal("public attestation pair overwrote existing evidence")
	}
}
