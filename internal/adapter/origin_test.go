package adapter

import "testing"

func TestCanonicalOriginOwnsAllProviderAliases(t *testing.T) {
	for _, tc := range []struct {
		provider  Provider
		raw, want string
	}{
		{ForgejoProvider, "HTTPS://Git.Example.:00443/", "https://git.example"},
		{ForgejoProvider, "https://bücher.example/", "https://xn--bcher-kva.example"},
		{ForgejoProvider, "https://ＧＩＴ.example/", "https://git.example"},
		{ForgejoProvider, "https://[2001:0DB8:0:0::1]:443/", "https://[2001:db8::1]"},
		{ForgejoProvider, "https://[::ffff:192.0.2.1]:443/", "https://192.0.2.1"},
		{ForgejoProvider, "https://Git.Example:08443/", "https://git.example:8443"},
		{GitHubActionsProvider, "", "https://api.github.com"},
		{GitHubActionsProvider, "https://GH.Example:443/api/v3/", "https://gh.example/api/v3"},
		{GitLabProvider, "", "https://gitlab.com"},
		{GitLabProvider, "https://GIT.Example:443/Root/api/v4/", "https://git.example/Root"},
		{GitLabProvider, "https://git.example/%52oot/", "https://git.example/Root"},
		{VaultKVProvider, "https://VAULT.Example:00443/", "https://vault.example"},
		{VaultKVProvider, "https://VAULT.Example:443/Team/child", "https://vault.example/Team/child"},
		// Existing ParseOrigin admits leading slashes, then sends only the
		// trimmed namespace in X-Vault-Namespace and a bare HTTPS base.
		{VaultKVProvider, "https://vault.example//Team", "https://vault.example/Team"},
		{SealedWebhookProvider, "https://Receiver.Example:443/", "https://receiver.example"},
		{CloudflareProvider, "", "https://api.cloudflare.com"},
		{CloudflareProvider, "https://API.CLOUDFLARE.COM:443/", "https://api.cloudflare.com"},
		{AWSSecretsManagerProvider, "https://SECRETSMANAGER-FIPS.US-EAST-1.AMAZONAWS.COM:443/", "https://secretsmanager-fips.us-east-1.amazonaws.com"},
	} {
		t.Run(string(tc.provider)+"/"+tc.raw, func(t *testing.T) {
			got, err := CanonicalOrigin(tc.provider, tc.raw)
			if err != nil || got != tc.want {
				t.Fatalf("canonical(%q)=%q,%v; want%q", tc.raw, got, err, tc.want)
			}
			again, err := CanonicalOrigin(tc.provider, got)
			if err != nil || again != got {
				t.Fatalf("non-idempotent canonical origin=%q,%v", again, err)
			}
		})
	}
}

func TestCanonicalOriginPreservesNamespaceAndRejectsAmbiguousRouting(t *testing.T) {
	for _, tc := range []struct {
		provider Provider
		raw      string
	}{
		{ForgejoProvider, "https://git.example/api"},
		{GitHubActionsProvider, "https://git.example/%61pi/v3"},
		{GitLabProvider, "https://git.example/a%2Fb"},
		{GitLabProvider, "https://git.example/a/%2e%2e/b"},
		{GitLabProvider, "https://git.example//root"},
		{VaultKVProvider, "https://vault.example/Team/"},
		{VaultKVProvider, "https://vault.example/%54eam"},
		{VaultKVProvider, "https://vault.example/root"},
		{CloudflareProvider, "https://api.cloudflare.com.evil"},
		{SealedWebhookProvider, "https://receiver.example?"},
		{AWSSecretsManagerProvider, "https://aws.example/path"},
		{ForgejoProvider, "https://git.example:0"},
		{ForgejoProvider, "https://git.example:65536"},
		{ForgejoProvider, "https://user@git.example"},
		{ForgejoProvider, "https://[fe80::1%25en0]"},
	} {
		t.Run(string(tc.provider)+"/"+tc.raw, func(t *testing.T) {
			if got, err := CanonicalOrigin(tc.provider, tc.raw); err == nil {
				t.Fatalf("ambiguous origin accepted:%q", got)
			}
		})
	}
	a, _ := CanonicalOrigin(VaultKVProvider, "https://vault.example/Team")
	b, _ := CanonicalOrigin(VaultKVProvider, "https://vault.example/team")
	if a == b {
		t.Fatal("Vault namespace case was folded")
	}
}

func TestAWSOriginRegionNeverReroutesOrTrustsCustomAccountClaims(t *testing.T) {
	for _, raw := range []string{"https://secretsmanager.us-east-1.amazonaws.com", "https://secretsmanager-fips.us-east-1.amazonaws.com", "https://vpce-abc.secretsmanager.us-east-1.vpce.amazonaws.com", "https://vpce-abc-secretsmanager-fips.us-east-1.vpce.amazonaws.com"} {
		region, ok := AWSOriginRegion(raw)
		if !ok || region != "us-east-1" {
			t.Fatalf("region(%q)=%q,%v", raw, region, ok)
		}
		canonical, err := CanonicalOrigin(AWSSecretsManagerProvider, raw)
		if err != nil || canonical != raw {
			t.Fatalf("selected AWS endpoint rerouted:%q,%v", canonical, err)
		}
	}
	if region, ok := AWSOriginRegion("https://secretsmanager.cn-north-1.amazonaws.com.cn"); !ok || region != "cn-north-1.cn" {
		t.Fatalf("China partition=%q,%v", region, ok)
	}
	for _, raw := range []string{"https://attacker.example", "https://secretsmanager.us-east-1.amazonaws.com.attacker.example", "https://secretsmanager.cn-north-1.amazonaws.com", "https://secretsmanager.us-east-1.amazonaws.com:8443"} {
		if region, ok := AWSOriginRegion(raw); ok {
			t.Fatalf("untrusted endpoint claims AWS region:%q,%q", raw, region)
		}
	}
}
