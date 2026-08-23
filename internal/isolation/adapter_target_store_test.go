package isolation

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestAdapterUpdateTargetRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		open func(*testing.T) *store.DB
	}{
		{name: "sqlite", open: openSQLite},
		{name: "postgres", open: openPostgres},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := seededDB(t, tt.open)
			seedAdapterUpdateTarget(t, db)

			scope := domain.Scope{Org: orgA, Project: prjA1}
			at := time.Date(2026, time.August, 23, 12, 34, 56, 123456000, time.UTC)
			var updated store.AdapterTargetUpdateResult
			err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
				proof, err := az.Authorize(ctx, authz.Identity{Principal: alice}, authz.OpAdapterConfigure, scope)
				if err != nil {
					return err
				}
				updated, err = repos.Adapters().UpdateTarget(ctx, proof, store.AdapterTargetUpdate{
					ExpectedGeneration:   1,
					AuthorityPrincipalID: string(alice),
					At:                   at,
					Target: store.AdapterTargetMutation{
						ID: "tgt_dialect", AdapterID: "adp_dialect", EnvironmentID: string(envA1),
						DestinationKind: "repository", DestinationOwner: "hikyo", DestinationName: "core",
						DestinationID: 42, NamePrefix: "UPDATED_", KeyIDs: []string{keyA1},
					},
				})
				return err
			})
			if err != nil {
				t.Fatal(err)
			}

			var stored store.AdapterTarget
			var keyIDs []string
			err = storetx.Read(t.Context(), db, func(ctx context.Context, repos store.ReadRepos, az *authz.TxAuthorizer) error {
				proof, err := az.Authorize(ctx, authz.Identity{Principal: alice}, authz.OpAdapterConfigure, scope)
				if err != nil {
					return err
				}
				stored, err = repos.Adapters().Target(ctx, proof, "tgt_dialect")
				if err != nil {
					return err
				}
				keyIDs, err = repos.Adapters().TargetKeyIDs(ctx, proof, "tgt_dialect")
				return err
			})
			if err != nil {
				t.Fatal(err)
			}

			if updated.Target.NamePrefix != "UPDATED_" || updated.Target.Generation != 2 ||
				stored.NamePrefix != "UPDATED_" || stored.Generation != 2 ||
				!slices.Equal(keyIDs, []string{keyA1}) || updated.Enqueue.JobID == "" {
				t.Fatalf("updated=%+v stored=%+v key_ids=%v", updated, stored, keyIDs)
			}
		})
	}
}

func seedAdapterUpdateTarget(t *testing.T, db *store.DB) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_adapter_dialect','usr_alice','manage-adapters','org_a','prj_a1',NULL,` + ts + `)`,
		`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_dialect','org_a','prj_a1','forgejo','https://git.example','usr_alice','active',` + ts + `)`,
		`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_dialect','org_a','prj_a1','env_a1','adp_dialect','repository','hikyo','core',42,'INITIAL_',1,'active','never',` + ts + `)`,
		`INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES ('org_a','prj_a1','env_a1','tgt_dialect','adp_dialect','key_a1')`,
	} {
		execRaw(t, db, statement)
	}
	seedOrigins(t, db)
}
