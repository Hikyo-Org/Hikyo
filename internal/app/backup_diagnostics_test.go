package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
	"github.com/Hikyo-Org/hikyo/internal/store"
	gatefixture "github.com/Hikyo-Org/hikyo/internal/upgradegate/testfixture"
)

func TestBackupRestoreDiagnosticsLevels(t *testing.T) {
	t.Run("x25519", func(t *testing.T) { backupRestoreDiagnosticsLevels(t, backup.GenerateIdentity) })
	t.Run("hybrid", func(t *testing.T) { backupRestoreDiagnosticsLevels(t, backup.GenerateHybridIdentity) })
}

func backupRestoreDiagnosticsLevels(t *testing.T, generate func() (string, string, error)) {
	source := upgradeDrillDatabase(t, store.EngineSQLite)
	_, material := gatefixture.PrepareWithMaterial(t, backupGateConfig(source), store.MigrationsFS, "migrations/sqlite", bytes.Repeat([]byte{61}, 32))
	identity, recipient, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	identityPath := filepath.Join(t.TempDir(), "private-identity-path-sentinel")
	if err := os.WriteFile(identityPath, []byte(identity), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		Dev: true, Store: config.Datastore{Engine: config.EngineSQLite, Path: source.Path, DSN: "private-dsn-sentinel"},
		Upgrade:          config.UpgradeConfiguration{StateDirectory: filepath.Dir(filepath.Dir(material.Directory))},
		BackupRecipients: []string{recipient},
	}
	for level := 0; level <= 3; level++ {
		t.Run(fmt.Sprint(level), func(t *testing.T) {
			var diagnostic, result bytes.Buffer
			ctx := diagnostics.With(t.Context(), level, &diagnostic)
			destination := t.TempDir()
			if err := RunBackup(ctx, cfg, quietLogger(), []string{"export", "--out", destination}, &result, nil, nil); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(result.String(), "wrote ") || strings.Contains(result.String(), "hikyo:") {
				t.Fatalf("export result changed: %s", result.String())
			}
			assertBackupDiagnostics(t, diagnostic.String(), level,
				"backup: exporting and encrypting consistent snapshot", "encrypted_bytes=", "backup snapshot export and encryption elapsed=",
				identity, recipient, source.Path, cfg.Store.DSN, destination, identityPath)
			archives, err := filepath.Glob(filepath.Join(destination, "*.age"))
			if err != nil || len(archives) != 1 {
				t.Fatalf("expected one encrypted archive: %v, %v", archives, err)
			}
			diagnostic.Reset()
			result.Reset()
			target := upgradeDrillDatabase(t, store.EngineSQLite)
			if err := RunRestore(ctx, backupTargetConfig(cfg, target), quietLogger(), []string{"run", "--from", archives[0], "--identity-file", identityPath}, &result, nil, nil); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(result.String(), "restored sqlite") || strings.Contains(result.String(), "hikyo:") {
				t.Fatalf("restore result changed: %s", result.String())
			}
			assertBackupDiagnostics(t, diagnostic.String(), level,
				"restore: decrypting and authenticating complete archive", "restore: engine=sqlite", "restore data publication elapsed=",
				identity, recipient, source.Path, cfg.Store.DSN, destination, identityPath, target.Path)
		})
	}
}

func TestUpgradeDrillDiagnosticsLevels(t *testing.T) {
	for level := 0; level <= 3; level++ {
		t.Run(fmt.Sprint(level), func(t *testing.T) {
			f := newUpgradeDrillFixture(t, store.EngineSQLite, true, true)
			var diagnostic bytes.Buffer
			root := crypto.EncodeRootKey(f.request.RootKey)
			result, err := DrillUpgrade(diagnostics.With(t.Context(), level, &diagnostic), f.request)
			if err != nil {
				t.Fatal(err)
			}
			if !result.HierarchyReadable || result.SecretProof != "existing-secret-readable" || result.CredentialProof != "reconciled-minted-revoked" {
				t.Fatalf("drill did not prove restored capability: %+v", result)
			}
			assertBackupDiagnostics(t, diagnostic.String(), level,
				"upgrade drill: proving stored value readability", "upgrade drill: pending_principals=", "upgrade drill archive authentication elapsed=",
				f.request.Unlock.Identity, root, "synthetic-never-output-secret", string(f.request.Principal), f.archive, f.request.Scratch.Path)
		})
	}
}

