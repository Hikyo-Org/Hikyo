package isolation

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestAdapterMoveGeneratedQueriesRejectForeignChain(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		seedGitLabMoves(t, db, "staging")
		for _, statement := range []string{
			`INSERT INTO orgs(id,name,active,metadata,created_at) VALUES ('org_move_foreign','Foreign',TRUE,'{}','2026-08-17T00:00:00Z')`,
			`INSERT INTO projects(id,org_id,name,created_at) VALUES ('prj_move_foreign','org_move_foreign','Foreign','2026-08-17T00:00:00Z')`,
			`INSERT INTO projects(id,org_id,name,created_at) VALUES ('prj_move_sibling','org_gitlab','Sibling','2026-08-17T00:00:00Z')`,
			`INSERT INTO grants(id,principal_id,capability,org_id,project_id,created_at) VALUES ('gr_move_foreign','usr_gitlab','manage-adapters','org_move_foreign','prj_move_foreign','2026-08-17T00:00:00Z')`,
			`INSERT INTO grants(id,principal_id,capability,org_id,project_id,created_at) VALUES ('gr_move_sibling','usr_gitlab','manage-adapters','org_gitlab','prj_move_sibling','2026-08-17T00:00:00Z')`,
		} {
			execRealAdoption(t, db, statement)
		}
		pending := gitLabPendingTarget("tgt_gitlab_a", "production", "new-api")
		activeTarget := gitLabPendingTarget("tgt_gitlab_b", "staging", "new-api-b")
		activeTarget.EnvironmentID, activeTarget.VariableProtected = "env_gitlab_second", false
		if err := gitLabMoveWrite(t, db, func(ctx context.Context, repos store.Repos, proof authz.Proof) error {
			_, err := repos.Adapters().MoveTarget(ctx, proof, store.AdapterRouteMoveMutation{MoveID: "arm_scope_guard", Target: pending, ExpectedGeneration: 1, AuthorityPrincipalID: "usr_gitlab", KeepRemote: true, At: time.Now().UTC()})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		gitLabAttention(t, db, "arm_scope_guard")
		read := func() store.AdapterMove {
			var move store.AdapterMove
			if err := gitLabMoveWrite(t, db, func(ctx context.Context, repos store.Repos, proof authz.Proof) error {
				var err error
				move, err = repos.Adapters().Move(ctx, proof, "arm_scope_guard")
				return err
			}); err != nil {
				t.Fatal(err)
			}
			return move
		}
		before := read()
		claimsBefore := queryInt(t, db, "SELECT COUNT(*) FROM adapter_route_move_claims WHERE move_id='arm_scope_guard'")
		jobsBefore := queryInt(t, db, "SELECT COUNT(*) FROM adapter_outbox WHERE route_move_id='arm_scope_guard'")
		for _, scope := range []domain.Scope{{Org: "org_gitlab", Project: "prj_move_sibling"}, {Org: "org_move_foreign", Project: "prj_move_foreign"}} {
			for _, action := range []string{"read", "cancel", "replace-target", "begin-target", "begin-origin"} {
				err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
					proof, err := az.Authorize(ctx, authz.Identity{Principal: "usr_gitlab"}, authz.OpAdapterConfigure, scope)
					if err != nil {
						t.Fatalf("foreign scope fixture lacks valid authority: %v", err)
					}
					switch action {
					case "read":
						_, err = repos.Adapters().Move(ctx, proof, "arm_scope_guard")
					case "cancel":
						_, err = repos.Adapters().CancelMove(ctx, proof, "arm_scope_guard", "usr_gitlab", time.Now().UTC())
					case "replace-target":
						_, err = repos.Adapters().ReplaceMoveTarget(ctx, proof, "arm_scope_guard", pending, "usr_gitlab", time.Now().UTC())
					case "begin-target":
						_, err = repos.Adapters().MoveTarget(ctx, proof, store.AdapterRouteMoveMutation{MoveID: "arm_foreign_target", Target: activeTarget, ExpectedGeneration: 1, AuthorityPrincipalID: "usr_gitlab", KeepRemote: true, At: time.Now().UTC()})
					case "begin-origin":
						_, err = repos.Adapters().MoveOrigin(ctx, proof, store.AdapterOriginMoveMutation{MoveID: "arm_foreign_origin", AdapterID: "adp_gitlab", Origin: "https://foreign.example", PendingCredentialCiphertext: []byte("sealed-fixture"), AuthorityPrincipalID: "usr_gitlab", KeepRemote: true, At: time.Now().UTC()})
					}
					return err
				})
				if !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("%s with foreign chain %+v: %v, want not found", action, scope, err)
				}
			}
		}
		if after := read(); !reflect.DeepEqual(before, after) {
			t.Fatalf("foreign chain changed pending move: before=%+v after=%+v", before, after)
		}
		if claims := queryInt(t, db, "SELECT COUNT(*) FROM adapter_route_move_claims WHERE move_id='arm_scope_guard'"); claims != claimsBefore {
			t.Fatalf("foreign chain changed pending claims: %d -> %d", claimsBefore, claims)
		}
		if jobs := queryInt(t, db, "SELECT COUNT(*) FROM adapter_outbox WHERE route_move_id='arm_scope_guard'"); jobs != jobsBefore {
			t.Fatalf("foreign chain changed queued jobs: %d -> %d", jobsBefore, jobs)
		}
	})
}
