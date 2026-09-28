package app

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/adapter/forgejo"
	"github.com/Hikyo-Org/hikyo/internal/adapter/githubactions"
	"github.com/Hikyo-Org/hikyo/internal/adapter/vaultkv"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestAdapterModuleFactoryRegistryIsTotal(t *testing.T) {
	registry := deploymentProviderRegistry(nil, adapterProviderPolicy{})
	if len(registry) != len(adapter.SupportedProviders()) {
		t.Fatalf("registry entries = %d, supported providers = %d", len(registry), len(adapter.SupportedProviders()))
	}
	for _, provider := range adapter.SupportedProviders() {
		if registry[provider] == nil {
			t.Fatalf("provider %q has no construction entry", provider)
		}
	}
}

func TestAdapterModuleFactoryDispatchesCompiledInProviders(t *testing.T) {
	factory := newAdapterModuleFactory(nil, nil, adapterProviderPolicy{})
	forgejoLease, err := factory.Build(adapter.ForgejoProvider, adapter.Config{Origin: "https://forgejo.example"}, "scoped-token")
	if err != nil {
		t.Fatal(err)
	}
	defer forgejoLease.Release()
	if _, ok := forgejoLease.Module.(*forgejo.Module); !ok {
		t.Fatalf("forgejo module = %T", forgejoLease.Module)
	}

	githubLease, err := factory.Build(adapter.GitHubActionsProvider, adapter.Config{Origin: "https://api.github.com"}, "github_pat_fine")
	if err != nil {
		t.Fatal(err)
	}
	defer githubLease.Release()
	if _, ok := githubLease.Module.(*githubactions.Module); !ok {
		t.Fatalf("github module = %T", githubLease.Module)
	}

	vaultLease, err := factory.Build(adapter.VaultKVProvider, adapter.Config{Origin: "https://vault.example:8200/team-a"}, "hvs.static")
	if err != nil {
		t.Fatal(err)
	}
	defer vaultLease.Release()
	if _, ok := vaultLease.Module.(*vaultkv.Module); !ok {
		t.Fatalf("vault module = %T", vaultLease.Module)
	}
	awsLease, err := factory.Build(adapter.AWSSecretsManagerProvider, adapter.Config{Origin: "https://secretsmanager.eu-west-1.amazonaws.com"}, `{"mode":"static","access_key_id":"AKIAHIKYOTEST0000001","secret_access_key":"fixture"}`)
	if err != nil {
		t.Fatal(err)
	}
	defer awsLease.Release()
	if _, ok := awsLease.Module.(*awssm.Module); !ok {
		t.Fatalf("aws module = %T", awsLease.Module)
	}
}

// The node operator's workload-identity opt-in is the only switch that lets
// an adapter borrow the server's AWS identity; the factory enforces it.
func TestAWSWorkloadIdentityFollowsNodePolicy(t *testing.T) {
	ambient := `{"mode":"ambient"}`
	origin := adapter.Config{Origin: "https://secretsmanager.eu-west-1.amazonaws.com"}
	_, err := newAdapterModuleFactory(nil, nil, adapterProviderPolicy{}).Build(adapter.AWSSecretsManagerProvider, origin, ambient)
	// A caller-correctable 400 on the API, and terminal (never retried) for
	// the outbox worker.
	if !errors.Is(err, awssm.ErrWorkloadIdentityDisabled) || !errors.Is(err, domain.ErrInvalid) || !errors.Is(err, adapter.ErrProviderAuth) {
		t.Fatalf("ambient without node opt-in = %v", err)
	}
	_, err = newAdapterModuleFactory(nil, nil, adapterProviderPolicy{}).Build(adapter.AWSSecretsManagerProvider, origin, `{"mode":"static","access_key_id":"short"}`)
	var detail interface{ SafeDetail() string }
	if !errors.Is(err, domain.ErrInvalid) || errors.Is(err, adapter.ErrProviderAuth) || !errors.As(err, &detail) || detail.SafeDetail() == "" {
		t.Fatalf("bad descriptor = %v", err)
	}
	lease, err := newAdapterModuleFactory(nil, nil, adapterProviderPolicy{awsWorkloadIdentity: true}).Build(adapter.AWSSecretsManagerProvider, origin, ambient)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
}

