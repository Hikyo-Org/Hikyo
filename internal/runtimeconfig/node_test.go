package runtimeconfig_test

import (
	"maps"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
)

func nodeProjection() map[string]string {
	return map[string]string{"HIKYO_LISTEN": "127.0.0.1:8080", "HIKYO_OPERATIONAL_LISTEN": "127.0.0.1:8081", "HIKYO_ADMISSION_BUDGET_MIB": "272"}
}

func TestNodeBundleRetainsIndependentExactNodeProjections(t *testing.T) {
	first := nodeProjection()
	second := nodeProjection()
	second["HIKYO_LISTEN"] = "127.0.0.1:9090"
	raw, err := runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{"node-a": first, "node-b": second})
	if err != nil {
		t.Fatal(err)
	}
	first["HIKYO_LISTEN"] = "changed-before-prepare"
	bundle, err := runtimeconfig.Prepare(map[string]string{config.ManagedNodeOverridesKey: raw, "HIKYO_ARGON2_TIME": "4", "HIKYO_UPDATE_CHANNEL": "off"})
	if err != nil {
		t.Fatal(err)
	}
	if !bundle.HasNodeValues() || len(bundle.OwnerValues()) != 1 || bundle.OwnerValues()["HIKYO_ARGON2_TIME"] != "4" {
		t.Fatal("node settings entered owner projection or disappeared")
	}
	a, err := bundle.NodeValues("node-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := bundle.NodeValues("node-b")
	if err != nil {
		t.Fatal(err)
	}
	if a["HIKYO_LISTEN"] != "127.0.0.1:8080" || b["HIKYO_LISTEN"] != "127.0.0.1:9090" {
		t.Fatal("one node inherited another node's configuration")
	}
	a["HIKYO_LISTEN"] = "mutated"
	clear(b)
	again, err := bundle.NodeValues("node-a")
	if err != nil || again["HIKYO_LISTEN"] != "127.0.0.1:8080" {
		t.Fatal("node projection aliases caller state")
	}
	if _, err := bundle.NodeValues("unknown-node"); err == nil {
		t.Fatal("unknown node silently inherited a peer")
	}
	if err := bundle.ValidateNodeMembership([]string{"node-b", "node-a"}); err != nil {
		t.Fatal(err)
	}
	for _, membership := range [][]string{nil, {"node-a"}, {"node-a", "other-node"}, {"node-a", "node-a"}, {"node-a", "node-b", "node-c"}} {
		if err := bundle.ValidateNodeMembership(membership); err == nil {
			t.Fatalf("incorrect admitted membership accepted: %v", membership)
		}
	}
}

func TestPrepareForNodeUsesExactActualNodeBudgetAndEngine(t *testing.T) {
	first := nodeProjection()
	first["HIKYO_ADMISSION_BUDGET_MIB"] = "1040"
	second := nodeProjection()
	raw, err := runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{"node-a": first, "node-b": second})
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{config.ManagedNodeOverridesKey: raw, "HIKYO_ARGON2_MEMORY_KIB": "524288"}
	base := &config.Config{Store: config.Datastore{Engine: config.EngineSQLite, Path: "not-opened.db"}}
	if _, err := runtimeconfig.Prepare(values); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeconfig.PrepareForNode(values, base, "node-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeconfig.PrepareForNode(values, base, "node-b"); err == nil {
		t.Fatal("small node borrowed the larger node's admission capacity")
	}
	if _, err := runtimeconfig.PrepareForNode(values, base, "node-c"); err == nil {
		t.Fatal("missing node activated another node's projection")
	}
	if _, err := runtimeconfig.PrepareForConfig(values, base); err == nil {
		t.Fatal("owner-only consumer accepted unconsumed node settings")
	}
	if _, err := runtimeconfig.PrepareForNode(values, nil, "node-a"); err == nil {
		t.Fatal("missing actual bootstrap context accepted")
	}
	pooled := maps.Clone(first)
	pooled["HIKYO_PG_POOL_MAX"] = "3"
	raw, err = runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{"node-a": pooled})
	if err != nil {
		t.Fatal(err)
	}
	values[config.ManagedNodeOverridesKey] = raw
	if _, err := runtimeconfig.PrepareForNode(values, base, "node-a"); err == nil {
		t.Fatal("SQLite silently accepted a PostgreSQL pool override")
	}
}

func TestOwnerOnlyBundleDoesNotInventManagedNodes(t *testing.T) {
	bundle, err := runtimeconfig.Prepare(nil)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.HasNodeValues() {
		t.Fatal("legacy owner-only bundle invented node configuration")
	}
	if _, err := bundle.NodeValues("local"); err == nil {
		t.Fatal("legacy bundle returned an implicit node projection")
	}
	if err := bundle.ValidateNodeMembership([]string{"local"}); err == nil {
		t.Fatal("legacy bundle claimed managed node membership")
	}
}

func TestNodeOverridesHaveSecretCatalogueClassificationAndMatchingSizeLimit(t *testing.T) {
	if config.MaxManagedNodeValueBytes != schema.MaxValueBytes {
		t.Fatal("node document and project-cell size limits diverged")
	}
	found := false
	for _, key := range runtimeconfig.Catalogue() {
		if key.Name == config.ManagedNodeOverridesKey {
			found = true
			if !key.Secret || key.Classification != schema.Secret {
				t.Fatal("node TLS private keys would enter a public project cell")
			}
		}
	}
	if !found {
		t.Fatal("node document missing from runtime catalogue")
	}
	for _, key := range []string{"HIKYO_TLS_CERT_PEM", "HIKYO_TLS_KEY_PEM", "HIKYO_ADAPTER_EGRESS_POLICY_JSON", "HIKYO_OIDC_EGRESS_POLICY_JSON", "HIKYO_DYNAMIC_EGRESS_POLICY_JSON"} {
		if _, err := runtimeconfig.Prepare(map[string]string{key: "not-a-global-setting"}); err == nil {
			t.Fatalf("%s escaped the exact per-node document", key)
		}
	}
}

func TestDevelopmentNodeControlsRequireActualDevelopmentDeployment(t *testing.T) {
	for _, key := range []string{"HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE", "HIKYO_DEV_SERVICE_BUDGETS_DISABLED", "HIKYO_DEV_ADAPTER_FAKE_PROVIDER"} {
		t.Run(key, func(t *testing.T) {
			node := nodeProjection()
			value := "true"
			if key == "HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE" {
				value = "500"
			}
			node[key] = value
			raw, err := runtimeconfig.EncodeNodeOverrides(map[string]map[string]string{"node-a": node})
			if err != nil {
				t.Fatal(err)
			}
			values := map[string]string{config.ManagedNodeOverridesKey: raw}
			base := &config.Config{Store: config.Datastore{Engine: config.EngineSQLite, Path: "not-opened.db"}}
			if _, err := runtimeconfig.PrepareForNode(values, base, "node-a"); err == nil {
				t.Fatal("production context admitted development override")
			}
			base.Dev = true
			if _, err := runtimeconfig.PrepareForNode(values, base, "node-a"); err != nil {
				t.Fatal(err)
			}
			if _, err := runtimeconfig.Prepare(map[string]string{key: value}); err == nil {
				t.Fatal("development node setting escaped into owner scope")
			}
		})
	}
}
