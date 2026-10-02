package service

import "testing"

func TestSCIMSubjectAttributesAreCaseInsensitiveAndUnambiguous(t *testing.T) {
	for _, tc := range []struct {
		name       string
		body       map[string]any
		path, want string
	}{
		{"core", map[string]any{"externalId": "subject"}, "EXTERNALID", "subject"},
		{"extension", map[string]any{"URN:EXAMPLE:USER": map[string]any{"Employee": map[string]any{"ID": "subject"}}}, "urn:example:user:employee.id", "subject"},
		{"ambiguous-core", map[string]any{"externalId": "one", "EXTERNALID": "two"}, "externalId", ""},
		{"ambiguous-extension", map[string]any{"urn:example:user": map[string]any{"id": "one"}, "URN:EXAMPLE:USER": map[string]any{"id": "two"}}, "urn:example:user:id", ""},
		{"not-string", map[string]any{"EXTERNALID": 42}, "externalId", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractAttribute(tc.body, tc.path); got != tc.want {
				t.Fatalf("subject = %q, want %q", got, tc.want)
			}
		})
	}
}
