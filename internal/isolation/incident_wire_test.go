package isolation

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// The incident routes (#1039 S4) carry x-hikyo-class: unauthenticated, the
// credential-reset precedent, because their authority is dynamic: an account
// target needs credential-reset at its single org or at the instance, which only
// the service can resolve from the target. That class leaves them out of the
// tenant probe harness, so these tests are their probe and their route tests: the
// real router over the real services, both engines.

// The wire validates ids as prefixed UUIDs, so the principals these tests name in
// a request body cannot be the harness's short fixture ids.
const (
	wireResponder = domain.PrincipalID("usr_00000000-0000-7000-8000-0000000000a1")
	wireForeign   = domain.PrincipalID("usr_00000000-0000-7000-8000-0000000000a2")
	wireNobody    = domain.PrincipalID("usr_00000000-0000-7000-8000-0000000000a3")
	wireMember    = domain.PrincipalID("usr_00000000-0000-7000-8000-0000000000a4")
	wireTarget    = domain.PrincipalID("usr_00000000-0000-7000-8000-0000000000b1")
	wireGhost     = domain.PrincipalID("usr_00000000-0000-7000-8000-0000000000b2")
	wireNoIncId   = "inc_00000000-0000-7000-8000-000000000000"
)

type incidentWire struct {
	w   *incidentWorld
	srv *httptest.Server
	env accessWireEnv
}

