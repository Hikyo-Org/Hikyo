package isolation

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Run two real service transactions with the winner paused after taking its
// principal lock. Observe the loser's actual PG row-lock query or SQLite
// writer-pool wait before permitting the winner to commit. No timing sleeps.
func pendingProofInterleave(t *testing.T, db *store.DB, pauseQuery string, first, second func() error) (error, error) {
	t.Helper()
	paused, release, loserAtLock := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var pauseOnce, releaseOnce, loserOnce sync.Once
	var locks atomic.Int32
	restore := authn.SetQueryObserver(func(query string) {
		if strings.Contains(query, "-- name: LockPrincipalRow ") && locks.Add(1) >= 2 {
			loserOnce.Do(func() { close(loserAtLock) })
		}
		if strings.Contains(query, "-- name: "+pauseQuery+" ") {
			pauseOnce.Do(func() { close(paused); <-release })
		}
	})
	defer restore()
	defer releaseOnce.Do(func() { close(release) })
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { firstDone <- first() }()
	select {
	case <-paused:
	case err := <-firstDone:
		t.Fatalf("winner returned before locked mutation: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("winner did not reach locked mutation")
	}
	var before int64
	if db.Engine() == store.EngineSQLite {
		before = db.SQLiteWrite().Stats().WaitCount
	}
	go func() { secondDone <- second() }()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	if db.Engine() == store.EnginePostgres {
		select {
		case <-loserAtLock:
		case err := <-secondDone:
			t.Fatalf("loser returned without contending on principal lock: %v", err)
		case <-deadline.C:
			t.Fatal("loser did not reach principal lock")
		}
	} else {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for db.SQLiteWrite().Stats().WaitCount <= before {
			select {
			case <-ticker.C:
			case err := <-secondDone:
				t.Fatalf("loser returned without waiting on SQLite writer: %v", err)
			case <-deadline.C:
				t.Fatal("loser did not wait on SQLite writer")
			}
		}
	}
	releaseOnce.Do(func() { close(release) })
	return <-firstDone, <-secondDone
}

func pendingProofRevoke(ctx context.Context, db *store.DB) error {
	return tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		if err := az.LockTargetPrincipal(ctx, root); err != nil {
			return err
		}
		return az.AdvanceGeneration(ctx, root)
	})
}

func pendingProofStart(t *testing.T, ws *service.Workspace, seed string) (state, verifier string) {
	t.Helper()
	verifier, challenge := pkcePair(seed)
	started, err := ws.StartHandoff(t.Context(), service.HandoffRequest{
		Origin: stepUpOrigin, RedirectURI: stepUpOrigin + "/workspace/callback", PKCEChallenge: challenge, Purpose: service.HandoffEstablishment,
	})
	if err != nil {
		t.Fatal(err)
	}
	return started.State, verifier
}

func pendingProofWorkspace(t *testing.T, db *store.DB) (*service.Workspace, service.Actor) {
	t.Helper()
	ws := stepUpWorkspace(t, db)
	if _, err := ws.AddOrigin(t.Context(), service.LocalPrincipal(root), stepUpOrigin); err != nil {
		t.Fatal(err)
	}
	return ws, service.Bearer(seedSessionFactors(t, db, root, `["password","totp"]`))
}

func TestWorkspaceApprovalSerializesGenerationRevocation(t *testing.T) {
	for _, revokeFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "revocation-wins", false: "approval-wins"}[revokeFirst], func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				ws, actor := pendingProofWorkspace(t, db)
				state, verifier := pendingProofStart(t, ws, "approval-revocation-race")
				var code string
				approve := func() error { var err error; code, _, err = ws.ApproveHandoff(t.Context(), actor, state); return err }
				revoke := func() error { return pendingProofRevoke(t.Context(), db) }
				var approvalErr, revokeErr error
				if revokeFirst {
					revokeErr, approvalErr = pendingProofInterleave(t, db, "AdvancePrincipalGeneration", revoke, approve)
				} else {
					approvalErr, revokeErr = pendingProofInterleave(t, db, "ApproveWorkspaceHandoff", approve, revoke)
				}
				if revokeErr != nil {
					t.Fatalf("revocation failed: %v", revokeErr)
				}
				if revokeFirst {
					if !errors.Is(approvalErr, domain.ErrUnauthenticated) || code != "" {
						t.Fatalf("stale approving session survived revocation: %v", approvalErr)
					}
				} else {
					if approvalErr != nil || code == "" {
						t.Fatalf("winning approval failed: %v", approvalErr)
					}
					if _, err := ws.RedeemHandoff(t.Context(), code, verifier, stepUpOrigin); !errors.Is(err, service.ErrHandoffInvalid) {
						t.Fatalf("pending winning approval survived later revocation: %v", err)
					}
				}
				fresh := service.Bearer(seedSessionFactors(t, db, root, `["password","totp"]`))
				state, verifier = pendingProofStart(t, ws, "fresh-after-approval-race")
				code, _, err := ws.ApproveHandoff(t.Context(), fresh, state)
				if err != nil {
					t.Fatalf("fresh approval failed: %v", err)
				}
				if _, err := ws.RedeemHandoff(t.Context(), code, verifier, stepUpOrigin); err != nil {
					t.Fatalf("fresh redemption failed: %v", err)
				}
			})
		})
	}
}

