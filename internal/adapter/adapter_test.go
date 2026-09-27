package adapter

import (
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

func TestCredentialAADPreservesAdapterCredentialContract(t *testing.T) {
	want := crypto.ProjectFieldAAD{
		OrgID: "org_1", ProjectID: "prj_1",
		OwnerTable: "adapters", OwnerRowID: "adp_1", FieldTag: "credential",
	}
	if got := CredentialAAD("org_1", "prj_1", "adp_1"); got != want {
		t.Fatalf("CredentialAAD() = %#v, want %#v", got, want)
	}
}

func TestValidateManifestRefusesLossyForgejoNamesAndValues(t *testing.T) {
	tests := []struct {
		name string
		row  ManifestEntry
	}{
		{"lowercase", ManifestEntry{CanonicalName: "Database_URL", Classification: SecretClassification, Value: "x"}},
		{"leading digit", ManifestEntry{CanonicalName: "1_TOKEN", Classification: SecretClassification, Value: "x"}},
		{"reserved prefix", ManifestEntry{CanonicalName: "FORGEJO_TOKEN", Classification: SecretClassification, Value: "x"}},
		{"variable CI", ManifestEntry{CanonicalName: "CI", Classification: ConfigClassification, Value: "x"}},
		{"carriage return", ManifestEntry{CanonicalName: "TOKEN", Classification: SecretClassification, Value: "a\r\nb"}},
		{"prefixed sentinel", ManifestEntry{CanonicalName: SentinelName, Classification: SecretClassification, Value: "x"}},
		{"too long", ManifestEntry{CanonicalName: strings.Repeat("A", 124), Classification: SecretClassification, Value: "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix := ""
			if tt.name == "prefixed sentinel" || tt.name == "too long" {
				prefix = "PROD_"
			}
			if err := ValidateManifest(prefix, []ManifestEntry{tt.row}); err == nil || !strings.Contains(err.Error(), tt.row.CanonicalName) {
				t.Fatalf("ValidateManifest() = %v, want named refusal", err)
			}
		})
	}

	if err := ValidateManifest("PROD_", []ManifestEntry{
		{KeyID: "key_1", CanonicalName: "CI", Classification: ConfigClassification, Value: "on"},
		{KeyID: "key_2", CanonicalName: "TOKEN", Classification: SecretClassification, Value: "x"},
	}); err != nil {
		t.Fatalf("valid manifest refused: %v", err)
	}
}

func TestWorkflowUsesCanonicalNamesAtRuntime(t *testing.T) {
	got, err := WorkflowForProvider(string(ForgejoProvider), "PROD_", []ManifestEntry{
		{CanonicalName: "DATABASE_URL", Classification: SecretClassification},
		{CanonicalName: "LOG_LEVEL", Classification: ConfigClassification},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "env:\n  DATABASE_URL: ${{ secrets.PROD_DATABASE_URL }}\n  LOG_LEVEL: ${{ vars.PROD_LOG_LEVEL }}\n"
	if got != want {
		t.Fatalf("WorkflowForProvider() =\n%s\nwant:\n%s", got, want)
	}
}

func TestGitHubWorkflowAllowsForeignPrefixesAndCIWhileBanningGitHub(t *testing.T) {
	entries := []ManifestEntry{
		{CanonicalName: "FORGEJO_TOKEN", Classification: SecretClassification},
		{CanonicalName: "GITEA_URL", Classification: ConfigClassification},
		{CanonicalName: "CI", Classification: ConfigClassification},
	}
	workflow, err := WorkflowForProvider("github-actions", "", entries)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"FORGEJO_TOKEN", "GITEA_URL", "CI"} {
		if !strings.Contains(workflow, name+":") {
			t.Fatalf("workflow omitted GitHub-valid name %q:\n%s", name, workflow)
		}
	}
	if _, err := WorkflowForProvider("github-actions", "", []ManifestEntry{{CanonicalName: "GITHUB_TOKEN", Classification: SecretClassification}}); err == nil {
		t.Fatal("GitHub workflow accepted GITHUB_ reserved prefix")
	}
}

func TestGitHubManifestRefusesUnrepresentableNonUTF8ByName(t *testing.T) {
	err := ValidateGitHubActionsManifest("", []ManifestEntry{{CanonicalName: "BINARY", Classification: SecretClassification, Value: string([]byte{0xff, 0xfe})}}, true)
	if err == nil || !strings.Contains(err.Error(), "BINARY") || !strings.Contains(err.Error(), "non-UTF-8") {
		t.Fatalf("ValidateGitHubActionsManifest() = %v, want named byte-exactness refusal", err)
	}
}

func TestIndexLedgerPreservesMissing(t *testing.T) {
	rows := []LedgerEntry{{Surface: Variable, EffectiveName: "MODE", State: Owned, Missing: true}}

	indexed, err := IndexLedger(rows)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := indexed[NewLedgerKey(Variable, "mode")]
	if !ok || !got.Missing || got.State != Owned || got.EffectiveName != "MODE" {
		t.Fatalf("IndexLedger() = %+v, want owned-missing MODE", indexed)
	}
}

func TestLedgerMissingRequiresProviderCustody(t *testing.T) {
	for _, state := range []LedgerState{Reserved, Released} {
		t.Run(string(state), func(t *testing.T) {
			if _, err := IndexLedger([]LedgerEntry{{Surface: Variable, EffectiveName: "MODE", State: state, Missing: true}}); err == nil {
				t.Fatalf("IndexLedger() accepted %s+missing", state)
			}
			if err := ValidateCompletion(Completion{Outcome: OutcomeFailure, State: state, Missing: true}); err == nil {
				t.Fatalf("ValidateCompletion() accepted %s+missing", state)
			}
		})
	}
}

func TestCompletionRequiresClosedOutcomeAndExplicitLedgerDisposition(t *testing.T) {
	valid := []Completion{
		{Outcome: OutcomeSuccess, State: Owned},
		{Outcome: OutcomeFailure, State: Dispatched},
		{Outcome: OutcomeUnknown, State: Dispatched},
		{Outcome: OutcomeFailure, ReleaseLedger: true},
	}
	for _, completion := range valid {
		if err := ValidateCompletion(completion); err != nil {
			t.Fatalf("ValidateCompletion(%+v) = %v", completion, err)
		}
	}

	invalid := []Completion{
		{},
		{Outcome: Outcome("sucess"), State: Owned},
		{Outcome: OutcomeSuccess},
		{Outcome: OutcomeSuccess, State: Owned, ReleaseLedger: true},
		{Outcome: OutcomeFailure, ReleaseLedger: true, Missing: true},
	}
	for _, completion := range invalid {
		if err := ValidateCompletion(completion); err == nil {
			t.Fatalf("ValidateCompletion(%+v) accepted invalid completion", completion)
		}
	}
}

func TestDesiredRowsOrderSentinelsFirst(t *testing.T) {
	rows := DesiredRows("PROD_", []ManifestEntry{
		{KeyID: "key_variable", CanonicalName: "MODE", Classification: ConfigClassification},
		{KeyID: "key_secret", CanonicalName: "TOKEN", Classification: SecretClassification},
	}, true)
	want := []DesiredRow{
		{ManifestEntry: ManifestEntry{Classification: SecretClassification, Value: SentinelName}, Surface: Secret, EffectiveName: "PROD_" + SentinelName},
		{ManifestEntry: ManifestEntry{Classification: ConfigClassification, Value: SentinelName}, Surface: Variable, EffectiveName: "PROD_" + SentinelName},
		{ManifestEntry: ManifestEntry{KeyID: "key_secret", CanonicalName: "TOKEN", Classification: SecretClassification}, Surface: Secret, EffectiveName: "PROD_TOKEN"},
		{ManifestEntry: ManifestEntry{KeyID: "key_variable", CanonicalName: "MODE", Classification: ConfigClassification}, Surface: Variable, EffectiveName: "PROD_MODE"},
	}
	if !slices.Equal(rows, want) {
		t.Fatalf("DesiredRows() = %+v, want %+v", rows, want)
	}
}

func TestProviderKindsAreClosedAndRejectUnknownValues(t *testing.T) {
	want := []Provider{ForgejoProvider, GitHubActionsProvider, SealedWebhookProvider, CloudflareProvider, VaultKVProvider, AWSSecretsManagerProvider, GitLabProvider}
	if got := SupportedProviders(); !slices.Equal(got, want) {
		t.Fatalf("SupportedProviders() = %v, want %v", got, want)
	}
	for _, provider := range want {
		got, err := ParseProvider(string(provider))
		if err != nil || got != provider {
			t.Fatalf("ParseProvider(%q) = %q, %v", provider, got, err)
		}
	}
	for _, raw := range []string{"", "gitlab-ci", "GITLAB", "FORGEJO", "webhook", "sealed_webhook"} {
		if _, err := ParseProvider(raw); err == nil {
			t.Fatalf("ParseProvider(%q) accepted unknown provider", raw)
		}
	}
}

func TestGitLabMaskingRuleIsPinned(t *testing.T) {
	for value, want := range map[string]bool{
		"abcdefgh":             true,
		"A-Za_z0.9~+/=@:":      true,
		"abcdefg":              false,
		"abc defgh":            false,
		"abcdefgh\n":           false,
		"abcdefgh!":            false,
		"abcdefgh$":            false,
		"\u00e9abcdefgh":       false,
		"eyJhbGciOiJIUzI1NiJ9": true,
	} {
		if got := GitLabMaskable(value); got != want {
			t.Errorf("GitLabMaskable(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestValidateGitLabManifestNamesKeysNeverValues(t *testing.T) {
	const secret = "not maskable!"
	for _, tt := range []struct {
		entry ManifestEntry
		want  string
	}{
		{entry: ManifestEntry{CanonicalName: "TOKEN", Classification: SecretClassification, Value: secret}, want: "cannot be masked"},
		{entry: ManifestEntry{CanonicalName: "CI_JOB_TOKEN", Classification: ConfigClassification, Value: "x"}, want: "predefined"},
		{entry: ManifestEntry{CanonicalName: "gitlab_user", Classification: ConfigClassification, Value: "x"}, want: "predefined"},
		{entry: ManifestEntry{CanonicalName: "9LIVES", Classification: ConfigClassification, Value: "x"}, want: "key syntax"},
		{entry: ManifestEntry{CanonicalName: SentinelName, Classification: ConfigClassification, Value: "x"}, want: "sentinel"},
	} {
		err := ValidateGitLabManifest("", []ManifestEntry{tt.entry}, true)
		if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), tt.entry.CanonicalName) || strings.Contains(err.Error(), secret) {
			t.Errorf("%s: err = %v, want %q naming the key without plaintext", tt.entry.CanonicalName, err, tt.want)
		}
	}
	ok := []ManifestEntry{
		{CanonicalName: "mixedCase", Classification: ConfigClassification, Value: "multi\nline $ok"},
		{CanonicalName: "TOKEN", Classification: SecretClassification, Value: "abcdefgh"},
	}
	if err := ValidateGitLabManifest("", ok, true); err != nil {
		t.Fatal(err)
	}
	if err := ValidateGitLabManifest("", []ManifestEntry{{CanonicalName: "A", Classification: ConfigClassification}, {CanonicalName: "a", Classification: ConfigClassification}}, false); err == nil {
		t.Fatal("case-insensitive collision accepted")
	}
	// Name-only validation (plan, workflow rendering) never sees values.
	if err := ValidateGitLabManifest("", []ManifestEntry{{CanonicalName: "TOKEN", Classification: SecretClassification}}, false); err != nil {
		t.Fatal(err)
	}
}

// Every compiled-in provider names the destination kinds it accepts, and no
// provider accepts another's kinds: the seam cannot route a CI target into a
// cloud secret manager or the reverse.
func TestDestinationKindsArePartitionedByProvider(t *testing.T) {
	entries := []ManifestEntry{{KeyID: "key", CanonicalName: "TOKEN", Classification: SecretClassification, Value: "abcdefgh"}}
	cases := map[Provider][]Destination{
		GitLabProvider:            {{Kind: Repository, Owner: "o", Name: "r"}, {Kind: Organization, Owner: "o"}},
		SealedWebhookProvider:     {{Kind: Organization, Owner: "receiver"}},
		CloudflareProvider:        {{Kind: WorkersScript, Owner: "account", Name: "script"}, {Kind: PagesProject, Owner: "account", Name: "project", Environment: "preview"}},
		VaultKVProvider:           {{Kind: Repository, Owner: "secret", Name: "app"}},
		ForgejoProvider:           {{Kind: Repository, Owner: "o", Name: "r"}, {Kind: Organization, Owner: "o"}},
		GitHubActionsProvider:     {{Kind: Repository, Owner: "o", Name: "r"}, {Kind: Organization, Owner: "o"}, {Kind: Environment, Owner: "o", Name: "r", Environment: "e"}},
		AWSSecretsManagerProvider: {{Kind: JSONObject, Owner: "123456789012", Name: "app"}, {Kind: PerKey, Owner: "123456789012"}},
	}
	for provider, destinations := range cases {
		for _, destination := range destinations {
			if err := ValidateTargetManifest(string(provider), destination, "", entries, true); err != nil {
				t.Errorf("%s rejected its own kind %s: %v", provider, destination.Kind, err)
			}
		}
		for other, foreign := range cases {
			if other == provider || (provider != AWSSecretsManagerProvider && other != AWSSecretsManagerProvider) {
				continue
			}
			for _, destination := range foreign {
				if err := ValidateTargetManifest(string(provider), destination, "", entries, true); err == nil {
					t.Errorf("%s accepted %s's destination kind %s", provider, other, destination.Kind)
				}
			}
		}
	}
	if len(cases) != len(SupportedProviders()) {
		t.Fatalf("destination partition covers %d providers, compiled-in set has %d", len(cases), len(SupportedProviders()))
	}
}

func TestVaultKVManifestIsOnePathSegmentPerNameAcrossSurfaces(t *testing.T) {
	ok := []ManifestEntry{
		{CanonicalName: "DATABASE_URL", Classification: SecretClassification, Value: "postgres://x"},
		{CanonicalName: "log.level", Classification: ConfigClassification, Value: ""},
		{CanonicalName: "GITHUB_TOKEN", Classification: SecretClassification, Value: "v"},
	}
	if err := ValidateVaultKVManifest("APP_", ok, true); err != nil {
		t.Fatalf("valid manifest refused: %v", err)
	}
	cases := map[string][]ManifestEntry{
		"reserved for the management sentinel": {{CanonicalName: "managed_by_hikyo", Classification: SecretClassification}},
		"single safe KV path segment":          {{CanonicalName: "a/b", Classification: SecretClassification}},
		"collides case-insensitively": {
			{CanonicalName: "TOKEN", Classification: SecretClassification},
			{CanonicalName: "token", Classification: ConfigClassification},
		},
		"non-UTF-8":            {{CanonicalName: "BIN", Classification: SecretClassification, Value: "\xff"}},
		"KV v2 delivery limit": {{CanonicalName: "BIG", Classification: SecretClassification, Value: strings.Repeat("x", VaultKVValueLimit+1)}},
	}
	for want, entries := range cases {
		err := ValidateVaultKVManifest("", entries, true)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ValidateVaultKVManifest(%v) = %v, want %q", entries[0].CanonicalName, err, want)
		}
	}
	// Names-only validation ignores values so Plan and configuration never
	// need plaintext.
	if err := ValidateVaultKVManifest("", []ManifestEntry{{CanonicalName: "BIN", Classification: SecretClassification, Value: "\xff"}}, false); err != nil {
		t.Fatalf("names-only validation inspected a value: %v", err)
	}
	if err := ValidateProviderManifest(string(VaultKVProvider), "", ok, true); err != nil {
		t.Fatalf("provider dispatch refused vault-kv manifest: %v", err)
	}
}

func TestVaultKVMappingNamesPathsOnly(t *testing.T) {
	got, err := VaultKVMapping("secret", "apps/pay", "P_", []ManifestEntry{
		{CanonicalName: "B", Classification: ConfigClassification, Value: "plaintext-b"},
		{CanonicalName: "A", Classification: SecretClassification, Value: "plaintext-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Vault/OpenBao KV v2: one secret per key, field \"value\".\nenv:\n  A: secret/apps/pay/P_A#value\n  B: secret/apps/pay/P_B#value\n"
	if got != want || strings.Contains(got, "plaintext") {
		t.Fatalf("VaultKVMapping() = %q, want %q", got, want)
	}
}

func TestGitLabManifestRefusesCanonicalAliasCollision(t *testing.T) {
	for _, names := range [][]string{{"A", "P_A"}, {"p_a", "A"}, {"P_MANAGED_BY_HIKYO"}} {
		entries := make([]ManifestEntry, 0, len(names))
		for _, name := range names {
			entries = append(entries, ManifestEntry{CanonicalName: name, Classification: ConfigClassification})
		}
		if err := ValidateGitLabManifest("P_", entries, false); err == nil || !strings.Contains(err.Error(), "canonical alias") {
			t.Fatalf("names %v: %v", names, err)
		}
		if _, err := WorkflowForProvider("gitlab", "P_", entries); err == nil {
			t.Fatalf("workflow accepted aliases %v", names)
		}
	}
	if err := ValidateGitLabManifest("", []ManifestEntry{{CanonicalName: "A", Classification: ConfigClassification}, {CanonicalName: "P_A", Classification: ConfigClassification}}, false); err != nil {
		t.Fatal(err)
	}
}

func TestVaultWorkflowRequiresDestinationMapping(t *testing.T) {
	entries := []ManifestEntry{{CanonicalName: "TOKEN", Classification: SecretClassification}}
	if got, err := WorkflowForProvider("vault-kv", "P_", entries); err == nil || got != "" {
		t.Fatalf("generic workflow escaped Vault boundary: %q %v", got, err)
	}
	got, err := VaultKVMapping("secret", "apps/pay", "P_", entries)
	if err != nil || !strings.Contains(got, "secret/apps/pay/P_TOKEN#value") || strings.Contains(got, "${{") {
		t.Fatalf("destination mapping=%q err=%v", got, err)
	}
}
