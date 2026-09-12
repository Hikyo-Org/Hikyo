package service

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/parameters"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestParameterContractVersionCompatibility(t *testing.T) {
	for _, raw := range []string{`{}`, `{"version":0}`, `{"declarations":{},"schemas":{}}`, `{"version":1}`, `{"version":1,"future_metadata":true,"declarations":{"NAME":".*"}}`, `{"version":1,"future_metadata":true,"schemas":{"VALUE":"{}"}}`} {
		if _, err := decodeParameterContract(raw); err != nil {
			t.Errorf("compatible contract %s: %v", raw, err)
		}
	}
	for _, raw := range []string{`{"declarations":{"NAME":".*"}}`, `{"schemas":{"VALUE":"{}"}}`, `{"version":0,"declarations":{"NAME":".*"}}`, `{"version":0,"schemas":{"VALUE":"{}"}}`, `{"version":2}`, `{"version":-1}`, `{"version":"1"}`, `{} {}`, `null`, `[]`} {
		if _, err := decodeParameterContract(raw); err == nil {
			t.Errorf("accepted incompatible contract %s", raw)
		}
	}
}

func TestParameterValidationPreservesLiteralSchemas(t *testing.T) {
	key := store.CatalogueKey{Name: "VALUE", Classification: "config", Declaration: `{"rule":{"type":"enum","members":["${VALUE}"]}}`}
	if err := validateValueWithParameters(key, "${VALUE}", nil); err != nil {
		t.Fatal("legacy literal refused", err)
	}
	if err := validateValueWithParameters(key, "${OTHER}", nil); err == nil {
		t.Fatal("literal bypassed schema")
	}
	if err := validateValueWithParameters(key, "$${VALUE}", map[string]string{"NAME": ".*"}); err != nil {
		t.Fatal("escaped literal schema refused", err)
	}
	if err := validateValueWithParameters(key, "$${OTHER}", map[string]string{"NAME": ".*"}); err == nil {
		t.Fatal("escaped-only schema validation deferred")
	}
}

func TestParameterContractsResolveOnlyCapturedConfig(t *testing.T) {
	for _, tc := range []struct {
		name           string
		contract       parameters.Contract
		classification string
		want           string
	}{
		{"pre-feature literal", parameters.Contract{}, "config", "$${NUMBER}"},
		{"parameterized config", parameters.Contract{Version: 1, Schemas: map[string]string{"VALUE": `{"rule":{"type":"string"}}`}}, "config", "${NUMBER}"},
		{"secret literal", parameters.Contract{Version: 1, Schemas: map[string]string{"VALUE": `{"rule":{"type":"string"}}`}}, "secret", "$${NUMBER}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveConfig(tc.contract, map[string]string{"NUMBER": "123"}, "VALUE", tc.classification, "$${NUMBER}")
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}
