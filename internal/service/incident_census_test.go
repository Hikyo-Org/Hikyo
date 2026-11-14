package service

import (
	"context"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// The account census counts what is stored, not what the gated authorization
// reader would answer: a contained or privacy-restricted target is exactly when
// the count matters, and that reader answers none for it.
func TestIncidentCensusCountsRulesOfAGatedTarget(t *testing.T) {
	db := adapterServiceDB(t)
	for _, stmt := range []string{
		`INSERT INTO principals (id,kind,created_at) VALUES ('usr_census','human','2026-08-17T00:00:00Z')`,
		`INSERT INTO grants (id,principal_id,capability,org_id,created_at) VALUES ('gr_census','usr_census','read','org_adapter','2026-08-17T00:00:00Z')`,
		`INSERT INTO rules (id,principal_id,capability,org_id,env_mode,key_mode,created_by,created_at) VALUES ('rul_census','usr_census','read','org_adapter','all','all','usr_census','2026-08-17T00:00:00Z')`,
		`INSERT INTO rule_items (id,rule_id,org_id,axis,project_id) VALUES ('rli_census','rul_census','org_adapter','project','prj_adapter')`,
	} {
		if _, err := db.SQLiteWrite().ExecContext(t.Context(), stmt); err != nil {
			t.Fatal(err)
		}
	}
	s := &Incidents{DB: db}
	census := func() IncidentCensus {
		t.Helper()
		var got IncidentCensus
		err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			c := newCollector(0)
			a := incidentAccess{kind: domain.IncidentScopeAccount, scopeID: "usr_census", footprint: []domain.OrgID{"org_adapter"}}
			if err := s.collectAccount(ctx, az, a, s.now(), c); err != nil {
				return err
			}
			got = c.census
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := census(); got.Rules != 1 || got.Grants != 1 {
		t.Fatalf("census of an active target = %+v, want 1 grant and 1 rule", got)
	}
	if _, err := db.SQLiteWrite().ExecContext(t.Context(), `UPDATE principals SET privacy_state = 'restricted', containment_state = 'contained' WHERE id = 'usr_census'`); err != nil {
		t.Fatal(err)
	}
	if got := census(); got.Rules != 1 || got.Grants != 1 {
		t.Fatalf("census of a restricted, contained target = %+v, want the same 1 grant and 1 rule", got)
	}
}
