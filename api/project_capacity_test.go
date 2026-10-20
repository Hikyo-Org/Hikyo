package api

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestProjectCapacityRequestRequiresExplicitInheritanceAndReviewedPolicy(t *testing.T) {
	doc, err := Doc()
	if err != nil {
		t.Fatal(err)
	}
	schema := doc.Components.Schemas["SetProjectCapacityRequest"].Value
	valid := func() map[string]any {
		return map[string]any{"key_limit_override": nil, "expected_revision": 0, "expected_instance_default": 1000, "expected_instance_maximum": 10000}
	}
	for _, limit := range []any{nil, 1, 2500, 10000} {
		body := valid()
		body["key_limit_override"] = limit
		if err := schema.VisitJSON(body, openapi3.EnableJSONSchema2020()); err != nil {
			t.Fatalf("valid override %v: %v", limit, err)
		}
	}
	for _, field := range []string{"key_limit_override", "expected_revision", "expected_instance_default", "expected_instance_maximum"} {
		body := valid()
		delete(body, field)
		if err := schema.VisitJSON(body, openapi3.EnableJSONSchema2020()); err == nil {
			t.Errorf("omitted %s accepted", field)
		}
	}
	for _, limit := range []any{0, -1, 10001, 1.5, "2500"} {
		body := valid()
		body["key_limit_override"] = limit
		if err := schema.VisitJSON(body, openapi3.EnableJSONSchema2020()); err == nil {
			t.Errorf("invalid override %v accepted", limit)
		}
	}
}
