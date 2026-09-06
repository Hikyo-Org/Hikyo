package config

import (
	"encoding/json"
	"testing"
)

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

func TestManagedBootstrapRoundTripPreservesUpgradeAndTopology(t *testing.T) {
	raw := `{"version":1,"database_source":"database","root_source":"root","upgrade_source":"upgrade","topology":{"ha":true,"node_id":"replacement"}}`
	selected, err := ParseManagedBootstrapSources(raw)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := ParseManagedBootstrapSources(string(encoded))
	if err != nil || roundTrip != selected || roundTrip.UpgradeSource != "upgrade" || roundTrip.Topology.NodeID != "replacement" || !roundTrip.Topology.HA {
		t.Fatalf("combined bootstrap roundtrip lost selection: %+v %v", roundTrip, err)
	}
}
