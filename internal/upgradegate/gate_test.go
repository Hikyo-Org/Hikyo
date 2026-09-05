package upgradegate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/jackc/pgx/v5"
)

func gateConfig(t *testing.T, engine releaseidentity.Engine) upgrade.Config {
	t.Helper()
	if engine == releaseidentity.SQLite {
		return upgrade.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "gate.db")}
	}
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("HIKYO_TEST_POSTGRES_DSN required for real PostgreSQL acceptance")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("fixture requires PostgreSQL URL")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("hikyo_gate_%d", time.Now().UnixNano())
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		_ = admin.Close(ctx)
	})
	u.Path = "/" + name
	check, err := pgx.Connect(t.Context(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	var actual string
	err = check.QueryRow(t.Context(), "SELECT current_database()").Scan(&actual)
	_ = check.Close(t.Context())
	if err != nil || actual != name {
		t.Fatal("scratch fixture connected to wrong database")
	}
	return upgrade.Config{Engine: engine, DSN: u.String()}
}

// GateCurrentSchemaForTest derives the current-target fixture catalog from the
// embedded migrations, never the immutable historical genesis pin.
// The separate scratch database leaves the gate's fresh source untouched.
func GateCurrentSchemaForTest(t *testing.T, engine releaseidentity.Engine) releaseidentity.Digest {
	t.Helper()
	_, catalog, err := upgrade.BuildScratchSchema(t.Context(), gateConfig(t, engine), store.MigrationsFS, "migrations/"+string(engine))
	if err != nil {
		t.Fatal(err)
	}
	return catalog.Digest()
}

func signedFreshGate(t *testing.T, engine releaseidentity.Engine) (Request, []byte, func(upgradecompat.VerifiedNode) error) {
	t.Helper()
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
	fixture := testfixture.Write(t, source, []testfixture.Target{{Version: "1.0.1", Sequence: 1, Commit: strings.Repeat("a", 40), Migrations: manifest, SchemaSHA256: schema}})
	claim, err := os.ReadFile(filepath.Join(fixture.Directory, "releases", string(fixture.Target.ManifestSHA256), "upgrade-compatibility.json"))
	if err != nil {
		t.Fatal(err)
	}
	request := Request{Store: cfg, BundleDirectory: fixture.Directory, Pinned: fixture.Pinned, Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(engine), Mode: Migrate, RootKey: bytes.Repeat([]byte{37}, crypto.KeySize)}
	verify := func(node upgradecompat.VerifiedNode) error {
		if !node.Valid() || node.Identity() != fixture.Target {
			return errors.New("different test build")
		}
		return nil
	}
	return request, claim, verify
}

func TestFreshGatePersistsSchemaOnlyAndRefusesDifferentResume(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			request, claim, verify := signedFreshGate(t, engine)
			result, err := run(t.Context(), request, claim, upgrade.Production, verify)
			if err != nil {
				t.Fatal(err)
			}
			if !result.SchemaOnly || !result.State.Maintenance || result.State.Pending.Phase != upgrade.SchemaApplied {
				t.Fatalf("migrate claimed runtime readiness: %+v", result)
			}
			stored, err := upgrade.InspectControl(t.Context(), request.Store)
			if err != nil || stored.Pending.Phase != upgrade.SchemaApplied {
				t.Fatalf("missing durable boundary: %v", err)
			}
			if _, err := run(t.Context(), request, claim, upgrade.LocalDevelopment, verify); err == nil {
				t.Fatal("production source adopted by development")
			}
			if _, err := run(t.Context(), request, append(bytes.Clone(claim), ' '), upgrade.Production, verify); err == nil {
				t.Fatal("different exact embedded declaration resumed")
			}
			after, err := upgrade.InspectControl(t.Context(), request.Store)
			if err != nil || after.Generation != stored.Generation || after.Pending.Phase != stored.Pending.Phase {
				t.Fatalf("refused resume changed state: %v", err)
			}
			again, err := run(t.Context(), request, claim, upgrade.Production, verify)
			if err != nil || again.State.Generation != stored.Generation || !again.SchemaOnly {
				t.Fatalf("exact schema-only resume: %v", err)
			}
			request.Mode = Boot
			booted, err := run(t.Context(), request, claim, upgrade.Production, verify)
			if err != nil || !booted.Admission.Valid() || booted.State.Maintenance || booted.State.Pending.Phase != upgrade.Healthy {
				t.Fatalf("candidate health/admission: %v", err)
			}
			restarted, err := run(t.Context(), request, claim, upgrade.Production, verify)
			if err != nil || !restarted.Admission.Valid() || restarted.State.Generation != booted.State.Generation {
				t.Fatalf("healthy restart: %v", err)
			}
			request.RootKey = bytes.Repeat([]byte{99}, crypto.KeySize)
			if _, err := run(t.Context(), request, claim, upgrade.Production, verify); err == nil {
				t.Fatal("wrong root admitted healthy restart")
			}

		})
	}
}

func TestFreshGateInvalidTrustAndRootHaveNoDatastoreEffects(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			request, claim, verify := signedFreshGate(t, engine)
			badRoot := request
			badRoot.RootKey = nil
			badRoot.Mode = Boot
			if _, err := run(t.Context(), badRoot, claim, upgrade.Production, verify); err == nil {
				t.Fatal("missing root accepted")
			}
			request.Pinned.RecoveryPublicKey = []byte("not a public key")
			if _, err := run(t.Context(), request, claim, upgrade.Production, verify); err == nil {
				t.Fatal("invalid root signature accepted")
			}
			if _, err := upgrade.InspectControl(t.Context(), request.Store); !errors.Is(err, upgrade.ErrAbsent) {
				t.Fatalf("refused trust created control state: %v", err)
			}
			if engine == releaseidentity.SQLite {
				if _, err := os.Stat(request.Store.Path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("refused trust created SQLite file")
				}
			}
		})
	}
}