func newIncidentWire(t *testing.T, db *store.DB) incidentWire {
	t.Helper()
	w := newIncidentWorld(t, db)
	for _, p := range []domain.PrincipalID{wireResponder, wireForeign, wireNobody, wireMember, wireTarget} {
		execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('`+string(p)+`', 'human', `+ts+`)`)
	}
	for i, g := range []struct {
		p        domain.PrincipalID
		cap, org string
	}{
		{wireResponder, "credential-reset", "org_a"}, {wireResponder, "manage-members", "org_a"},
		{wireResponder, "manage-identities", "org_a"}, {wireResponder, "manage-adapters", "org_a"},
		{wireResponder, "read", "org_a"}, {wireForeign, "credential-reset", "org_b"}, {wireMember, "edit", "org_a"}, {wireTarget, "read", "org_a"},
	} {
		execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_wire_%02d', '%s', '%s', '%s', %s)`, i, g.p, g.cap, g.org, ts))
	}
	execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('acc_wire_t', '`+string(wireTarget)+`', 'wire-target', 'Wire Target', 'wire-target@example.test', `+ts+`, `+ts+`)`)
	execRaw(t, db, `INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('xid_wire','acc_wire_t','oidc','https://idp.example.test','subject-wire','',`+acctEpoch(t, db)+`,`+ts+`)`)
	execRaw(t, db, `INSERT INTO sessions (id, principal_id, verifier, artifact, session_generation, credential_epoch, auth_method, factors, authenticated_at, created_at, last_seen_at, idle_expires_at, absolute_expires_at, source_ip, user_agent) `+
		`VALUES ('ses_wire_t', '`+string(wireTarget)+`', `+blobLit(db, []byte("verifier-wire-t"))+`, 'cli', 1, 0, 'local', '[]', `+ts+`, `+ts+`, `+ts+`, '2099-01-01T00:00:00.000000Z', '2099-01-01T00:00:00.000000Z', '', '')`)
	w.realOwners()
	srv := httptest.NewTLSServer(server.New(&server.API{Auth: authService(t, db), Incidents: w.svc}, nil))
	t.Cleanup(srv.Close)
	return incidentWire{w: w, srv: srv, env: accessWireEnv{srv: srv}}
}

// session mints a CLI session for a seeded principal.
func (iw incidentWire) session(t *testing.T, p domain.PrincipalID) string {
	t.Helper()
	return iw.sessionWithFactors(t, p, `["password","totp"]`, "ses_wire_"+string(p))
}

// sessionWithFactors mints a session whose sign-in recorded exactly these factors.
func (iw incidentWire) sessionWithFactors(t *testing.T, p domain.PrincipalID, factors, id string) string {
	t.Helper()
	artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactCLISession)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	err = tx.Write(t.Context(), iw.w.db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		generation, err := az.PrincipalGeneration(ctx, p)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		return az.MintSession(ctx, authz.NewSession{
			ID: id, PrincipalID: p, Verifier: verifier, Artifact: "cli",
			SessionGeneration: generation, CredentialEpoch: epoch, AuthMethod: "local", Factors: factors,
			AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(2 * time.Hour),
			SourceIP: "127.0.0.1", UserAgent: "incident-wire",
		})
	})
	if err != nil {
		t.Fatalf("mint session for %s: %v", p, err)
	}
	return artifact
}

func (iw incidentWire) call(t *testing.T, token, method, path string, body any) (int, []byte) {
	t.Helper()
	return iw.env.callAs(t, token, method, api.PathPrefix+path, body)
}

func decodeWire[T any](t *testing.T, body []byte) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return out
}

// The six routes end to end for the operator who may use them: open, read, page
// the items, attest, resume and close, with the statuses and bodies the contract
// declares.
func TestIncidentWireLifecycle(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		iw := newIncidentWire(t, db)
		tok := iw.session(t, wireResponder)

		unknown, _, err := crypto.NewArtifact(crypto.ArtifactCLISession)
		if err != nil {
			t.Fatal(err)
		}
		if code, _ := iw.call(t, unknown, http.MethodPost, "/incidents", map[string]string{"scope_kind": "account", "principal": string(wireTarget)}); code != http.StatusUnauthorized {
			t.Fatalf("open without a session = %d, want 401", code)
		}
		code, body := iw.call(t, tok, http.MethodPost, "/incidents", map[string]string{"scope_kind": "account", "principal": string(wireTarget)})
		if code != http.StatusOK {
			t.Fatalf("open = %d %s", code, body)
		}
		inc := decodeWire[apigen.Incident](t, body)
		if inc.Status != "open" || inc.ScopeId != string(wireTarget) || inc.Counts.Pending == 0 {
			t.Fatalf("opened incident = %+v", inc)
		}
		path := "/incidents/" + inc.Id

		if code, body = iw.call(t, tok, http.MethodGet, path, nil); code != http.StatusOK || decodeWire[apigen.Incident](t, body).Id != inc.Id {
			t.Fatalf("get = %d %s", code, body)
		}

		// Paging: a full page names the cursor; walking it reads every item once.
		seen := map[string]apigen.IncidentItem{}
		after := ""
		for page := 0; page < 20; page++ {
			q := path + "/items?limit=2"
			if after != "" {
				q += "&after=" + after
			}
			code, body = iw.call(t, tok, http.MethodGet, q, nil)
			if code != http.StatusOK {
				t.Fatalf("items = %d %s", code, body)
			}
			list := decodeWire[apigen.IncidentItemList](t, body)
			for _, it := range list.Items {
				if _, dup := seen[it.Id]; dup {
					t.Fatalf("item %s listed twice while paging", it.Id)
				}
				seen[it.Id] = it
			}
			if list.NextAfter == nil {
				break
			}
			after = *list.NextAfter
		}
		want := queryInt(t, db, `SELECT COUNT(*) FROM incident_items WHERE incident_id='`+inc.Id+`'`)
		if int64(len(seen)) != int64(want) {
			t.Fatalf("paged %d items, ledger has %d", len(seen), want)
		}
		var session, identity apigen.IncidentItem
		for _, it := range seen {
			switch it.ObjectId {
			case "ses_wire_t":
				session = it
			case "xid_wire":
				identity = it
			}
		}
		// attestable is the server's own verdict: an observable session is not
		// offered, an identity at its provider is.
		if session.Attestable || !identity.Attestable {
			t.Fatalf("attestable: session %v identity %v, want false and true", session.Attestable, identity.Attestable)
		}

		if code, body = iw.call(t, tok, http.MethodPost, path+"/items/"+session.Id+"/attest", nil); code != http.StatusConflict {
			t.Fatalf("attest of an observable item = %d %s, want 409", code, body)
		}
		if code, body = iw.call(t, tok, http.MethodPost, path+"/items/"+identity.Id+"/attest", nil); code != http.StatusNoContent {
			t.Fatalf("attest = %d %s, want 204", code, body)
		}
		code, body = iw.call(t, tok, http.MethodGet, path+"/items?open_only=true&limit=500", nil)
		if code != http.StatusOK {
			t.Fatalf("open items = %d %s", code, body)
		}
		for _, it := range decodeWire[apigen.IncidentItemList](t, body).Items {
			if it.Id == identity.Id || it.Outcome == "done" || it.Outcome == "attested" {
				t.Fatalf("open_only listed a settled item: %+v", it)
			}
		}

		// Residuals stay listed: a clean close with items still open is refused.
		if code, body = iw.call(t, tok, http.MethodPost, path+"/close", map[string]bool{"with_residuals": false}); code != http.StatusConflict {
			t.Fatalf("clean close with open items = %d %s, want 409", code, body)
		}
		// Resume needs an operation-bound proof (ADR O2): over the wire without one it is
		// refused with 400 and runs nothing. The proof path itself, with a real account
		// and factor, is TestIncidentResumeNeedsAFreshProof; this world has no password,
		// so the pass runs through the service as the local operator.
		if code, body = iw.call(t, tok, http.MethodPost, path+"/resume", nil); code != http.StatusBadRequest {
			t.Fatalf("resume without a proof = %d %s, want 400", code, body)
		}
		if res := iw.w.resume(t, wireResponder, inc.Id); res.Examined == 0 {
			t.Fatalf("resume examined nothing: %+v", res)
		}
		// Everything observable is now confirmed or attested, so the clean close holds.
		if code, body = iw.call(t, tok, http.MethodPost, path+"/close", map[string]bool{"with_residuals": false}); code != http.StatusOK || decodeWire[apigen.Incident](t, body).Status != "closed" {
			t.Fatalf("close = %d %s", code, body)
		}
	})
}

// An operator of another org, a member of the same org without the incident
// capability and a caller who is signed in but holds nothing all get the answer a
// nonexistent incident gets, byte for byte, on all six routes, and nothing moves:
// no item or incident changes and the only audit rows added are the denials
// themselves (grant.denied, the security trail's record of a refusal).
func TestIncidentWireRefusalsAreUniformAcrossTenants(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		iw := newIncidentWire(t, db)
		owner := iw.session(t, wireResponder)
		real := decodeWire[apigen.Incident](t, mustCall(t, iw, owner, http.MethodPost, "/incidents", map[string]string{"scope_kind": "account", "principal": string(wireTarget)}))
		var itemID string
		for _, it := range decodeWire[apigen.IncidentItemList](t, mustCall(t, iw, owner, http.MethodGet, "/incidents/"+real.Id+"/items?limit=500", nil)).Items {
			itemID = it.Id
			break
		}
		if itemID == "" {
			t.Fatal("the account incident listed no items")
		}
		ledger := func() string {
			items := iw.w.itemMap(t, wireResponder, real.Id)
			keys := make([]string, 0, len(items))
			for k, v := range items {
				keys = append(keys, k+"="+v)
			}
			slices.Sort(keys)
			return fmt.Sprintf("%v incidents=%d closed=%d audit=%s", keys,
				queryInt(t, db, `SELECT COUNT(*) FROM incidents`), queryInt(t, db, `SELECT COUNT(*) FROM incidents WHERE status <> 'open'`),
				queryStrings(t, db, `SELECT type || ' ' FROM (SELECT type FROM audit_tenant_events UNION ALL SELECT type FROM audit_instance_events) AS e WHERE type <> 'grant.denied' ORDER BY type`))
		}
		callers := []domain.PrincipalID{wireForeign, wireMember, wireNobody}
		tokens := map[domain.PrincipalID]string{}
		for _, p := range callers {
			tokens[p] = iw.session(t, p) // minted first: a session is not a ledger change
		}
		before := ledger()
		ghost := wireNoIncId

		type probe struct {
			name, method, suffix string
			body                 any
		}
		probes := func(id string) []probe {
			return []probe{
				{"get", http.MethodGet, id, nil},
				{"items", http.MethodGet, id + "/items?limit=5", nil},
				{"resume", http.MethodPost, id + "/resume", map[string]string{"proof": "000000"}},
				{"attest", http.MethodPost, id + "/items/" + itemID + "/attest", nil},
				{"close", http.MethodPost, id + "/close", map[string]bool{"with_residuals": true}},
			}
		}
		// The security trail records a denial for the caller's own refusal, so the
		// count is part of what the two reads must share (tenant-isolation ADR:
		// equal refusal reads for missing and inaccessible).
		denials := func() int64 {
			return queryInt(t, db, `SELECT (SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'grant.denied') + (SELECT COUNT(*) FROM audit_instance_events WHERE type = 'grant.denied')`)
		}
		type result struct {
			code    int
			body    []byte
			denials int64
		}
		run := func(tok, method, path string, body any) result {
			d := denials()
			c, b := iw.call(t, tok, method, path, body)
			return result{c, b, denials() - d}
		}
		// Wire ids must match the API's id shape, so the org and project the
		// foreign probes name are seeded under a UUID-shaped id.
		const (
			orgReal    = "org_00000000-0000-7000-8000-0000000000c1"
			prjReal    = "prj_00000000-0000-7000-8000-0000000000c2"
			orgMissing = "org_00000000-0000-7000-8000-0000000000c3"
			prjMissing = "prj_00000000-0000-7000-8000-0000000000c4"
			adapterID  = "adp_00000000-0000-7000-8000-0000000000c5"
		)
		execRaw(t, db, `INSERT INTO orgs (id, name, active, metadata, created_at) VALUES ('`+orgReal+`', 'org-real', TRUE, '{}', `+ts+`)`)
		execRaw(t, db, `INSERT INTO projects (id, org_id, name, created_at) VALUES ('`+prjReal+`', '`+orgReal+`', 'prj-real', `+ts+`)`)
		type opening struct {
			name          string
			foreign, miss map[string]string
		}
		openings := []opening{
			{"account", map[string]string{"scope_kind": "account", "principal": string(wireTarget)}, map[string]string{"scope_kind": "account", "principal": string(wireGhost)}},
			{"org", map[string]string{"scope_kind": "org", "org": orgReal}, map[string]string{"scope_kind": "org", "org": orgMissing}},
			{"project", map[string]string{"scope_kind": "project", "org": orgReal, "project": prjReal}, map[string]string{"scope_kind": "project", "org": orgReal, "project": prjMissing}},
			{"adapter", map[string]string{"scope_kind": "adapter", "org": orgReal, "project": prjReal, "adapter": adapterID}, map[string]string{"scope_kind": "adapter", "org": orgReal, "project": prjMissing, "adapter": adapterID}},
		}
		for _, p := range callers {
			tok := tokens[p]
			ghosts := probes(ghost)
			for i, pr := range probes(real.Id) {
				g := run(tok, ghosts[i].method, "/incidents/"+ghosts[i].suffix, ghosts[i].body)
				r := run(tok, pr.method, "/incidents/"+pr.suffix, pr.body)
				if g.code != http.StatusNotFound {
					t.Fatalf("%s: a nonexistent incident answers %d %s, want 404", pr.name, g.code, g.body)
				}
				if r.code != g.code || !bytes.Equal(r.body, g.body) {
					t.Errorf("%s as %s: foreign incident answered %d %q, nonexistent %d %q", pr.name, p, r.code, r.body, g.code, g.body)
				}
				if r.denials != g.denials {
					t.Errorf("%s as %s: a foreign incident wrote %d grant.denied rows, a nonexistent one %d", pr.name, p, r.denials, g.denials)
				}
			}
			// Opening a target the caller has no authority over reads like opening
			// one that does not exist, for every scope kind.
			for _, o := range openings {
				want := run(tok, http.MethodPost, "/incidents", o.miss)
				got := run(tok, http.MethodPost, "/incidents", o.foreign)
				if got.code != http.StatusNotFound || got.code != want.code || !bytes.Equal(got.body, want.body) {
					t.Errorf("open %s as %s answered %d %q, a missing target %d %q", o.name, p, got.code, got.body, want.code, want.body)
				}
				if got.denials != want.denials {
					t.Errorf("open %s as %s wrote %d grant.denied rows, a missing target %d", o.name, p, got.denials, want.denials)
				}
			}
		}
		if after := ledger(); after != before {
			t.Fatalf("refused probes moved the ledger:\n before %s\n after  %s", before, after)
		}
	})
}

func mustCall(t *testing.T, iw incidentWire, token, method, path string, body any) []byte {
	t.Helper()
	code, out := iw.call(t, token, method, path, body)
	if code != http.StatusOK {
		t.Fatalf("%s %s = %d %s", method, path, code, out)
	}
	return out
}
