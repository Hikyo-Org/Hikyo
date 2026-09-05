package api_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/getkin/kin-openapi/openapi3"
)

// Each newly open schema must accept unknown strings in the runtime schema
// validator and preserve them through the generated server model.
func TestPreFreezeOpenEnums(t *testing.T) {
	doc, err := api.Doc()
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct{ name, property, wire string }{
		{"AdapterProvider", "", `"future-provider"`},
		{"SamlProviderWarning", "code", `{"code":"future-warning","effective_at":"2026-09-01T00:00:00Z","message":"Server diagnostic","severity":"error"}`},
		{"IdentityProviderKind", "", `"future-kind"`},
		{"OidcStartRequest", "purpose", `{"purpose":"future-purpose"}`},
		{"SamlStartRequest", "purpose", `{"purpose":"future-purpose"}`},
		{"GrantOrigin", "kind", `{"kind":"future-origin","subject":"holder"}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			open := doc.Components.Schemas[fixture.name].Value
			if fixture.property != "" {
				open = open.Properties[fixture.property].Value
			}
			if _, declared := open.Extensions[api.ExtOpenEnum]; !declared || len(open.Enum) != 0 {
				t.Fatal("schema must declare x-extensible-enum without a closed enum")
			}
			var value interface{}
			if err := json.Unmarshal([]byte(fixture.wire), &value); err != nil {
				t.Fatal(err)
			}
			if err := doc.Components.Schemas[fixture.name].Value.VisitJSON(value, openapi3.EnableJSONSchema2020()); err != nil {
				t.Fatalf("runtime schema rejected unknown value: %v", err)
			}
		})
	}
	checkOpenEnumRoundtrip(t, apigen.AdapterProvider("future-provider"), `"future-provider"`)
	checkOpenEnumRoundtrip(t, apigen.SamlProviderWarning{Code: "future-warning", EffectiveAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Message: "Server diagnostic", Severity: apigen.SamlProviderWarningSeverityError}, `{"code":"future-warning","effective_at":"2026-09-01T00:00:00Z","message":"Server diagnostic","severity":"error"}`)
	checkOpenEnumRoundtrip(t, apigen.IdentityProviderKind("future-kind"), `"future-kind"`)
	checkOpenEnumRoundtrip(t, apigen.OidcStartRequest{Purpose: "future-purpose"}, `{"purpose":"future-purpose"}`)
	checkOpenEnumRoundtrip(t, apigen.SamlStartRequest{Purpose: "future-purpose"}, `{"purpose":"future-purpose"}`)
	checkOpenEnumRoundtrip(t, apigen.GrantOrigin{Kind: "future-origin", Subject: "holder"}, `{"kind":"future-origin","subject":"holder"}`)
}

func checkOpenEnumRoundtrip[T comparable](t *testing.T, want T, wire string) {
	t.Helper()
	var got T
	if err := json.Unmarshal([]byte(wire), &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("unknown value lost: got %+v, want %+v", got, want)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != wire {
		t.Fatalf("round trip = %s, want %s", encoded, wire)
	}
}

// Dynamic provider kinds are the explicitly closed PostgreSQL-only 1.0
// contract. Making unrelated response discriminators open must not widen it.
func TestDynamicProviderKindRemainsClosed(t *testing.T) {
	doc, err := api.Doc()
	if err != nil {
		t.Fatal(err)
	}
	schema := doc.Components.Schemas["DynamicProviderKind"].Value
	if err := schema.VisitJSON("postgres", openapi3.EnableJSONSchema2020()); err != nil {
		t.Fatal(err)
	}
	if err := schema.VisitJSON("future-provider", openapi3.EnableJSONSchema2020()); err == nil {
		t.Fatal("unknown dynamic provider accepted")
	}
}
