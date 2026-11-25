package isolation

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The six incident routes admit a human session only, and a session that did not
// pass multi-factor sign-in is not enough. The routes carry the unauthenticated
// class, so no generic artifact matrix covers them; this is their matrix. A
// machine credential, a developer credential and a single-factor session each read
// a real incident exactly as a nonexistent one, on every route, and move nothing.
// The two credential classes also leave one named refusal event per request.
func TestIncidentWireRefusesOtherArtifactsAndWeakSessions(t *testing.T) {
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

		identities := &service.Identities{DB: db, Auth: authService(t, db)}
		minted, err := identities.MintCredential(t.Context(), service.LocalPrincipal(wireResponder),
			domain.Scope{Org: "org_a", Project: "prj_a1"}, "sa_it", service.MintRequest{})
		if err != nil {
			t.Fatalf("mint machine credential: %v", err)
		}
		developer := seedSecurityDeveloper(t, db, iw.session(t, wireTarget), domain.Scope{Org: "org_a", Project: "prj_a1", Env: "env_a1"}, "incident")
		weak := iw.sessionWithFactors(t, wireResponder, `["password"]`, "ses_wire_weak")

		type route struct {
			name, method, path string
			body               any
		}
		routes := func(id string) []route {
			return []route{
				{"open", http.MethodPost, "/incidents", map[string]string{"scope_kind": "account", "principal": string(wireTarget)}},
				{"get", http.MethodGet, "/incidents/" + id, nil},
				{"items", http.MethodGet, "/incidents/" + id + "/items?limit=5", nil},
				{"attest", http.MethodPost, "/incidents/" + id + "/items/" + itemID + "/attest", nil},
				{"resume", http.MethodPost, "/incidents/" + id + "/resume", map[string]any{"proof": "000000", "open_items": 0}},
				{"close", http.MethodPost, "/incidents/" + id + "/close", map[string]bool{"with_residuals": true}},
			}
		}
		state := func() string {
			return queryStrings(t, db, `SELECT outcome || '/' || COALESCE(reason, '') || ' ' FROM incident_items ORDER BY id`) +
				queryStrings(t, db, `SELECT status || ' ' FROM incidents ORDER BY id`)
		}
		denied := func() int64 {
			return queryInt(t, db, `SELECT (SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'grant.denied' AND actor_id = '`+string(wireResponder)+`') + `+
				`(SELECT COUNT(*) FROM audit_instance_events WHERE type = 'grant.denied' AND actor_id = '`+string(wireResponder)+`')`)
		}
		refused := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.artifact_class_refused'`)
		}

		// The control is what a caller with no such incident reads.
		nobody := iw.session(t, wireNobody)
		control := map[string][]byte{}
		for _, r := range routes(wireNoIncId) {
			code, body := iw.call(t, nobody, r.method, r.path, r.body)
			if code != http.StatusNotFound {
				t.Fatalf("control %s = %d %s, want 404", r.name, code, body)
			}
			control[r.name] = body
		}

		before := state()
		for _, c := range []struct {
			name, token string
			named       bool
		}{
			{"machine credential", minted.Value, true},
			{"developer credential", developer, true},
			{"single-factor session", weak, false},
		} {
			eventsBefore, deniedBefore := refused(), denied()
			for _, r := range routes(real.Id) {
				code, body := iw.call(t, c.token, r.method, r.path, r.body)
				if code != http.StatusNotFound || !bytes.Equal(body, control[r.name]) {
					t.Errorf("%s on %s answered %d %q, want the nonexistent answer 404 %q", c.name, r.name, code, body, control[r.name])
				}
			}
			if got, want := refused()-eventsBefore, int64(len(routes(real.Id))); c.named && got != want {
				t.Errorf("%s: %d auth.artifact_class_refused rows, want %d", c.name, got, want)
			} else if !c.named && got != 0 {
				t.Errorf("%s: %d auth.artifact_class_refused rows, want none (the artifact class is admitted)", c.name, got)
			}
			// A single-factor session is an admitted artifact class, so the refusal is the
			// capability's own denial, recorded as one.
			if !c.named && denied() == deniedBefore {
				t.Errorf("%s: no grant.denied row recorded for the refused session", c.name)
			}
		}
		if after := state(); after != before {
			t.Fatalf("refused artifacts moved the ledger:\n before %s\n after  %s", before, after)
		}
	})
}
