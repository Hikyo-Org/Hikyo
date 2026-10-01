package isolation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestPasswordReplacementRetiresPendingLoginProof(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		adm := bootstrapAdmin(t, db, adminOpts{
			username: "pending-proof", displayName: "Pending proof", password: "original password long enough",
		})
		auth, ctx := adm.auth, t.Context()
		clock := time.Now().UTC()
		auth.Now = func() time.Time { return clock }
		login, err := auth.LocalLogin(ctx, "pending-proof", adm.password, service.ArtifactCLI)
		if err != nil {
			t.Fatal(err)
		}
		uri, err := auth.EnrolTOTPStart(ctx, login.SessionToken, adm.password)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, clock)); err != nil {
			t.Fatal(err)
		}
		pending, err := auth.LocalLogin(ctx, "pending-proof", adm.password, service.ArtifactBrowser)
		if err != nil || pending.Challenge == nil {
			t.Fatalf("pending challenge: %+v, %v", pending, err)
		}
		reset, err := auth.BreakGlassResetCredential(ctx, string(adm.boot.PrincipalID), "terminal")
		if err != nil {
			t.Fatal(err)
		}
		const replacement = "replacement password long enough"
		if err := auth.EstablishCredential(ctx, reset.Authority, replacement); err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(30 * time.Second)
		if _, err := auth.LoginChallengeTOTP(ctx, pending.Challenge.ID, totpCode(t, uri, clock)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("old password proof survived replacement: %v", err)
		}
		// The factor is still valid: refusing the old password proof must not
		// accidentally disable the account's legitimate new login.
		fresh, err := auth.LocalLogin(ctx, "pending-proof", replacement, service.ArtifactBrowser)
		if err != nil || fresh.Challenge == nil {
			t.Fatalf("fresh challenge: %+v, %v", fresh, err)
		}
		completed, err := auth.LoginChallengeTOTP(ctx, fresh.Challenge.ID, totpCode(t, uri, clock))
		if err != nil || completed.SessionToken == "" {
			t.Fatalf("fresh completion: %+v, %v", completed, err)
		}
	})
}

func TestGenerationRevocationRetiresApprovedWorkspaceProof(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		ws := stepUpWorkspace(t, db)
		if _, err := ws.AddOrigin(ctx, service.LocalPrincipal(root), stepUpOrigin); err != nil {
			t.Fatal(err)
		}
		actor := service.Bearer(seedSessionFactors(t, db, root, `["password","totp"]`))
		verifier, challenge := pkcePair("revoked-workspace-proof")
		started, err := ws.StartHandoff(ctx, service.HandoffRequest{
			Origin: stepUpOrigin, RedirectURI: stepUpOrigin + "/workspace/callback", PKCEChallenge: challenge, Purpose: service.HandoffEstablishment,
		})
		if err != nil {
			t.Fatal(err)
		}
		code, _, err := ws.ApproveHandoff(ctx, actor, started.State)
		if err != nil {
			t.Fatal(err)
		}
		if err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			return az.AdvanceGeneration(ctx, root)
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := ws.RedeemHandoff(ctx, code, verifier, stepUpOrigin); !errors.Is(err, service.ErrHandoffInvalid) {
			t.Fatalf("approved proof survived generation revocation: %v", err)
		}
		freshActor := service.Bearer(seedSessionFactors(t, db, root, `["password","totp"]`))
		freshVerifier, freshChallenge := pkcePair("fresh-workspace-proof")
		fresh, err := ws.StartHandoff(ctx, service.HandoffRequest{
			Origin: stepUpOrigin, RedirectURI: stepUpOrigin + "/workspace/callback", PKCEChallenge: freshChallenge, Purpose: service.HandoffEstablishment,
		})
		if err != nil {
			t.Fatal(err)
		}
		freshCode, _, err := ws.ApproveHandoff(ctx, freshActor, fresh.State)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ws.RedeemHandoff(ctx, freshCode, freshVerifier, stepUpOrigin); err != nil {
			t.Fatal(err)
		}
	})
}
