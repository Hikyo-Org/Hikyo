package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func seedGitLabMoves(t *testing.T, db *store.DB, secondScope string) {
	t.Helper()
	seedGitLabDB(t, db, gitLabE2E{origin: "https://gitlab.old.example"}, adapter.Target{ID: "tgt_gitlab_a", Environment: "env_gitlab_e2e", NamePrefix: "P_", Destination: adapter.Destination{Owner: "team", Name: "api-a", Scope: "production", NumericID: 42}})
	execRealAdoption(t, db, `INSERT INTO environments(id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_gitlab_second','org_gitlab','prj_gitlab','second','','2026-08-17T00:00:00Z',1)`)
	execRealAdoption(t, db, `UPDATE adapters SET credential_ciphertext=$1 WHERE id='adp_gitlab'`, []byte("sealed-fixture"))
	for _, q := range []string{
		`INSERT INTO grants(id,principal_id,capability,org_id,project_id,created_at) VALUES ('gr_gitlab_moves','usr_gitlab','manage-adapters','org_gitlab','prj_gitlab','2026-08-17T00:00:00Z')`,
		`INSERT INTO grants(id,principal_id,capability,org_id,project_id,created_at) VALUES ('gr_gitlab_reveal','usr_gitlab','reveal','org_gitlab','prj_gitlab','2026-08-17T00:00:00Z')`,
		`INSERT INTO keys(id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES ('key_gitlab_move','org_gitlab','prj_gitlab','TOKEN','','secret','',FALSE,'','optional','none','none','2026-08-17T00:00:00Z')`,
		`INSERT INTO adapter_target_keys(org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES ('org_gitlab','prj_gitlab','env_gitlab_e2e','tgt_gitlab_a','adp_gitlab','key_gitlab_move')`,
	} {
		execRealAdoption(t, db, q)
	}
	execRealAdoption(t, db, `INSERT INTO adapter_targets(id,org_id,project_id,environment_id,adapter_id,destination_kind,destination_owner,destination_name,destination_id,name_prefix,generation,state,sync_status,created_at,destination_scope) VALUES ('tgt_gitlab_b','org_gitlab','prj_gitlab','env_gitlab_second','adp_gitlab','repository','team','api-b',43,'P_',1,'active','never','2026-08-17T00:00:00Z',$1)`, secondScope)
	execRealAdoption(t, db, `INSERT INTO adapter_target_keys(org_id,project_id,environment_id,target_id,adapter_id,key_id) VALUES ('org_gitlab','prj_gitlab','env_gitlab_second','tgt_gitlab_b','adp_gitlab','key_gitlab_move')`)
}
func gitLabMoveWrite(t *testing.T, db *store.DB, fn func(context.Context, store.Repos, authz.Proof) error) error {
	t.Helper()
	return storetx.Write(t.Context(), db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.Authorize(ctx, authz.Identity{Principal: "usr_gitlab"}, authz.OpAdapterConfigure, domain.Scope{Org: "org_gitlab", Project: "prj_gitlab"})
		if err != nil {
			return err
		}
		return fn(ctx, r, p)
	})
}
func gitLabPendingTarget(id, scope, name string) store.AdapterTargetMutation {
	return store.AdapterTargetMutation{ID: id, AdapterID: "adp_gitlab", EnvironmentID: "env_gitlab_e2e", DestinationKind: "repository", DestinationOwner: "team", DestinationName: name, DestinationScope: scope, NamePrefix: "P_", KeyIDs: []string{"key_gitlab_move"}}
}
func gitLabAttention(t *testing.T, db *store.DB, move string) {
	t.Helper()
	execRealAdoption(t, db, `UPDATE adapter_outbox SET state='superseded' WHERE route_move_id=$1`, move)
	execRealAdoption(t, db, `UPDATE adapter_targets SET active_job_id=NULL,provider_lease_job_id=NULL,provider_lease_expires_at=NULL WHERE id IN (SELECT target_id FROM adapter_route_move_targets WHERE move_id=$1)`, move)
	execRealAdoption(t, db, `UPDATE adapter_route_moves SET state='attention_required' WHERE id=$1`, move)
}
func assertGitLabMoveScopes(t *testing.T, move store.AdapterMove, want map[string]string) {
	t.Helper()
	if len(move.Targets) != len(want) {
		t.Fatalf("targets=%d want%d", len(move.Targets), len(want))
	}
	for _, target := range move.Targets {
		if target.DestinationScope != want[target.TargetID] {
			t.Fatalf("target%s scope=%q want%q", target.TargetID, target.DestinationScope, want[target.TargetID])
		}
	}
}
func TestGitLabOriginMoveScopesSurviveResume(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		// Same GitLab destination and prefix, separated only by scope.
		execRealAdoption(t, db, `UPDATE adapter_targets SET destination_name='api-a',destination_id=42 WHERE id='tgt_gitlab_b'`)
		err := gitLabMoveWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
			_, err := r.Adapters().MoveOrigin(ctx, p, store.AdapterOriginMoveMutation{MoveID: "arm_scopes", AdapterID: "adp_gitlab", Origin: "https://gitlab.new.example", PendingCredentialCiphertext: []byte("sealed-fixture"), AuthorityPrincipalID: "usr_gitlab", KeepRemote: true, At: time.Now().UTC()})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"tgt_gitlab_a": "production", "tgt_gitlab_b": "staging"}
		if err := gitLabMoveWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
			m, err := r.Adapters().Move(ctx, p, "arm_scopes")
			if err == nil {
				assertGitLabMoveScopes(t, m, want)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		gitLabAttention(t, db, "arm_scopes")
		if err := gitLabMoveWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
			m, err := r.Adapters().ReplaceMoveOrigin(ctx, p, "arm_scopes", "https://gitlab.retry.example", []byte("sealed-replacement"), "usr_gitlab", time.Now().UTC())
			if err == nil {
				assertGitLabMoveScopes(t, m, want)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	})
}
func TestGitLabTargetMovesScopeClaimsAndResume(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		move := func(id, scope, moveID string) error {
			return gitLabMoveWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
				_, err := r.Adapters().MoveTarget(ctx, p, store.AdapterRouteMoveMutation{MoveID: moveID, Target: gitLabPendingTarget(id, scope, "new-api"), ExpectedGeneration: 1, AuthorityPrincipalID: "usr_gitlab", KeepRemote: true, At: time.Now().UTC()})
				return err
			})
		}
		if err := move("tgt_gitlab_a", "staging", "arm_wrong_scope"); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("initial move changed scope: %v", err)
		}
		if err := move("tgt_gitlab_a", "production", "arm_a"); err != nil {
			t.Fatal(err)
		}
		add := func(scope string) error {
			return gitLabMoveWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
				target := gitLabPendingTarget("tgt_gitlab_c", scope, "new-api")
				target.EnvironmentID = "env_gitlab_second"
				target.DestinationID = 99
				_, err := r.Adapters().AddTarget(ctx, p, store.AdapterTargetUpdate{Target: target, AuthorityPrincipalID: "usr_gitlab", At: time.Now().UTC()})
				return err
			})
		}
		if err := add("production"); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("same pending scope allowed: %v", err)
		}
		if err := add("staging"); err != nil {
			t.Fatalf("distinct pending scope refused: %v", err)
		}
		gitLabAttention(t, db, "arm_a")
		replace := func(scope string) error {
			m, err := (&service.Adapters{DB: db}).ResumeTargetMove(t.Context(), service.LocalPrincipal("usr_gitlab"), domain.Scope{Org: "org_gitlab", Project: "prj_gitlab"}, "arm_a", service.UpdateAdapterTargetRequest{
				TargetID: "tgt_gitlab_a", Target: service.AdapterTargetInput{EnvironmentID: "env_gitlab_e2e", DestinationKind: "repository", DestinationOwner: "team", DestinationName: "retry-api", DestinationScope: scope, NamePrefix: "P_", KeyIDs: []string{"key_gitlab_move"}},
			})
			if err == nil {
				assertGitLabMoveScopes(t, m, map[string]string{"tgt_gitlab_a": "production"})
			}
			return err
		}
		if err := replace("staging"); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("scope mutation accepted: %v", err)
		}
		// Older clients omit scope; service must retain the immutable pending scope.
		if err := replace(""); err != nil {
			t.Fatalf("resume lost scope: %v", err)
		}
	})
}
func TestGitLabConfiguredMoveScopeConflicts(t *testing.T) {
	for _, scope := range []string{"production", "staging"} {
		t.Run(scope, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				seedGitLabMoves(t, db, scope)
				err := gitLabMoveWrite(t, db, func(ctx context.Context, r store.Repos, p authz.Proof) error {
					_, err := r.Adapters().MoveTarget(ctx, p, store.AdapterRouteMoveMutation{MoveID: "arm_configured", Target: gitLabPendingTarget("tgt_gitlab_a", "production", "api-b"), ExpectedGeneration: 1, AuthorityPrincipalID: "usr_gitlab", KeepRemote: true, At: time.Now().UTC()})
					return err
				})
				if scope == "production" && !errors.Is(err, domain.ErrConflict) {
					t.Fatalf("configured same-scope collision: %v", err)
				}
				if scope == "staging" && err != nil {
					t.Fatalf("different configured scope refused: %v", err)
				}
			})
		})
	}
}
