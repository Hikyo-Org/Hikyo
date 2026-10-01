package adapter

import (
	"context"
	"fmt"
	"sync"
)

// Provider is the closed set of compiled-in deployment providers.
type Provider string

const (
	ForgejoProvider           Provider = "forgejo"
	GitHubActionsProvider     Provider = "github-actions"
	SealedWebhookProvider     Provider = "sealed-webhook"
	CloudflareProvider        Provider = "cloudflare"
	VaultKVProvider           Provider = "vault-kv"
	AWSSecretsManagerProvider Provider = "aws-secrets-manager"
	GitLabProvider            Provider = "gitlab"
)

var supportedProviders = [...]Provider{ForgejoProvider, GitHubActionsProvider, SealedWebhookProvider, CloudflareProvider, VaultKVProvider, AWSSecretsManagerProvider, GitLabProvider}

// SupportedProviders returns the complete compiled-in provider set.
func SupportedProviders() []Provider {
	return append([]Provider(nil), supportedProviders[:]...)
}

// ParseProvider rejects missing and unknown persisted provider identities.
// API defaults must resolve to an explicit provider before this boundary.
func ParseProvider(raw string) (Provider, error) {
	provider := Provider(raw)
	for _, supported := range supportedProviders {
		if provider == supported {
			return provider, nil
		}
	}
	return "", fmt.Errorf("adapter: unknown provider %q", raw)
}

// ModuleLease binds one constructed module to its idempotent cleanup.
type ModuleLease struct {
	Module  Module
	release func(context.Context)
}

// NewModuleLease transfers module cleanup ownership to one release-once value.
func NewModuleLease(module Module, release func()) (*ModuleLease, error) {
	var cleanup func(context.Context)
	if release != nil {
		cleanup = func(context.Context) { release() }
	}
	return NewModuleLeaseWithContext(module, cleanup)
}

// NewModuleLeaseWithContext transfers cleanup ownership without detaching
// provider work from the request or worker attempt that constructed it.
func NewModuleLeaseWithContext(module Module, release func(context.Context)) (*ModuleLease, error) {
	if module == nil {
		return nil, fmt.Errorf("adapter: provider factory returned no module")
	}
	if release == nil {
		release = func(context.Context) {}
	}
	var once sync.Once
	return &ModuleLease{Module: module, release: func(ctx context.Context) {
		once.Do(func() { release(ctx) })
	}}, nil
}

// Release drops provider resources exactly once.
func (l *ModuleLease) Release() {
	l.ReleaseContext(context.Background())
}

// ReleaseContext drops resources exactly once, even when ctx is canceled.
// Context-aware provider cleanup may not revive canceled network work.
func (l *ModuleLease) ReleaseContext(ctx context.Context) {
	if l != nil && l.release != nil {
		l.release(ctx)
	}
}

// ModuleFactory is the shared worker/service construction seam.
type ModuleFactory func(Provider, Config, string) (*ModuleLease, error)
