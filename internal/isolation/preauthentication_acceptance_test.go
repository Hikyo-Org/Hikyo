package isolation

import (
	"context"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// assertChargedBeforeWork proves one dead artifact at the last slot of its
// source allowance: the request is still resolved with the unchanged uniform
// refusal and read count, and the next presentation from the same source is a
// 429 that performs no credential read at all (the refusal signal's own window write is separate).
// outside counts reads that precede the source charge and are not credential
// reads: a workspace bearer's Origin header costs one cross-origin allowlist
// read in the CORS layer, recorded as an open boundary in the handoff.
func assertChargedBeforeWork(t *testing.T, db *store.DB, token string, kind crypto.ArtifactType, path string, reads, outside int64, uniformBody *string) {
	t.Helper()
	limiter := preauthLimiter(t)
	for range admission.MetaPerIPPerMinute - 1 {
		if err := limiter.AdmitDiscovery("192.0.2.83"); err != nil {
			t.Fatal(err)
		}
	}
	handler := server.New(&server.API{Auth: &service.Auth{DB: db}, Orgs: &service.Orgs{DB: db}, Workspace: &service.Workspace{DB: db}, Admission: limiter}, nil)
	var queries atomic.Int64
	restore := authn.SetQueryObserver(func(string) { queries.Add(1) })
	defer restore()
	rec := preauthRequest(handler, token, kind, "192.0.2.83", http.MethodGet, path)
	if rec.Code != http.StatusUnauthorized || queries.Load() != reads+outside {
		t.Fatalf("last allowance = %d %s, %d queries, want uniform 401 and %d reads", rec.Code, rec.Body.String(), queries.Load(), reads+outside)
	}
	if *uniformBody == "" {
		*uniformBody = rec.Body.String()
	}
	if rec.Body.String() != *uniformBody {
		t.Fatal("credential state changed the unauthenticated body")
	}
	queries.Store(0)
	rec = preauthRequest(handler, token, kind, "192.0.2.83", http.MethodGet, path)
	if rec.Code != http.StatusTooManyRequests || queries.Load() != outside || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("overflow = %d %s, %d queries, want 429 without resolution", rec.Code, rec.Body.String(), queries.Load())
	}
}

// assertLive is the control for assertChargedBeforeWork: the same route, fixture
// and request shape with a live credential must be resolved and served, so a dead
// state is refused for its state and not for a malformed request.
func assertLive(t *testing.T, db *store.DB, token string, kind crypto.ArtifactType, path string, want int) {
	t.Helper()
	handler := server.New(&server.API{Auth: &service.Auth{DB: db}, Orgs: &service.Orgs{DB: db}, Workspace: &service.Workspace{DB: db}, Admission: preauthLimiter(t)}, nil)
	if rec := preauthRequest(handler, token, kind, "192.0.2.84", http.MethodGet, path); rec.Code != want {
		t.Fatalf("live control = %d %s, want %d", rec.Code, rec.Body.String(), want)
	}
}

