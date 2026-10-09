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
		{"no consent", 3600, false}, {"negative", -1, true}, {"over hard ceiling", 28801, true},
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
	zero := int64(0)
	for _, body := range []*apigen.MintDeveloperCredentialJSONRequestBody{
		nil, {}, {ConsentCurrentAndFuture: true, LifetimeSeconds: &zero},
	} {
		api := &API{} // A refused request must not invoke the service.
		_, err := api.MintDeveloperCredential(context.Background(), apigen.MintDeveloperCredentialRequestObject{Body: body})
		if !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("error=%v, want invalid", err)
		}
	}
}

func TestDeveloperCredentialReauthTransportAllowsOmittedDefault(t *testing.T) {
	intent, err := developerCredentialReauthIntent("env_test", []string{"key_test"}, apigen.DeveloperCredentialReauthIntent{LifetimeSeconds: 0, ConsentCurrentAndFuture: true})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := intent.Operation()
	if err != nil || string(operation) != "developer-credential.mint" {
		t.Fatalf("operation=%s error=%v", operation, err)
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
