package server

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/schema"
)

// An empty json_schema string stays absent: the declaration still has a
// canonical form instead of failing to marshal an empty raw value.
func TestEmptyJSONSchemaStringIsAbsent(t *testing.T) {
	empty := ""
	rule, err := domainRule(apigen.KeyRule{Type: apigen.KeyRuleTypeJson, JsonSchema: &empty})
	if err != nil || rule.JSONSchema != nil {
		t.Fatalf("domainRule = %#v, %v", rule.JSONSchema, err)
	}
	if _, err := schema.Canonical(schema.Declaration{Rule: &rule}); err != nil {
		t.Fatalf("Canonical: %v", err)
	}
}