// TestPreauthenticationDeadSiblingArtifacts extends
// TestPreauthenticationDeadArtifacts, which covers cli, browser and workload
// rows. The workspace session, automation bearer and instance connection share a
// resolver with those but each has its own artifact grammar and admission
// branch, so each is driven through its own real route here.
func TestPreauthenticationDeadSiblingArtifacts(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		execRaw(t, db, "UPDATE auth_instance_state SET credential_epoch = credential_epoch + 1")
		identityFixtures(t, db)
		identities := identitySvc(db)
		actor := service.LocalPrincipal(identAdmin)
		epoch := queryInt(t, db, "SELECT credential_epoch FROM auth_instance_state")
		past := ts

		t.Run("ws", func(t *testing.T) {
			assertLive(t, db, preauthSession(t, db, crypto.ArtifactWorkspaceSession, "live"), crypto.ArtifactWorkspaceSession, "/me/sessions", http.StatusOK)
			var uniform string
			for _, state := range []string{"unknown", "expired", "revoked", "epoch", "generation"} {
				t.Run(state, func(t *testing.T) {
					value := preauthSession(t, db, crypto.ArtifactWorkspaceSession, state)
					// The caller's own session listing is the self-scoped surface, the door that admits a workspace bearer.
					assertChargedBeforeWork(t, db, value, crypto.ArtifactWorkspaceSession, "/me/sessions", 3, 1, &uniform)
				})
			}
		})

		t.Run("au", func(t *testing.T) {
			sa, err := identities.CreateServiceAccount(t.Context(), actor, prjScope(), "preauth-automation", domain.ClassAutomation)
			if err != nil {
				t.Fatal(err)
			}
			live, err := identities.MintCredential(t.Context(), actor, prjScope(), sa.ID, service.MintRequest{})
			if err != nil {
				t.Fatal(err)
			}
			// The automation account has no organisation, so a served credential ends in the uniform 404.
			assertLive(t, db, live.Value, crypto.ArtifactAutomation, "/me/orgs", http.StatusNotFound)
			var uniform string
			for _, state := range []string{"unknown", "expired", "revoked", "epoch"} {
				t.Run(state, func(t *testing.T) {
					value, _, err := crypto.NewArtifact(crypto.ArtifactAutomation)
					if err != nil {
						t.Fatal(err)
					}
					if state != "unknown" {
						minted, err := identities.MintCredential(t.Context(), actor, prjScope(), sa.ID, service.MintRequest{})
						if err != nil {
							t.Fatal(err)
						}
						value = minted.Value
						switch state {
						case "expired":
							execRaw(t, db, "UPDATE machine_credentials SET expires_at = "+past+" WHERE id = '"+minted.Credential.ID+"'")
						case "revoked":
							err = identities.RevokeCredential(t.Context(), actor, prjScope(), sa.ID, minted.Credential.ID)
						case "epoch":
							execRaw(t, db, "UPDATE machine_credentials SET credential_epoch = credential_epoch - 1 WHERE id = '"+minted.Credential.ID+"'")
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					assertChargedBeforeWork(t, db, value, crypto.ArtifactAutomation, "/me/orgs", 3, 0, &uniform)
				})
			}
		})

		t.Run("ic", func(t *testing.T) {
			live := mintInstanceConnection(t, db, "live")
			execRaw(t, db, "UPDATE instance_connections SET credential_epoch = "+strconv.FormatInt(epoch, 10)+" WHERE id = 'icn_live'")
			assertLive(t, db, live, crypto.ArtifactInstanceConn, "/instance/directory", http.StatusOK)
			var uniform string
			for _, state := range []string{"unknown", "expired", "revoked", "epoch"} {
				t.Run(state, func(t *testing.T) {
					value, _, err := crypto.NewArtifact(crypto.ArtifactInstanceConn)
					if err != nil {
						t.Fatal(err)
					}
					if state != "unknown" {
						id := "dead_" + state
						value = mintInstanceConnection(t, db, id)
						row := "icn_" + id
						// The fixture mints at epoch 1, so each state below isolates its own cause.
						if state != "epoch" {
							execRaw(t, db, "UPDATE instance_connections SET credential_epoch = "+strconv.FormatInt(epoch, 10)+" WHERE id = '"+row+"'")
						}
						switch state {
						case "expired":
							execRaw(t, db, "UPDATE instance_connections SET expires_at = "+past+" WHERE id = '"+row+"'")
						case "revoked":
							err = tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
								revoked, err := az.RevokeInstanceConnection(ctx, row, time.Now().UTC())
								if err == nil && !revoked {
									err = errors.New("fixture was not live to revoke")
								}
								return err
							})
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					// The connection's own route, one read fewer than the session and machine legs.
					assertChargedBeforeWork(t, db, value, crypto.ArtifactInstanceConn, "/instance/directory", 2, 0, &uniform)
				})
			}
		})
	})
}

