package isolation

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func preauthLimiter(t *testing.T) *admission.Limiter {
	t.Helper()
	l, err := admission.New(admission.Config{
		ArgonMemoryKiB: 64 * 1024,
		Now:            func() time.Time { return time.Date(2026, 10, 28, 12, 0, 30, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// Drive the production router, service and in-transaction authorizer. The
// query observer proves that overflow prevents resolution, rather than merely
// changing the status after the protected work has already happened.
func TestPreauthenticationRealBearerResolution(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for _, kind := range []crypto.ArtifactType{
			crypto.ArtifactCLISession, crypto.ArtifactBrowserSession,
			crypto.ArtifactWorkspaceSession, crypto.ArtifactWorkload,
			crypto.ArtifactAutomation, crypto.ArtifactInstanceConn,
			crypto.ArtifactDeveloper, crypto.ArtifactSCIM,
		} {
			t.Run(string(kind), func(t *testing.T) {
				token, _, err := crypto.NewArtifact(kind)
				if err != nil {
					t.Fatal(err)
				}
				limiter := preauthLimiter(t)
				handler := server.New(&server.API{Auth: &service.Auth{DB: db}, Admission: limiter}, nil)
				var queries atomic.Int64
				restore := authn.SetQueryObserver(func(string) { queries.Add(1) })
				defer restore()
				var withinBody string
				var withinQueries int64
				for i := 0; i <= admission.MetaPerIPPerMinute; i++ {
					before := queries.Load()
					req := httptest.NewRequest(http.MethodGet, api.PathPrefix+"/auth/whoami", nil)
					req.RemoteAddr = "192.0.2.83:1234"
					// An untrusted peer cannot choose a fresh bucket per attempt.
					req.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i+1))
					if kind == crypto.ArtifactBrowserSession {
						req.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: token})
					} else {
						req.Header.Set("Authorization", "Bearer "+token)
					}
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, req)
					work := queries.Load() - before
					if i < admission.MetaPerIPPerMinute {
						if rec.Code != http.StatusUnauthorized || work == 0 {
							t.Fatalf("attempt %d = %d, %d queries, want 401 with resolution", i, rec.Code, work)
						}
						if i == 0 {
							withinBody, withinQueries = rec.Body.String(), work
						}
						if rec.Body.String() != withinBody || work != withinQueries {
							t.Fatalf("attempt %d changed the refusal work/body", i)
						}
					} else if rec.Code != http.StatusTooManyRequests || work != 0 || rec.Header().Get("Retry-After") == "" {
						t.Fatalf("overflow = %d %s, %d queries, want 429 before resolution", rec.Code, rec.Body.String(), work)
					}
				}
				before := queries.Load()
				req := httptest.NewRequest(http.MethodGet, api.PathPrefix+"/auth/whoami", nil)
				req.RemoteAddr = "192.0.2.84:1234"
				if kind == crypto.ArtifactBrowserSession {
					req.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: token})
				} else {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				if rec.Code != http.StatusUnauthorized || queries.Load()-before != withinQueries || strings.TrimSpace(rec.Body.String()) != strings.TrimSpace(withinBody) {
					t.Fatalf("second source = %d %s, want normal bounded resolution", rec.Code, rec.Body.String())
				}
			})
		}
	})
}

