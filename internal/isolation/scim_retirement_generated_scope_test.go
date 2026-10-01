package isolation

import (
	"context"
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"

	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

func TestSCIMGeneratedConnectionRetirementPreservesLiveOwners(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for _, principal := range []struct{ id, kind, class string }{
			{"mch_retire_own", "machine", "provisioning-connection"},
			{"mch_retire_foreign", "machine", "provisioning-connection"},
			{"mch_retire_service", "machine", "service"},
			{"usr_retire_human", "human", "provisioning-connection"},
		} {
			execRealAdoption(t, db, `INSERT INTO principals(id,kind,class,created_at) VALUES ($1,$2,$3,'2026-08-17T00:00:00Z')`, principal.id, principal.kind, principal.class)
		}
		for _, binding := range []struct{ id, org, principal string }{
			{"scb_retire_own", string(orgA), "mch_retire_own"},
			{"scb_retire_foreign", string(orgB), "mch_retire_foreign"},
		} {
			execRealAdoption(t, db, `INSERT INTO scim_bindings(id,org_id,provider_kind,provider_id,provider_slug,provider_issuer,subject_source,connection_principal_id,created_at) VALUES ($1,$2,'oidc',$1,$1,'https://idp.example','externalId',$3,'2026-08-17T00:00:00Z')`, binding.id, binding.org, binding.principal)
		}
		var retire func(string) (int64, error)
		if db.Engine() == store.EngineSQLite {
			q := sqlitegen.New(db.SQLiteWrite())
			retire = func(id string) (int64, error) { return q.RetireSCIMConnectionPrincipal(t.Context(), id) }
		} else {
			q := pggen.New(db.PG())
			retire = func(id string) (int64, error) { return q.RetireSCIMConnectionPrincipal(t.Context(), id) }
		}
		for _, id := range []string{"mch_retire_own", "mch_retire_foreign", "mch_retire_service", "usr_retire_human"} {
			if n, err := retire(id); err != nil || n != 0 {
				t.Fatalf("retire protected principal %s: rows=%d err=%v", id, n, err)
			}
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM principals WHERE id IN ('mch_retire_own','mch_retire_foreign','mch_retire_service','usr_retire_human')`); n != 4 {
			t.Fatalf("protected principal rows=%d want4", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM scim_bindings WHERE id IN ('scb_retire_own','scb_retire_foreign')`); n != 2 {
			t.Fatalf("retirement changed live bindings: %d", n)
		}
		// Production obtains this ID from its proof-scoped binding before deleting it.
		// Remove only that owning fixture row, then exercise the generated final cleanup.
		execRealAdoption(t, db, `DELETE FROM scim_bindings WHERE id='scb_retire_own' AND org_id=$1`, string(orgA))
		if n, err := retire("mch_retire_own"); err != nil || n != 1 {
			t.Fatalf("retire released own principal: rows=%d err=%v", n, err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM principals WHERE id='mch_retire_own'`); n != 0 {
			t.Fatalf("released principal still exists: %d", n)
		}
		if n, err := retire("mch_retire_foreign"); err != nil || n != 0 {
			t.Fatalf("foreign live owner after own teardown: rows=%d err=%v", n, err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM scim_bindings b JOIN principals p ON p.id=b.connection_principal_id WHERE b.id='scb_retire_foreign' AND p.id='mch_retire_foreign'`); n != 1 {
			t.Fatalf("foreign owner/principal changed: %d", n)
		}
		if n, err := retire("mch_retire_own"); err != nil || n != 0 {
			t.Fatalf("repeat retirement: rows=%d err=%v", n, err)
		}
	})
}

func TestSCIMRetirementUsesProofScopedBindingPrincipal(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		binding, _ := newSCIMBinding(t, db, "retirement")
		connection := queryString(t, db, `SELECT connection_principal_id FROM scim_bindings WHERE id='`+binding+`'`)
		// Give the same actor real teardown authority in the other org. A refusal
		// must come from the scoped binding lookup, not from a missing capability.
		execRealAdoption(t, db, `INSERT INTO grants(id,principal_id,capability,org_id,created_at) VALUES ('gr_retire_foreign',$1,'manage-members',$2,'2026-08-17T00:00:00Z')`, string(orgAdmin), string(orgB))
		err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
			proof, err := az.Authorize(ctx, authz.Identity{Principal: orgAdmin}, authz.OpSCIMBindingDelete, domain.Scope{Org: orgB})
			if err != nil {
				t.Fatalf("foreign org fixture lacks valid teardown authority: %v", err)
			}
			_, err = repos.SCIM().Binding(ctx, proof, binding)
			return err
		})
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("foreign proof binding lookup=%v want not found", err)
		}
		if err := scimSvc(db).DeleteBinding(t.Context(), service.LocalPrincipal(orgAdmin), orgB, binding); err == nil {
			t.Fatal("foreign scoped teardown succeeded")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM scim_bindings WHERE id='`+binding+`' AND connection_principal_id='`+connection+`'`); n != 1 {
			t.Fatalf("foreign teardown changed binding: %d", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM principals WHERE id='`+connection+`'`); n != 1 {
			t.Fatalf("foreign teardown retired principal: %d", n)
		}
		if err := scimSvc(db).DeleteBinding(t.Context(), service.LocalPrincipal(orgAdmin), orgA, binding); err != nil {
			t.Fatalf("owning teardown: %v", err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM scim_bindings WHERE id='`+binding+`'`); n != 0 {
			t.Fatalf("owning binding survived teardown: %d", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM principals WHERE id='`+connection+`'`); n != 0 {
			t.Fatalf("captured connection survived owning teardown: %d", n)
		}
	})
}
