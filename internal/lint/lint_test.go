package lint

// Helpers and unit tests for the analyzers' disk readers and the analysis
// contexts. The analyzers themselves run over the repository and the negative
// fixtures through the vet driver (vetdriver_test.go, vetfixtures_test.go).

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestReencryptCoverageCatchesUnclassifiedColumn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "999_new.sql"),
		[]byte("CREATE TABLE new_secrets (\n    id TEXT PRIMARY KEY,\n    sneaky_ciphertext BLOB NOT NULL\n);\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	findings := checkReencryptCoverage(osSource{}, dir, reencryptCovered, reencryptExemptBlobs)
	var got bool
	for _, f := range findings {
		if strings.Contains(f, "new_secrets.sneaky_ciphertext") && strings.Contains(f, "unclassified") {
			got = true
		}
	}
	if !got {
		t.Fatalf("expected an unclassified finding for new_secrets.sneaky_ciphertext, got %v", findings)
	}
}

func TestFileScansAcceptDoubleDotsInsidePathComponents(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo..prod")
	queryDir := filepath.Join(repo, "queries")
	if err := os.MkdirAll(queryDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(queryDir, "001..safe.sql"), []byte("-- name: Safe :one\nSELECT 1;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseQueries(queryDir); err != nil {
		t.Fatalf("valid dotted query path rejected: %v", err)
	}

	storeDir := filepath.Join(repo, "internal", "store")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeDir, "..safe.sql"), []byte("SELECT 1;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, finding := range checkNoSyncCommitDowngrade(osSource{}, repo, []string{".go", ".sql"}) {
		if strings.Contains(finding, "invalid file path") {
			t.Fatalf("valid dotted walked path rejected: %s", finding)
		}
	}
}

func TestQueryScanRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not reliably available on Windows")
	}
	queryDir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.sql")
	if err := os.WriteFile(outside, []byte("-- name: Outside :one\nSELECT 1;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(queryDir, "escape.sql")); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseQueries(queryDir); err == nil {
		t.Fatal("query scan followed a symlink outside its root")
	} else if !strings.Contains(err.Error(), "read target") {
		t.Fatalf("query scan hid target-read cause: %v", err)
	}
}

func TestCheckSQLPredicatesReportsReadCauseWithoutDuplicatePrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not reliably available on Windows")
	}
	repo := t.TempDir()
	migrationDir := filepath.Join(repo, "internal", "store", "migrations", "sqlite")
	if err := os.MkdirAll(migrationDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.sql")
	if err := os.WriteFile(outside, []byte("CREATE TABLE outside (id TEXT);\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(migrationDir, "escape.sql")); err != nil {
		t.Fatal(err)
	}

	findings := checkSQLPredicates(osSource{}, repo, false)
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want one refused read", findings)
	}
	if count := strings.Count(findings[0], "sqlpredicate:"); count != 1 {
		t.Fatalf("finding has %d subsystem prefixes, want one: %s", count, findings[0])
	}
	if !strings.Contains(findings[0], "read target") {
		t.Fatalf("finding hid target-read cause: %s", findings[0])
	}
}

func TestAppendOnlyScanRejectsSymlinkEscapeWithReadCause(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not reliably available on Windows")
	}
	repo := t.TempDir()
	storeDir := filepath.Join(repo, "internal", "store")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.sql")
	if err := os.WriteFile(outside, []byte("SELECT 1;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(storeDir, "escape.sql")); err != nil {
		t.Fatal(err)
	}

	findings := checkNoSyncCommitDowngrade(osSource{}, repo, []string{".go", ".sql"})
	if len(findings) != 1 {
		t.Fatalf("findings = %v, want one refused read", findings)
	}
	if !strings.Contains(findings[0], "read target") {
		t.Fatalf("append-only scan hid target-read cause: %s", findings[0])
	}
}

// --- negative fixtures: every analyzer must catch its target violations ---

// The forgery guard's exemption must be an exact path match: a neighbouring
// package whose path merely starts with the authorization package's path
// (internal/authzforge) would otherwise be skipped entirely and could
// reflect on live proof values.
func TestForgeryExemptionIsNotPrefixMatched(t *testing.T) {
	neighbours := []string{
		Module + "/internal/authzforge",
		Module + "/internal/authz/forge",
		Module + "/internal/authzutil",
	}
	for _, p := range neighbours {
		if authzExempt[p] {
			t.Errorf("%s is exempt from the forgery guard — exemption must be exact-path", p)
		}
	}
	if !authzExempt[Module+"/internal/authz"] {
		t.Error("the authorization package must stay exempt")
	}
}

