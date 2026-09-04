package lint

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestReadGeneratedContractsUsesCallerFacingAPI(t *testing.T) {
	dir := t.TempDir()
	src := `package fixture
import "context"
type Queries struct{}
type Item struct { ID string; Alias string }
type GetItemParams struct { ChainOrgID string; ID string }
type GetItemRow struct { ID string; Alias string }
const getItem = "SELECT id, alias FROM items WHERE org_id = $1 AND id = $2"
func (q *Queries) GetItem(ctx context.Context, arg GetItemParams) (GetItemRow, error) { q.db.QueryRow(ctx, getItem, arg.ChainOrgID, arg.ID); panic("fixture") }
func (q *Queries) ListItems(ctx context.Context, orgID string) ([]GetItemRow, error) { panic("fixture") }
func (q *Queries) GetModel(ctx context.Context, id string) (Item, error) { panic("fixture") }
`
	if err := os.WriteFile(filepath.Join(dir, "queries.sql.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	contracts, err := readGeneratedContracts(dir, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := fieldNames(contracts["GetItem"].Parameters), []string{"OrgID", "ID"}; !slices.Equal(got, want) {
		t.Fatalf("parameters = %v, want %v", got, want)
	}
	if got, want := fieldNames(contracts["GetItem"].Results), []string{"ID", "Alias"}; !slices.Equal(got, want) {
		t.Fatalf("results = %v, want %v", got, want)
	}
	if !contracts["GetItem"].ResultOrderSignificant {
		t.Fatal("query-specific row result order is not significant")
	}
	if got, want := contracts["GetItem"].BindSites, []string{"OrgID", "ID"}; !contracts["GetItem"].BindSitesKnown || !slices.Equal(got, want) {
		t.Fatalf("bind sites = %v, known %t; want %v", got, contracts["GetItem"].BindSitesKnown, want)
	}
	if contracts["GetModel"].ResultOrderSignificant {
		t.Fatal("model result field order should not affect its named caller API")
	}
	if got, want := fieldNames(contracts["ListItems"].Results), []string{"ID", "Alias"}; !slices.Equal(got, want) {
		t.Fatalf("slice results = %v, want %v", got, want)
	}
}

func TestApprovedContractDifferenceIsExactPinned(t *testing.T) {
	name := "PinnedDifference"
	sqlite := Query{Name: name, Cmd: "one", SQL: "SELECT id, alias FROM items WHERE id = ?"}
	postgres := Query{Name: name, Cmd: "one", SQL: "SELECT id, alias FROM items WHERE id = $1"}
	sqliteAPI := generatedContract{Parameters: []apiField{{Name: "ID", Type: "string"}}, Results: []apiField{{Name: "ID", Type: "string"}, {Name: "Alias", Type: "string"}}, BindSites: []string{"ID"}, BindSitesKnown: true}
	postgresAPI := generatedContract{Parameters: []apiField{{Name: "ID", Type: "string"}}, Results: []apiField{{Name: "ID", Type: "string"}, {Name: "Alias", Type: "pgtype.Text"}}, BindSites: []string{"ID"}, BindSitesKnown: true}
	approvedContractDifferences[name] = approvedContractDifference{
		SQLiteSQLHash: sqlite.Hash(), PostgresSQLHash: postgres.Hash(),
		SQLiteAPIHash: sqliteAPI.hash(), PostgresAPIHash: postgresAPI.hash(),
		Reason: "test-only exact exception",
	}
	defer delete(approvedContractDifferences, name)

	if findings := compareQueryContracts(name, sqlite, postgres, sqliteAPI, postgresAPI); len(findings) != 0 {
		t.Fatalf("exact pin rejected: %v", findings)
	}

	mutatedSQL := postgres
	mutatedSQL.SQL = "SELECT id, replacement FROM items WHERE id = $1"
	assertFindings(t, compareQueryContracts(name, sqlite, mutatedSQL, sqliteAPI, postgresAPI), []string{"differs from its approved exact cross-engine pin"})

	mutatedAPI := postgresAPI
	mutatedAPI.Results = slices.Clone(postgresAPI.Results)
	mutatedAPI.Results[1].Type = "string"
	assertFindings(t, compareQueryContracts(name, sqlite, postgres, sqliteAPI, mutatedAPI), []string{"differs from its approved exact cross-engine pin"})
}

func TestBindOrdinalsAreDialectAware(t *testing.T) {
	postgres := `SELECT payload ? 'key' FROM items
WHERE b = $2 AND a = $1 OR c = $2
AND note = '$3' AND body = $tag$ $4 $tag$
AND escaped = E'it\'s $5'
/* outer $6 /* inner $7 */ still outer $8 */`
	if got, want := bindOrdinals(postgres, "postgres"), []int{2, 1, 2}; !slices.Equal(got, want) {
		t.Fatalf("postgres ordinals = %v, want %v", got, want)
	}
	sqlite := "SELECT '?' FROM items WHERE b = ?2 AND a = ?1 OR c = ?2 -- ?3\n"
	if got, want := bindOrdinals(sqlite, "sqlite"), []int{2, 1, 2}; !slices.Equal(got, want) {
		t.Fatalf("sqlite ordinals = %v, want %v", got, want)
	}
}

func TestParameterAliasesDoNotCrossConcepts(t *testing.T) {
	query := Query{Name: "UnpinnedQuery", Cmd: "exec", SQL: "UPDATE items SET touched = 1 WHERE id = ?"}
	sqlite := generatedContract{Parameters: []apiField{{Name: "AccountID", Type: "string"}}, BindSites: []string{"AccountID"}, BindSitesKnown: true}
	postgres := generatedContract{Parameters: []apiField{{Name: "SnapshotID", Type: "string"}}, BindSites: []string{"SnapshotID"}, BindSitesKnown: true}
	assertFindings(t, compareQueryContracts(query.Name, query, query, sqlite, postgres), []string{"parameter contract differs"})
}

func TestDomainTypeFamiliesCannotUseGenericFallbacks(t *testing.T) {
	query := Query{Name: "TypedQuery", Cmd: "one", SQL: "SELECT active FROM items"}
	base := generatedContract{ResultOrderSignificant: true, BindSitesKnown: true}

	sqlite := base
	sqlite.Results = []apiField{{Name: "Active", Type: "int64"}}
	postgres := base
	postgres.Results = []apiField{{Name: "Active", Type: "int32"}}
	assertFindings(t, compareQueryContracts(query.Name, query, query, sqlite, postgres), []string{"result types differ"})

	sqlite.Results = []apiField{{Name: "ExpiresAt", Type: "sql.NullString"}}
	postgres.Results = []apiField{{Name: "ExpiresAt", Type: "pgtype.Text"}}
	assertFindings(t, compareQueryContracts(query.Name, query, query, sqlite, postgres), []string{"result types differ"})
}
