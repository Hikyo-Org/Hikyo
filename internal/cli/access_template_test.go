package cli

import "testing"

func TestInstanceAdminTemplateMapsToTheV1WireName(t *testing.T) {
	for _, in := range []string{"instance-admin", "operator"} {
		if got := templateWire(in); got != "operator" {
			t.Fatalf("templateWire(%q) = %q, want operator", in, got)
		}
	}
	if got := templateWire("admin"); got != "admin" {
		t.Fatalf("templateWire(admin) = %q", got)
	}
	if got := templateLabel(templateWire("operator")); got != "instance-admin" {
		t.Fatalf("templateLabel(operator) = %q, want instance-admin", got)
	}
}