func TestSQLPredicateCatchesViolations(t *testing.T) {
	rules := map[string]TableRule{
		"environments": {Class: "environment", Chain: []string{"org_id", "project_id"}},
		"grants":       {Class: "authn"},
	}
	cases := []struct {
		name string
		q    Query
		want string
	}{
		{"missing chain conjunct",
			Query{Name: "Bad1", SQL: "SELECT id FROM environments WHERE id = ?"},
			"missing top-level chain conjunct"},
		{"no where at all",
			Query{Name: "Bad2", SQL: "SELECT id FROM environments"},
			"without a WHERE clause"},
		{"or beside the tenant conjunct",
			Query{Name: "Bad3", SQL: "SELECT id FROM environments WHERE org_id = ? AND project_id = ? OR name = ?"},
			"not a provable"},
		{"union branch",
			Query{Name: "Bad4", SQL: "SELECT id FROM environments WHERE org_id = ? AND project_id = ? UNION SELECT id FROM environments"},
			"unprovable shape"},
		{"cte",
			Query{Name: "Bad5", SQL: "WITH x AS (SELECT id FROM environments) SELECT id FROM x"},
			"unprovable shape"},
		{"join",
			Query{Name: "Bad6", SQL: "SELECT e.id FROM environments e JOIN projects p ON p.id = e.project_id"},
			"unprovable joined read"},
		{"set on chain column",
			Query{Name: "Bad7", SQL: "UPDATE environments SET org_id = ? WHERE org_id = ? AND project_id = ? AND id = ?"},
			"immutable column"},
		{"insert omitting chain column",
			Query{Name: "Bad8", SQL: "INSERT INTO environments (id, name) VALUES (?, ?)"},
			"omits chain column"},
		{"unannotated query on authn table",
			Query{Name: "Bad9", SQL: "SELECT capability FROM grants WHERE principal_id = ?"},
			"resolution surface"},
		{"unknown table",
			Query{Name: "Bad10", SQL: "SELECT id FROM widgets WHERE id = ?"},
			"not in the derived scope registry"},
		{"parenthesised chain predicate",
			Query{Name: "Bad11", SQL: "SELECT id FROM environments WHERE org_id = ? AND project_id IN (?)"},
			"not a provable"},
		{"upsert with an update action",
			Query{Name: "Bad12", SQL: "INSERT INTO environments (id, org_id, project_id, name) VALUES (?, ?, ?, ?) ON CONFLICT (id) DO UPDATE SET name = ?"},
			"unprovable shape (ON CONFLICT)"},
		{"sqlite insert or replace",
			Query{Name: "Bad13", SQL: "INSERT OR REPLACE INTO environments (id, org_id, project_id, name) VALUES (?, ?, ?, ?)"},
			"unrecognised statement shape"},
		{"group by beside a missing chain conjunct",
			Query{Name: "Bad14", SQL: "SELECT id, MAX(display_order) AS display_order FROM environments WHERE org_id = ? GROUP BY id"},
			"missing top-level chain conjunct"},
		{"group by cannot hide a range on a chain column",
			Query{Name: "Bad15", SQL: "SELECT id, MAX(display_order) AS display_order FROM environments WHERE org_id = ? AND project_id > ? GROUP BY id"},
			"chain conjuncts must be equality"},
	}
	for _, tc := range cases {
		findings := checkQuery("test", tc.q, rules)
		found := false
		for _, f := range findings {
			if strings.Contains(f, tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: findings %v do not contain %q", tc.name, findings, tc.want)
		}
	}
}

// A provable good shape must pass, so the negative cases above are meaningful.
func TestSQLPredicateAcceptsProvableShapes(t *testing.T) {
	rules := map[string]TableRule{
		"environments": {Class: "environment", Chain: []string{"org_id", "project_id"}},
	}
	good := []Query{
		{Name: "Good1", SQL: "SELECT id, name FROM environments WHERE org_id = ? AND project_id = ? AND id = ?"},
		{Name: "Good2", SQL: "UPDATE environments SET note = $1 WHERE org_id = $2 AND project_id = $3 AND id = $4"},
		{Name: "Good3", SQL: "INSERT INTO environments (id, org_id, project_id, name) VALUES (?, ?, ?, ?)"},
		{Name: "Good4", SQL: "INSERT INTO environments (id, org_id, project_id, name) VALUES (?, ?, ?, ?) ON CONFLICT (id) DO NOTHING"},
		{Name: "Good5", SQL: "SELECT id, MAX(display_order) AS display_order FROM environments WHERE org_id = ? AND project_id = ? GROUP BY id"},
	}
	for _, q := range good {
		if findings := checkQuery("test", q, rules); len(findings) != 0 {
			t.Errorf("%s: unexpected findings %v", q.Name, findings)
		}
	}
}

func TestCrossEngineQueryContractsCatchDrift(t *testing.T) {
	base := Query{
		Name: "GetEnvironment", Cmd: "one",
		SQL: "SELECT id, name FROM environments WHERE org_id = ? AND id = ?",
	}
	baseAPI := generatedContract{
		Parameters:             []apiField{{Name: "OrgID", Type: "string"}, {Name: "ID", Type: "string"}},
		Results:                []apiField{{Name: "ID", Type: "string"}, {Name: "Name", Type: "string"}},
		ResultOrderSignificant: true,
		BindSites:              []string{"OrgID", "ID"},
		BindSitesKnown:         true,
	}
	cases := []struct {
		name  string
		pg    Query
		pgAPI generatedContract
		want  string
	}{
		{
			name:  "command",
			pg:    Query{Name: base.Name, Cmd: "many", SQL: "SELECT id, name FROM environments WHERE org_id = $1 AND id = $2"},
			pgAPI: baseAPI,
			want:  `query "GetEnvironment" command differs`,
		},
		{
			name:  "annotation",
			pg:    Query{Name: base.Name, Cmd: base.Cmd, Annotation: "instance-scoped", SQL: "SELECT id, name FROM environments WHERE org_id = $1 AND id = $2"},
			pgAPI: baseAPI,
			want:  `query "GetEnvironment" annotation differs`,
		},
		{
			name:  "parameter identity",
			pg:    Query{Name: base.Name, Cmd: base.Cmd, SQL: "SELECT id, name FROM environments WHERE org_id = $1 AND id = $2"},
			pgAPI: generatedContract{Parameters: []apiField{{Name: "OrgID", Type: "string"}, {Name: "Alias", Type: "string"}}, Results: baseAPI.Results},
			want:  `query "GetEnvironment" parameter contract differs`,
		},
		{
			name:  "parameter count",
			pg:    Query{Name: base.Name, Cmd: base.Cmd, SQL: "SELECT id, name FROM environments WHERE org_id = $1"},
			pgAPI: generatedContract{Parameters: []apiField{{Name: "OrgID", Type: "string"}}, Results: baseAPI.Results},
			want:  `query "GetEnvironment" parameter contract differs`,
		},
		{
			name:  "parameter type",
			pg:    Query{Name: base.Name, Cmd: base.Cmd, SQL: "SELECT id, name FROM environments WHERE org_id = $1 AND id = $2"},
			pgAPI: generatedContract{Parameters: []apiField{{Name: "OrgID", Type: "int64"}, {Name: "ID", Type: "string"}}, Results: baseAPI.Results},
			want:  `query "GetEnvironment" parameter types differ`,
		},
		{
			name:  "bind order",
			pg:    Query{Name: base.Name, Cmd: base.Cmd, SQL: "SELECT id, name FROM environments WHERE id = $1 AND org_id = $2"},
			pgAPI: generatedContract{Parameters: baseAPI.Parameters, Results: baseAPI.Results, BindSites: []string{"ID", "OrgID"}},
			want:  `query "GetEnvironment" bind-site order/reuse differs`,
		},
		{
			name:  "bind reuse",
			pg:    Query{Name: base.Name, Cmd: base.Cmd, SQL: "SELECT id, name FROM environments WHERE org_id = $1 AND id = $2 AND parent_id = $2"},
			pgAPI: generatedContract{Parameters: baseAPI.Parameters, Results: baseAPI.Results, BindSites: []string{"OrgID", "ID", "ID"}},
			want:  `query "GetEnvironment" bind-site order/reuse differs`,
		},
		{
			name:  "same-width result replacement",
			pg:    Query{Name: base.Name, Cmd: base.Cmd, SQL: "SELECT id, alias FROM environments WHERE org_id = $1 AND id = $2"},
			pgAPI: generatedContract{Parameters: baseAPI.Parameters, Results: []apiField{{Name: "ID", Type: "string"}, {Name: "Alias", Type: "string"}}},
			want:  `query "GetEnvironment" result shape differs`,
		},
		{
			name:  "result order",
			pg:    Query{Name: base.Name, Cmd: base.Cmd, SQL: "SELECT name, id FROM environments WHERE org_id = $1 AND id = $2"},
			pgAPI: generatedContract{Parameters: baseAPI.Parameters, Results: []apiField{{Name: "Name", Type: "string"}, {Name: "ID", Type: "string"}}, ResultOrderSignificant: true},
			want:  `query "GetEnvironment" result shape differs`,
		},
		{
			name:  "result type",
			pg:    Query{Name: base.Name, Cmd: base.Cmd, SQL: "SELECT id, name FROM environments WHERE org_id = $1 AND id = $2"},
			pgAPI: generatedContract{Parameters: baseAPI.Parameters, Results: []apiField{{Name: "ID", Type: "string"}, {Name: "Name", Type: "int64"}}},
			want:  `query "GetEnvironment" result types differ`,
		},
		{
			name:  "result nullability",
			pg:    Query{Name: base.Name, Cmd: base.Cmd, SQL: "SELECT id, name FROM environments WHERE org_id = $1 AND id = $2"},
			pgAPI: generatedContract{Parameters: baseAPI.Parameters, Results: []apiField{{Name: "ID", Type: "string"}, {Name: "Name", Type: "pgtype.Text"}}},
			want:  `query "GetEnvironment" result types differ`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pgAPI := tc.pgAPI
			pgAPI.BindSitesKnown = true
			if pgAPI.BindSites == nil {
				pgAPI.BindSites = baseAPI.BindSites
			}
			findings := compareQueryContracts(base.Name, base, tc.pg, baseAPI, pgAPI)
			assertFindings(t, findings, []string{tc.want})
		})
	}
}

