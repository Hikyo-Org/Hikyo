package app

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestFailedRestoreDrillCleanupPreservesExistingTarget(t *testing.T) {
	for _, failure := range []string{"decrypt", "manifest", "existing-target"} {
		t.Run(failure, func(t *testing.T) {
			dir := t.TempDir()
			write := func(name string, body []byte) string {
				t.Helper()
				path := filepath.Join(dir, name)
				if err := os.WriteFile(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
				return path
			}
			identity, recipient, err := backup.GenerateIdentity()
			if err != nil {
				t.Fatal(err)
			}
			identityPath := write("identity", []byte(identity))
			root, err := crypto.GenerateRootKey()
			if err != nil {
				t.Fatal(err)
			}
			defer crypto.Zero(root)
			rootPath := write("root", []byte(crypto.EncodeRootKey(root)))
			target := write("existing.db", []byte("existing datastore must survive"))
			for _, suffix := range []string{"-wal", "-shm"} {
				write("existing.db"+suffix, []byte("existing sidecar must survive"))
			}
			before := make(map[string][]byte)
			for _, suffix := range []string{"", "-wal", "-shm"} {
				before[target+suffix], err = os.ReadFile(target + suffix)
				if err != nil {
					t.Fatal(err)
				}
			}
			payload := []byte("not a tar archive")
			if failure == "existing-target" {
				manifest, err := json.Marshal(store.Manifest{Format: store.ArchiveFormat, Engine: store.EngineSQLite, SchemaVersion: MinRestoreSchemaVersion, CreatedAt: time.Now().UTC()})
				if err != nil {
					t.Fatal(err)
				}
				var plain bytes.Buffer
				tw := tar.NewWriter(&plain)
				if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(manifest))}); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write(manifest); err != nil {
					t.Fatal(err)
				}
				if err := tw.Close(); err != nil {
					t.Fatal(err)
				}
				payload = plain.Bytes()
			}
			var sealed bytes.Buffer
			w, err := backup.Encrypt(&sealed, backup.Options{Recipients: []string{recipient}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.Write(payload); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			archive := sealed.Bytes()
			if failure == "decrypt" {
				archive = archive[:len(archive)-1]
			}
			archivePath := write("archive.age", archive)
			cfg := &config.Config{Store: config.Datastore{Engine: config.EngineSQLite, Path: filepath.Join(dir, "live.db")}, BackupRTOTarget: time.Minute}
			err = RunRestore(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), []string{"drill", "--from", archivePath, "--identity-file", identityPath, "--root-key-file", rootPath, "--principal", "operator", "--project", "org/project", "--target-sqlite", target, "--cleanup"}, io.Discard, nil, nil)
			if err == nil {
				t.Fatal("invalid drill succeeded")
			}
			for path, expected := range before {
				actual, err := os.ReadFile(path)
				if err != nil {
					t.Errorf("failed drill removed existing target %s: %v", filepath.Base(path), err)
					continue
				}
				if !bytes.Equal(actual, expected) {
					t.Errorf("failed drill changed existing target %s", filepath.Base(path))
				}
			}
		})
	}
}
