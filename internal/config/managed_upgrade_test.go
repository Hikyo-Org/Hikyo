package config

import "testing"

func TestManagedUpgradeSourceIsAliasOnly(t *testing.T) {
	selected, err := ParseManagedBootstrapSources(`{"version":1,"upgrade_source":"next"}`)
	if err != nil || selected.UpgradeSource != "next" {
		t.Fatalf("selection: %+v %v", selected, err)
	}
	for _, raw := range []string{`{"version":1,"upgrade_source":"/run/private"}`, `{"version":1,"upgrade_source":"next","state_directory":"/tmp"}`, `{"version":1,"upgrade_source":null}`, `{"version":1,"upgrade_source":""}`} {
		if _, err := ParseManagedBootstrapSources(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
