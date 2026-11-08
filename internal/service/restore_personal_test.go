package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// personalValuesSchemaVersion is a hand-written pin on a migration number; a
// too-high pin would skip the restore purge on archives that hold values, so
// it is asserted against the migration files themselves.
func TestPersonalValuesSchemaVersionMatchesTheMigration(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		entries, err := store.MigrationsFS.ReadDir("migrations/" + engine)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), "_personal_values.sql") {
				continue
			}
			found = true
			var version int64
			if _, err := fmt.Sscanf(e.Name(), "%d_", &version); err != nil {
				t.Fatalf("%s/%s: unparseable migration number: %v", engine, e.Name(), err)
			}
			if version != personalValuesSchemaVersion {
				t.Errorf("%s personal_values migration is %05d but personalValuesSchemaVersion = %d", engine, version, personalValuesSchemaVersion)
			}
		}
		if !found {
			t.Errorf("%s has no personal_values migration to pin against", engine)
		}
	}
}
