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
	want := []Provider{ForgejoProvider, GitHubActionsProvider}
	if got := SupportedProviders(); !slices.Equal(got, want) {
		t.Fatalf("SupportedProviders() = %v, want %v", got, want)
	}
	for _, provider := range want {
		got, err := ParseProvider(string(provider))
		if err != nil || got != provider {
			t.Fatalf("ParseProvider(%q) = %q, %v", provider, got, err)
		}
	}
	for _, raw := range []string{"", "gitlab", "FORGEJO"} {
		if _, err := ParseProvider(raw); err == nil {
			t.Fatalf("ParseProvider(%q) accepted unknown provider", raw)
		}
	}
}
