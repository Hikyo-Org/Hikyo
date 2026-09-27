package adapter

import (
	"strings"
	"testing"
)

func TestValidateCloudflareManifestRefusesUnrepresentableRows(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		row    ManifestEntry
	}{
		{"leading digit", "", ManifestEntry{CanonicalName: "1_TOKEN", Classification: SecretClassification, Value: "x"}},
		{"dash", "", ManifestEntry{CanonicalName: "API-TOKEN", Classification: SecretClassification, Value: "x"}},
		{"sentinel", "PROD_", ManifestEntry{CanonicalName: SentinelName, Classification: SecretClassification, Value: "x"}},
		{"sentinel case", "", ManifestEntry{CanonicalName: "managed_by_hikyo", Classification: ConfigClassification, Value: "x"}},
		{"too long", "P_", ManifestEntry{CanonicalName: strings.Repeat("A", 63), Classification: SecretClassification, Value: "x"}},
		{"too large", "", ManifestEntry{CanonicalName: "BIG", Classification: SecretClassification, Value: strings.Repeat("x", CloudflareValueLimit+1)}},
		{"non utf8", "", ManifestEntry{CanonicalName: "BIN", Classification: SecretClassification, Value: "\xff"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCloudflareManifest(tt.prefix, []ManifestEntry{tt.row}, true); err == nil || !strings.Contains(err.Error(), tt.row.CanonicalName) {
				t.Fatalf("ValidateCloudflareManifest() = %v, want named refusal", err)
			}
		})
	}
	if err := ValidateCloudflareManifest("", []ManifestEntry{
		{CanonicalName: "Token", Classification: SecretClassification},
		{CanonicalName: "TOKEN", Classification: ConfigClassification},
	}, false); err == nil || !strings.Contains(err.Error(), "case-insensitively") {
		t.Fatalf("case collision = %v, want refusal", err)
	}
	if err := ValidateProviderManifest(string(CloudflareProvider), "prod_", []ManifestEntry{
		{CanonicalName: "Database_URL", Classification: SecretClassification, Value: "postgres://x"},
		{CanonicalName: "LOG_LEVEL", Classification: ConfigClassification, Value: ""},
	}, true); err != nil {
		t.Fatalf("valid Cloudflare manifest refused: %v", err)
	}
}

func TestCloudflareBindingsRenderNamesOnly(t *testing.T) {
	got, err := WorkflowForProvider(string(CloudflareProvider), "PROD_", []ManifestEntry{
		{CanonicalName: "LOG_LEVEL", Classification: ConfigClassification, Value: "debug"},
		{CanonicalName: "DATABASE_URL", Classification: SecretClassification, Value: "postgres://secret"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "# Cloudflare secret_text bindings, read as env.<binding>\nDATABASE_URL: env.PROD_DATABASE_URL\nLOG_LEVEL: env.PROD_LOG_LEVEL\n"
	if got != want {
		t.Fatalf("bindings = %q, want %q", got, want)
	}
}
