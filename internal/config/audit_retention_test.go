package config

import "testing"

func TestAuditRetentionConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, access, security string
		valid                  bool
	}{
		{"defaults", "", "", true}, {"bounded", "30", "90", true}, {"ceiling", "3650", "3650", true},
		{"zero", "0", "365", false}, {"negative", "-1", "365", false}, {"unlimited", "90", "unlimited", false},
		{"inverted", "100", "90", false}, {"overflow", "90", "3651", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _, err := Load("server", []string{"--dev"}, env("HIKYO_AUDIT_ACCESS_RETAIN_DAYS", tc.access, "HIKYO_AUDIT_SECURITY_RETAIN_DAYS", tc.security), nil)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if tc.name == "defaults" && (cfg.AuditAccessRetainDays != 90 || cfg.AuditSecurityRetainDays != 365) {
				t.Fatalf("unexpected defaults: %d/%d", cfg.AuditAccessRetainDays, cfg.AuditSecurityRetainDays)
			}
		})
	}
}
