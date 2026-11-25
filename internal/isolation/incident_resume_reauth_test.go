package isolation

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Resume performs the containment the ledger enumerated, so it needs a fresh
// account-security proof of its own (ADR O2). Each refusal below leaves the
// incident untouched, and the proof is spent by one call.
func TestIncidentResumeNeedsAFreshProof(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		administrator := bootstrapFactorAdmin(t, db)
		auth, accountID, password := administrator.auth, administrator.accountID, administrator.password
		principal := administrator.boot.PrincipalID
		ctx := t.Context()
		base := time.Now().UTC()
		clk := base
		auth.Now = func() time.Time { return clk }

		login, err := auth.LocalLogin(ctx, "factor-admin", password, service.ArtifactCLI)
		if err != nil {
			t.Fatal(err)
		}
		uri, err := auth.EnrolTOTPStart(ctx, login.SessionToken, password)
		if err != nil {
			t.Fatalf("enrol start: %v", err)
		}
		clk = base.Add(30 * time.Second)
		confirmed, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, clk))
		if err != nil {
			t.Fatalf("enrol confirm: %v", err)
		}
		clk = base.Add(60 * time.Second)
		stepped, err := auth.StepUpTOTP(ctx, confirmed.SessionToken, totpCode(t, uri, clk))
		if err != nil {
			t.Fatalf("step-up: %v", err)
		}
		actor := service.Bearer(stepped.SessionToken)

		if queryInt(t, db, `SELECT COUNT(*) FROM orgs WHERE id='org_a'`) == 0 {
			execRaw(t, db, `INSERT INTO orgs (id, name, active, metadata, created_at) VALUES ('org_a', 'org-a', TRUE, '{}', `+ts+`)`)
		}
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_resume_reauth', '`+string(principal)+`', 'credential-reset', 'org_a', `+ts+`)`)
		incidents := &service.Incidents{DB: db, Auth: auth, Adapters: &service.Adapters{DB: db}, Dynamic: &service.Dynamic{DB: db}, SCIM: &service.SCIM{DB: db}}
		opened, err := incidents.Open(ctx, actor, service.IncidentTarget{Kind: domain.IncidentScopeOrg, Scope: domain.Scope{Org: orgA}})
		if err != nil {
			t.Fatal(err)
		}
		id := opened.Incident.ID
		events := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM incident_items WHERE incident_id='`+id+`' AND outcome <> 'pending'`)
		}
		before := events()

		// No proof: refused, and nothing ran.
		if _, err := incidents.Resume(ctx, actor, id, ""); !errors.Is(err, service.ErrReauthProofRequired) {
			t.Fatalf("resume without a proof = %v, want the proof refusal", err)
		}
		// A wrong proof: refused as a failed proof, and nothing ran.
		if _, err := incidents.Resume(ctx, actor, id, "000000"); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("resume with a wrong proof = %v, want unauthenticated", err)
		}
		auth.Admission.RecordSuccess(accountID)
		if got := events(); got != before {
			t.Fatalf("a refused resume ran %d items", got-before)
		}
		// A caller reading a missing incident is told it does not exist, before any
		// proof is asked for.
		if _, err := incidents.Resume(ctx, actor, wireNoIncId, ""); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("resume on a missing incident without a proof = %v, want not found", err)
		}

		// The right proof runs the pass, and the same code does not run a second one.
		clk = base.Add(120 * time.Second)
		code := totpCode(t, uri, clk)
		if _, err := incidents.Resume(ctx, actor, id, code); err != nil {
			t.Fatalf("resume with a fresh proof: %v", err)
		}
		if _, err := incidents.Resume(ctx, actor, id, code); err == nil {
			t.Fatal("the same TOTP code resumed the incident twice")
		}
	})
}
