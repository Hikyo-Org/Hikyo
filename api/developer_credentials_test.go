package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// The closed wire allowlist prevents a bounded developer delegation from
// reaching metadata, pins, reconciliation or account-management operations.
func TestDeveloperCredentialArtifactOnlyAdmittedToCurrentDelivery(t *testing.T) {
	loadOnce.Do(load)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	count := 0
	for _, op := range operations {
		if !op.AdmitsArtifact(ArtifactDeveloperCredential) {
			continue
		}
		count++
		if op.ID != "fetchDelivery" || op.AuthzOp != "delivery.fetch" {
			t.Fatalf("developer credential admitted to %s (%s)", op.ID, op.AuthzOp)
		}
	}
	if count != 1 {
		t.Fatalf("developer artifact admitted to %d routes, want exactly current delivery", count)
	}
}

func TestDeveloperCredentialCeremoniesSupportFullCatalogue(t *testing.T) {
	doc, err := Doc()
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{501, 1000, 1001} {
		keys := make([]any, count)
		for i := range keys {
			keys[i] = fmt.Sprintf("key_01989abc-def0-7123-8123-%012d", i)
		}
		target := map[string]any{"lifetime_seconds": float64(3600), "consent_current_and_future": true}
		for schema, body := range map[string]map[string]any{
			"MintDeveloperCredentialRequest":       {"key_ids": keys, "consent_current_and_future": true, "lifetime_seconds": float64(3600)},
			"TotpDeveloperCredentialReauthRequest": {"purpose": "developer-credential", "environment_id": "env_01989abc-def0-7123-8123-000000000001", "key_ids": keys, "developer_credential": target, "code": "123456"},
			"CLIReauthStartRequest":                {"purpose": "developer-credential", "operation": "developer-credential.mint", "environment_ids": []any{"env_01989abc-def0-7123-8123-000000000001"}, "key_ids": keys, "developer_credential": target, "pkce_challenge": strings.Repeat("a", 43), "redirect_uri": "http://127.0.0.1:1234/callback"},
			"WebauthnReauthStartRequest":           {"operation": "developer-credential", "environment_id": "env_01989abc-def0-7123-8123-000000000001", "key_ids": keys, "developer_credential": target},
		} {
			err := doc.Components.Schemas[schema].Value.VisitJSON(body, openapi3.EnableJSONSchema2020())
			if (err == nil) != (count <= 1000) {
				t.Errorf("%s count=%d validation=%v", schema, count, err)
			}
			if schema == "CLIReauthStartRequest" && count == 501 {
				body["purpose"], body["operation"] = "reveal", "value.reveal"
				delete(body, "developer_credential")
				if err := doc.Components.Schemas[schema].Value.VisitJSON(body, openapi3.EnableJSONSchema2020()); err == nil {
					t.Error("ordinary CLI disclosure accepted more than its existing 500-key limit")
				}
			}
		}
	}
}

func TestDeveloperCredentialWireRequiresConfirmedPositiveLifetime(t *testing.T) {
	doc, err := Doc()
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range []string{"MintDeveloperCredentialRequest", "DeveloperCredentialReauthIntent"} {
		for _, seconds := range []float64{0, -1, 28801} {
			body := map[string]any{"consent_current_and_future": true, "lifetime_seconds": seconds}
			if schema == "MintDeveloperCredentialRequest" {
				body["key_ids"] = []any{}
			}
			if err := doc.Components.Schemas[schema].Value.VisitJSON(body, openapi3.EnableJSONSchema2020()); err == nil {
				t.Errorf("%s accepted lifetime %v", schema, seconds)
			}
		}
		body := map[string]any{"consent_current_and_future": true}
		if schema == "MintDeveloperCredentialRequest" {
			body["key_ids"] = []any{}
		}
		if err := doc.Components.Schemas[schema].Value.VisitJSON(body, openapi3.EnableJSONSchema2020()); err == nil {
			t.Errorf("%s accepted omitted lifetime", schema)
		}
		body["lifetime_seconds"] = float64(3600)
		if err := doc.Components.Schemas[schema].Value.VisitJSON(body, openapi3.EnableJSONSchema2020()); err != nil {
			t.Errorf("%s rejected concrete lifetime: %v", schema, err)
		}
	}
}
