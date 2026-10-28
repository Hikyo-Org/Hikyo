package isolation

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// These are compatibility controls for #1034's proposed audit amendment, not
// acceptance of a new refusal producer. In particular, a 401 does not prove
// that an operator can find a retained, correlated refusal record.
func TestAuthenticationRefusalWireCompatibility(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		auth := authService(t, db)
		metrics := server.NewMetrics(nil)
		var logs bytes.Buffer
		handler := server.New(&server.API{
			Auth: auth, Settings: &service.ProjectSettings{DB: db, Auth: auth},
			Metrics: metrics, Log: slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		}, nil)
		const settings = api.PathPrefix + "/orgs/org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fee/projects/prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fdd/environments/env_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fcc/settings"
		const secret = "REFUSAL-REQUEST-DATA-CANARY"
		var outputs bytes.Buffer
		var tokens []string
		request := func(method, path, token string, cookie bool, change func(*http.Request)) *httptest.ResponseRecorder {
			t.Helper()
			r := httptest.NewRequest(method, path, nil)
			r.RemoteAddr = "192.0.2.103:12345"
			r.Header.Set("X-Forwarded-For", "198.51.100.99") // untrusted
			r.Header.Set("User-Agent", "refusal-test "+token)
			if token != "" {
				if cookie {
					r.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: token})
				} else {
					r.Header.Set("Authorization", "Bearer "+token)
				}
			}
			if change != nil {
				change(r)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			fmt.Fprint(&outputs, w.Header(), w.Body.String())
			return w
		}
		count := func(typ string) int64 {
			return queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type='"+typ+"'") +
				queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='"+typ+"'")
		}
		var refusalBody string
		refused := func(label, path, token string, cookie bool) {
			t.Helper()
			before := count("grant.denied")
			eventsBefore := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events")
			w := request(http.MethodGet, path, token, cookie, nil)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s: status %d, want 401: %s", label, w.Code, w.Body)
			}
			if refusalBody == "" {
				refusalBody = w.Body.String()
			}
			if w.Body.String() != refusalBody {
				t.Fatalf("%s: authentication response differs from missing artifact", label)
			}
			if count("grant.denied") != before {
				t.Fatalf("%s: unauthenticated request emitted an authorization denial", label)
			}
			// Report the current producer gap without making silence an invariant
			// that a later approved producer would have to break to add coverage.
			t.Logf("%s: 401, instance event delta %d", label,
				queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events")-eventsBefore)
		}
		refused("missing", api.PathPrefix+"/auth/whoami", "", false)
		for _, artifact := range []crypto.ArtifactType{crypto.ArtifactCLISession, crypto.ArtifactBrowserSession} {
			cookie := artifact == crypto.ArtifactBrowserSession
			for _, state := range []string{"unknown", "expired", "revoked"} {
				token := refusalSession(t, db, artifact, state)
				tokens = append(tokens, token)
				refused(string(artifact)+"/"+state, api.PathPrefix+"/auth/whoami", token, cookie)
			}
			refused(string(artifact)+"/malformed", api.PathPrefix+"/auth/whoami", "malformed", cookie)
			live := refusalSession(t, db, artifact, "live")
			tokens = append(tokens, live)
			if w := request(http.MethodGet, api.PathPrefix+"/auth/whoami", live, cookie, nil); w.Code != http.StatusOK {
				t.Fatalf("live %s positive control: %d: %s", artifact, w.Code, w.Body)
			}
			// The middleware exits before the handler on conflicting channels.
			w := request(http.MethodGet, api.PathPrefix+"/auth/whoami", live, cookie, func(r *http.Request) {
				r.Header.Set("Authorization", "Bearer "+live)
				r.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: live})
			})
			if w.Code != http.StatusUnauthorized || w.Body.String() != refusalBody {
				t.Fatalf("conflicting channels: %d: %s", w.Code, w.Body)
			}
			if cookie {
				w := request(http.MethodPost, api.PathPrefix+"/auth/logout", live, true, func(r *http.Request) {
					r.Body = io.NopCloser(strings.NewReader(secret))
					r.Header.Set("X-Hikyo-CSRF", secret)
				})
				if w.Code != http.StatusUnauthorized || w.Body.String() != refusalBody {
					t.Fatalf("live cookie CSRF mismatch: %d: %s", w.Code, w.Body)
				}
			} else {
				before := count("grant.denied")
				w := request(http.MethodGet, settings, live, false, nil)
				if w.Code != http.StatusNotFound || count("grant.denied") != before+1 {
					t.Fatalf("authenticated missing scope must produce exactly one denial: %d: %s", w.Code, w.Body)
				}
			}
		}
		wProof := request(http.MethodGet, api.PathPrefix+"/auth/whoami", "", false, func(r *http.Request) {
			r.Header.Set("Authorization", "DPoP "+secret)
			r.Header.Set("DPoP", secret)
		})
		if wProof.Code != http.StatusUnauthorized || wProof.Body.String() != refusalBody {
			t.Fatalf("DPoP on disallowed route: %d: %s", wProof.Code, wProof.Body)
		}
		svc := identitySvc(db)
		actor := service.LocalPrincipal(identAdmin)
		sa, err := svc.CreateServiceAccount(t.Context(), actor, prjScope(), "refusal-wire", domain.ClassWorkload)
		if err != nil {
			t.Fatal(err)
		}
		var liveMachine string
		for _, state := range []string{"unknown", "expired", "revoked", "live"} {
			var token string
			if state == "unknown" {
				token, _, err = crypto.NewArtifact(crypto.ArtifactWorkload)
			} else {
				minted, mintErr := svc.MintCredential(t.Context(), actor, prjScope(), sa.ID, service.MintRequest{})
				err, token = mintErr, minted.Value
				if err == nil && state == "revoked" {
					err = svc.RevokeCredential(t.Context(), actor, prjScope(), sa.ID, minted.Credential.ID)
				}
				if err == nil && state == "expired" {
					execRaw(t, db, "UPDATE machine_credentials SET expires_at = "+ts+" WHERE id='"+minted.Credential.ID+"'")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			tokens = append(tokens, token)
			if state != "live" {
				refused("machine/"+state, settings, token, false)
				continue
			}
			before := count("auth.artifact_class_refused")
			w := request(http.MethodGet, settings, token, false, nil)
			if w.Code != http.StatusNotFound || count("auth.artifact_class_refused") != before+1 {
				t.Fatalf("live disallowed machine must produce exactly one dedicated refusal: %d: %s", w.Code, w.Body)
			}
			liveMachine = token
			if source := queryString(t, db, "SELECT source_ip FROM audit_instance_events WHERE type='auth.artifact_class_refused' ORDER BY seq DESC LIMIT 1"); source != "192.0.2.103" {
				t.Fatalf("dedicated refusal source %q, want socket peer rather than spoofed forwarding header", source)
			}
		}
		for _, state := range []string{"generation", "epoch"} {
			cli := refusalSession(t, db, crypto.ArtifactCLISession, "live")
			browser := refusalSession(t, db, crypto.ArtifactBrowserSession, "live")
			tokens = append(tokens, cli, browser)
			if state == "generation" {
				execRaw(t, db, "UPDATE principals SET session_generation = session_generation + 1 WHERE id='usr_ident'")
			} else {
				execRaw(t, db, "UPDATE auth_instance_state SET credential_epoch = credential_epoch + 1 WHERE id=1")
			}
			refused("cli/"+state, api.PathPrefix+"/auth/whoami", cli, false)
			refused("browser/"+state, api.PathPrefix+"/auth/whoami", browser, true)
			if state == "epoch" {
				refused("machine/epoch", settings, liveMachine, false)
			}
		}
		// Vary secret-shaped paths and queries through the real router. RED
		// telemetry must keep its closed class labels and never echo either.
		for i := 0; i < 32; i++ {
			request(http.MethodGet, api.PathPrefix+"/"+secret+fmt.Sprint(i)+"?secret="+secret, "", false, nil)
		}
		op := server.NewOperational(nil, mcpOperationalHealth{}, metrics)
		scrape := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		scrape.RemoteAddr = "127.0.0.1:12345"
		w := httptest.NewRecorder()
		op.ServeHTTP(w, scrape)
		if w.Code != http.StatusOK {
			t.Fatalf("metrics capture: %d", w.Code)
		}
		outputs.Write(w.Body.Bytes())
		outputs.Write(logs.Bytes())
		// Export the complete captured trails, not just the payload field.
		audits := &service.Audits{DB: db}
		if err := audits.InstanceExport(t.Context(), root, store.AuditFilter{}, 100, &outputs); err != nil {
			t.Fatal(err)
		}
		if err := audits.Export(t.Context(), root, domain.Scope{Org: orgA}, store.AuditFilter{}, 100, &outputs); err != nil {
			t.Fatal(err)
		}
		for _, canary := range append(tokens, secret) {
			if strings.Contains(outputs.String(), canary) {
				t.Fatal("credential or request-data canary leaked in captured wire, log, metrics or audit output")
			}
		}
	})
}

func refusalSession(t *testing.T, db *store.DB, artifact crypto.ArtifactType, state string) string {
	t.Helper()
	token, verifier, err := crypto.NewArtifact(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if state == "unknown" {
		return token
	}
	now := time.Now().UTC()
	err = tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		generation, err := az.PrincipalGeneration(ctx, identAdmin)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		kind := "cli"
		if artifact == crypto.ArtifactBrowserSession {
			kind = "browser"
		}
		expires := now.Add(time.Hour)
		if state == "expired" {
			expires = now.Add(-time.Hour)
		}
		if err := az.MintSession(ctx, authz.NewSession{
			ID: "ses_" + id.String(), PrincipalID: identAdmin, Verifier: verifier, Artifact: kind,
			SessionGeneration: generation, CredentialEpoch: epoch,
			AuthMethod: "local", Factors: `["password","totp"]`,
			AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: expires,
			AbsoluteExpiresAt: now.Add(2 * time.Hour), SourceIP: "192.0.2.103", UserAgent: "refusal-test",
		}); err != nil {
			return err
		}
		if state == "revoked" {
			return az.RevokeSession(ctx, "ses_"+id.String())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}