func TestCrossEngineQueryContractsRequireGeneratedAPIs(t *testing.T) {
	query := Query{Name: "GetEnvironment", Cmd: "one", SQL: "SELECT id FROM environments"}
	findings := crossEngine(
		map[string][]Query{"sqlite": {query}, "postgres": {query}},
		map[string]map[string]TableRule{},
		map[string]map[string]generatedContract{},
		false,
	)
	assertFindings(t, findings, []string{`query "GetEnvironment" has no generated API contract`})
}

func TestCrossEngineQueryContractsAcceptDialectSpecificSQL(t *testing.T) {
	sqlite := Query{Name: "GetEnvironment", Cmd: "one", SQL: "SELECT id, active FROM environments WHERE org_id = ? AND id = ?"}
	postgres := Query{Name: "GetEnvironment", Cmd: "one", SQL: "SELECT id, active::boolean AS active FROM environments WHERE org_id = $1 AND id = $2 FOR UPDATE"}
	sqliteAPI := generatedContract{
		Parameters: []apiField{{Name: "OrgID", Type: "string"}, {Name: "ID", Type: "string"}},
		Results:    []apiField{{Name: "ID", Type: "string"}, {Name: "Active", Type: "int64"}},
		BindSites:  []string{"OrgID", "ID"}, BindSitesKnown: true,
	}
	postgresAPI := generatedContract{
		Parameters: []apiField{{Name: "OrgID", Type: "string"}, {Name: "ID", Type: "string"}},
		Results:    []apiField{{Name: "ID", Type: "string"}, {Name: "Active", Type: "bool"}},
		BindSites:  []string{"OrgID", "ID"}, BindSitesKnown: true,
	}
	if findings := compareQueryContracts(sqlite.Name, sqlite, postgres, sqliteAPI, postgresAPI); len(findings) != 0 {
		t.Fatalf("dialect-specific query has false-positive drift: %v", findings)
	}
}

