package isolation

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestSelfConfigReauthExactDecision(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := bootstrapFactorAdmin(t, db)
		auth := fixture.auth
		clock := time.Now().UTC()
		auth.Now = func() time.Time { return clock }
		login, err := auth.LocalLogin(t.Context(), "factor-admin", fixture.password, service.ArtifactCLI)
		if err != nil {
			t.Fatal(err)
		}
		uri, err := auth.EnrolTOTPStart(t.Context(), login.SessionToken, fixture.password)
		if err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(30 * time.Second)
		confirmed, err := auth.EnrolTOTPConfirm(t.Context(), login.SessionToken, totpCode(t, uri, clock))
		if err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(30 * time.Second)
		elevated, err := auth.StepUpTOTP(t.Context(), confirmed.SessionToken, totpCode(t, uri, clock))
		if err != nil {
			t.Fatal(err)
		}
		var owner string
		err = tx.Read(t.Context(), db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			var e error
			owner, e = az.InstanceIdentity(ctx)
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		target := service.SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: owner, Revision: 3, SchemaVersion: 1, ExpectedGeneration: 2}
		intent, err := service.NewSelfConfigReauthIntent(target)
		if err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(30 * time.Second)
		opened, err := auth.ReauthTOTP(t.Context(), elevated.SessionToken, intent, totpCode(t, uri, clock))
		if err != nil {
			t.Fatal(err)
		}
		if !opened.SingleDecision {
			t.Fatal("configuration decision opened a sliding window")
		}
		consume := func(candidate service.ReauthIntent) error {
			return tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				caller, err := az.Authenticate(ctx, opened.SessionToken, clock)
				if err != nil {
					return err
				}
				return auth.ConsumeSelfConfigReauth(ctx, az, caller, candidate, clock)
			})
		}
		for _, change := range []func(*service.SelfConfigReauthTarget){func(v *service.SelfConfigReauthTarget) { v.Revision++ }, func(v *service.SelfConfigReauthTarget) { v.ExpectedGeneration++ }, func(v *service.SelfConfigReauthTarget) { v.ConfirmRestoredCredentials = true }, func(v *service.SelfConfigReauthTarget) { v.SchemaVersion++ }, func(v *service.SelfConfigReauthTarget) { v.Action = "mail-test"; v.To = "admin@example.com" }} {
			other := target
			change(&other)
			candidate, err := service.NewSelfConfigReauthIntent(other)
			if err != nil {
				t.Fatal(err)
			}
			if err := consume(candidate); !errors.Is(err, service.ErrReauthUnitMismatch) {
				t.Fatalf("changed decision accepted: %v", err)
			}
		}
		other := target
		other.OwnerInstanceID = "another-owner"
		foreign, err := service.NewSelfConfigReauthIntent(other)
		if err != nil {
			t.Fatal(err)
		}
		if err := consume(foreign); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("wrong owner: %v", err)
		}
		if err := consume(intent); err != nil {
			t.Fatal(err)
		}
		if err := consume(intent); !errors.Is(err, service.ErrReauthWindowSpent) {
			t.Fatalf("replayed decision: %v", err)
		}
	})
}

func TestSelfConfigCLIReauthPreservesExactDecision(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := bootstrapFactorAdmin(t, db)
		auth := fixture.auth
		clock := time.Now().UTC()
		auth.Now = func() time.Time { return clock }
		login, err := auth.LocalLogin(t.Context(), "factor-admin", fixture.password, service.ArtifactCLI)
		if err != nil {
			t.Fatal(err)
		}
		uri, err := auth.EnrolTOTPStart(t.Context(), login.SessionToken, fixture.password)
		if err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(30 * time.Second)
		confirmed, err := auth.EnrolTOTPConfirm(t.Context(), login.SessionToken, totpCode(t, uri, clock))
		if err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(30 * time.Second)
		cli, err := auth.StepUpTOTP(t.Context(), confirmed.SessionToken, totpCode(t, uri, clock))
		if err != nil {
			t.Fatal(err)
		}
		var owner string
		err = tx.Read(t.Context(), db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			var e error
			owner, e = az.InstanceIdentity(ctx)
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		target := service.SelfConfigReauthTarget{Action: "mail-test", OwnerInstanceID: owner, Revision: 3, SchemaVersion: 1, ExpectedGeneration: 2, To: "admin@example.com"}
		intent, err := service.NewSelfConfigReauthIntent(target)
		if err != nil {
			t.Fatal(err)
		}
		verifierBytes := sha256.Sum256([]byte("self config exact CLI decision"))
		verifier := base64.RawURLEncoding.EncodeToString(verifierBytes[:])
		challengeBytes := sha256.Sum256([]byte(verifier))
		start, err := auth.StartCLIReauth(t.Context(), cli.SessionToken, intent, base64.RawURLEncoding.EncodeToString(challengeBytes[:]), "http://127.0.0.1:40123/callback")
		if err != nil {
			t.Fatal(err)
		}
		browser, err := auth.LocalLogin(t.Context(), "factor-admin", fixture.password, service.ArtifactBrowser)
		if err != nil {
			t.Fatal(err)
		}
		csrf := browser.CSRFToken
		clock = clock.Add(30 * time.Second)
		browser, err = auth.StepUpTOTP(t.Context(), browser.SessionToken, totpCode(t, uri, clock))
		if err != nil {
			t.Fatal(err)
		}
		transaction, err := auth.CLIReauthTransaction(t.Context(), service.Bearer(browser.SessionToken), start.State)
		if err != nil {
			t.Fatal(err)
		}
		if transaction.SelfConfig == nil || *transaction.SelfConfig != target || len(transaction.Environments) != 0 {
			t.Fatalf("handoff lost the exact instance target: %+v", transaction)
		}
		clock = clock.Add(30 * time.Second)
		opened, err := auth.ReauthTOTP(t.Context(), browser.SessionToken, intent, totpCode(t, uri, clock))
		if err != nil {
			t.Fatal(err)
		}
		if err := auth.VerifyBrowserCSRF(t.Context(), opened.SessionToken, csrf); err != nil {
			t.Fatalf("browser rotation lost its synchronizer token: %v", err)
		}
		approved, err := auth.ApproveCLIReauth(t.Context(), service.Bearer(opened.SessionToken), start.State)
		if err != nil {
			t.Fatal(err)
		}
		redeemed, err := auth.RedeemCLIReauth(t.Context(), approved.Code, verifier)
		if err != nil {
			t.Fatal(err)
		}
		consume := func(candidate service.ReauthIntent) error {
			return tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				caller, err := az.Authenticate(ctx, redeemed.SessionToken, clock)
				if err != nil {
					return err
				}
				return auth.ConsumeSelfConfigReauth(ctx, az, caller, candidate, clock)
			})
		}
		other := target
		other.To = "another@example.com"
		changed, err := service.NewSelfConfigReauthIntent(other)
		if err != nil {
			t.Fatal(err)
		}
		if err := consume(changed); !errors.Is(err, service.ErrReauthUnitMismatch) {
			t.Fatalf("recipient retargeted: %v", err)
		}
		if err := consume(intent); err != nil {
			t.Fatal(err)
		}
		if err := consume(intent); !errors.Is(err, service.ErrReauthWindowSpent) {
			t.Fatalf("CLI decision replay: %v", err)
		}
	})
}