func preauthRequest(handler http.Handler, token string, kind crypto.ArtifactType, source, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, api.PathPrefix+path, nil)
	req.RemoteAddr = source + ":1234"
	if kind == crypto.ArtifactBrowserSession {
		req.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: token})
	} else if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func preauthSession(t *testing.T, db *store.DB, kind crypto.ArtifactType, state string, csrf ...string) string {
	t.Helper()
	value, verifier, err := crypto.NewArtifact(kind)
	if err != nil {
		t.Fatal(err)
	}
	if state == "unknown" {
		return value
	}
	id := "ses_preauth_" + string(kind) + "_" + state
	err = tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		generation, err := az.PrincipalGeneration(ctx, alice)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		artifact := "cli"
		if kind == crypto.ArtifactBrowserSession {
			artifact = "browser"
		}
		now := time.Now().UTC()
		session := authz.NewSession{
			ID: id, PrincipalID: alice, Verifier: verifier, Artifact: artifact,
			SessionGeneration: generation, CredentialEpoch: epoch,
			AuthMethod: "local", Factors: `["password","totp"]`,
			AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(time.Hour),
			AbsoluteExpiresAt: now.Add(2 * time.Hour), SourceIP: "192.0.2.83", UserAgent: "fixture",
		}
		if len(csrf) == 1 {
			session.CSRFVerifier = crypto.ArtifactVerifier(csrf[0])
		}
		if state == "expired" {
			session.CreatedAt = now.Add(-3 * time.Hour)
			session.IdleExpiresAt = now.Add(-time.Hour)
			session.AbsoluteExpiresAt = now.Add(-time.Minute)
		}
		if state == "epoch" {
			if epoch < 2 {
				return errors.New("stale fixture needs a previous credential epoch")
			}
			session.CredentialEpoch--
		}
		if err := az.MintSession(ctx, session); err != nil {
			return err
		}
		if state == "revoked" {
			return az.RevokeSession(ctx, id)
		}
		if state == "generation" {
			return az.AdvanceGeneration(ctx, alice)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestPreauthenticationCheapRejectsAndBrowserMutation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		t.Run("cheap_rejects", func(t *testing.T) {
			limiter := preauthLimiter(t)
			for range admission.MetaPerIPPerMinute {
				if err := limiter.AdmitDiscovery("192.0.2.83"); err != nil {
					t.Fatal(err)
				}
			}
			handler := server.New(&server.API{Auth: &service.Auth{DB: db}, Admission: limiter}, nil)
			wrongClass, _, err := crypto.NewArtifact(crypto.ArtifactCSRF)
			if err != nil {
				t.Fatal(err)
			}
			var queries atomic.Int64
			restore := authn.SetQueryObserver(func(string) { queries.Add(1) })
			defer restore()
			for _, token := range []string{"", "malformed", wrongClass} {
				rec := preauthRequest(handler, token, crypto.ArtifactCLISession, "192.0.2.83", http.MethodGet, "/auth/whoami")
				if rec.Code != http.StatusUnauthorized || queries.Load() != 0 {
					t.Fatalf("cheap reject = %d %s, %d queries", rec.Code, rec.Body.String(), queries.Load())
				}
			}
		})
		t.Run("browser_mutation_charges_once", func(t *testing.T) {
			csrf, _, err := crypto.NewArtifact(crypto.ArtifactCSRF)
			if err != nil {
				t.Fatal(err)
			}
			token := preauthSession(t, db, crypto.ArtifactBrowserSession, "live", csrf)
			limiter := preauthLimiter(t)
			budget := service.NewBudgetWithClock(func() time.Time { return time.Date(2026, 10, 28, 12, 0, 30, 0, time.UTC) })
			for range admission.MetaPerIPPerMinute - 1 {
				if err := limiter.AdmitDiscovery("192.0.2.83"); err != nil {
					t.Fatal(err)
				}
			}
			const sessionID = "ses_preauth_br_live"
			for range service.BudgetAuthenticatedAPIBurst - 1 {
				if err := budget.AdmitAuthenticatedAPI(sessionID); err != nil {
					t.Fatal(err)
				}
			}
			handler := server.New(&server.API{Auth: &service.Auth{DB: db}, Admission: limiter, RequestBudget: budget}, nil)
			request := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, api.PathPrefix+"/auth/logout", nil)
				req.RemoteAddr = "192.0.2.83:1234"
				req.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: token})
				req.AddCookie(&http.Cookie{Name: "__Host-hikyo-csrf", Value: csrf})
				req.Header.Set("X-Hikyo-CSRF", csrf)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				return rec
			}
			var sessionReads atomic.Int64
			restore := authn.SetQueryObserver(func(q string) {
				if strings.Contains(q, "-- name: GetSessionByVerifier ") {
					sessionReads.Add(1)
				}
			})
			defer restore()
			rec := request()
			if rec.Code != http.StatusNoContent || sessionReads.Load() != 2 {
				t.Fatalf("logout = %d %s, %d session reads, want CSRF and handler resolution admitted once", rec.Code, rec.Body.String(), sessionReads.Load())
			}
			if err := budget.AdmitAuthenticatedAPI(sessionID); !errors.Is(err, admission.ErrOverloaded) {
				t.Fatalf("authenticated quota was not charged exactly once: %v", err)
			}
			sessionReads.Store(0)
			rec = request()
			if rec.Code != http.StatusTooManyRequests || sessionReads.Load() != 0 || rec.Header().Get("Retry-After") == "" {
				t.Fatalf("CSRF overflow = %d %s, %d reads", rec.Code, rec.Body.String(), sessionReads.Load())
			}
		})
	})
}

