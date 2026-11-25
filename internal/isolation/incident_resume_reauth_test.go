package isolation

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

// openDigest is the digest an operator would send for the incident's open items now.
func (f resumeFixture) openDigest(t *testing.T, db *store.DB) string {
	t.Helper()
	ids := strings.Fields(queryStrings(t, db, `SELECT id || ' ' FROM incident_items WHERE incident_id='`+f.id+`' AND outcome IN ('pending','unknown','blocked')`))
	return api.IncidentItemsDigest(ids)
}

// swapOpenSet settles one open item and reopens one out-of-scope one, so the open
// set changes while its size does not.
func (f resumeFixture) swapOpenSet(t *testing.T, db *store.DB) {
	t.Helper()
	execRaw(t, db, `UPDATE incident_items SET outcome='done', reason='' WHERE id=(SELECT MIN(id) FROM incident_items WHERE incident_id='`+f.id+`' AND outcome IN ('pending','unknown','blocked'))`)
	execRaw(t, db, `UPDATE incident_items SET outcome='pending', reason='' WHERE id=(SELECT MIN(id) FROM incident_items WHERE incident_id='`+f.id+`' AND outcome='out_of_scope')`)
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
		confirmed := f.openDigest(t, db)
		// Same size, different members: one item settled and another reopened.
		f.swapOpenSet(t, db)
		if got := f.openItems(t, db); got != open {
			t.Fatalf("the swap changed the open count: %d, want %d", got, open)
		}
		if _, err := f.incidents.Resume(ctx, f.actor, f.id, code, &confirmed); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("resume of a swapped open set = %v, want a conflict", err)
		}
		if got := f.openItems(t, db); got != open {
			t.Fatalf("a refused resume changed the open set: %d, want %d", got, open)
		}
		// The same unspent code runs the set the operator lists again.
		again := f.openDigest(t, db)
		if _, err := f.incidents.Resume(ctx, f.actor, f.id, code, &again); err != nil {
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

		if status, body := env.callAs(t, bearer, http.MethodPost, path, map[string]any{"proof": code, "items_digest": api.IncidentItemsDigest(nil)}); status != http.StatusConflict {
			t.Fatalf("resume with the wrong set = %d %s, want 409", status, body)
		}
		// A digest below and one above the real one are both refused, so the comparison is equality.
		if status, body := env.callAs(t, bearer, http.MethodPost, path, map[string]any{"proof": code, "items_digest": strings.Repeat("f", 64)}); status != http.StatusConflict {
			t.Fatalf("resume with a larger digest = %d %s, want 409", status, body)
		}
		if status, body := env.callAs(t, bearer, http.MethodPost, path, map[string]any{"proof": code, "items_digest": strings.Repeat("0", 64)}); status != http.StatusConflict {
			t.Fatalf("resume with a smaller digest = %d %s, want 409", status, body)
		}
		status, body := env.callAs(t, bearer, http.MethodPost, path, map[string]any{"proof": code, "items_digest": f.openDigest(t, db)})
		if status != http.StatusOK {
			t.Fatalf("resume with a valid code = %d %s, want 200", status, body)
		}
		if got := decodeWire[apigen.IncidentResumeResult](t, body); int(got.Examined+got.Skipped) != open {
			t.Fatalf("examined+skipped = %d, want %d", got.Examined+got.Skipped, open)
		}
		if status, body := env.callAs(t, bearer, http.MethodPost, path, map[string]any{"proof": code, "items_digest": strings.Repeat("0", 64)}); status == http.StatusOK {
			t.Fatalf("a replayed code resumed again: %d %s", status, body)
		}
	})
}

