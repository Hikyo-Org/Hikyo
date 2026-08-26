package service

import (
	"testing"
)

func benchAttributes() map[string]any {
	return map[string]any{
		"userName":  "[EMAIL]",
		"active":    true,
		"nickName":  "wren",
		"locale":    "en-GB",
		"timezone":  "Europe/Amsterdam",
		"title":     "Engineer",
		"name":      map[string]any{"givenName": "Wren", "familyName": "De Vries", "formatted": "Wren de Vries"},
		"emails":    []any{map[string]any{"value": "[EMAIL]", "primary": true, "type": "work"}},
		"phoneNbrs": []any{map[string]any{"value": "+31 20 555 0100", "type": "work"}},
		"urn:ietf:params:scim:schemas:extension:enterprise:2.0:User:employeeNumber": "4711",
	}
}

func BenchmarkSCIMAttributesRoundTrip(b *testing.B) {
	raw, err := marshalAttributes(benchAttributes())
	if err != nil {
		b.Fatal(err)
	}
	if raw == "" {
		b.Fatal("empty fixture")
	}
	b.ReportAllocs()
	for b.Loop() {
		out, err := unmarshalAttributes(raw)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := marshalAttributes(out); err != nil {
			b.Fatal(err)
		}
	}
}