func TestWorkspaceRedemptionSerializesGenerationRevocation(t *testing.T) {
	for _, revokeFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "revocation-wins", false: "redemption-wins"}[revokeFirst], func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				ws, actor := pendingProofWorkspace(t, db)
				state, verifier := pendingProofStart(t, ws, "redemption-revocation-race")
				code, _, err := ws.ApproveHandoff(t.Context(), actor, state)
				if err != nil {
					t.Fatal(err)
				}
				var session service.WorkspaceSession
				redeem := func() error {
					var err error
					session, err = ws.RedeemHandoff(t.Context(), code, verifier, stepUpOrigin)
					return err
				}
				revoke := func() error { return pendingProofRevoke(t.Context(), db) }
				var redeemErr, revokeErr error
				if revokeFirst {
					revokeErr, redeemErr = pendingProofInterleave(t, db, "AdvancePrincipalGeneration", revoke, redeem)
				} else {
					redeemErr, revokeErr = pendingProofInterleave(t, db, "ConsumeWorkspaceHandoff", redeem, revoke)
				}
				if revokeErr != nil {
					t.Fatalf("revocation failed: %v", revokeErr)
				}
				if revokeFirst {
					if !errors.Is(redeemErr, service.ErrHandoffInvalid) || session.Value != "" {
						t.Fatalf("stale approval minted a session: %v", redeemErr)
					}
				} else {
					if redeemErr != nil || session.Value == "" {
						t.Fatalf("winning redemption failed: %v", redeemErr)
					}
					err := tx.Read(t.Context(), db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
						_, err := az.AuthenticateCaller(ctx, session.Value, time.Now().UTC())
						return err
					})
					if !errors.Is(err, domain.ErrUnauthenticated) {
						t.Fatalf("winning workspace session survived later revocation: %v", err)
					}
					err = tx.Read(t.Context(), db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
						handoff, err := az.WorkspaceHandoffByCode(ctx, crypto.ArtifactVerifier(code))
						if err == nil && (handoff.ConsumedAt.IsZero() || handoff.ID != session.HandoffID || handoff.PrincipalID != root) {
							t.Error("consumed handoff lost session provenance")
						}
						return err
					})
					if err != nil {
						t.Fatalf("generation revocation deleted consumed provenance: %v", err)
					}
				}
			})
		})
	}
}

func TestLoginChallengeFinishSerializesGenerationRevocation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		adm := bootstrapAdmin(t, db, adminOpts{username: "concurrent-proof", displayName: "Concurrent proof", password: "original password long enough"})
		auth := adm.auth
		clock := time.Now().UTC()
		auth.Now = func() time.Time { return clock }
		login, err := auth.LocalLogin(t.Context(), "concurrent-proof", adm.password, service.ArtifactCLI)
		if err != nil {
			t.Fatal(err)
		}
		uri, err := auth.EnrolTOTPStart(t.Context(), login.SessionToken, adm.password)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := auth.EnrolTOTPConfirm(t.Context(), login.SessionToken, totpCode(t, uri, clock)); err != nil {
			t.Fatal(err)
		}
		pending, err := auth.LocalLogin(t.Context(), "concurrent-proof", adm.password, service.ArtifactBrowser)
		if err != nil || pending.Challenge == nil {
			t.Fatalf("issue pending password proof: %v", err)
		}
		clock = clock.Add(30 * time.Second)
		code := totpCode(t, uri, clock)
		revoke := func() error {
			return tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				if err := az.LockTargetPrincipal(ctx, adm.boot.PrincipalID); err != nil {
					return err
				}
				return az.AdvanceGeneration(ctx, adm.boot.PrincipalID)
			})
		}
		var finished service.LoginResult
		finish := func() error {
			var err error
			finished, err = auth.LoginChallengeTOTP(t.Context(), pending.Challenge.ID, code)
			return err
		}
		revokeErr, finishErr := pendingProofInterleave(t, db, "AdvancePrincipalGeneration", revoke, finish)
		if revokeErr != nil {
			t.Fatal(revokeErr)
		}
		if !errors.Is(finishErr, domain.ErrNotFound) || finished.SessionToken != "" {
			t.Fatalf("old password proof minted a session after revocation: %v", finishErr)
		}
		fresh, err := auth.LocalLogin(t.Context(), "concurrent-proof", adm.password, service.ArtifactBrowser)
		if err != nil || fresh.Challenge == nil {
			t.Fatalf("fresh password proof: %v", err)
		}
		// The denied pending proof must not consume the valid factor step; the
		// same code can complete a fresh proof at the new principal generation.
		finished, err = auth.LoginChallengeTOTP(t.Context(), fresh.Challenge.ID, code)
		if err != nil || finished.SessionToken == "" {
			t.Fatalf("fresh factor completion after revocation: %v", err)
		}
		if err := tx.Read(t.Context(), db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			_, err := az.Authenticate(ctx, finished.SessionToken, clock)
			return err
		}); err != nil {
			t.Fatalf("fresh completion minted an unusable session: %v", err)
		}
	})
}
