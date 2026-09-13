package upgradegate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func TestAuthenticatedBackupPreparationPinsFinalTargetAndRefusesMissingProof(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := gateConfig(t, engine)
			empty := releaseidentity.MigrationManifest{Engine: engine, Entries: []releaseidentity.Migration{}}
			inspected, err := upgrade.Inspect(t.Context(), cfg, empty)
			if err != nil {
				t.Fatal(err)
			}
			manifest, err := releaseidentity.BuildMigrationManifest(store.MigrationsFS, "migrations/"+string(engine), engine)
			if err != nil {
				t.Fatal(err)
			}
			schema := GateCurrentSchemaForTest(t, engine)
			source := upgradecompat.InstalledSource{Identity: releaseidentity.Source{Genesis: releaseidentity.FreshGenesisV1}, Migrations: empty, SchemaSHA256: inspected.CatalogDigest}
			fixture := testfixture.Write(t, source, []testfixture.Target{
				{Version: "1.0.1", Sequence: 1, Commit: strings.Repeat("a", 40), Migrations: manifest, SchemaSHA256: schema},
				{Version: "1.0.2", Sequence: 2, Commit: strings.Repeat("b", 40), Migrations: manifest, SchemaSHA256: schema},
				{Version: "1.0.3", Sequence: 3, Commit: strings.Repeat("c", 40), Migrations: manifest, SchemaSHA256: schema},
			})
			stateDirectory, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(stateDirectory, 0700); err != nil {
				t.Fatal(err)
			}
			request := Request{Store: cfg, BundleDirectory: fixture.Directory, Pinned: fixture.Pinned, Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(engine), Mode: Boot, AllowMigrations: true, RootKey: bytes.Repeat([]byte{37}, crypto.KeySize), StateDirectory: stateDirectory, InitialOperatorPublicKey: fixture.Signer.PrimaryPublic}
			invoke := func(request Request, index int) (Result, error) {
				identity := fixture.Identities[index]
				claim, err := os.ReadFile(filepath.Join(fixture.Directory, "releases", string(identity.ManifestSHA256), "upgrade-compatibility.json"))
				if err != nil {
					return Result{}, err
				}
				return run(t.Context(), request, claim, upgrade.Production, func(node upgradecompat.VerifiedNode) error {
					if !node.Valid() || node.Identity() != identity {
						return errors.New("wrong signed build")
					}
					return nil
				})
			}
			booted, err := invoke(request, 0)
			if err != nil {
				t.Fatal(err)
			}
			if !booted.Admission.Valid() {
				t.Fatal("fresh source not admitted")
			}
			request.Mode = PrepareBackup
			wrong := request
			wrong.Target = fixture.Identities[1]
			if _, err := invoke(wrong, 2); err == nil {
				t.Fatal("coordinator targeted a different image")
			}
			frozen, err := invoke(request, 2)
			if err != nil {
				t.Fatal(err)
			}
			if frozen.Admission.Valid() || !frozen.SchemaOnly || frozen.State.Applied != booted.State.Applied || frozen.State.Pending.Phase != upgrade.BackupPreparing || frozen.State.Pending.Preparation.Target != fixture.Target {
				t.Fatal("fence lost exact source/target binding")
			}
			again, err := invoke(request, 2)
			if err != nil || !sameOperatorState(again.State, frozen.State) {
				t.Fatalf("exact retry changed fence: %v", err)
			}
			if _, err := invoke(request, 1); err == nil {
				t.Fatal("another signed target replaced pinned route")
			}
			request.Mode = Boot
			if _, err := invoke(request, 0); !errors.Is(err, ErrNextBinary) {
				t.Fatalf("old image restarted frozen source: %v", err)
			}
			request.Mode = MaintenanceConfiguration
			called := false
			request.CheckConfiguration = func(context.Context, *upgrade.CandidateConfiguration, map[string]string) error {
				called = true
				return nil
			}
			if _, err := invoke(request, 1); err == nil || called {
				t.Fatal("another image read the maintenance transport")
			}
			if _, err := invoke(request, 2); err == nil || called {
				t.Fatal("missing saved configuration fell back to bootstrap transport")
			}
			request.Mode = PrepareRoute
			if _, err := invoke(request, 2); err == nil {
				t.Fatal("missing restore proof authorized migration")
			}
			after, err := upgrade.InspectControl(t.Context(), cfg)
			if err != nil || !sameOperatorState(after, frozen.State) {
				t.Fatalf("failed proof changed source: %v", err)
			}
		})
	}
}
