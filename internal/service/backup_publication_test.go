package service

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestExportDirectorySyncFailurePreservesPublishedArtifact(t *testing.T) {
	for _, failAt := range []int{0, 1, 2} {
		t.Run([]string{"destination", "new-parent", "existing-ancestor"}[failAt], func(t *testing.T) {
			identity, recipient, err := backup.GenerateIdentity()
			if err != nil {
				t.Fatal(err)
			}
			base := t.TempDir()
			dir := filepath.Join(base, "new", "backups")
			injected := errors.New("injected directory sync failure")
			calls := 0
			var published string
			var failedDirectory string
			svc := &Backup{DB: pruneDB(t), Options: backup.Options{Recipients: []string{recipient}}}
			svc.syncDirectory = func(path string) error {
				// The final artifact must already exist and be complete before
				// directory sync. Failure must not remove that recovery option.
				files, err := filepath.Glob(filepath.Join(dir, "*.age"))
				if err != nil || len(files) != 1 {
					t.Fatalf("published artifacts at sync: %v, error %v", files, err)
				}
				published = files[0]
				assertReadableExport(t, published, identity)
				current := calls
				calls++
				if current == failAt {
					failedDirectory = path
					return injected
				}
				return filedurability.SyncDirectory(path)
			}
			result, err := svc.Export(t.Context(), dir)
			if !errors.Is(err, ErrBackupDurabilityUnconfirmed) || !errors.Is(err, injected) || !strings.Contains(err.Error(), published) {
				t.Fatalf("error = %v, want named published artifact and durability failure", err)
			}
			if !reflect.DeepEqual(result, ExportResult{}) {
				t.Fatalf("failed publication returned a successful result: %+v", result)
			}
			if calls != failAt+1 {
				t.Fatalf("sync calls = %d, want stop after %d", calls, failAt+1)
			}
			assertReadableExport(t, published, identity)
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(published) {
				t.Fatalf("failed publication should retain only final artifact: %v, error %v", entries, err)
			}
			// The directories now exist, but that does not make their entries
			// durable. A retry must still sync the ancestor that failed.
			var retrySynced []string
			svc.syncDirectory = func(path string) error {
				retrySynced = append(retrySynced, path)
				return filedurability.SyncDirectory(path)
			}
			retry, err := svc.Export(t.Context(), dir)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(retrySynced, failedDirectory) {
				t.Fatalf("retry skipped previously failed directory %s: %v", failedDirectory, retrySynced)
			}
			if retry.Path == published {
				t.Fatal("retry replaced the artifact whose durability was uncertain")
			}
			assertReadableExport(t, published, identity)
			assertReadableExport(t, retry.Path, identity)
		})
	}
}

func TestExportSyncsDirectoryAncestryWithoutOverwriting(t *testing.T) {
	identity, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	dir := filepath.Join(base, "new", "backups")
	var synced []string
	svc := &Backup{
		DB: pruneDB(t), Options: backup.Options{Recipients: []string{recipient}},
		Now: func() time.Time { return time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC) },
		syncDirectory: func(path string) error {
			synced = append(synced, path)
			return filedurability.SyncDirectory(path)
		},
	}
	first, err := svc.Export(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(synced) < 3 || synced[0] != resolved || synced[1] != filepath.Dir(resolved) || filepath.Dir(synced[len(synced)-1]) != synced[len(synced)-1] {
		t.Fatalf("sync order = %v, want destination through filesystem root", synced)
	}
	firstSynced := slices.Clone(synced)
	before, err := os.ReadFile(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	synced = nil
	second, err := svc.Export(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(synced, firstSynced) {
		t.Fatalf("existing directory ancestry = %v, want %v", synced, firstSynced)
	}
	if first.Path == second.Path || !strings.HasSuffix(second.Path, "-2.age") {
		t.Fatalf("same-second publication overwrote or misnamed an artifact: %s, %s", first.Path, second.Path)
	}
	after, err := os.ReadFile(first.Path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("first backup changed: %v", err)
	}
	assertReadableExport(t, first.Path, identity)
	assertReadableExport(t, second.Path, identity)
}

func assertReadableExport(t *testing.T, path, identity string) {
	t.Helper()
	sealed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	if err := backup.ExtractTo(&plain, bytes.NewReader(sealed), backup.Unlock{Identity: identity}); err != nil {
		t.Fatalf("published artifact is not a complete authenticated backup: %v", err)
	}
	manifest, err := store.ReadManifest(&plain)
	if err != nil || manifest.Engine != store.EngineSQLite {
		t.Fatalf("published backup manifest = %+v, error %v", manifest, err)
	}
}