// A responder with neither a password nor a confirmed authenticator (a
// federated-only account) cannot resume: Resume is refused with
// ErrNoProofCredential and a stable wire detail, and runs nothing, spends nothing
// and leaves the incident open. There is no proof-less path (#1590 tracks step-up).
func TestIncidentResumeRefusesAResponderWithNoProofCredential(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newResumeFixture(t, db)
		execRaw(t, db, `DELETE FROM totp_credentials WHERE account_id='`+f.accountID+`'`)
		execRaw(t, db, `DELETE FROM password_credentials WHERE account_id='`+f.accountID+`'`)
		open := f.openItems(t, db)
		for _, proof := range []string{"", "anything", totpCode(t, f.uri, *f.clk)} {
			_, err := f.incidents.Resume(t.Context(), f.actor, f.id, proof, nil)
			if !errors.Is(err, service.ErrNoProofCredential) {
				t.Fatalf("resume of an account with no proof credential (proof %q) = %v, want ErrNoProofCredential", proof, err)
			}
			var sd interface{ SafeDetail() string }
			if !errors.As(err, &sd) || sd.SafeDetail() != "no_proof_credential" {
				t.Fatalf("the refusal carries no stable detail: %v", err)
			}
		}
		if got := f.openItems(t, db); got != open {
			t.Errorf("a refused resume changed the open items: %d, want %d", got, open)
		}
		if got := queryString(t, db, `SELECT status FROM incidents WHERE id='`+f.id+`'`); got != string(domain.IncidentOpen) {
			t.Errorf("incident status = %q, want open", got)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM incident_items WHERE incident_id='`+f.id+`' AND outcome='done'`); n != 0 {
			t.Errorf("%d items ran", n)
		}
		// The shared check keeps its order for every other caller: an empty proof is
		// "proof required" before any credential is looked up, even for this account.
		if _, err := f.auth.VerifyReauthProof(t.Context(), f.token, ""); !errors.Is(err, service.ErrReauthProofRequired) {
			t.Errorf("VerifyReauthProof(session, \"\") on an account with no proof credential = %v, want ErrReauthProofRequired", err)
		}

		// Over HTTP the same refusal is a 400 bad_request whose detail names it, which
		// is what the web dialog keys on.
		srv := httptest.NewTLSServer(server.New(&server.API{Auth: f.auth, Incidents: f.incidents}, nil))
		t.Cleanup(srv.Close)
		env := accessWireEnv{srv: srv}
		path := api.PathPrefix + "/incidents/" + f.id + "/resume"
		status, body := env.callAs(t, f.token, http.MethodPost, path, map[string]any{"proof": "anything", "items_digest": f.openDigest(t, db)})
		if status != http.StatusBadRequest {
			t.Fatalf("resume without a proof credential over HTTP = %d %s, want 400", status, body)
		}
		refusal := decodeWire[apigen.Error](t, body)
		if refusal.Error.Code != apigen.ErrorCodeBadRequest || refusal.Error.Detail == nil || *refusal.Error.Detail != "no_proof_credential" {
			t.Fatalf("the 400 body = %s, want code bad_request and detail no_proof_credential", body)
		}
	})
}

// An empty confirmed digest is a value, not "no digest": Resume called in
// process with one is refused as a changed open set before anything is spent, and
// the same unspent code then runs the real set.
func TestIncidentResumeRefusesAnEmptyDigestBeforeSpendingAnything(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newResumeFixture(t, db)
		open := f.openItems(t, db)
		*f.clk = f.base.Add(120 * time.Second)
		code := totpCode(t, f.uri, *f.clk)
		empty := ""
		if _, err := f.incidents.Resume(t.Context(), f.actor, f.id, code, &empty); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("resume with an empty digest = %v, want a conflict", err)
		}
		if got := f.openItems(t, db); got != open {
			t.Fatalf("a refused resume changed the open items: %d, want %d", got, open)
		}
		digest := f.openDigest(t, db)
		if _, err := f.incidents.Resume(t.Context(), f.actor, f.id, code, &digest); err != nil {
			t.Fatalf("the code spent by an empty-digest refusal: %v", err)
		}
	})
}

// The open set is read in full, page by page: with more open items than one page,
// the digest of the first page is a changed set (a conflict that runs nothing) and
// the digest of all of them runs every item.
func TestIncidentResumeReadsEveryPageOfOpenItems(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newResumeFixture(t, db)
		execRaw(t, db, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 250) `+
			`INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, created_at, updated_at) `+
			`SELECT 'itm_page_' || i, '`+f.id+`', 'session.revoke', 'session', 'ses_page_' || i, 'instance', 'pending', '', `+ts+`, `+ts+` FROM n`)
		open := f.openItems(t, db)
		if open <= 250 {
			t.Fatalf("the fixture has %d open items, want more than 250", open)
		}
		*f.clk = f.base.Add(120 * time.Second)
		code := totpCode(t, f.uri, *f.clk)
		firstPage := api.IncidentItemsDigest(strings.Fields(queryStrings(t, db, `SELECT id || ' ' FROM (SELECT id FROM incident_items WHERE incident_id='`+f.id+`' AND outcome IN ('pending','unknown','blocked') ORDER BY id LIMIT 100) p`)))
		if _, err := f.incidents.Resume(t.Context(), f.actor, f.id, code, &firstPage); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("resume confirming only the first page = %v, want a conflict", err)
		}
		all := f.openDigest(t, db)
		res, err := f.incidents.Resume(t.Context(), f.actor, f.id, code, &all)
		if err != nil {
			t.Fatalf("resume of every page: %v", err)
		}
		if res.Examined+res.Skipped != open {
			t.Fatalf("resume visited %d items, want all %d", res.Examined+res.Skipped, open)
		}
	})
}

// Resume runs the snapshot it checked, not the open set as it reads later: an
// item that becomes open after the digest check does not run in this call.
func TestIncidentResumeRunsOnlyTheCheckedSnapshot(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newResumeFixture(t, db)
		open := f.openItems(t, db)
		*f.clk = f.base.Add(120 * time.Second)
		code := totpCode(t, f.uri, *f.clk)
		digest := f.openDigest(t, db)
		f.incidents.AfterSnapshot = func() {
			execRaw(t, db, `INSERT INTO incident_items (id, incident_id, owner_function, object_kind, object_id, scope, outcome, reason, created_at, updated_at) VALUES ('itm_zz_late', '`+f.id+`', 'session.revoke', 'session', 'ses_zz_late', 'instance', 'pending', '', `+ts+`, `+ts+`)`)
		}
		res, err := f.incidents.Resume(t.Context(), f.actor, f.id, code, &digest)
		if err != nil {
			t.Fatal(err)
		}
		if res.Examined+res.Skipped != open {
			t.Fatalf("resume visited %d items, want the %d it checked", res.Examined+res.Skipped, open)
		}
		if got := queryString(t, db, `SELECT outcome || '/' || reason FROM incident_items WHERE id='itm_zz_late'`); got != "pending/" {
			t.Fatalf("the item added after the check = %q, want it untouched", got)
		}
	})
}
