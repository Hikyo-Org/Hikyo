package isolation

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

// countedAuthorizeOp is countedAuthorize for any operation and a full caller
// identity, so the personal owner fallback's refusal work can be compared.
func countedAuthorizeOp(t *testing.T, db *store.DB, caller authz.Identity, op authz.Operation, scope domain.Scope) (int, error) {
	t.Helper()
	ctx := t.Context()
	count := 0
	tok := authz.NewTxToken()
	defer tok.Invalidate()
	var r *authn.Resolver
	if db.Engine() == store.EnginePostgres {
		pgtx, err := db.PG().BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = pgtx.Rollback(ctx) }()
		var dbtx pggen.DBTX = countingPGTx{tx: pgtx, n: &count}
		r = authn.NewPG(dbtx)
	} else {
		sqtx, err := db.SQLiteRead().BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = sqtx.Rollback() }()
		var dbtx sqlitegen.DBTX = countingSqliteTx{tx: sqtx, n: &count}
		r = authn.NewSQLite(dbtx)
	}
	_, err := authz.NewTxAuthorizer(r, tok).Authorize(ctx, caller, op, scope)
	return count, err
}

// The owner lists masked and deletes their own personal values whatever their
// grants, while set and reveal still need read, reveal and qualification
// (personal-values ADR, Who can do what). The refusal for a missing chain does
// the same work as the refusal for an existing chain the caller cannot reach.
func TestPersonalOwnerListAndDeleteWhateverTheirGrants(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-owner")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		owner := e.admin.boot.PrincipalID
		human := authz.Identity{Principal: owner, Class: domain.ClassHuman}
		missing := domain.Scope{Org: orgA, Project: prjA1, Env: "env_missing"}

		// With the grants, the formula admits the owner: chain and grants only.
		if n, err := countedAuthorizeOp(t, db, human, authz.OpPersonalValueList, envScope(envA1)); err != nil || n != 2 {
			t.Fatalf("granted owner list: queries=%d err=%v; want 2 and a proof", n, err)
		}

		// The owner loses read and reveal; their values stay theirs to manage.
		execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id IN ('g_cer_personal-owner_read', 'g_cer_personal-owner_reveal')`)
		execRaw(t, db, `DELETE FROM grants WHERE id IN ('g_cer_personal-owner_read', 'g_cer_personal-owner_reveal')`)
		owned := service.Bearer(e.admin.token)
		if own, err := e.pv.List(ctx, owned, envScope(envA1)); err != nil || len(own) != 1 || !own[0].Set || own[0].Value != "" {
			t.Fatalf("grantless owner list: err=%v entries=%d", err, len(own))
		}
		if _, err := e.pv.Set(ctx, owned, envScope(envA1), ceremonySecretA, "replacement"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("grantless owner set = %v, want the nonexistent answer", err)
		}
		if _, err := e.pv.Reveal(ctx, owned, envScope(envA1), ceremonySecretA); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("grantless owner reveal = %v, want the nonexistent answer", err)
		}
		for _, op := range []authz.Operation{authz.OpPersonalValueList, authz.OpPersonalValueDelete} {
			if n, err := countedAuthorizeOp(t, db, human, op, envScope(envA1)); err != nil || n != 4 {
				t.Fatalf("%s holding a value: queries=%d err=%v; want 4 and a proof", op, n, err)
			}
			if n, err := countedAuthorizeOp(t, db, human, op, missing); !errors.Is(err, domain.ErrNotFound) || n != 4 {
				t.Fatalf("%s on a missing chain: queries=%d err=%v; want 4 and the nonexistent answer", op, n, err)
			}
		}
		// The fallback is for the owner's own human session only.
		for _, class := range []domain.PrincipalClass{domain.ClassWorkload, ""} {
			if _, err := countedAuthorizeOp(t, db, authz.Identity{Principal: owner, Class: class}, authz.OpPersonalValueList, envScope(envA1)); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("class %q reached the owner fallback: %v", class, err)
			}
		}
		if _, err := countedAuthorizeOp(t, db, human, authz.OpPersonalValueReveal, envScope(envA1)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("owner fallback reached reveal: %v", err)
		}

		// An administrator's removal leaves a notice, which still admits the
		// grantless owner to see and dismiss it.
		e.adminTok = personalReauth(t, e.admin.auth, e.adminTok, e.adminDev, e.keyA)
		if err := e.pv.DeleteMember(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, "personal-owner"); err != nil {
			t.Fatal(err)
		}
		if own, err := e.pv.List(ctx, owned, envScope(envA1)); err != nil || len(own) != 1 || own[0].Set || own[0].Notice != service.PersonalCauseAdministrator {
			t.Fatalf("grantless owner notice list: err=%v entries=%d", err, len(own))
		}
		if err := e.pv.Delete(ctx, owned, envScope(envA1), ceremonySecretA); err != nil {
			t.Fatalf("grantless owner dismissal: %v", err)
		}

		// Holding nothing, the grantless owner is refused, with the same work as
		// a missing chain.
		if _, err := e.pv.List(ctx, owned, envScope(envA1)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("grantless nonholder list = %v", err)
		}
		if n, err := countedAuthorizeOp(t, db, human, authz.OpPersonalValueList, envScope(envA1)); !errors.Is(err, domain.ErrNotFound) || n != 4 {
			t.Fatalf("existing denial: queries=%d err=%v; want 4 like the missing chain", n, err)
		}
	})
}
