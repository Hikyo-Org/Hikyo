package isolation

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// resumeFixture is a TOTP-enrolled, stepped-up administrator holding an open org
// incident, with a clock the test advances.
type resumeFixture struct {
	auth      *service.Auth
	incidents *service.Incidents
	actor     service.Actor
	token     string
	accountID string
	uri       string
	id        string
	base      time.Time
	clk       *time.Time
}

func newResumeFixture(t *testing.T, db *store.DB) resumeFixture {
	t.Helper()
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
	// A member of the org with a live session, so the incident has open items.
	execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('usr_resume_member', 'human', `+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_resume_member', 'usr_resume_member', 'read', 'org_a', `+ts+`)`)
	execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('acc_resume_member', 'usr_resume_member', 'resume-member', 'Resume Member', 'resume-member@example.test', `+ts+`, `+ts+`)`)
	execRaw(t, db, `INSERT INTO sessions (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent) `+
		`VALUES ('ses_resume_member', 'usr_resume_member', `+blobLit(db, []byte("verifier-resume-member"))+`, 'cli', 1, 0, 'local', '[]', `+ts+`, `+ts+`, `+ts+`, '2099-01-01T00:00:00.000000Z', '2099-01-01T00:00:00.000000Z', '', '')`)
	incidents := &service.Incidents{DB: db, Auth: auth, Adapters: &service.Adapters{DB: db}, Dynamic: &service.Dynamic{DB: db}, SCIM: &service.SCIM{DB: db}}
	opened, err := incidents.Open(ctx, actor, service.IncidentTarget{Kind: domain.IncidentScopeAccount, Principal: "usr_resume_member"})
	if err != nil {
		t.Fatal(err)
	}
	id := opened.Incident.ID
	return resumeFixture{auth: auth, incidents: incidents, actor: actor, token: stepped.SessionToken, accountID: accountID, uri: uri, id: id, base: base, clk: &clk}
}

func (f resumeFixture) openItems(t *testing.T, db *store.DB) int {
	t.Helper()
	return int(queryInt(t, db, `SELECT COUNT(*) FROM incident_items WHERE incident_id='`+f.id+`' AND outcome IN ('pending','unknown','blocked')`))
}

// Resume performs the containment the ledger enumerated, so it needs a fresh
// account-security proof of its own (ADR O2). Each refusal below leaves the
// incident untouched, and a one-time code is spent by one call.
func TestIncidentResumeNeedsAFreshProof(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newResumeFixture(t, db)
		ctx := t.Context()
		events := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM incident_items WHERE incident_id='`+f.id+`' AND outcome <> 'pending'`)
		}
		before := events()

		// No proof: refused, and nothing ran.
		if _, err := f.incidents.Resume(ctx, f.actor, f.id, "", nil); !errors.Is(err, service.ErrReauthProofRequired) {
			t.Fatalf("resume without a proof = %v, want the proof refusal", err)
		}
		// A wrong proof: refused as a failed proof, and nothing ran.
		if _, err := f.incidents.Resume(ctx, f.actor, f.id, "000000", nil); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("resume with a wrong proof = %v, want unauthenticated", err)
		}
		f.auth.Admission.RecordSuccess(f.accountID)
		if got := events(); got != before {
			t.Fatalf("a refused resume ran %d items", got-before)
		}
		// A caller reading a missing incident is told it does not exist, before any
		// proof is asked for.
		if _, err := f.incidents.Resume(ctx, f.actor, wireNoIncId, "", nil); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("resume on a missing incident without a proof = %v, want not found", err)
		}

		// The right proof runs the pass, and the same code does not run a second one.
		*f.clk = f.base.Add(120 * time.Second)
		code := totpCode(t, f.uri, *f.clk)
		if _, err := f.incidents.Resume(ctx, f.actor, f.id, code, nil); err != nil {
			t.Fatalf("resume with a fresh proof: %v", err)
		}
		if _, err := f.incidents.Resume(ctx, f.actor, f.id, code, nil); err == nil {
			t.Fatal("the same TOTP code resumed the incident twice")
		}
	})
}

// The operator confirms a listing of the open items. When the open set has since
// changed the call is a conflict that spends no proof, so the same code then runs
// the confirmed set.
func TestIncidentResumeRefusesAChangedOpenSetWithoutSpendingTheProof(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newResumeFixture(t, db)
		ctx := t.Context()
		open := f.openItems(t, db)
		if open == 0 {
			t.Fatalf("the fixture incident has no open items: %s", queryStrings(t, db, `SELECT owner_function || '|' || object_kind || '|' || outcome || ' ' FROM incident_items`))
		}
		*f.clk = f.base.Add(120 * time.Second)
		code := totpCode(t, f.uri, *f.clk)
		stale := open + 1
		if _, err := f.incidents.Resume(ctx, f.actor, f.id, code, &stale); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("resume of a changed open set = %v, want a conflict", err)
		}
		if got := f.openItems(t, db); got != open {
			t.Fatalf("a refused resume changed the open set: %d, want %d", got, open)
		}
		if _, err := f.incidents.Resume(ctx, f.actor, f.id, code, &open); err != nil {
			t.Fatalf("the unspent code did not run the confirmed set: %v", err)
		}
	})
}

// Over HTTP, a valid TOTP code and the confirmed count resume the incident; a
// stale count is a 409 that leaves the code unspent; a replayed code is refused.
func TestIncidentResumeOverHTTPWithAValidProof(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newResumeFixture(t, db)
		srv := httptest.NewTLSServer(server.New(&server.API{Auth: f.auth, Incidents: f.incidents}, nil))
		t.Cleanup(srv.Close)
		env := accessWireEnv{srv: srv}
		bearer := f.token
		path := api.PathPrefix + "/incidents/" + f.id + "/resume"
		open := f.openItems(t, db)
		*f.clk = f.base.Add(120 * time.Second)
		code := totpCode(t, f.uri, *f.clk)

		if status, body := env.callAs(t, bearer, http.MethodPost, path, map[string]any{"proof": code, "open_items": open + 1}); status != http.StatusConflict {
			t.Fatalf("resume with a stale count = %d %s, want 409", status, body)
		}
		status, body := env.callAs(t, bearer, http.MethodPost, path, map[string]any{"proof": code, "open_items": open})
		if status != http.StatusOK {
			t.Fatalf("resume with a valid code = %d %s, want 200", status, body)
		}
		if got := decodeWire[apigen.IncidentResumeResult](t, body); int(got.Examined+got.Skipped) != open {
			t.Fatalf("examined+skipped = %d, want %d", got.Examined+got.Skipped, open)
		}
		if status, body := env.callAs(t, bearer, http.MethodPost, path, map[string]any{"proof": code, "open_items": 0}); status == http.StatusOK {
			t.Fatalf("a replayed code resumed again: %d %s", status, body)
		}
	})
}
