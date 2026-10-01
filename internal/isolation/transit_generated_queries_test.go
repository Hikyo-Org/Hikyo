package isolation

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Exercise the generated variable-list binding through real proof verification,
// including authorized scopes that must not match the addressed key.
func TestTransitGeneratedStateListScopeAndCAS(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		svc, _ := transitEnv(t, db, "generated")
		key := mustTransitKey(t, svc, service.CreateTransitKeyRequest{Name: "generated-list", Algorithm: "xchacha20-poly1305"})
		now := time.Now().UTC().Truncate(time.Microsecond)
		reachedRepository := false
		change := func(scope domain.Scope, from []string, to string) error {
			reachedRepository = false
			return storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
				proof, err := az.Authorize(ctx, authz.Identity{Principal: alice}, authz.OpTransitKeyLifecycle, scope)
				if err != nil {
					return err
				}
				reachedRepository = true
				return repos.Transit().ChangeState(ctx, proof, store.TransitStateChange{KeyID: key.ID, From: from, To: to, At: now})
			})
		}
		for index, outside := range []domain.Scope{{Org: orgA, Project: prjA1, Env: envA2}, {Org: orgA, Project: prjA2, Env: envA2}, {Org: orgB, Project: prjB1, Env: envB1}} {
			// The first chain is forged while the latter two are real authorized tenants.
			if index > 0 {
				execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_tr_generated_%d','usr_alice','crypto-manage','%s','%s',NULL,%s)`, index, outside.Org, outside.Project, ts))
			}
			before := queryString(t, db, "SELECT state||'|'||CAST(updated_at AS TEXT) FROM transit_keys WHERE id='"+key.ID+"'")
			err := change(outside, []string{"active", "disabled"}, "disabled")
			if err == nil {
				t.Fatalf("state write escaped scope %+v", outside)
			}
			if index > 0 && (!reachedRepository || !errors.Is(err, store.ErrNotFound)) {
				t.Fatalf("authorized foreign scope did not reach scoped SQL refusal: %v reached=%v", err, reachedRepository)
			}
			if index == 0 && reachedRepository {
				t.Fatal("invalid owner chain unexpectedly reached repository")
			}
			if after := queryString(t, db, "SELECT state||'|'||CAST(updated_at AS TEXT) FROM transit_keys WHERE id='"+key.ID+"'"); after != before {
				t.Fatalf("foreign state write changed state/time: %s -> %s", before, after)
			}
		}
		if err := change(transitScope, nil, "disabled"); err == nil {
			t.Fatalf("empty from list: %v", err)
		}
		if err := change(transitScope, []string{"disabled", "active", "active"}, "disabled"); err != nil {
			t.Fatalf("multi-state list: %v", err)
		}
		if err := change(transitScope, []string{"active"}, "active"); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("stale state CAS: %v", err)
		}
		if err := change(transitScope, []string{"disabled"}, "destroyed"); err == nil {
			t.Fatalf("direct destruction: %v", err)
		}
		view, err := svc.GetKey(t.Context(), service.LocalPrincipal(alice), transitScope, key.Name)
		if err != nil || view.State != "disabled" {
			t.Fatalf("retained scoped state: %+v %v", view, err)
		}
		if err := change(transitScope, []string{"disabled"}, "active"); err != nil {
			t.Fatalf("single-state list: %v", err)
		}
	})
}