func TestPreauthenticationDeadArtifacts(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		// Leave an actual previous epoch for the stale-row fixtures, without
		// running a restore or changing any other authentication predicate.
		execRaw(t, db, "UPDATE auth_instance_state SET credential_epoch = credential_epoch + 1")
		identityFixtures(t, db)
		identities := identitySvc(db)
		actor := service.LocalPrincipal(identAdmin)
		sa, err := identities.CreateServiceAccount(t.Context(), actor, prjScope(), "preauth", domain.ClassWorkload)
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []crypto.ArtifactType{crypto.ArtifactCLISession, crypto.ArtifactBrowserSession, crypto.ArtifactWorkload} {
			var uniformBody string
			for _, state := range []string{"unknown", "expired", "revoked", "epoch", "generation"} {
				if kind == crypto.ArtifactWorkload && state == "generation" {
					continue // Machine credentials have an epoch, no session generation.
				}
				t.Run(string(kind)+"/"+state, func(t *testing.T) {
					var token string
					if kind == crypto.ArtifactWorkload {
						if state == "unknown" {
							token, _, err = crypto.NewArtifact(kind)
						} else {
							var minted service.MintResult
							minted, err = identities.MintCredential(t.Context(), actor, prjScope(), sa.ID, service.MintRequest{})
							token = minted.Value
							if err == nil {
								switch state {
								case "expired":
									execRaw(t, db, "UPDATE machine_credentials SET expires_at = "+ts+" WHERE id = '"+minted.Credential.ID+"'")
								case "revoked":
									err = identities.RevokeCredential(t.Context(), actor, prjScope(), sa.ID, minted.Credential.ID)
								case "epoch":
									execRaw(t, db, "UPDATE machine_credentials SET credential_epoch = credential_epoch - 1 WHERE id = '"+minted.Credential.ID+"'")
								}
							}
						}
						if err != nil {
							t.Fatal(err)
						}
					} else {
						token = preauthSession(t, db, kind, state)
					}
					limiter := preauthLimiter(t)
					for range admission.MetaPerIPPerMinute - 1 {
						if err := limiter.AdmitDiscovery("192.0.2.83"); err != nil {
							t.Fatal(err)
						}
					}
					handler := server.New(&server.API{Auth: &service.Auth{DB: db}, Orgs: &service.Orgs{DB: db}, Admission: limiter}, nil)
					var queries atomic.Int64
					restore := authn.SetQueryObserver(func(string) { queries.Add(1) })
					defer restore()
					// /me/orgs exercises Actor.resolve; whoami above exercises
					// the account-security session surface instead.
					rec := preauthRequest(handler, token, kind, "192.0.2.83", http.MethodGet, "/me/orgs")
					if rec.Code != http.StatusUnauthorized || queries.Load() != 3 {
						t.Fatalf("last allowance = %d %s, %d queries, want uniform 401 and three reads", rec.Code, rec.Body.String(), queries.Load())
					}
					if uniformBody == "" {
						uniformBody = rec.Body.String()
					}
					if rec.Body.String() != uniformBody {
						t.Fatal("credential state changed the unauthenticated body")
					}
					queries.Store(0)
					rec = preauthRequest(handler, token, kind, "192.0.2.83", http.MethodGet, "/me/orgs")
					if rec.Code != http.StatusTooManyRequests || queries.Load() != 0 {
						t.Fatalf("overflow = %d %s, %d queries, want 429 without resolution", rec.Code, rec.Body.String(), queries.Load())
					}
				})
			}
		}
	})
}

type preauthShared struct {
	admission.SharedStore
	fail  atomic.Bool
	calls atomic.Int64
}

func (s *preauthShared) BumpWindow(ctx context.Context, bucket, subject string, window time.Time) (int64, error) {
	s.calls.Add(1)
	if s.fail.Load() {
		return 0, errors.New("synthetic coordination failure")
	}
	return s.SharedStore.BumpWindow(ctx, bucket, subject, window)
}

func TestPreauthenticationSharedNodesAndFailure(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		liveToken := preauthSession(t, db, crypto.ArtifactCLISession, "live")
		shared := &preauthShared{SharedStore: db.Coordination()}
		nodes := make([]http.Handler, 2)
		for i := range nodes {
			limiter := preauthLimiter(t)
			limiter.UseShared(shared, slog.New(slog.NewTextHandler(io.Discard, nil)))
			nodes[i] = server.New(&server.API{Auth: &service.Auth{DB: db}, Admission: limiter}, nil)
		}
		token, _, err := crypto.NewArtifact(crypto.ArtifactCLISession)
		if err != nil {
			t.Fatal(err)
		}
		var queries atomic.Int64
		restore := authn.SetQueryObserver(func(string) { queries.Add(1) })
		defer restore()
		for i := 0; i <= admission.MetaPerIPPerMinute; i++ {
			queries.Store(0)
			rec := preauthRequest(nodes[i%2], token, crypto.ArtifactCLISession, "192.0.2.83", http.MethodGet, "/auth/whoami")
			want, work := http.StatusUnauthorized, int64(3)
			if i == admission.MetaPerIPPerMinute {
				want, work = http.StatusTooManyRequests, 0
			}
			if rec.Code != want || queries.Load() != work || shared.calls.Load() != int64(i+1) {
				t.Fatalf("node %d attempt %d = %d %s, %d queries, %d charges", i%2, i, rec.Code, rec.Body.String(), queries.Load(), shared.calls.Load())
			}
		}
		queries.Store(0)
		rec := preauthRequest(nodes[1], token, crypto.ArtifactCLISession, "192.0.2.84", http.MethodGet, "/auth/whoami")
		if rec.Code != http.StatusUnauthorized || queries.Load() != 3 {
			t.Fatalf("second source = %d, %d queries", rec.Code, queries.Load())
		}
		shared.fail.Store(true)
		for i, node := range nodes {
			queries.Store(0)
			presented := token
			if i == 1 {
				presented = liveToken
			}
			rec := preauthRequest(node, presented, crypto.ArtifactCLISession, "192.0.2.85", http.MethodGet, "/auth/whoami")
			if rec.Code != http.StatusTooManyRequests || queries.Load() != 0 || rec.Header().Get("Retry-After") != "5" {
				t.Fatalf("unreachable backend = %d %s, %d queries", rec.Code, rec.Body.String(), queries.Load())
			}
		}
	})
}
