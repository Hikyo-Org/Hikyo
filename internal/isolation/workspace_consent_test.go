package isolation

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestWorkspaceConsentOriginAndLiveness(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		ws := stepUpWorkspace(t, db)
		now := time.Now().UTC()
		ws.Now = func() time.Time { return now }
		approver := service.Bearer(seedSessionFactors(t, db, root, `["password","totp"]`))
		origins := []string{"https://first.example:8443", "https://second.example", "https://xn--bcher-kva.example"}
		for _, origin := range origins {
			if _, err := ws.AddOrigin(ctx, service.LocalPrincipal(root), origin); err != nil {
				t.Fatal(err)
			}
		}
		for i, origin := range origins {
			t.Run(fmt.Sprintf("origin-%d", i), func(t *testing.T) {
				verifier, challenge := pkcePair(fmt.Sprintf("consent-%d", i))
				req := service.HandoffRequest{Origin: origin, RedirectURI: origin + "/workspace/callback", PKCEChallenge: challenge, Purpose: service.HandoffEstablishment}
				started, err := ws.StartHandoff(ctx, req)
				if err != nil {
					t.Fatal(err)
				}
				// A later caller-side request mutation cannot rewrite the stored recipient.
				req.Origin = origins[(i+1)%len(origins)]
				req.RedirectURI = req.Origin + "/workspace/callback"
				view, err := ws.ShowHandoff(ctx, approver, started.State)
				if err != nil {
					t.Fatal(err)
				}
				if view.RequestingOrigin != origin {
					t.Fatalf("recipient=%q want stored%q", view.RequestingOrigin, origin)
				}
				if !view.ExpiresAt.Equal(started.ExpiresAt) {
					t.Fatal("summary expiry differs from transaction")
				}
				code, redirect, err := ws.ApproveHandoff(ctx, approver, started.State)
				if err != nil {
					t.Fatal(err)
				}
				if redirect != origin+"/workspace/callback" {
					t.Fatal("approval did not retain stored callback")
				}
				if _, err := ws.RedeemHandoff(ctx, code, verifier, req.Origin); !errors.Is(err, service.ErrHandoffInvalid) {
					t.Fatalf("foreign redemption origin: %v", err)
				}
				if _, err := ws.RedeemHandoff(ctx, code, verifier, origin); err != nil {
					t.Fatal(err)
				}
				if _, err := ws.ShowHandoff(ctx, approver, started.State); !errors.Is(err, service.ErrHandoffInvalid) {
					t.Fatalf("consumed summary: %v", err)
				}
			})
		}
		_, challenge := pkcePair("expired-consent")
		started, err := ws.StartHandoff(ctx, service.HandoffRequest{Origin: origins[0], RedirectURI: origins[0] + "/workspace/callback", PKCEChallenge: challenge, Purpose: service.HandoffEstablishment})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ws.ShowHandoff(ctx, approver, started.State); err != nil {
			t.Fatal(err)
		}
		now = started.ExpiresAt
		if _, err := ws.ShowHandoff(ctx, approver, started.State); !errors.Is(err, service.ErrHandoffInvalid) {
			t.Fatalf("expired summary: %v", err)
		}
		if _, _, err := ws.ApproveHandoff(ctx, approver, started.State); !errors.Is(err, service.ErrHandoffInvalid) {
			t.Fatalf("expired approval: %v", err)
		}
	})
}

func TestWorkspaceConsentConcurrentApprovalAndConsumption(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		ws := stepUpWorkspace(t, db)
		if _, err := ws.AddOrigin(ctx, service.LocalPrincipal(root), stepUpOrigin); err != nil {
			t.Fatal(err)
		}
		actor := service.Bearer(seedSessionFactors(t, db, root, `["password","totp"]`))
		verifier, challenge := pkcePair("concurrent-consent")
		started, err := ws.StartHandoff(ctx, service.HandoffRequest{Origin: stepUpOrigin, RedirectURI: stepUpOrigin + "/workspace/callback", PKCEChallenge: challenge, Purpose: service.HandoffEstablishment})
		if err != nil {
			t.Fatal(err)
		}
		// Both consent surfaces can read the same still-live summary. Approval and
		// redemption must each settle once even if both humans click together.
		for range 2 {
			if _, err := ws.ShowHandoff(ctx, actor, started.State); err != nil {
				t.Fatal(err)
			}
		}
		type approved struct {
			code string
			err  error
		}
		approvals := make(chan approved, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() {
				<-start
				code, _, err := ws.ApproveHandoff(ctx, actor, started.State)
				approvals <- approved{code, err}
			})
		}
		close(start)
		wg.Wait()
		close(approvals)
		var code string
		success := 0
		for result := range approvals {
			if result.err == nil {
				success++
				code = result.code
			} else if !errors.Is(result.err, service.ErrHandoffInvalid) {
				t.Fatalf("unexpected approval refusal: %v", result.err)
			}
		}
		if success != 1 {
			t.Fatalf("successful approvals=%d", success)
		}
		results := make(chan error, 2)
		start = make(chan struct{})
		for range 2 {
			wg.Go(func() { <-start; _, err := ws.RedeemHandoff(ctx, code, verifier, stepUpOrigin); results <- err })
		}
		close(start)
		wg.Wait()
		close(results)
		success = 0
		for err := range results {
			if err == nil {
				success++
			} else if !errors.Is(err, service.ErrHandoffInvalid) {
				t.Fatalf("unexpected redemption refusal: %v", err)
			}
		}
		if success != 1 {
			t.Fatalf("successful redemptions=%d", success)
		}
		if _, err := ws.ShowHandoff(ctx, actor, started.State); !errors.Is(err, service.ErrHandoffInvalid) {
			t.Fatalf("consumed summary: %v", err)
		}
	})
}
