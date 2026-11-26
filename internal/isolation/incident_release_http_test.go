package isolation

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

const releaseMember = "usr_resume_member"

func releaseBody(principal, proof string) map[string]any {
	return map[string]any{"principal": principal, "proof": proof}
}

// Release over the wire: it needs a fresh account-security proof of its own, spent by
// the one call that releases; each refusal leaves the principal contained and spends
// nothing; and an unknown incident, an unknown account and an account the incident did
// not contain answer the same 404.
func TestIncidentReleaseOverHTTPWithAValidProof(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newResumeFixture(t, db)
		ctx := t.Context()
		f.incidents.Owners = service.NewIncidentOwners(f.incidents, service.IncidentOwnerServices{
			Identities: &service.Identities{DB: db}, Invites: &service.EmailInvitations{DB: db},
			SCIM: &service.SCIM{DB: db}, Dynamic: &service.Dynamic{DB: db}, Adapters: &service.Adapters{DB: db},
		})
		execRaw(t, db, `INSERT INTO password_credentials (account_id, verifier, kdf_memory_kib, kdf_time, kdf_parallelism, dek_version, credential_epoch, row_version, updated_at) VALUES ('acc_resume_member', `+blobLit(db, []byte("pw-verifier"))+`, 1, 1, 1, 1, `+acctEpoch(t, db)+`, 1, `+ts+`)`)
		execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('usr_http_other', 'human', `+ts+`)`)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_http_other', 'usr_http_other', 'read', 'org_a', `+ts+`)`)
		execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('acc_http_other', 'usr_http_other', 'http-other', 'Other', 'http-other@example.test', `+ts+`, `+ts+`)`)
		*f.clk = f.base.Add(120 * time.Second)
		if _, err := f.incidents.Resume(ctx, f.actor, f.id, totpCode(t, f.uri, *f.clk), nil); err != nil {
			t.Fatal(err)
		}
		srv := httptest.NewTLSServer(server.New(&server.API{Auth: f.auth, Incidents: f.incidents}, nil))
		t.Cleanup(srv.Close)
		env := accessWireEnv{srv: srv}
		path := api.PathPrefix + "/incidents/" + f.id + "/release"
		*f.clk = f.base.Add(200 * time.Second)
		code := totpCode(t, f.uri, *f.clk)
		state := func() string {
			return queryString(t, db, `SELECT containment_state FROM principals WHERE id='`+releaseMember+`'`)
		}

		// Authorization comes before any proof: three different misses, one answer.
		unknownIncident, unknownBody := env.callAs(t, f.token, http.MethodPost, api.PathPrefix+"/incidents/"+wireNoIncId+"/release", releaseBody(releaseMember, ""))
		unknownAccount, unknownAccountBody := env.callAs(t, f.token, http.MethodPost, path, releaseBody("usr_nobody", code))
		outside, outsideBody := env.callAs(t, f.token, http.MethodPost, path, releaseBody("usr_http_other", code))
		for name, got := range map[string]int{"unknown incident": unknownIncident, "unknown account": unknownAccount, "account outside the incident": outside} {
			if got != http.StatusNotFound {
				t.Fatalf("release of %s = %d, want 404", name, got)
			}
		}
		if string(unknownBody) != string(unknownAccountBody) || string(unknownBody) != string(outsideBody) {
			t.Fatalf("the three 404 bodies differ: %s | %s | %s", unknownBody, unknownAccountBody, outsideBody)
		}

		// A wrong proof is refused and the account stays contained.
		if status, body := env.callAs(t, f.token, http.MethodPost, path, releaseBody(releaseMember, "000000")); status != http.StatusUnauthorized {
			t.Fatalf("release with a wrong proof = %d %s, want 401", status, body)
		}
		// A missing proof is a refusal too, before anything is released.
		if status, body := env.callAs(t, f.token, http.MethodPost, path, releaseBody(releaseMember, "")); status == http.StatusNoContent {
			t.Fatalf("release without a proof = %d %s", status, body)
		}
		// No establishment yet: refused with 409, and the refusal spends nothing.
		if status, body := env.callAs(t, f.token, http.MethodPost, path, releaseBody(releaseMember, code)); status != http.StatusConflict {
			t.Fatalf("release before an establishment = %d %s, want 409", status, body)
		}
		if s := state(); s != "contained" {
			t.Fatalf("containment_state = %q after the refusals, want contained", s)
		}
		r, err := f.auth.ResetCredential(ctx, f.actor, releaseMember, "response")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.auth.EstablishCredential(ctx, r.Authority, "a fresh password"); err != nil {
			t.Fatal(err)
		}
		if status, body := env.callAs(t, f.token, http.MethodPost, path, releaseBody(releaseMember, code)); status != http.StatusNoContent {
			t.Fatalf("release with a valid proof = %d %s, want 204", status, body)
		}
		if s := state(); s != "none" {
			t.Fatalf("containment_state = %q after the release, want none", s)
		}
		// The proof was spent by the release: replaying it releases nothing and is refused.
		if status, body := env.callAs(t, f.token, http.MethodPost, path, releaseBody(releaseMember, code)); status == http.StatusNoContent {
			t.Fatalf("a replayed proof released again: %d %s", status, body)
		}
	})
}

// A responder with neither a password nor an authenticator cannot release: the 400
// names no_proof_credential, nothing is spent and the account stays contained.
func TestIncidentReleaseRefusesAResponderWithNoProofCredentialOverHTTP(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := newResumeFixture(t, db)
		f.incidents.Owners = service.NewIncidentOwners(f.incidents, service.IncidentOwnerServices{
			Identities: &service.Identities{DB: db}, Invites: &service.EmailInvitations{DB: db},
			SCIM: &service.SCIM{DB: db}, Dynamic: &service.Dynamic{DB: db}, Adapters: &service.Adapters{DB: db},
		})
		execRaw(t, db, `INSERT INTO password_credentials (account_id, verifier, kdf_memory_kib, kdf_time, kdf_parallelism, dek_version, credential_epoch, row_version, updated_at) VALUES ('acc_resume_member', `+blobLit(db, []byte("pw-verifier"))+`, 1, 1, 1, 1, `+acctEpoch(t, db)+`, 1, `+ts+`)`)
		*f.clk = f.base.Add(120 * time.Second)
		if _, err := f.incidents.Resume(t.Context(), f.actor, f.id, totpCode(t, f.uri, *f.clk), nil); err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `DELETE FROM totp_credentials WHERE account_id='`+f.accountID+`'`)
		execRaw(t, db, `DELETE FROM password_credentials WHERE account_id='`+f.accountID+`'`)
		srv := httptest.NewTLSServer(server.New(&server.API{Auth: f.auth, Incidents: f.incidents}, nil))
		t.Cleanup(srv.Close)
		env := accessWireEnv{srv: srv}
		status, body := env.callAs(t, f.token, http.MethodPost, api.PathPrefix+"/incidents/"+f.id+"/release", releaseBody(releaseMember, "anything"))
		if status != http.StatusBadRequest {
			t.Fatalf("release without a proof credential = %d %s, want 400", status, body)
		}
		refusal := decodeWire[apigen.Error](t, body)
		if refusal.Error.Code != apigen.ErrorCodeBadRequest || refusal.Error.Detail == nil || *refusal.Error.Detail != "no_proof_credential" {
			t.Fatalf("the 400 body = %s, want code bad_request and detail no_proof_credential", body)
		}
		if s := queryString(t, db, `SELECT containment_state FROM principals WHERE id='`+releaseMember+`'`); s != "contained" {
			t.Fatalf("containment_state = %q, want contained", s)
		}
	})
}
