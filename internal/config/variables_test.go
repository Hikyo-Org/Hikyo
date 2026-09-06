package config

import (
	"slices"
	"testing"
)

func TestVariableInventoryCoversEveryRecognizedEnvironmentKey(t *testing.T) {
	descriptors := VariableInventory()
	if len(descriptors) != len(knownEnv) {
		t.Fatalf("inventory has %d keys; parser recognizes %d", len(descriptors), len(knownEnv))
	}
	seen := make(map[string]bool, len(descriptors))
	previous := ""
	for _, descriptor := range descriptors {
		if !knownEnv[descriptor.Key] || seen[descriptor.Key] {
			t.Errorf("unknown or duplicate inventory key: %s", descriptor.Key)
		}
		if descriptor.Key <= previous {
			t.Errorf("inventory is not key-sorted at %s", descriptor.Key)
		}
		previous = descriptor.Key
		seen[descriptor.Key] = true
		if !slices.Contains([]VariableAudience{VariableServer, VariableClient, VariableCommand, VariableRetired}, descriptor.Audience) {
			t.Errorf("%s has no recognized audience", descriptor.Key)
		}
		if !slices.Contains([]VariableScope{VariableOwner, VariableNode}, descriptor.Scope) {
			t.Errorf("%s has no recognized scope", descriptor.Key)
		}
		if !slices.Contains([]VariableActivation{VariableLive, VariableComponent, VariableAppReload, VariableBootstrap, VariableDeployment, VariableNone}, descriptor.Activation) {
			t.Errorf("%s has no recognized activation lifecycle", descriptor.Key)
		}
		if !slices.Contains([]VariableImport{VariableValue, VariableFileContent, VariableExternal}, descriptor.Import) {
			t.Errorf("%s has no recognized import boundary", descriptor.Key)
		}
	}
	for key := range knownEnv {
		if !seen[key] {
			t.Errorf("recognized key %s has no inventory decision", key)
		}
	}
}

func TestVariableInventoryCannotBeMutatedByCallers(t *testing.T) {
	first := VariableInventory()
	original := first[0]
	first[0].Key = "HIKYO_INJECTED"
	first[0].Import = VariableFileContent
	first[0].FileContentKey = "HIKYO_ROOT_KEY"
	if got := VariableInventory()[0]; got != original {
		t.Fatalf("caller mutated inventory: %+v", got)
	}
}

func TestVariableInventoryKeepsOperatorAndClientSecretsOutsideGenericImports(t *testing.T) {
	for _, descriptor := range VariableInventory() {
		switch {
		case descriptor.Audience != VariableServer:
			if descriptor.Import != VariableExternal || descriptor.Activation != VariableNone {
				t.Errorf("%s permits a server import or activation", descriptor.Key)
			}
		case descriptor.Activation == VariableBootstrap || descriptor.Activation == VariableDeployment:
			if descriptor.Import != VariableExternal {
				t.Errorf("%s permits a generic import across the bootstrap/deployment boundary", descriptor.Key)
			}
		}
		switch descriptor.Key {
		case "HIKYO_ROOT_KEY", "HIKYO_TOKEN":
			if !descriptor.Secret || descriptor.Import != VariableExternal || descriptor.FileContentKey != "" {
				t.Errorf("%s does not preserve the raw credential boundary", descriptor.Key)
			}
		case "HIKYO_ROOT_KEY_FILE", "HIKYO_NEW_ROOT_KEY_FILE":
			if descriptor.Import != VariableExternal || descriptor.FileContentKey != "" {
				t.Errorf("%s permits generic root-key file loading", descriptor.Key)
			}
		case "HIKYO_DB", "HIKYO_DIRECTORY_PROXY", "HIKYO_MAIL_PASSWORD", "HIKYO_MAIL_PASSWORD_FILE":
			if !descriptor.Secret {
				t.Errorf("%s does not classify credential-bearing contents as secret", descriptor.Key)
			}
		}
	}
}

