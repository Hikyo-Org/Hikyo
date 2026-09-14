package config

import (
	"strings"
	"testing"
)

func TestPostgresStorageConfig(t *testing.T) {
	settings := []string{"HIKYO_PG_STORAGE_KUBELET_URL", "https://tokyo:10250", "HIKYO_PG_STORAGE_NAMESPACE", "hikyo-dbugit", "HIKYO_PG_STORAGE_PVC", "data-hikyo-dbugit-db-0", "HIKYO_PG_STORAGE_NODE", "tokyo"}
	for mask := 0; mask < 16; mask++ {
		pairs := []string{"HIKYO_DB", "postgres://u:p@localhost/hikyo"}
		for index := 0; index < 4; index++ {
			if mask&(1<<index) != 0 {
				pairs = append(pairs, settings[index*2:index*2+2]...)
			}
		}
		cfg, warnings, err := Load("server", nil, env(pairs...), environFrom(pairs...))
		if mask != 0 && mask != 15 {
			if err == nil || !strings.Contains(err.Error(), "HIKYO_PG_STORAGE_") {
				t.Fatalf("mask %d should refuse partial config: %v", mask, err)
			}
			continue
		}
		if err != nil || len(warnings) != 0 {
			t.Fatalf("mask %d: err=%v warnings=%v", mask, err, warnings)
		}
		if mask == 15 && cfg.Store.PostgresStorage != (PostgresStorage{KubeletURL: settings[1], Namespace: settings[3], PVC: settings[5], Node: settings[7]}) {
			t.Fatalf("unexpected mapping: %+v", cfg.Store.PostgresStorage)
		}
	}
	for _, tt := range []struct{ name, key, value string }{
		{"high port", "HIKYO_PG_STORAGE_KUBELET_URL", "https://tokyo:70000"},
		{"zero port", "HIKYO_PG_STORAGE_KUBELET_URL", "https://tokyo:0"},
		{"empty port", "HIKYO_PG_STORAGE_KUBELET_URL", "https://tokyo:"},
		{"noncanonical port", "HIKYO_PG_STORAGE_KUBELET_URL", "https://tokyo:010250"},
		{"bad host", "HIKYO_PG_STORAGE_KUBELET_URL", "https://bad_host:10250"},
		{"invalid name", "HIKYO_PG_STORAGE_PVC", "invalid_name"},
		{"space", "HIKYO_PG_STORAGE_NODE", "to kyo"},
		{"long namespace", "HIKYO_PG_STORAGE_NAMESPACE", strings.Repeat("a", 64)},
		{"long PVC", "HIKYO_PG_STORAGE_PVC", strings.Repeat("a", 254)},
		{"sqlite", "HIKYO_DB", "sqlite:/tmp/test.db"},
		{"http", "HIKYO_PG_STORAGE_KUBELET_URL", "http://tokyo:10250"},
		{"path", "HIKYO_PG_STORAGE_KUBELET_URL", "https://tokyo:10250/stats/summary"},
		{"credentials", "HIKYO_PG_STORAGE_KUBELET_URL", "https://user:password@tokyo:10250"},
		{"query", "HIKYO_PG_STORAGE_KUBELET_URL", "https://tokyo:10250?token=abc"},
		{"fragment", "HIKYO_PG_STORAGE_KUBELET_URL", "https://tokyo:10250#abc"},
		{"whitespace", "HIKYO_PG_STORAGE_NODE", " tokyo"},
		{"noncanonical", "HIKYO_PG_STORAGE_KUBELET_URL", "https://TOKYO:10250"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pairs := append([]string{"HIKYO_DB", "postgres://u:p@localhost/hikyo"}, settings...)
			pairs = append(pairs, tt.key, tt.value)
			if _, _, err := Load("server", nil, env(pairs...), nil); err == nil || !strings.Contains(err.Error(), "HIKYO_PG_STORAGE_") {
				t.Fatalf("expected named refusal, got %v", err)
			}
		})
	}
}
