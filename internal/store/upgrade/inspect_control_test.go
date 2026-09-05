package upgrade

import (
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
)

func TestControlInspectionAllowsPostWriteSchemaAndRefusesPartialControl(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		if _, err := InspectControl(t.Context(), cfg); !errors.Is(err, ErrAbsent) {
			t.Fatalf("fresh control: %v", err)
		}
		if cfg.Engine == releaseidentity.SQLite {
			if _, err := os.Stat(cfg.Path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("control inspection created database")
			}
		}
		state := bootstrap(t, cfg)
		query(t, cfg, `CREATE TABLE candidate_postwrite_probe(id INTEGER PRIMARY KEY)`)
		query(t, cfg, `CREATE TABLE goose_db_version(unrecognized TEXT)`)
		actual, err := InspectControl(t.Context(), cfg)
		if err != nil || !reflect.DeepEqual(state, actual) {
			t.Fatalf("pending inspection rejected domain drift: %v", err)
		}
		query(t, cfg, `DROP TABLE upgrade_pending`)
		if _, err := InspectControl(t.Context(), cfg); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("partial control not refused as corrupt: %v", err)
		}
	})
}
