package boundary

import "testing"

func TestForbiddenEdgesMatchPackageBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, importer, imported string
		forbidden                bool
	}{
		{"config remains a leaf", "/internal/config", "/internal/domain", true},
		{"config child remains a leaf", "/internal/config/child", "/internal/domain", true},
		{"crypto remains a leaf", "/internal/crypto", "/internal/domain", true},
		{"crypto child remains a leaf", "/internal/crypto/backup", "/internal/store", true},
		{"configrollout may use config", "/internal/configrollout", "/internal/config", false},
		{"configrollout may use domain", "/internal/configrollout", "/internal/domain", false},
		{"handler authz remains forbidden", "/internal/server", "/internal/authz", true},
		{"handler store child remains forbidden", "/internal/server/child", "/internal/store/child", true},
		{"generated postgres queries remain forbidden", "/internal/service", "/internal/store/pggen", true},
		{"generated sqlite child remains forbidden", "/internal/service", "/internal/store/sqlitegen/child", true},
		{"similarly named import is distinct", "/internal/service", "/internal/store/pggenextra", false},
		{"cmd trailing slash retains children", "/cmd/hikyo", "/internal/store", true},
		{"cmd trailing slash retains exact package", "/cmd", "/internal/store", true},
		{"similarly named command is distinct", "/cmdline", "/internal/store", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			matched := false
			for _, rule := range forbidden {
				if packageWithin(module+tc.importer, rule.importer) && packageWithin(module+tc.imported, rule.imports) {
					matched = true
				}
			}
			if matched != tc.forbidden {
				t.Fatalf("forbidden edge %s -> %s = %t, want %t", tc.importer, tc.imported, matched, tc.forbidden)
			}
		})
	}
}
