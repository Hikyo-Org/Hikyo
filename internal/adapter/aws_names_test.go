package adapter

import (
	"slices"
	"strings"
	"testing"
)

func TestAWSManifestRejectsDuplicateSecretAndConfigName(t *testing.T) {
	for _, kind := range []DestinationKind{JSONObject, PerKey} {
		for _, values := range []bool{false, true} {
			for _, reverse := range []bool{false, true} {
				destination := Destination{Kind: kind, Owner: "123456789012", Name: "prod/app"}
				if kind == PerKey {
					destination.Name = "prod/"
				}
				entries := []ManifestEntry{
					{KeyID: "key_secret", CanonicalName: "TOKEN", Classification: SecretClassification, Value: "secret-value-canary"},
					{KeyID: "key_config", CanonicalName: "TOKEN", Classification: ConfigClassification, Value: "config-value-canary"},
				}
				if reverse {
					slices.Reverse(entries)
				}
				err := ValidateAWSSecretsManagerManifest(destination, "APP_", entries, values)
				if err == nil || !strings.Contains(err.Error(), `effective name "APP_TOKEN" collides case-insensitively`) {
					t.Fatalf("kind=%s values=%v reverse=%v: duplicate name was not refused", kind, values, reverse)
				}
				for _, entry := range entries {
					if strings.Contains(err.Error(), entry.Value) {
						t.Fatal("collision diagnostic disclosed a value")
					}
				}
			}
		}
	}
}

func TestAWSJSONDocumentRejectsDuplicateMember(t *testing.T) {
	entries := []ManifestEntry{
		{KeyID: "key_secret", CanonicalName: "TOKEN", Classification: SecretClassification, Value: "secret-value-canary"},
		{KeyID: "key_config", CanonicalName: "TOKEN", Classification: ConfigClassification, Value: "config-value-canary"},
	}
	for range 2 {
		document, err := AWSJSONDocument("APP_", entries)
		if document != "" || err == nil || err.Error() != `aws-secrets-manager: TOKEN: json member "APP_TOKEN" is mapped twice` {
			t.Fatal("duplicate JSON member did not refuse the entire document with a names-only diagnostic")
		}
		slices.Reverse(entries)
	}
}