func TestUpgradeExportDiagnosticsPreserveJSON(t *testing.T) {
	f := newUpgradeDrillFixture(t, store.EngineSQLite, true, true)
	identity, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Store: config.Datastore{Engine: config.EngineSQLite, Path: f.cfg.Path}, BackupRecipients: []string{recipient}}
	trust := TrustContext{BundleDirectory: f.bundle.Directory, Pinned: f.bundle.Pinned, Target: f.bundle.Target, OperatorPin: f.request.Operator}
	for level := 0; level <= 3; level++ {
		t.Run(fmt.Sprint(level), func(t *testing.T) {
			var diagnostic, output bytes.Buffer
			destination := t.TempDir()
			err := RunUpgradeBackup(diagnostics.With(t.Context(), level, &diagnostic), cfg, []string{"upgrade-export", "--out", destination, "--json"}, &output, trust)
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Ciphertext string `json:"ciphertext"`
				Receipt    string `json:"receipt"`
			}
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatalf("verbose diagnostics corrupted JSON: %v", err)
			}
			for _, path := range []string{result.Ciphertext, result.Receipt} {
				info, err := os.Stat(path)
				if err != nil || info.Size() == 0 {
					t.Fatalf("export result artifact missing: %v", err)
				}
			}
			assertBackupDiagnostics(t, diagnostic.String(), level,
				"upgrade backup: exporting encrypted snapshot and receipt", "upgrade backup: encrypted_bytes=", "upgrade backup snapshot export and encryption elapsed=",
				identity, recipient, f.cfg.Path, f.request.Unlock.Identity, crypto.EncodeRootKey(f.root), destination, "synthetic-never-output-secret")
		})
	}
}

func TestRestoreDiagnosticsAuthenticationFailureStopsBeforeTarget(t *testing.T) {
	identity, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	identityPath := filepath.Join(dir, "private-identity-path-sentinel")
	archivePath := filepath.Join(dir, "private-archive-path-sentinel")
	if err := os.WriteFile(identityPath, []byte(identity), 0600); err != nil {
		t.Fatal(err)
	}
	var encrypted bytes.Buffer
	w, err := backup.Encrypt(&encrypted, backup.Options{Recipients: []string{recipient}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("private-payload-sentinel")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, encrypted.Bytes()[:encrypted.Len()-1], 0600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Store: config.Datastore{Engine: config.EngineSQLite, Path: filepath.Join(dir, "must-not-exist.db")}}
	var diagnostic, result bytes.Buffer
	err = RunRestore(diagnostics.With(t.Context(), 3, &diagnostic), cfg, quietLogger(), []string{"run", "--from", archivePath, "--identity-file", identityPath}, &result, nil, nil)
	if err == nil {
		t.Fatal("truncated archive accepted")
	}
	for _, suffix := range []string{"", ".lock", "-wal", "-shm"} {
		if _, err := os.Stat(cfg.Store.Path + suffix); !os.IsNotExist(err) {
			t.Fatalf("failed authentication touched target: %v", err)
		}
	}
	if result.Len() != 0 || strings.Contains(diagnostic.String(), "validating manifest") || strings.Contains(diagnostic.String(), "publishing data") || strings.Contains(diagnostic.String(), "restore complete") {
		t.Fatalf("failed authentication advanced restore: result=%q diagnostics=%q", result.String(), diagnostic.String())
	}
	assertBackupDiagnostics(t, diagnostic.String(), 3,
		"restore: decrypting and authenticating complete archive", "restore archive authentication elapsed=", "restore archive authentication elapsed=",
		identity, recipient, identityPath, archivePath, "private-payload-sentinel")
}

func assertBackupDiagnostics(t *testing.T, output string, level int, phase, detail, timing string, private ...string) {
	t.Helper()
	if level == 0 && output != "" {
		t.Fatalf("quiet command emitted diagnostics: %s", output)
	}
	for _, marker := range []struct {
		text  string
		level int
	}{{phase, 1}, {detail, 2}, {timing, 3}} {
		if strings.Contains(output, marker.text) != (level >= marker.level) {
			t.Fatalf("level %d diagnostic marker %q mismatch: %s", level, marker.text, output)
		}
	}
	if level < 3 && strings.Contains(output, "elapsed=") {
		t.Fatalf("timing exposed below level 3: %s", output)
	}
	for _, value := range private {
		if value != "" && strings.Contains(output, value) {
			t.Fatal("diagnostics exposed private input")
		}
	}
}
