package conformance

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func init() {
	corpus = append(corpus, scenario{"value_schema_excludes_masked", scenarioSchemaExcludesMasked})
	corpus = append(corpus, scenario{"value_schema_rejects_masked_definitions", scenarioSchemaRejectsMaskedDefinitions})
}

// Inspect the applied schema, including later ALTERs, rather than migration
// source. Flat-model C2 excludes the former third presence state.
func scenarioSchemaExcludesMasked(t *testing.T, db *store.DB) {
	if count := maskedSchemaDefinitionCount(t, db); count != 0 {
		t.Fatalf("applied schema contains %d definitions of the deleted masked state", count)
	}
}

func maskedSchemaDefinitionCount(t *testing.T, db *store.DB) int {
	t.Helper()
	var count int
	var err error
	if db.Engine() == store.EnginePostgres {
		err = db.PG().QueryRow(t.Context(), `
			SELECT COUNT(*) FROM (
				SELECT table_name::text AS definition FROM information_schema.tables WHERE table_schema = 'public'
				UNION ALL SELECT column_name FROM information_schema.columns WHERE table_schema = 'public'
				UNION ALL SELECT column_default FROM information_schema.columns WHERE table_schema = 'public'
				UNION ALL SELECT indexdef FROM pg_indexes WHERE schemaname = 'public'
				UNION ALL SELECT definition FROM pg_views WHERE schemaname = 'public'
				UNION ALL SELECT definition FROM pg_matviews WHERE schemaname = 'public'
				UNION ALL SELECT pg_get_triggerdef(tr.oid) FROM pg_trigger tr
				JOIN pg_class rel ON rel.oid = tr.tgrelid JOIN pg_namespace n ON n.oid = rel.relnamespace
				WHERE n.nspname = 'public' AND NOT tr.tgisinternal
				UNION ALL SELECT pg_get_functiondef(p.oid) FROM pg_proc p
				JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = 'public' AND p.prokind IN ('f', 'p')
				UNION ALL SELECT pg_get_constraintdef(c.oid) FROM pg_constraint c
				JOIN pg_namespace n ON n.oid = c.connamespace WHERE n.nspname = 'public'
				UNION ALL SELECT e.enumlabel FROM pg_enum e JOIN pg_type ty ON ty.oid = e.enumtypid
				JOIN pg_namespace n ON n.oid = ty.typnamespace WHERE n.nspname = 'public'
			) AS definitions WHERE lower(definition) LIKE '%masked%'`).Scan(&count)
	} else {
		err = db.SQLiteRead().QueryRowContext(t.Context(),
			`SELECT COUNT(*) FROM sqlite_schema WHERE lower(sql) LIKE '%masked%'`).Scan(&count)
	}
	if err != nil {
		t.Fatal(err)
	}
	return count
}

// Exercise effective-schema definitions that a column-name/CHECK scan misses.
func scenarioSchemaRejectsMaskedDefinitions(t *testing.T, db *store.DB) {
	execConformance(t, db, `CREATE TABLE slop_619_probe (value TEXT)`)
	defer execConformance(t, db, `DROP TABLE slop_619_probe`)
	cases := []struct{ name, create, drop string }{
		{"default", `CREATE TABLE slop_619_default (value TEXT DEFAULT 'masked')`, `DROP TABLE slop_619_default`},
		{"index", `CREATE INDEX slop_619_index ON slop_619_probe ((COALESCE(value, 'masked')))`, `DROP INDEX slop_619_index`},
		{"view", `CREATE VIEW slop_619_view AS SELECT 'masked' AS presence`, `DROP VIEW slop_619_view`},
	}
	if db.Engine() == store.EnginePostgres {
		execConformance(t, db, `CREATE FUNCTION slop_619_trigger_fn() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`)
		defer execConformance(t, db, `DROP FUNCTION slop_619_trigger_fn()`)
		cases = append(cases,
			struct{ name, create, drop string }{"trigger", `CREATE TRIGGER slop_619_trigger BEFORE INSERT ON slop_619_probe FOR EACH ROW WHEN (NEW.value = 'masked') EXECUTE FUNCTION slop_619_trigger_fn()`, `DROP TRIGGER slop_619_trigger ON slop_619_probe`},
			struct{ name, create, drop string }{"function", `CREATE FUNCTION slop_619_fn() RETURNS text LANGUAGE sql AS $$ SELECT 'masked'::text $$`, `DROP FUNCTION slop_619_fn()`},
		)
	} else {
		cases = append(cases, struct{ name, create, drop string }{"trigger", `CREATE TRIGGER slop_619_trigger AFTER INSERT ON slop_619_probe BEGIN UPDATE slop_619_probe SET value = 'masked'; END`, `DROP TRIGGER slop_619_trigger`})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if count := maskedSchemaDefinitionCount(t, db); count != 0 {
				t.Fatalf("schema probe starts with %d masked definitions", count)
			}
			execConformance(t, db, tc.create)
			defer execConformance(t, db, tc.drop)
			if maskedSchemaDefinitionCount(t, db) == 0 {
				t.Fatal("schema probe accepted the deleted masked state")
			}
		})
	}
}

// Check parsed contract fields and enum values. Descriptions may explain the
// deleted state without introducing it into the wire contract.
func TestMaskedIsAbsentFromSchemaAndAPI(t *testing.T) {
	var document yaml.Node
	if err := yaml.Unmarshal(api.SpecYAML, &document); err != nil {
		t.Fatal(err)
	}
	var visit func(*yaml.Node)
	visit = func(node *yaml.Node) {
		if node.Kind == yaml.ScalarNode && strings.Contains(strings.ToLower(node.Value), "masked") {
			t.Errorf("api/openapi.yaml:%d: deleted masked state in contract field %q", node.Line, node.Value)
		}
		if node.Kind == yaml.MappingNode {
			for i := 0; i < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				visit(key)
				if value.Kind != yaml.ScalarNode || (key.Value != "description" && key.Value != "summary") {
					visit(value)
				}
			}
			return
		}
		for _, child := range node.Content {
			visit(child)
		}
	}
	visit(&document)
}