// A request that the source allowance admitted spends it exactly once, however
// many times the resolver, CSRF precheck and service resolve the same artifact.
// The counting store sits behind the real limiter, so a second charge cannot hide.
func TestPreauthenticationLegitimateTrafficIsChargedOnce(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		identities := identitySvc(db)
		actor := service.LocalPrincipal(identAdmin)
		sa, err := identities.CreateServiceAccount(t.Context(), actor, prjScope(), "preauth-legit", domain.ClassWorkload)
		if err != nil {
			t.Fatal(err)
		}
		minted, err := identities.MintCredential(t.Context(), actor, prjScope(), sa.ID, service.MintRequest{})
		if err != nil {
			t.Fatal(err)
		}
		shared := &preauthShared{SharedStore: db.Coordination()}
		limiter := preauthLimiter(t)
		limiter.UseShared(shared, slog.New(slog.NewTextHandler(io.Discard, nil)))
		handler := server.New(&server.API{Auth: &service.Auth{DB: db}, Orgs: &service.Orgs{DB: db}, Admission: limiter, RequestBudget: service.NewBudgetWithClock(func() time.Time { return time.Date(2026, 10, 28, 12, 0, 30, 0, time.UTC) })}, nil)

		t.Run("machine_credential", func(t *testing.T) {
			for i := range 3 {
				before := shared.calls.Load()
				rec := preauthRequest(handler, minted.Value, crypto.ArtifactWorkload, "192.0.2.86", http.MethodGet, "/me/orgs")
				// The workload has no organisation, so a resolved credential ends in the uniform 404.
				if rec.Code != http.StatusNotFound || shared.calls.Load()-before != 1 {
					t.Fatalf("request %d = %d %s, %d source charges, want a resolved credential and one", i, rec.Code, rec.Body.String(), shared.calls.Load()-before)
				}
			}
		})
		// A session is charged to the source until this node has proven it live, then
		// to its own request budget: never the same limit twice.
		for _, kind := range []crypto.ArtifactType{crypto.ArtifactCLISession, crypto.ArtifactBrowserSession} {
			t.Run(string(kind), func(t *testing.T) {
				token := preauthSession(t, db, kind, "charged-once")
				for i := range 4 {
					before := shared.calls.Load()
					rec := preauthRequest(handler, token, kind, "192.0.2.87", http.MethodGet, "/auth/whoami")
					want := int64(0)
					if i == 0 {
						want = 1
					}
					if rec.Code != http.StatusOK || shared.calls.Load()-before != want {
						t.Fatalf("presentation %d = %d %s, %d source charges, want %d", i, rec.Code, rec.Body.String(), shared.calls.Load()-before, want)
					}
				}
			})
		}
	})
}

