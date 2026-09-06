package config

import (
	"encoding/json"
	"testing"
)

func TestManagedSingletonTopologyParsing(t *testing.T) {
	for _, raw := range []string{`{"version":1,"topology":{"ha":true,"node_id":"server-a"}}`, `{"version":1,"database_source":"main","topology":{"ha":false,"node_id":"server-b"}}`} {
		value, err := ParseManagedBootstrapSources(raw)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ParseManagedBootstrapSources(string(encoded))
		if err != nil || again != value {
			t.Fatalf("roundtrip: %v", err)
		}
	}
	for _, raw := range []string{`{"version":1,"topology":{"ha":true}}`, `{"version":1,"topology":{"node_id":"server"}}`, `{"version":1,"topology":{"ha":null,"node_id":"server"}}`, `{"version":1,"topology":{"ha":"true","node_id":"server"}}`, `{"version":1,"topology":{"ha":true,"node_id":"server","replicas":2}}`, `{"version":1,"topology":{"ha":true,"node_id":"../other"}}`, `{"version":1,"topology":{"ha":true,"node_id":"server","ha":false}}`} {
		if _, err := ParseManagedBootstrapSources(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	raw, err := json.Marshal(ManagedBootstrapSources{Version: 1, DatabaseSource: "main"})
	if err != nil || string(raw) != `{"version":1,"database_source":"main"}` {
		t.Fatalf("changed source-only protocol %s %v", raw, err)
	}
}