func TestAdapterModuleFactoryReleasesPartialConstructionOnce(t *testing.T) {
	wantErr := errors.New("partial construction")
	releases := 0
	factory := &adapterModuleFactory{
		providers: map[adapter.Provider]providerConstructor{
			adapter.ForgejoProvider: func(adapter.Config, string, []netip.Prefix) (adapter.Module, func(), error) {
				return nil, func() { releases++ }, wantErr
			},
		},
	}
	if _, err := factory.Build(adapter.ForgejoProvider, adapter.Config{Origin: "https://forgejo.example"}, "scoped-token"); !errors.Is(err, wantErr) {
		t.Fatalf("Build() = %v, want %v", err, wantErr)
	}
	if releases != 1 {
		t.Fatalf("partial construction releases = %d, want 1", releases)
	}
}

func TestAdapterModuleLeaseReleasesSuccessOnce(t *testing.T) {
	releases := 0
	factory := &adapterModuleFactory{
		providers: map[adapter.Provider]providerConstructor{
			adapter.ForgejoProvider: func(adapter.Config, string, []netip.Prefix) (adapter.Module, func(), error) {
				return stubProviderModule{}, func() { releases++ }, nil
			},
		},
	}
	lease, err := factory.Build(adapter.ForgejoProvider, adapter.Config{Origin: "https://forgejo.example"}, "scoped-token")
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	lease.Release()
	if releases != 1 {
		t.Fatalf("successful construction releases = %d, want 1", releases)
	}
}

func TestDeploymentModuleRefusesClassicGitHubPAT(t *testing.T) {
	_, err := newAdapterModuleFactory(nil, nil, adapterProviderPolicy{}).Build(adapter.GitHubActionsProvider, adapter.Config{Origin: "https://api.github.com"}, "ghp_classic")
	if err == nil || !strings.Contains(err.Error(), "classic") {
		t.Fatalf("deploymentModule() = %v, want named classic PAT refusal", err)
	}
}

func TestDeploymentModuleNeverInfersProviderFromCredential(t *testing.T) {
	_, err := newAdapterModuleFactory(nil, nil, adapterProviderPolicy{}).Build(adapter.Provider(""), adapter.Config{Origin: "https://api.github.com"}, "github_pat_fine")
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("deploymentModule() = %v, want missing persisted provider refusal", err)
	}
}

type stubProviderModule struct{}

func (stubProviderModule) ValidateConfig(adapter.Config) error { return nil }
func (stubProviderModule) TestConnection(context.Context, adapter.ConnectionRequest) (adapter.Connection, error) {
	return adapter.Connection{}, nil
}
func (stubProviderModule) Plan(context.Context, adapter.PlanRequest) (adapter.Plan, error) {
	return adapter.Plan{}, nil
}
func (stubProviderModule) Sync(context.Context, adapter.SyncRequest, adapter.Journal) (adapter.SyncResult, error) {
	return adapter.SyncResult{}, nil
}

func TestAdapterEgressOriginDropsVaultNamespaceOnly(t *testing.T) {
	cases := []struct {
		provider adapter.Provider
		origin   string
		want     string
	}{
		{adapter.VaultKVProvider, "https://vault.example:8200/team-a/child", "https://vault.example:8200"},
		{adapter.VaultKVProvider, "https://vault.example:8200", "https://vault.example:8200"},
		{adapter.GitHubActionsProvider, "https://ghes.example/api/v3", "https://ghes.example/api/v3"},
		{adapter.ForgejoProvider, "https://forgejo.example", "https://forgejo.example"},
	}
	for _, tc := range cases {
		if got := egressOrigin(tc.provider, tc.origin); got != tc.want {
			t.Errorf("egressOrigin(%s, %s) = %s, want %s", tc.provider, tc.origin, got, tc.want)
		}
	}
}