// Source refusals ride the one #1034 admission signal. The signal carries a
// closed stage, the transport-derived source and the request id, so a flood of
// refusals neither grows the durable audit trail nor leaks the presented value.
func TestPreauthenticationRefusalsAreBoundedAndSecretFree(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		const refused = 5
		admissionStage := slices.Index(audit.RefusalStages(), audit.RefusalAdmission)
		for _, kind := range []crypto.ArtifactType{crypto.ArtifactCLISession, crypto.ArtifactBrowserSession, crypto.ArtifactWorkload, crypto.ArtifactAutomation, crypto.ArtifactInstanceConn} {
			t.Run(string(kind), func(t *testing.T) {
				// The window seals into a durable audit event when it rolls over a minute,
				// so start only with room left in the current one.
				if left := 60 - time.Now().Second(); left < 10 {
					time.Sleep(time.Duration(left) * time.Second)
				}
				var before audit.RefusalWindow
				if err := json.Unmarshal([]byte(queryString(t, db, "SELECT state FROM auth_refusal_window WHERE id = 1")), &before); err != nil {
					t.Fatal(err)
				}
				limiter := preauthLimiter(t)
				const source = "192.0.2.105"
				for range admission.MetaPerIPPerMinute {
					if err := limiter.AdmitDiscovery(source); err != nil {
						t.Fatal(err)
					}
				}
				handler := server.New(&server.API{Auth: authService(t, db), Orgs: &service.Orgs{DB: db}, Admission: limiter, Refusals: service.NewRefusalSignals(db)}, nil)
				token, _, err := crypto.NewArtifact(kind)
				if err != nil {
					t.Fatal(err)
				}
				events := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events")
				var queries atomic.Int64
				restore := authn.SetQueryObserver(func(q string) {
					if !strings.Contains(q, "auth_refusal_window") {
						queries.Add(1)
					}
				})
				defer restore()
				for i := range refused {
					rec := preauthRequest(handler, token, kind, source, http.MethodGet, "/me/orgs")
					if rec.Code != http.StatusTooManyRequests {
						t.Fatalf("refusal %d = %d %s", i, rec.Code, rec.Body.String())
					}
				}
				if queries.Load() != 0 {
					t.Fatalf("%d credential reads behind a refused source", queries.Load())
				}
				raw := queryString(t, db, "SELECT state FROM auth_refusal_window WHERE id = 1")
				var state audit.RefusalWindow
				if err := json.Unmarshal([]byte(raw), &state); err != nil {
					t.Fatal(err)
				}
				if state.Counts[admissionStage]-before.Counts[admissionStage] != refused || len(state.Samples)-len(before.Samples) != refused {
					t.Fatalf("admission signal = %+v after %+v, want one sample per refusal", state, before)
				}
				for i, count := range state.Counts {
					if i == admissionStage {
						continue
					}
					if count != before.Counts[i] {
						t.Fatalf("stage %d counted %d more: a source refusal was also reported as a credential outcome", i, count-before.Counts[i])
					}
				}
				for _, sample := range state.Samples[len(before.Samples):] {
					if sample.Stage != audit.RefusalAdmission || sample.SourceIP != source {
						t.Fatalf("sample = %+v", sample)
					}
				}
				if strings.Contains(raw, token) || strings.Contains(raw, hex.EncodeToString(crypto.ArtifactVerifier(token))) {
					t.Fatal("refusal state carries the presented credential or its verifier")
				}
				if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events"); got != events {
					t.Fatalf("source refusals wrote %d durable audit events", got-events)
				}
			})
		}
	})
}

// A federated bearer keeps its dedicated per-source admission. It is neither
// charged to the bearer source allowance nor refused by that allowance's
// exhaustion, so the new hook cannot change which limit federation spends.
func TestPreauthenticationFederationKeepsItsDedicatedAdmission(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		rig := newFedRig(t, db)
		limiter := preauthLimiter(t)
		rig.fed.Admission = limiter
		const source = "192.0.2.88"
		for range admission.MetaPerIPPerMinute {
			if err := limiter.AdmitDiscovery(source); err != nil {
				t.Fatal(err)
			}
		}
		var sourceCharges atomic.Int64
		ctx := audit.WithContext(t.Context(), audit.Context{SourceIP: source, Origin: audit.OriginAPI})
		ctx = operation.WithPreauthenticationAdmission(ctx, func() error { sourceCharges.Add(1); return limiter.AdmitDiscovery(source) })
		for i := range admission.PerIPPerMinute + 1 {
			_, err := rig.fed.Authenticate(ctx, "not-a-jwt")
			if i < admission.PerIPPerMinute && !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("attempt %d inside the dedicated allowance: %v", i, err)
			}
			if i == admission.PerIPPerMinute && !errors.Is(err, admission.ErrOverloaded) {
				t.Fatalf("attempt past the dedicated allowance: %v", err)
			}
		}
		if sourceCharges.Load() != 0 {
			t.Fatalf("federation charged the bearer source allowance %d times", sourceCharges.Load())
		}
	})
}
