package upgradegate

import (
	"net/url"
	"os"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/jackc/pgx/v5"
)

func TestPostgresSignedGateCanonicalRendering(t *testing.T) {
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("HIKYO_TEST_POSTGRES_DSN required")
	}
	baseline, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parameters := baseline.Query()
	parameters.Set("timezone", "UTC")
	baseline.RawQuery = parameters.Encode()
	t.Setenv("HIKYO_TEST_POSTGRES_DSN", baseline.String())
	// The authorized test bundle declares the independently built UTC catalog.
	request, claim, verify := signedFreshGate(t, releaseidentity.Postgres)
	changed, err := url.Parse(request.Store.DSN)
	if err != nil {
		t.Fatal(err)
	}
	parameters = changed.Query()
	parameters.Set("timezone", "Europe/Amsterdam")
	parameters.Set("DateStyle", "SQL,DMY")
	changed.RawQuery = parameters.Encode()
	request.Store.DSN = changed.String()
	migrated, err := run(t.Context(), request, claim, upgrade.Production, verify)
	if err != nil {
		t.Fatalf("fresh non-UTC signed admission: %v", err)
	}
	if !migrated.SchemaOnly {
		t.Fatal("migration admitted serving")
	}
	request.Mode = Boot
	booted, err := run(t.Context(), request, claim, upgrade.Production, verify)
	if err != nil || !booted.Admission.Valid() {
		t.Fatalf("non-UTC boot: %v", err)
	}
	parameters.Set("timezone", "America/New_York")
	changed.RawQuery = parameters.Encode()
	request.Store.DSN = changed.String()
	restarted, err := run(t.Context(), request, claim, upgrade.Production, verify)
	if err != nil || !restarted.Admission.Valid() || restarted.State.SchemaDigest != booted.State.SchemaDigest {
		t.Fatalf("existing signed catalog in another timezone: %v", err)
	}
	if _, err := run(t.Context(), request, claim, upgrade.LocalDevelopment, verify); err == nil {
		t.Fatal("development adopted production trust")
	}
	conn, err := pgx.Connect(t.Context(), request.Store.DSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(t.Context())
	for _, mutation := range []struct{ name, change, restore string }{
		{"changed default", `ALTER TABLE workspace_handoffs ALTER COLUMN authenticated_at SET DEFAULT '1970-01-02T00:00:00Z'`, `ALTER TABLE workspace_handoffs ALTER COLUMN authenticated_at SET DEFAULT '1970-01-01T00:00:00Z'`},
		{"added object", `CREATE TABLE unexpected_catalog_object(id integer)`, `DROP TABLE unexpected_catalog_object`},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			if _, err := conn.Exec(t.Context(), mutation.change); err != nil {
				t.Fatal(err)
			}
			if _, err := run(t.Context(), request, claim, upgrade.Production, verify); err == nil {
				t.Fatal("actual schema drift admitted")
			}
			if _, err := conn.Exec(t.Context(), mutation.restore); err != nil {
				t.Fatal(err)
			}
			if result, err := run(t.Context(), request, claim, upgrade.Production, verify); err != nil || !result.Admission.Valid() {
				t.Fatalf("restored exact catalog not admitted: %v", err)
			}
		})
	}
}
