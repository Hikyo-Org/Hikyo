package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func moveProviderTarget(t *testing.T, db *store.DB, move string, target store.AdapterTargetMutation) error {
	t.Helper()
	return adapterControlProof(t, db, authz.OpAdapterConfigure, func(ctx context.Context, repos store.Repos, p authz.Proof) error {
		_, err := repos.Adapters().MoveTarget(ctx, p, store.AdapterRouteMoveMutation{MoveID: move, Target: target, ExpectedGeneration: 1, AuthorityPrincipalID: "usr_adapter", KeepRemote: true, At: time.Now().UTC()})
		return err
	})
}

func TestProviderMoveClaimsUseSecretSurfaceForConfig(t *testing.T) {
	for _, provider := range []string{"cloudflare", "vault-kv"} {
		t.Run(provider, func(t *testing.T) {
			db := adapterControlDB(t)
			if _, err := db.SQLiteWrite().ExecContext(t.Context(), `UPDATE adapters SET provider=? WHERE id='adp_1'`, provider); err != nil {
				t.Fatal(err)
			}
			kind := "repository"
			if provider == "cloudflare" {
				kind = "workers-script"
			}
			target := store.AdapterTargetMutation{ID: "tgt_1", AdapterID: "adp_1", EnvironmentID: "env_adapter", DestinationKind: kind, DestinationOwner: "account", DestinationName: "new-route", KeyIDs: []string{"key_a"}}
			if provider == "cloudflare" {
				invalid := target
				invalid.RepositoryID = 42
				if err := moveProviderTarget(t, db, "move_invalid", invalid); !errors.Is(err, domain.ErrInvalid) {
					t.Fatalf("repository id accepted: %v", err)
				}
			}
			for _, q := range []string{
				`INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) SELECT 'key_collision',org_id,project_id,'P_APP_MODE',folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at FROM keys WHERE id='key_a'`,
				`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) SELECT 'tgt_configured',org_id,project_id,environment_id,adapter_id,'` + kind + `','account','new-route',99,'P_',1,'active','never',created_at FROM adapter_targets WHERE id='tgt_1'`,
				`INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES ('org_adapter','prj_adapter','env_adapter','tgt_configured','adp_1','key_a')`,
			} {
				if _, err := db.SQLiteWrite().ExecContext(t.Context(), q); err != nil {
					t.Fatal(err)
				}
			}
			collision := target
			collision.KeyIDs = []string{"key_collision"}
			if err := moveProviderTarget(t, db, "move_config_collision", collision); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("configured config overlap accepted: %v", err)
			}
			for _, q := range []string{`DELETE FROM adapter_target_keys WHERE target_id='tgt_configured'`, `DELETE FROM adapter_targets WHERE id='tgt_configured'`} {
				if _, err := db.SQLiteWrite().ExecContext(t.Context(), q); err != nil {
					t.Fatal(err)
				}
			}
			if err := moveProviderTarget(t, db, "move_secret", target); err != nil {
				t.Fatal(err)
			}
			var surface string
			if err := db.SQLiteRead().QueryRowContext(t.Context(), `SELECT surface FROM adapter_route_move_claims WHERE move_id='move_secret' AND key_id='key_a'`).Scan(&surface); err != nil {
				t.Fatal(err)
			}
			if surface != "secret" {
				t.Fatalf("config claim surface=%q", surface)
			}
		})
	}
}

func TestAWSMovesRefuseCrossKindCaseFoldedPendingClaim(t *testing.T) {
	db := adapterControlDB(t)
	for _, q := range []string{
		`UPDATE adapters SET provider='aws-secrets-manager' WHERE id='adp_1'`,
		`UPDATE adapter_targets SET destination_kind='json-object',destination_owner='123456789012',destination_name='DEST/app_mode',repository_id=0 WHERE id='tgt_1'`,
		`INSERT INTO adapters (id,org_id,project_id,provider,origin,authority_principal_id,state,created_at) VALUES ('adp_2','org_adapter','prj_adapter','aws-secrets-manager','https://pending.example','usr_adapter','active','2026-08-17T00:00:00Z')`,
		`INSERT INTO adapter_targets (id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at) VALUES ('tgt_2','org_adapter','prj_adapter','env_adapter','adp_2','json-object','123456789012','old-two',43,'',1,'active','never','2026-08-17T00:00:00Z')`,
		`INSERT INTO adapter_target_keys (org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES ('org_adapter','prj_adapter','env_adapter','tgt_2','adp_2','key_a')`,
	} {
		if _, err := db.SQLiteWrite().ExecContext(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
	target := store.AdapterTargetMutation{ID: "tgt_1", AdapterID: "adp_1", EnvironmentID: "env_adapter", DestinationKind: "json-object", DestinationOwner: "123456789012", DestinationName: "DEST/app_mode", KeyIDs: []string{"key_a"}}
	// Seed an already reserved route independently of the current route. A
	// resumed move must honor durable pending claims before activation.
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO adapter_route_moves (id,org_id,project_id,adapter_id,target_id,kind,authority_principal_id,state,keep_remote,created_at) VALUES ('move_first','org_adapter','prj_adapter','adp_1','tgt_1','target','usr_adapter','activating',1,'2026-08-17T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO adapter_route_move_targets (move_id,org_id,project_id,environment_id,target_id,destination_kind,destination_owner,destination_name,destination_environment,destination_id,repository_id,visibility,selected_repository_ids,name_prefix,orphaned_names) SELECT 'move_first',org_id,project_id,environment_id,id,destination_kind,destination_owner,destination_name,destination_environment,destination_id,repository_id,visibility,selected_repository_ids,name_prefix,'[]' FROM adapter_targets WHERE id='tgt_1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `INSERT INTO adapter_route_move_claims (move_id,org_id,project_id,environment_id,target_id,key_id,provider_origin,destination_kind,destination_owner,destination_name,destination_environment,surface,effective_name,normalized_name) VALUES ('move_first','org_adapter','prj_adapter','env_adapter','tgt_1',NULL,'https://pending.example','json-object','123456789012','DEST/app_mode','','secret','DEST/app_mode','DEST/APP_MODE')`); err != nil {
		t.Fatal(err)
	}
	target.ID = "tgt_2"
	target.AdapterID = "adp_2"
	target.DestinationKind = "per-key"
	target.DestinationName = "dest/"
	if err := moveProviderTarget(t, db, "move_collision", target); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("cross-kind pending overlap accepted: %v", err)
	}
	target.DestinationName = "other/"
	if err := moveProviderTarget(t, db, "move_distinct", target); err != nil {
		t.Fatalf("distinct pending name refused: %v", err)
	}
}
