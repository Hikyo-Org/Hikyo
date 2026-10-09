package server

import (
	"context"
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestDeveloperCredentialReauthTransportRejectsConsentAndInvalidLifetime(t *testing.T) {
	for _, tt := range []struct {
		name    string
		seconds int64
		consent bool
	}{
		{"no consent", 3600, false}, {"zero", 0, true}, {"negative", -1, true}, {"over hard ceiling", 28801, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := developerCredentialReauthIntent("env_test", []string{"key_test"}, apigen.DeveloperCredentialReauthIntent{LifetimeSeconds: tt.seconds, ConsentCurrentAndFuture: apigen.DeveloperCredentialReauthIntentConsentCurrentAndFuture(tt.consent)})
			if !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("error=%v, want invalid", err)
			}
		})
	}
}

func TestDeveloperCredentialMintTransportRefusesExplicitZeroAndMissingConsent(t *testing.T) {
	for _, body := range []*apigen.MintDeveloperCredentialJSONRequestBody{
		nil, {}, {ConsentCurrentAndFuture: true, LifetimeSeconds: 0},
	} {
		api := &API{} // A refused request must not invoke the service.
		_, err := api.MintDeveloperCredential(context.Background(), apigen.MintDeveloperCredentialRequestObject{Body: body})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("error=%v, want invalid", err)
		}
	}
}

func TestDeveloperCredentialCLIReauthTransportRejectsMixedIntent(t *testing.T) {
	api := &API{} // No ceremony may reach Auth through an ambiguous intent.
	developer := &apigen.DeveloperCredentialReauthIntent{LifetimeSeconds: 3600, ConsentCurrentAndFuture: true}
	for _, body := range []apigen.StartCLIReauthJSONRequestBody{
		{Purpose: apigen.CLIReauthStartRequestPurposeDeveloperCredential, Operation: apigen.CLIReauthStartRequestOperationDeveloperCredentialMint, EnvironmentIds: []apigen.ID{"env_a", "env_b"}, DeveloperCredential: developer},
		{Purpose: apigen.CLIReauthStartRequestPurposeReveal, Operation: apigen.CLIReauthStartRequestOperationValueReveal, EnvironmentIds: []apigen.ID{"env_a"}, DeveloperCredential: developer},
		{Purpose: apigen.CLIReauthStartRequestPurposeDeveloperCredential, Operation: apigen.CLIReauthStartRequestOperationValueReveal, EnvironmentIds: []apigen.ID{"env_a"}, DeveloperCredential: developer},
	} {
		_, err := api.StartCLIReauth(context.Background(), apigen.StartCLIReauthRequestObject{Body: &body})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("mixed intent error=%v, want invalid", err)
		}
	}
}
