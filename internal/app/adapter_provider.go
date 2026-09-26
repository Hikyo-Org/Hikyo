package app

import (
	"errors"
	"net/netip"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/adapter/awssm"
	"github.com/Hikyo-Org/hikyo/internal/adapter/forgejo"
	"github.com/Hikyo-Org/hikyo/internal/adapter/githubactions"
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

func deploymentProviderRegistry(policy adapterProviderPolicy) map[adapter.Provider]providerConstructor {
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
		adapter.AWSSecretsManagerProvider: func(config adapter.Config, credential string, allowed []netip.Prefix) (adapter.Module, func(), error) {
			client, err := awssm.NewClient(awssm.ClientConfig{Origin: config.Origin, Credential: credential, AllowedCIDRs: allowed, Deadline: 15 * time.Second, WorkloadIdentity: policy.awsWorkloadIdentity})
			if err != nil {
				return nil, nil, awsConstructionError(err)
			}
			return &awssm.Module{API: client}, client.Forget, nil
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

func newAdapterModuleFactory(egressPolicy map[string][]netip.Prefix, policy adapterProviderPolicy) *adapterModuleFactory {
	return &adapterModuleFactory{egressPolicy: egressPolicy, providers: deploymentProviderRegistry(policy)}
}

func (f *adapterModuleFactory) Build(provider adapter.Provider, config adapter.Config, credential string) (*adapter.ModuleLease, error) {
	if f == nil {
		return nil, errors.New("app: adapter module factory is not configured")
	}
	constructor := f.providers[provider]
	if constructor == nil {
		return nil, errors.New("app: unsupported deployment adapter provider")
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
