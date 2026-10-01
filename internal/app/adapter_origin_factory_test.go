package app

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func TestAdapterFactoryHoldsLegacyOriginsBeforeProviderConstruction(t *testing.T) {
	for _, tc := range []struct {
		provider adapter.Provider
		origin   string
	}{
		{adapter.ForgejoProvider, "https://GIT.example:443/"},
		{adapter.GitHubActionsProvider, ""},
		{adapter.GitLabProvider, ""},
		{adapter.CloudflareProvider, ""},
		{adapter.VaultKVProvider, "https://vault.example:443/Team"},
		{adapter.AWSSecretsManagerProvider, "https://secretsmanager.us-east-1.amazonaws.com/"},
		{adapter.SealedWebhookProvider, "https://receiver.example/"},
	} {
		t.Run(string(tc.provider), func(t *testing.T) {
			calls := 0
			factory := newAdapterModuleFactory(nil, nil, adapterProviderPolicy{})
			factory.providers[tc.provider] = func(adapter.Config, string, []netip.Prefix, []netip.Prefix) (adapter.Module, func(context.Context), error) {
				calls++
				return stubProviderModule{}, nil, nil
			}
			if _, err := factory.Build(tc.provider, adapter.Config{Origin: tc.origin}, "private-credential"); !errors.Is(err, adapter.ErrOperatorReview) {
				t.Fatalf("legacy factory=%v; want review", err)
			}
			if calls != 0 {
				t.Fatal("legacy origin reached provider constructor")
			}
		})
	}
}
