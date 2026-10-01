package service

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/devupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// Token and adapter tests need isolated current schemas, not independent
// installation identities. Keep owner, boot and migration fixtures fresh.
// Cache only schema-applied bytes and public evidence; every copy must finish
// the real gate with its own root and newly initialized encryption hierarchy.
type serviceSchemaFixture struct {
	database []byte
	bundle   map[string][]byte
	pinned   releasetrust.PinnedTrust
}

var preparedServiceSchema = sync.OnceValues(buildServiceSchemaFixture)

func buildServiceSchemaFixture() (*serviceSchemaFixture, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	directory, err := os.MkdirTemp("", "hikyo-service-schema-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return nil, err
	}
	material, err := devupgrade.Open(ctx, directory)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(directory, "schema.db")
	result, err := upgradegate.RunDevelopment(ctx, serviceSchemaRequest(path, material, nil, upgradegate.Migrate))
	if err != nil {
		return nil, err
	}
	if !result.SchemaOnly || result.Admission.Valid() {
		return nil, errors.New("service schema fixture unexpectedly admitted runtime access")
	}
	// The gate has closed every connection before its durable database is read.
	database, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fixture := &serviceSchemaFixture{database: database, bundle: map[string][]byte{}, pinned: material.Pinned}
	public := os.DirFS(material.Directory)
	err = fs.WalkDir(public, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return errors.New("service schema fixture contains non-regular public evidence")
		}
		contents, err := fs.ReadFile(public, name)
		if err == nil {
			fixture.bundle[name] = contents
		}
		return err
	})
	return fixture, err
}

func serviceSchemaRequest(path string, material devupgrade.Material, root []byte, mode upgradegate.Mode) upgradegate.Request {
	return upgradegate.Request{
		Store:           upgrade.Config{Engine: releaseidentity.SQLite, Path: path},
		BundleDirectory: material.Directory, Pinned: material.Pinned,
		Migrations: store.MigrationsFS, MigrationDirectory: "migrations/sqlite",
		RootKey: root, Mode: mode, AllowMigrations: mode == upgradegate.Migrate,
	}
}

func (fixture *serviceSchemaFixture) install(t testing.TB, path string) devupgrade.Material {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write(fixture.database)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
	material := devupgrade.Material{Directory: t.TempDir(), Pinned: fixture.pinned}
	material.Pinned.Root = bytes.Clone(fixture.pinned.Root)
	material.Pinned.RecoveryPublicKey = bytes.Clone(fixture.pinned.RecoveryPublicKey)
	for name, contents := range fixture.bundle {
		destination := filepath.Join(material.Directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(destination, contents, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return material
}

func openPreparedServiceFixture(t testing.TB, cfg store.Config) (*store.DB, error) {
	t.Helper()
	if cfg.Engine != store.EngineSQLite {
		return nil, errors.New("prepared service fixture requires SQLite")
	}
	fixture, err := preparedServiceSchema()
	if err != nil {
		return nil, err
	}
	material := fixture.install(t, cfg.Path)
	root, err := crypto.GenerateRootKey()
	if err != nil {
		return nil, err
	}
	gateRoot := bytes.Clone(root)
	defer crypto.Zero(gateRoot)
	result, err := upgradegate.RunDevelopment(t.Context(), serviceSchemaRequest(cfg.Path, material, gateRoot, upgradegate.Boot))
	if err != nil {
		crypto.Zero(root)
		return nil, err
	}
	return openAdmittedServiceFixture(t, cfg, root, result.Admission)
}
