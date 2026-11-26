package app

import (
	"fmt"
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

// A guarded recovery of a source that has the containment column lists the
// contained principals in its restore status and in a reconcile, as the
// ordinary restore path does; a source that predates it cannot be asked.
func TestRecoveryStatusListsContainedPrincipalsWhenTheSchemaHasContainment(t *testing.T) {
	const at = "2026-09-05T00:00:00Z"
	for _, tc := range []struct {
		version   uint64
		contained bool
	}{{87, false}, {89, true}} {
		for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
			t.Run(fmt.Sprintf("%s/%d", engine, tc.version), func(t *testing.T) {
				seed := func(t *testing.T, exec func(string, ...any), _ *crypto.Keyring) {
					t.Helper()
					exec(`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_drill','Drill',TRUE,'{}',$1)`, at)
					exec(`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_drill','org_drill','Drill',$1)`, at)
					for _, p := range []string{"usr_drill", "usr_held", "usr_plain"} {
						exec(`INSERT INTO principals (id,kind,created_at) VALUES ($1,'human',$2)`, p, at)
					}
					for _, capability := range []string{"manage-identities", "read"} {
						exec(`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ($1,'usr_drill',$2,'org_drill','prj_drill',NULL,$3)`, "gr_drill_"+capability, capability, at)
						exec(`INSERT INTO grant_origins (id,grant_id,kind,subject,created_at) VALUES ($1,$2,'manual','usr_drill',$3)`, "gor_drill_"+capability, "gr_drill_"+capability, at)
					}
					if tc.contained {
						exec(`UPDATE principals SET containment_state = 'contained', contained_at = $1 WHERE id = 'usr_held'`, at)
					}
				}
				f := recoverySchemaFixture(t, engine, tc.version, seed)
				if _, err := DrillUpgrade(t.Context(), f.request); err != nil {
					t.Fatal(err)
				}
				archive := authenticateDrillFixture(t, f)
				cfg := f.request.Scratch
				err := upgrade.WithLock(t.Context(), upgrade.Config{Engine: releaseidentity.Engine(engine), Path: cfg.Path, DSN: cfg.DSN}, func(session *upgrade.Session) error {
					admission, err := session.ScratchAdmission(t.Context(), archive, f.request.Plan)
					if err != nil {
						return err
					}
					db, err := store.OpenRecovery(t.Context(), cfg, admission)
					if err != nil {
						return err
					}
					defer func() {
						if err := db.Close(); err != nil {
							t.Error(err)
						}
					}()
					recovery := &service.Recovery{DB: db}
					status, err := recovery.Status(t.Context())
					if err != nil {
						t.Fatalf("schema%d recovery status: %v", tc.version, err)
					}
					want := []domain.PrincipalID(nil)
					if tc.contained {
						want = []domain.PrincipalID{"usr_held"}
					}
					if !slices.Equal(status.Contained, want) {
						t.Errorf("schema%d status contained = %v, want %v", tc.version, status.Contained, want)
					}
					after, err := recovery.Reconcile(t.Context(), "usr_plain")
					if err != nil {
						t.Fatalf("schema%d recovery reconcile: %v", tc.version, err)
					}
					if !slices.Equal(after.Contained, want) {
						t.Errorf("schema%d reconcile contained = %v, want %v", tc.version, after.Contained, want)
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
