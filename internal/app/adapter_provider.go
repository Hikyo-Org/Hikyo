package app

import (
	"errors"
	"net/netip"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/forgejo"
	"github.com/Hikyo-Org/hikyo/internal/adapter/githubactions"
	"github.com/Hikyo-Org/hikyo/internal/adapter/gitlab"
	"github.com/Hikyo-Org/hikyo/internal/adapter/sealedwebhook"
)

type providerConstructor func(adapter.Config, string, []netip.Prefix) (adapter.Module, func(), error)

type adapterModuleFactory struct {
	egressPolicy map[string][]netip.Prefix
	providers    map[adapter.Provider]providerConstructor
}

// sealedWebhookEndpoints is the activated instance-admin receiver registry,
// keyed by exact canonical origin.
type sealedWebhookEndpoints map[string]*sealedwebhook.Endpoint

func deploymentProviderRegistry(endpoints sealedWebhookEndpoints) map[adapter.Provider]providerConstructor {
	return map[adapter.Provider]providerConstructor{
		adapter.ForgejoProvider: func(config adapter.Config, credential string, allowed []netip.Prefix) (adapter.Module, func(), error) {
			client, err := forgejo.NewClient(forgejo.ClientConfig{Origin: config.Origin, Credential: credential, AllowedCIDRs: allowed, Deadline: 15 * time.Second})
			if err != nil {
				return nil, nil, err
			}
			return &forgejo.Module{API: client}, client.Forget, nil
		},
		adapter.GitHubActionsProvider: func(config adapter.Config, credential string, allowed []netip.Prefix) (adapter.Module, func(), error) {
			client, err := githubactions.NewClient(githubactions.ClientConfig{Origin: config.Origin, Credential: credential, AllowedCIDRs: allowed, Deadline: 15 * time.Second})
			if err != nil {
				return nil, nil, err
			}
			return &githubactions.Module{API: client}, client.Forget, nil
		},
		adapter.GitLabProvider: func(config adapter.Config, credential string, allowed []netip.Prefix) (adapter.Module, func(), error) {
			client, err := gitlab.NewClient(gitlab.ClientConfig{Origin: config.Origin, Credential: credential, AllowedCIDRs: allowed, Deadline: 15 * time.Second, SPKIPin: config.SPKIPin, CABundlePEM: config.CABundlePEM})
			if err != nil {
				return nil, nil, err
			}
			return &gitlab.Module{API: client}, client.Forget, nil
		},
		adapter.SealedWebhookProvider: func(config adapter.Config, credential string, allowed []netip.Prefix) (adapter.Module, func(), error) {
			endpoint := endpoints[config.Origin]
			if endpoint == nil {
				return nil, nil, errors.New("sealed-webhook: origin is not an instance-admin configured endpoint")
			}
			if credential == "" {
				return nil, nil, errors.New("sealed-webhook: a binding credential is required")
			}
			clientConfig := endpoint.ClientConfig()
			clientConfig.AllowedCIDRs = allowed
			client, err := sealedwebhook.NewClient(clientConfig)
			if err != nil {
				return nil, nil, err
			}
			return &sealedwebhook.Module{API: client, Endpoint: endpoint, Binding: credential}, client.Forget, nil
		},
	}
}

func newAdapterModuleFactory(egressPolicy map[string][]netip.Prefix, endpoints sealedWebhookEndpoints) *adapterModuleFactory {
	return &adapterModuleFactory{egressPolicy: egressPolicy, providers: deploymentProviderRegistry(endpoints)}
}

func (f *adapterModuleFactory) Build(provider adapter.Provider, config adapter.Config, credential string) (*adapter.ModuleLease, error) {
	if f == nil {
		return nil, errors.New("app: adapter module factory is not configured")
	}
	constructor := f.providers[provider]
	if constructor == nil {
		return nil, errors.New("app: unsupported deployment adapter provider")
	}
	if provider != adapter.GitLabProvider && config.HasProviderOptions() {
		// A pin or trust bundle that a provider would silently ignore is a
		// configuration error, never a no-op.
		return nil, errors.New("app: transport pinning, CA bundles, and personal-token opt-in are GitLab-only")
	}
	allowed := append([]netip.Prefix(nil), f.egressPolicy[config.Origin]...)
	module, release, err := constructor(config, credential, allowed)
	if err != nil {
		if release != nil {
			release()
		}
		return nil, err
	}
	lease, err := adapter.NewModuleLease(module, release)
	if err != nil {
		if release != nil {
			release()
		}
		return nil, err
	}
	return lease, nil
}
