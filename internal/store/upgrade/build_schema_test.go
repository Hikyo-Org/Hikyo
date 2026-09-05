package upgrade

import (
	"embed"
	"strings"
	"testing"
)

//go:embed testdata/build-schema/*.sql
var buildSchemaMigrations embed.FS

func TestBuildScratchSchemaSerializesEmptyCheckWithDDL(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		start := make(chan struct{})
		results := make(chan error, 2)
		for range 2 {
			go func() {
				<-start
				_, _, err := BuildScratchSchema(t.Context(), cfg, buildSchemaMigrations, "testdata/build-schema")
				results <- err
			}()
		}
		close(start)
		success, refused := 0, 0
		for range 2 {
			err := <-results
			if err == nil {
				success++
			} else if strings.Contains(err.Error(), "nonempty") {
				refused++
			} else {
				t.Error(err)
			}
		}
		if success != 1 || refused != 1 {
			t.Fatalf("concurrent builders: successful=%d refused=%d", success, refused)
		}
	})
}

func TestBuildScratchSchemaRefusesExistingObjectsWithoutGooseWrites(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		query(t, cfg, "CREATE TABLE operator_owned (id TEXT PRIMARY KEY)")
		var before Catalog
		if err := WithLock(t.Context(), cfg, func(s *Session) error {
			var err error
			before, err = inspectCatalog(t.Context(), s.conn, cfg.Engine)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := BuildScratchSchema(t.Context(), cfg, buildSchemaMigrations, "testdata/build-schema"); err == nil || !strings.Contains(err.Error(), "nonempty") {
			t.Fatalf("nonempty scratch was not refused: %v", err)
		}
		if err := WithLock(t.Context(), cfg, func(s *Session) error {
			after, err := inspectCatalog(t.Context(), s.conn, cfg.Engine)
			if err == nil && (after.Digest() != before.Digest() || len(after.Applied) != 0) {
				t.Error("refused scratch build mutated existing schema or goose history")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
	})
}
