package upgrade

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
)

func TestConfigurationMigrationOptionalAbsenceAtVersionFour(t *testing.T) {
	const name = "HIKYO_PAIRING_SPKI_PIN"
	const pin = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	for _, tc := range []struct {
		name                                           string
		optional, present, defaulted, applied, refused bool
	}{
		{name: "absent optional", optional: true},
		{name: "existing optional", optional: true, present: true},
		{name: "missing mandatory", refused: true},
		{name: "contradictory optional default", optional: true, defaulted: true, refused: true},
		{name: "contradictory despite existing value", optional: true, defaulted: true, present: true, refused: true},
		{name: "applied migration cannot repair missing declaration", optional: true, applied: true, refused: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projection := migrationCatalogue(t)
			for i, key := range projection.Catalogue {
				if key.Name == name {
					projection.Catalogue = append(projection.Catalogue[:i], projection.Catalogue[i+1:]...)
					break
				}
			}
			values := make(map[string]string)
			if tc.present {
				values[name] = pin
			}
			migration := runtimeconfig.Migration{Version: 5, Name: name, Optional: tc.optional}
			if tc.defaulted {
				fallback := pin
				migration.Default = &fallback
			}
			version := int64(4)
			if tc.applied {
				version = 5
			}
			added, err := planConfigurationMigrationsWith(projection, values, version, []runtimeconfig.Migration{migration})
			if tc.refused {
				if err == nil || !strings.Contains(err.Error(), name) {
					t.Fatalf("expected named fail-closed refusal, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(added) != 1 || added[0].Name != name || len(projection.Catalogue) != len(runtimeconfig.Catalogue()) {
				t.Fatal("optional setting declaration was not added")
			}
			value, present := values[name]
			if present != tc.present || (present && value != pin) {
				t.Fatal("optional migration fabricated or changed an operator value")
			}
		})
	}
}

func TestPairingMigrationCatalogueDefinesAbsentDefault(t *testing.T) {
	if runtimeconfig.MigrationVersion < 5 {
		t.Fatal("pairing migration version is not recorded")
	}
	var found bool
	for _, migration := range runtimeconfig.Migrations() {
		if migration.Name != "HIKYO_PAIRING_SPKI_PIN" {
			continue
		}
		if found || migration.Version != 5 || !migration.Optional || migration.Default != nil {
			t.Fatal("pairing migration must explicitly preserve absence")
		}
		found = true
	}
	if !found {
		t.Fatal("pairing setting has no release-defined migration")
	}
}