func assertFindings(t *testing.T, findings, wantSubstrings []string) {
	t.Helper()
	for _, want := range wantSubstrings {
		found := false
		for _, f := range findings {
			if strings.Contains(f, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("findings do not contain %q:\n%s", want, strings.Join(findings, "\n"))
		}
	}
}

// --- redaction + append-only (audit-model ADR, #45) ---

// The context environment is explicit: ambient GOFLAGS never leaks into the
// build-system query, and the windows context pins GOOS.
func TestContextEnvClearsAmbientGOFLAGS(t *testing.T) {
	t.Setenv("GOFLAGS", "-tags=hostile")
	t.Setenv("GOOS", "plan9")
	for _, ctx := range Contexts {
		env := ctx.Env()
		if !slices.Contains(env, "GOFLAGS=") || slices.Contains(env, "GOFLAGS=-tags=hostile") {
			t.Fatalf("%s env keeps ambient GOFLAGS: %v", ctx.Name, env)
		}
		if slices.Contains(env, "GOOS=plan9") {
			t.Fatalf("%s env keeps ambient GOOS", ctx.Name)
		}
		if ctx.GOOS != "" && !slices.Contains(env, "GOOS="+ctx.GOOS) {
			t.Fatalf("%s env does not pin GOOS: %v", ctx.Name, env)
		}
	}
}

// The grant-lock analyzer's writer set is derived from the query files, and must
// name the grant writes it exists to guard.
func TestGrantWritersIncludeInsertGrant(t *testing.T) {
	writers, err := grantWriters(osSource{}, repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if !writers["InsertGrant"] {
		t.Fatal("grantWriters omits InsertGrant: the analyzer would not enforce the grant surface")
	}
}

// The denial-writer classifier is derived from sqlc's command annotation, not
// from the query's name. These mutate and none of them starts with a verb a
// prefix list would have guessed, which is how a previous version let three real
// writers through.
func TestMutatingQueriesClassifyByCommand(t *testing.T) {
	mutating, unclassified, err := mutatingQueries(osSource{}, repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(unclassified) != 0 {
		t.Errorf("unclassified sqlc commands: %v", unclassified)
	}
	for _, name := range []string{"ConsumeCredentialAuthority", "TouchSession", "AdvancePrincipalGeneration", "InsertTenantAuditEvent"} {
		if !mutating[name] {
			t.Errorf("mutatingQueries omits %q: a write the analyzer would not enforce", name)
		}
	}
	for _, name := range []string{"GetPrincipalKind", "ResolveOrgChain", "ListGrantsForPrincipal", "GetSessionByVerifier"} {
		if mutating[name] {
			t.Errorf("mutatingQueries includes %q: a read misclassified as a write", name)
		}
	}
}

// The contexts select the source sets they exist for: the ui context sees the
// embedded web asset file, the windows context sees a windows-only file, and
// the default context sees neither. The build system is asked the same way the
// vet driver asks it.
func TestContextsSelectTaggedAndPlatformFiles(t *testing.T) {
	files := func(t *testing.T, ctx BuildContext, pkgPath string) []string {
		t.Helper()
		args := []string{"list", "-f", "{{join .GoFiles \"\\n\"}}"}
		args = append(args, ctx.BuildFlags...)
		overlay, err := ctx.WriteOverlay(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if overlay != "" {
			args = append(args, "-overlay="+overlay)
		}
		cmd := exec.CommandContext(t.Context(), "go", append(args, pkgPath)...)
		cmd.Dir = repoRoot(t)
		cmd.Env = ctx.Env()
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list %s under %s: %v", pkgPath, ctx.Name, err)
		}
		return strings.Fields(string(out))
	}
	byName := map[string]BuildContext{}
	for _, ctx := range Contexts {
		byName[ctx.Name] = ctx
	}
	webui, durability := Module+"/internal/webui", Module+"/internal/filedurability"
	if got := files(t, byName["default"], webui); slices.Contains(got, "embedded.go") || !slices.Contains(got, "absent.go") {
		t.Fatalf("default context webui files = %v", got)
	}
	if got := files(t, byName["ui"], webui); !slices.Contains(got, "embedded.go") || slices.Contains(got, "absent.go") {
		t.Fatalf("ui context webui files = %v, want embedded.go selected", got)
	}
	if got := files(t, byName["default"], durability); slices.Contains(got, "directory_windows.go") {
		t.Fatalf("default context filedurability files = %v", got)
	}
	if got := files(t, byName["windows"], durability); !slices.Contains(got, "directory_windows.go") || slices.Contains(got, "directory_unix.go") {
		t.Fatalf("windows context filedurability files = %v, want the windows leg selected", got)
	}
	if got := files(t, byName["darwin"], Module+"/internal/devupgrade"); !slices.Contains(got, "publish_darwin.go") {
		t.Fatalf("darwin context devupgrade files = %v, want the darwin leg selected", got)
	}
}
