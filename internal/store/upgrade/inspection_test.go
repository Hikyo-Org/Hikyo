package upgrade

import (
	"reflect"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func TestDomainCatalogAndSourceInspectionShareExactFingerprint(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		if err := migrateFixture(t, cfg); err != nil {
			t.Fatal(err)
		}
		manifest, err := PinnedLegacyManifest(cfg.Engine)
		if err != nil {
			t.Fatal(err)
		}
		before, err := InspectInstalled(t.Context(), cfg, manifest)
		if err != nil {
			t.Fatal(err)
		}
		if before.Ledger != nil || before.InstanceID == "" || before.Source.Genesis != LegacyGenesis || !before.RequiresLegacyStop {
			t.Fatal("preledger inspection invented authority")
		}
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			state, err := s.Bootstrap(t.Context(), manifest, legacyOperation(t, cfg, manifest), Production)
			if err != nil {
				return err
			}
			catalog, err := s.DomainCatalog(t.Context())
			if err != nil {
				return err
			}
			if catalog.Digest() != before.SchemaDigest || catalog.Digest() != state.SchemaDigest {
				t.Fatal("control install changed domain fingerprint")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		after, err := InspectInstalled(t.Context(), cfg, manifest)
		if err != nil {
			t.Fatal(err)
		}
		if after.Ledger == nil || after.SchemaDigest != before.SchemaDigest || after.InstanceID != before.InstanceID {
			t.Fatal("source identity changed across bootstrap")
		}
		query(t, cfg, `ALTER TABLE upgrade_control ADD COLUMN unrecognized TEXT`)
		if _, err := InspectInstalled(t.Context(), cfg, manifest); err == nil {
			t.Fatal("changed control schema ignored")
		}
	})
}

func TestCatalogRejectsUnknownObjectsBeyondTables(t *testing.T) {
	for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := testConfig(t, engine)
			if err := migrateFixture(t, cfg); err != nil {
				t.Fatal(err)
			}
			manifest, err := PinnedLegacyManifest(cfg.Engine)
			if err != nil {
				t.Fatal(err)
			}
			statement := `CREATE TRIGGER extra_trigger AFTER INSERT ON orgs BEGIN SELECT 1; END`
			if engine == releaseidentity.Postgres {
				statement = `CREATE FUNCTION public.extra_function() RETURNS integer LANGUAGE SQL AS 'SELECT 1'`
			}
			query(t, cfg, statement)
			if _, err := InspectInstalled(t.Context(), cfg, manifest); err == nil {
				t.Fatal("extra executable schema object accepted")
			}
		})
	}
}

func TestSnapshotRejectsAppliedHistoryDrift(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		if err := migrateFixture(t, cfg); err != nil {
			t.Fatal(err)
		}
		manifest, err := PinnedLegacyManifest(cfg.Engine)
		if err != nil {
			t.Fatal(err)
		}
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			_, err := s.Bootstrap(t.Context(), manifest, legacyOperation(t, cfg, manifest), Production)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		initial, err := InspectInstalled(t.Context(), cfg, manifest)
		if err != nil {
			t.Fatal(err)
		}
		query(t, cfg, `DELETE FROM goose_db_version WHERE version_id=1`)
		if _, err := InspectInstalled(t.Context(), cfg, manifest); err == nil {
			t.Fatal("missing migration accepted")
		}
		err = WithLock(t.Context(), cfg, func(s *Session) error {
			state, err := s.Read(t.Context())
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(state, *initial.Ledger) {
				t.Fatal("read-only refusal mutated ledger")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestPostgresNonTableCatalogDriftRefuses(t *testing.T) {
	for _, statement := range []string{
		`CREATE OPERATOR public.## (FUNCTION = int4pl, LEFTARG = integer, RIGHTARG = integer)`,
		`SELECT lo_create(0)`,
		`CREATE SCHEMA unexpected_namespace`,
		`CREATE STATISTICS public.unexpected_statistics ON id,name FROM orgs`,
	} {
		t.Run(statement, func(t *testing.T) {
			cfg := testConfig(t, releaseidentity.Postgres)
			if err := migrateFixture(t, cfg); err != nil {
				t.Fatal(err)
			}
			manifest, err := PinnedLegacyManifest(cfg.Engine)
			if err != nil {
				t.Fatal(err)
			}
			query(t, cfg, statement)
			if _, err := InspectInstalled(t.Context(), cfg, manifest); err == nil {
				t.Fatal("unsupported non-table schema drift accepted")
			}
		})
	}
}
