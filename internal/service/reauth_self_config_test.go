package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func TestSelfConfigIntentCannotWiden(t *testing.T) {
	target := SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: "instance-a", Revision: 2, SchemaVersion: 1, ExpectedGeneration: 4}
	intent, err := NewSelfConfigReauthIntent(target)
	if err != nil {
		t.Fatal(err)
	}
	original, err := intent.bindingFor("")
	if err != nil {
		t.Fatal(err)
	}
	if original.operation != authz.OpSelfConfigApply || original.purpose != PurposeSelfConfig {
		t.Fatalf("wrong binding: %+v", original)
	}
	parsed, ok, err := parseSelfConfigBinding(original.challengeBinding)
	if err != nil || !ok || parsed.keySet != intent.keySet {
		t.Fatalf("cannot round trip exact target: %v", err)
	}
	for _, change := range []func(*SelfConfigReauthTarget){func(v *SelfConfigReauthTarget) { v.OwnerInstanceID = "instance-b" }, func(v *SelfConfigReauthTarget) { v.Revision++ }, func(v *SelfConfigReauthTarget) { v.SchemaVersion++ }, func(v *SelfConfigReauthTarget) { v.ExpectedGeneration++ }, func(v *SelfConfigReauthTarget) { v.ConfirmRestoredCredentials = true }, func(v *SelfConfigReauthTarget) { v.Action = "mail-test"; v.To = "test@example.com" }} {
		other := target
		change(&other)
		different, err := NewSelfConfigReauthIntent(other)
		if err != nil {
			t.Fatal(err)
		}
		if different.keySet == intent.keySet {
			t.Fatal("different decision shares authorization")
		}
	}
	if _, err := NewDisclosureReauthIntent(PurposeSelfConfig, []string{"env-one"}, nil); err == nil {
		t.Fatal("disclosure constructor admitted self config")
	}
	for _, bad := range []SelfConfigReauthTarget{{Action: "apply", OwnerInstanceID: "a", Revision: 1, SchemaVersion: 1, PreviewToken: "unexpected"}, {Action: "adopt", OwnerInstanceID: "a", SchemaVersion: 1}, {Action: "mail-test", OwnerInstanceID: "a", SchemaVersion: 1, Revision: 1}, {Action: "anything", OwnerInstanceID: "a", SchemaVersion: 1}} {
		if _, err := NewSelfConfigReauthIntent(bad); err == nil {
			t.Fatalf("invalid target admitted: %+v", bad)
		}
	}
}

func TestSelfConfigReauthRequiresFreshSupportedFactorAndSingleUse(t *testing.T) {
	for _, test := range []struct {
		name   string
		factor string
		age    time.Duration
		want   error
	}{
		{"totp", "totp", time.Minute, nil},
		{"passkey", "webauthn", time.Minute, nil},
		{"federated_mfa_is_not_local_factor", "oidc", time.Minute, ErrReauthUnitMismatch},
		{"stale_totp", "totp", 5 * time.Minute, ErrReauthWindowExpired},
		{"stale_passkey", "webauthn", 5 * time.Minute, ErrReauthWindowExpired},
		{"future_factor", "totp", -time.Minute, ErrReauthWindowExpired},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, local := selfConfigFixture(t)
			actor, sessionID := selfConfigSession(t, s, local)
			status, err := s.Status(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			target := SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: status.OwnerInstanceID, Revision: 1, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 1}
			intent, err := NewSelfConfigReauthIntent(target)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := intent.bindingFor("")
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Second)
			err = tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				epoch, err := az.CredentialEpoch(ctx)
				if err != nil {
					return err
				}
				return az.OpenReauthWindow(ctx, authz.NewReauthWindow{ID: "raw_factor", SessionID: sessionID, EnvironmentID: intent.environmentID, CeremonyID: "factor-ceremony", FactorClass: test.factor, SingleDecision: true, AuthenticatedAt: now.Add(-test.age), WindowExpiresAt: now.Add(time.Hour), HardExpiresAt: now.Add(time.Hour), CredentialEpoch: epoch, CreatedAt: now, BoundPurpose: string(binding.purpose), BoundOperation: string(binding.operation), BoundKeySet: binding.keySet})
			})
			if err != nil {
				t.Fatal(err)
			}
			consume := func(intent ReauthIntent) error {
				return tx.Write(t.Context(), s.DB, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
					caller, err := az.Authenticate(ctx, actor.bearer, now)
					if err != nil {
						return err
					}
					return s.Auth.ConsumeSelfConfigReauth(ctx, az, caller, intent, now)
				})
			}
			wrongTarget := target
			wrongTarget.OwnerInstanceID = "other-owner"
			wrongOwner, err := NewSelfConfigReauthIntent(wrongTarget)
			if err != nil {
				t.Fatal(err)
			}
			if err := consume(wrongOwner); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("foreign owner accepted: %v", err)
			}
			wrongTarget = target
			wrongTarget.PlanDigest = strings.Repeat("a", 64)
			wrongPlan, err := NewSelfConfigReauthIntent(wrongTarget)
			if err != nil {
				t.Fatal(err)
			}
			if err := consume(wrongPlan); !errors.Is(err, ErrReauthUnitMismatch) {
				t.Fatalf("unreviewed deployment plan accepted: %v", err)
			}
			wrongTarget = target
			wrongTarget.Revision++
			wrongRevision, err := NewSelfConfigReauthIntent(wrongTarget)
			if err != nil {
				t.Fatal(err)
			}
			if err := consume(wrongRevision); !errors.Is(err, ErrReauthUnitMismatch) {
				t.Fatalf("foreign revision accepted: %v", err)
			}
			if err := consume(intent); !errors.Is(err, test.want) {
				t.Fatalf("consume = %v, want %v", err, test.want)
			}
			if test.want == nil {
				if err := consume(intent); !errors.Is(err, ErrReauthWindowSpent) {
					t.Fatalf("factor replay accepted: %v", err)
				}
			}
		})
	}
}
