// Package testfixture invokes the actual signed development gate for owned
// databases. It has no runtime-store dependency and constructs no authority.
package testfixture

import (
	"embed"
	"os"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/devupgrade"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

func Prepare(t testing.TB, cfg upgrade.Config, migrations embed.FS, directory string, root []byte) upgrade.Admission {
	t.Helper()
	admission, _ := PrepareWithMaterial(t, cfg, migrations, directory, root)
	return admission
}

// PrepareWithMaterial retains public bundle material for tests that must repeat
// genuine verification after an explicit restore. It exposes no signing key or
// alternate admission path.
func PrepareWithMaterial(t testing.TB, cfg upgrade.Config, migrations embed.FS, directory string, root []byte) (upgrade.Admission, devupgrade.Material) {
	t.Helper()
	custody, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(custody, 0700); err != nil {
		t.Fatal(err)
	}
	material, err := devupgrade.Open(t.Context(), custody)
	if err != nil {
		t.Fatal(err)
	}
	result, err := upgradegate.RunDevelopment(t.Context(), upgradegate.Request{
		Store: cfg, BundleDirectory: material.Directory, Pinned: material.Pinned,
		Migrations: migrations, MigrationDirectory: directory, RootKey: root,
		Mode: upgradegate.Boot, AllowMigrations: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Admission.Valid() {
		t.Fatal("development gate did not return runtime admission")
	}
	return result.Admission, material
}
