package app

import (
	"errors"
	"net/netip"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/adapter/cloudflare"
	"github.com/Hikyo-Org/hikyo/internal/adapter/forgejo"
	"github.com/Hikyo-Org/hikyo/internal/adapter/githubactions"
	"github.com/Hikyo-Org/hikyo/internal/adapter/gitlab"
	"github.com/Hikyo-Org/hikyo/internal/adapter/sealedwebhook"
	"github.com/Hikyo-Org/hikyo/internal/adapter/vaultkv"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

type providerConstructor func(adapter.Config, string, []netip.Prefix) (adapter.Module, func(), error)

type adapterModuleFactory struct {
	egressPolicy map[string][]netip.Prefix
	providers    map[adapter.Provider]providerConstructor
}

// adapterProviderPolicy is the node operator's provider-level posture. It is
// fixed at startup; no project-scoped request can change it.
type adapterProviderPolicy struct {
	// awsWorkloadIdentity admits AWS descriptors that borrow this node's own
	// AWS identity (HIKYO_ADAPTER_AWS_WORKLOAD_IDENTITY=allow).
	awsWorkloadIdentity bool
}

// sealedWebhookEndpoints is the activated instance-admin receiver registry,
// keyed by exact canonical origin.
type sealedWebhookEndpoints map[string]*sealedwebhook.Endpoint

func deploymentProviderRegistry(endpoints sealedWebhookEndpoints, policy adapterProviderPolicy) map[adapter.Provider]providerConstructor {
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
		adapter.AWSSecretsManagerProvider: func(config adapter.Config, credential string, allowed []netip.Prefix) (adapter.Module, func(), error) {
			client, err := awssm.NewClient(awssm.ClientConfig{Origin: config.Origin, Credential: credential, AllowedCIDRs: allowed, Deadline: 15 * time.Second, WorkloadIdentity: policy.awsWorkloadIdentity})
			if err != nil {
				return nil, nil, awsConstructionError(err)
			}
			return &awssm.Module{API: client}, client.Forget, nil
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
		adapter.VaultKVProvider: func(config adapter.Config, credential string, allowed []netip.Prefix) (adapter.Module, func(), error) {
			// Five sequential requests (mount check, metadata read, mark, CAS
			// write, finalize) plus a possible login must fit the write lease.
			client, err := vaultkv.NewClient(vaultkv.ClientConfig{Origin: config.Origin, Credential: credential, AllowedCIDRs: allowed, Deadline: 10 * time.Second})
			if err != nil {
				return nil, nil, err
			}
			return &vaultkv.Module{API: client}, client.Forget, nil
		},
		adapter.CloudflareProvider: func(config adapter.Config, credential string, allowed []netip.Prefix) (adapter.Module, func(), error) {
			client, err := cloudflare.NewClient(cloudflare.ClientConfig{Origin: config.Origin, Credential: credential, AllowedCIDRs: allowed, Deadline: 15 * time.Second})
			if err != nil {
				return nil, nil, err
			}
			return &cloudflare.Module{API: client}, client.Forget, nil
		},
	}
}

// awsConstructionError classifies an AWS module construction refusal. A bad
// origin or descriptor is the caller's to fix (400 with its safe detail); a
// descriptor that needs the node's workload identity while the node operator
// has not allowed it is also terminal for the worker, never retried.
func awsConstructionError(err error) error {
	var config *awssm.ConfigError
	if !errors.As(err, &config) {
		return err
	}
	if errors.Is(err, awssm.ErrWorkloadIdentityDisabled) {
		return errors.Join(domain.ErrInvalid, adapter.ErrProviderAuth, err)
	}
	return errors.Join(domain.ErrInvalid, err)
}

// egressOrigin is the operator egress-policy key for an adapter origin. The
// policy is keyed by bare https origins; a Vault/OpenBao origin may carry a
// namespace path, which does not change where the adapter dials.
func egressOrigin(provider adapter.Provider, origin string) string {
	if provider != adapter.VaultKVProvider {
		return origin
	}
	parsed, err := vaultkv.ParseOrigin(origin)
	if err != nil {
		return origin
	}
	return parsed.Base
}

func newAdapterModuleFactory(egressPolicy map[string][]netip.Prefix, endpoints sealedWebhookEndpoints, policy adapterProviderPolicy) *adapterModuleFactory {
	return &adapterModuleFactory{egressPolicy: egressPolicy, providers: deploymentProviderRegistry(endpoints, policy)}
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
	allowed := append([]netip.Prefix(nil), f.egressPolicy[egressOrigin(provider, config.Origin)]...)
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
