package config

import (
	"encoding/base64"
	"slices"
	"strings"
	"testing"
)

func TestPairingSPKIPinManagedOwnerAuthority(t *testing.T) {
	pin := base64.StdEncoding.EncodeToString(make([]byte, 32))
	cfg, _, err := Load("server", []string{"--dev"}, env("HIKYO_PAIRING_SPKI_PIN", pin), nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PairingSPKIPin != pin || cfg.ManagedOwnerValues()["HIKYO_PAIRING_SPKI_PIN"] != pin || !slices.Contains(ManagedOwnerKeys(), "HIKYO_PAIRING_SPKI_PIN") {
		t.Fatal("pairing pin did not pass through owner configuration")
	}
	for _, descriptor := range VariableInventory() {
		if descriptor.Key == "HIKYO_PAIRING_SPKI_PIN" && (descriptor.Secret || descriptor.Scope != VariableOwner || descriptor.Activation != VariableAppReload || descriptor.Import != VariableValue) {
			t.Fatal("pairing pin inventory ownership is incorrect")
		}
	}
	updated, err := ApplyManagedOwnerValues(cfg, cfg.ManagedOwnerValues())
	if err != nil || updated.PairingSPKIPin != pin {
		t.Fatalf("managed pairing pin round trip: %v", err)
	}
	values := cfg.ManagedOwnerValues()
	delete(values, "HIKYO_PAIRING_SPKI_PIN")
	cleared, err := ApplyManagedOwnerValues(cfg, values)
	if err != nil || cleared.PairingSPKIPin != "" || cfg.PairingSPKIPin != pin {
		t.Fatalf("managed removal retained stale pin or mutated base: %v", err)
	}
	bootstrap, _, err := LoadBootstrap("server", []string{"--dev"}, env("HIKYO_PAIRING_SPKI_PIN", pin), nil)
	if err != nil || bootstrap.PairingSPKIPin != "" || bootstrap.ManagedInputs["HIKYO_PAIRING_SPKI_PIN"] != pin {
		t.Fatalf("bootstrap did not defer pairing pin to managed owner: %v", err)
	}
}

func TestPairingSPKIPinRejectsNonCanonicalValues(t *testing.T) {
	valid := base64.StdEncoding.EncodeToString(make([]byte, 32))
	for _, pin := range []string{"do-not-echo", valid + "\n", strings.TrimSuffix(valid, "="), base64.StdEncoding.EncodeToString(make([]byte, 31)), "sha256/" + valid} {
		_, _, err := Load("server", []string{"--dev"}, env("HIKYO_PAIRING_SPKI_PIN", pin), nil)
		if err == nil || strings.Contains(err.Error(), pin) {
			t.Fatal("invalid fingerprint accepted or echoed")
		}
		if err := ValidateManagedOwnerValues(map[string]string{"HIKYO_PAIRING_SPKI_PIN": pin}); err == nil {
			t.Fatal("managed owner accepted invalid fingerprint")
		}
	}
}
