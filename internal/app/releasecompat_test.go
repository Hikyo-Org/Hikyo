package app

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/jackc/pgx/v5"
)

func TestReleaseCompatibilityMatchesActualBothEngineSchemas(t *testing.T) {
	dsn := os.Getenv("HIKYO_TEST_POSTGRES_DSN")
	if dsn == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("CI requires PostgreSQL for actual release schema generation")
		}
		t.Skip("HIKYO_TEST_POSTGRES_DSN not set")
	}
	admin, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("hikyo_f1_release_%d", time.Now().UnixNano())
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close(t.Context())
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		_ = admin.Close(context.Background())
	})
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("release fixture requires PostgreSQL URL DSN")
	}
	parsed.Path = "/" + name
	fixtureDSN := parsed.String()
	raw, declaration, err := buildcompat.Development()
	if err != nil {
		t.Fatal(err)
	}
	sources := map[releaseidentity.Engine][]upgradecompat.SourceEdge{}
	for _, engine := range declaration.Engines {
		sources[engine.Migrations.Engine] = engine.Sources
	}
	request := ReleaseCompatibilityRequest{Profile: declaration.Profile, Version: declaration.Version, Sequence: declaration.Sequence, Commit: declaration.Commit, Sources: sources, PostgreSQLDSN: fixtureDSN}
	generated, err := GenerateReleaseCompatibility(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, raw) {
		t.Fatal("committed development declaration differs from actual SQL bytes or migrated catalogs; regenerate and review")
	}
	db, err := pgx.Connect(t.Context(), fixtureDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close(context.Background())
	before, err := upgrade.DomainCatalogPostgres(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateReleaseCompatibility(t.Context(), request); err == nil || !strings.Contains(err.Error(), "nonempty") {
		t.Fatal("generator did not refuse existing PostgreSQL schema", err)
	}
	after, err := upgrade.DomainCatalogPostgres(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	if before.Digest() != after.Digest() {
		t.Fatal("refused generation changed existing catalog")
	}
}