func TestVariableInventoryFileImportsMatchManagedSeedAliases(t *testing.T) {
	aliases := map[string]string{
		"HIKYO_MAIL_PASSWORD_FILE":         "HIKYO_MAIL_PASSWORD",
		"HIKYO_MAIL_CA_FILE":               "HIKYO_MAIL_CA_PEM",
		"HIKYO_TLS_CERT_FILE":              "HIKYO_TLS_CERT_PEM",
		"HIKYO_TLS_KEY_FILE":               "HIKYO_TLS_KEY_PEM",
		"HIKYO_ADAPTER_EGRESS_POLICY_FILE": "HIKYO_ADAPTER_EGRESS_POLICY_JSON",
		"HIKYO_OIDC_EGRESS_POLICY_FILE":    "HIKYO_OIDC_EGRESS_POLICY_JSON",
		"HIKYO_DYNAMIC_EGRESS_POLICY_FILE": "HIKYO_DYNAMIC_EGRESS_POLICY_JSON",
	}
	for _, descriptor := range VariableInventory() {
		expected, isFileImport := aliases[descriptor.Key]
		if (descriptor.Import == VariableFileContent) != isFileImport || descriptor.FileContentKey != expected {
			t.Errorf("%s: file alias %q, import %q", descriptor.Key, descriptor.FileContentKey, descriptor.Import)
		}
	}
}

// These inputs are consumed by app.databaseGate during server Boot, or by the
// server-owned root rotation source. Their command use does not make them
// command-only settings. OperatorInstance is consumed only by RunBackup.
func TestVariableInventoryIncludesServerUpgradeAndRotationInputs(t *testing.T) {
	bootInputs := map[string]bool{
		"HIKYO_UPGRADE_BACKUP":                 true,
		"HIKYO_UPGRADE_BUNDLE":                 true,
		"HIKYO_UPGRADE_EVIDENCE":               true,
		"HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED": true,
		"HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY":    true,
		"HIKYO_UPGRADE_STATE_DIR":              true,
		"HIKYO_UPGRADE_TARGET_MANIFEST":        true,
		"HIKYO_NEW_ROOT_KEY_FILE":              true,
	}
	for _, descriptor := range VariableInventory() {
		if bootInputs[descriptor.Key] {
			if descriptor.Audience != VariableServer || descriptor.Activation != VariableBootstrap || descriptor.Import != VariableExternal {
				t.Errorf("%s excluded a server bootstrap/rotation input: %+v", descriptor.Key, descriptor)
			}
			delete(bootInputs, descriptor.Key)
		}
		if descriptor.Key == "HIKYO_UPGRADE_OPERATOR_INSTANCE" && (descriptor.Audience != VariableCommand || descriptor.Activation != VariableNone) {
			t.Errorf("backup command-only operator instance pin misclassified: %+v", descriptor)
		}
	}
	if len(bootInputs) != 0 {
		t.Errorf("server bootstrap/rotation inputs missing: %v", bootInputs)
	}
}

func TestVariableInventoryDistinguishesFilePathsFromSecretContents(t *testing.T) {
	for _, descriptor := range VariableInventory() {
		switch descriptor.Key {
		case "HIKYO_ROOT_KEY_FILE", "HIKYO_NEW_ROOT_KEY_FILE":
			if descriptor.Secret || !descriptor.ReferencedContentSecret || descriptor.Import != VariableExternal {
				t.Errorf("%s confuses external path metadata with secret file bytes: %+v", descriptor.Key, descriptor)
			}
		case "HIKYO_MAIL_PASSWORD_FILE", "HIKYO_TLS_KEY_FILE":
			if !descriptor.Secret || !descriptor.ReferencedContentSecret || descriptor.Import != VariableFileContent {
				t.Errorf("password content import lost its secret classification: %+v", descriptor)
			}
		}
	}
}

func TestVariableInventoryKeepsIngressTrustNodeScoped(t *testing.T) {
	for _, descriptor := range VariableInventory() {
		if descriptor.Key == "HIKYO_TRUSTED_PROXY_CIDRS" {
			if descriptor.Scope != VariableNode {
				t.Fatal("ingress trust classified as shared owner policy")
			}
			if slices.Contains(ManagedOwnerKeys(), descriptor.Key) {
				t.Fatal("node trust admitted as owner setting")
			}
			return
		}
	}
	t.Fatal("ingress policy absent from inventory")
}
