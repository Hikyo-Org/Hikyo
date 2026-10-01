package lint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type apiField struct {
	Name string
	Type string
}

type generatedContract struct {
	Parameters             []apiField
	Results                []apiField
	ResultOrderSignificant bool
	BindSites              []string
	BindSitesKnown         bool
}

type approvedContractDifference struct {
	SQLiteSQLHash   string
	PostgresSQLHash string
	SQLiteAPIHash   string
	PostgresAPIHash string
	Reason          string
}

// approvedParameterNames bridges only sqlc names that are known to describe
// the same caller-facing value for one query. Keep these query-specific so a
// cursor can never silently become an account, snapshot, or other id.
var approvedParameterNames = map[string]map[string]string{
	"ClampIndefiniteCredentials":           aliases("ExpiresAt", "Ceiling"),
	"CountLiveMachineCredentials":          aliases("ExpiresAt", "Now"),
	"CountLiveMachineCredentialsInProject": aliases("ExpiresAt", "Now"),
	"CountOpenPlans":                       aliases("ExpiresAt", "Now"),
	"DeleteEnvironment":                    aliases("ID", "EnvID"),
	"DeleteOrg":                            aliases("ID", "OrgID"),
	"DeleteProject":                        aliases("ID", "ProjectID"),
	"GetEnvironment":                       aliases("ID", "EnvID"),
	"GetEnvironmentSettings":               aliases("ID", "EnvID"),
	"GetOrg":                               aliases("ID", "OrgID"),
	"GetProject":                           aliases("ID", "ProjectID"),
	"ListCredentialsBeyondCeiling":         aliases("ExpiresAt", "Ceiling"),
	"ListOauth2ProvidersForReencrypt":      aliases("ID", "Cursor", "Limit", "PageLimit"),
	"ListOidcProvidersForReencrypt":        aliases("ID", "Cursor", "Limit", "PageLimit"),
	"ListPasswordCredsForReencrypt":        aliases("AccountID", "Cursor", "Limit", "PageLimit"),
	"ListPendingForReencrypt":              aliases("ID", "Cursor", "Limit", "PageLimit"),
	"ListPkiIssuersForReencrypt":           aliases("ID", "Cursor", "Limit", "PageLimit"),
	"ListRecoveryCodesForReencrypt":        aliases("AccountID", "Cursor", "Limit", "PageLimit"),
	"ListRemotesForReencrypt":              aliases("ID", "Cursor", "Limit", "PageLimit"),
	"ListSamlKeysForReencrypt":             aliases("ID", "Cursor", "Limit", "PageLimit"),
	"ListSnapshotEntriesForReencrypt":      aliases("ID", "Cursor", "Limit", "PageLimit"),
	"ListTotpCredsForReencrypt":            aliases("ID", "Cursor", "Limit", "PageLimit"),
	"ListValueEntriesForReencrypt":         aliases("ID", "Cursor", "Limit", "PageLimit"),
	"LockOrg":                              aliases("ID", "OrgID"),
	"LockProject":                          aliases("ID", "ProjectID"),
	"LockSnapshotForRetentionConsequence":  aliases("ID", "SnapshotID"),
	"MarkSnapshotCollected":                aliases("ID", "SnapshotID"),
	"PageSCIMGroups":                       aliases("Limit", "PageLimit", "Offset", "PageOffset"),
	"PageSCIMUsers":                        aliases("Limit", "PageLimit", "Offset", "PageOffset"),
	"PruneExpiredPlans":                    aliases("ExpiresAt", "Now"),
	"ReencryptPendingChange":               aliases("Ciphertext", "NewCiphertext", "Ciphertext_2", "OldCiphertext"),
	"ReencryptSnapshotEntry":               aliases("Ciphertext", "NewCiphertext", "Ciphertext_2", "OldCiphertext"),
	"ReencryptValueEntry":                  aliases("Ciphertext", "NewCiphertext", "Ciphertext_2", "OldCiphertext"),
	"RenameEnvironment":                    aliases("ID", "EnvID"),
	"RenameOrg":                            aliases("ID", "OrgID"),
	"RenameProject":                        aliases("ID", "ProjectID"),
	"SelectExpiredApprovalRequests":        aliases("ExpiresAt", "Now"),
	"SetEnvironmentSettings":               aliases("ID", "EnvID"),
	"SetOrgRetention":                      aliases("ID", "OrgID"),
	"SetProjectDefinitionsSource":          aliases("ID", "ProjectID"),
	"SetProjectMachineReveal":              aliases("ID", "ProjectID"),
	"SetProjectRetention":                  aliases("ID", "ProjectID"),
	"UpdateEnvironmentNote":                aliases("ID", "EnvID"),
}

func aliases(pairs ...string) map[string]string {
	out := make(map[string]string, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		out[pairs[i]] = pairs[i+1]
	}
	return out
}

func compareQueryContracts(name string, sqlite, postgres Query, sqliteAPI, postgresAPI generatedContract) []string {
	var findings []string
	if sqlite.Cmd != postgres.Cmd {
		findings = append(findings, fmt.Sprintf("sqlpredicate: query %q command differs between engines: sqlite :%s, postgres :%s", name, sqlite.Cmd, postgres.Cmd))
	}
	if sqlite.Annotation != postgres.Annotation {
		findings = append(findings, fmt.Sprintf("sqlpredicate: query %q annotation differs between engines: sqlite %q, postgres %q", name, sqlite.Annotation, postgres.Annotation))
	}
	if sqlite.Reason != postgres.Reason {
		findings = append(findings, fmt.Sprintf("sqlpredicate: query %q annotation authority reason differs between engines", name))
	}
	if !sqliteAPI.BindSitesKnown || !postgresAPI.BindSitesKnown {
		findings = append(findings, fmt.Sprintf("sqlpredicate: query %q has no generated bind-site contract: sqlite=%t postgres=%t", name, sqliteAPI.BindSitesKnown, postgresAPI.BindSitesKnown))
	}

	if approved, ok := approvedContractDifferences[name]; ok {
		actual := approvedContractDifference{
			SQLiteSQLHash: sqlite.Hash(), PostgresSQLHash: postgres.Hash(),
			SQLiteAPIHash: sqliteAPI.hash(), PostgresAPIHash: postgresAPI.hash(),
		}
		if actual.SQLiteSQLHash != approved.SQLiteSQLHash || actual.PostgresSQLHash != approved.PostgresSQLHash ||
			actual.SQLiteAPIHash != approved.SQLiteAPIHash || actual.PostgresAPIHash != approved.PostgresAPIHash {
			findings = append(findings, fmt.Sprintf("sqlpredicate: query %q differs from its approved exact cross-engine pin (%s): sqlite_sql=%s postgres_sql=%s sqlite_api=%s postgres_api=%s", name, approved.Reason, actual.SQLiteSQLHash, actual.PostgresSQLHash, actual.SQLiteAPIHash, actual.PostgresAPIHash))
		}
		return findings
	}

	if !equalParameterNames(name, sqliteAPI.Parameters, postgresAPI.Parameters) {
		findings = append(findings, fmt.Sprintf("sqlpredicate: query %q parameter contract differs between engines: sqlite %s, postgres %s", name, sqliteAPI.parameterKey(), postgresAPI.parameterKey()))
	} else if !compatibleParameterTypes(name, sqliteAPI.Parameters, postgresAPI.Parameters) {
		findings = append(findings, fmt.Sprintf("sqlpredicate: query %q parameter types differ between engines: sqlite %s, postgres %s", name, sqliteAPI.parameterKey(), postgresAPI.parameterKey()))
	}
	if !equalBindSites(name, sqliteAPI.BindSites, postgresAPI.BindSites) {
		findings = append(findings, fmt.Sprintf("sqlpredicate: query %q bind-site order/reuse differs between engines: sqlite %v, postgres %v", name, sqliteAPI.BindSites, postgresAPI.BindSites))
	}
	if sqlite.Cmd == "one" || sqlite.Cmd == "many" {
		if !equalResultNames(sqliteAPI, postgresAPI) {
			findings = append(findings, fmt.Sprintf("sqlpredicate: query %q result shape differs between engines: sqlite %s, postgres %s", name, sqliteAPI.resultKey(), postgresAPI.resultKey()))
		} else if !compatibleResultTypes(name, sqliteAPI, postgresAPI) {
			findings = append(findings, fmt.Sprintf("sqlpredicate: query %q result types differ between engines: sqlite %s, postgres %s", name, sqliteAPI.resultKey(), postgresAPI.resultKey()))
		}
	}
	return findings
}

func readGeneratedContracts(dir, engine string) (map[string]generatedContract, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var files []*ast.File
	structs := map[string][]apiField{}
	querySQL := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, fmt.Errorf("parse generated API %s: %w", path, err)
		}
		files = append(files, file)
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok {
				continue
			}
			switch gen.Tok {
			case token.TYPE:
				for _, spec := range gen.Specs {
					typeSpec := spec.(*ast.TypeSpec)
					if st, ok := typeSpec.Type.(*ast.StructType); ok {
						structs[typeSpec.Name.Name] = fieldsFromStruct(st)
					}
				}
			case token.CONST:
				for _, spec := range gen.Specs {
					valueSpec := spec.(*ast.ValueSpec)
					for i, name := range valueSpec.Names {
						if i >= len(valueSpec.Values) {
							continue
						}
						literal, ok := valueSpec.Values[i].(*ast.BasicLit)
						if !ok || literal.Kind != token.STRING {
							continue
						}
						value, err := strconv.Unquote(literal.Value)
						if err != nil {
							return nil, fmt.Errorf("unquote generated SQL %s: %w", name.Name, err)
						}
						querySQL[name.Name] = value
					}
				}
			}
		}
	}

	out := map[string]generatedContract{}
	for _, file := range files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !queriesReceiver(fn.Recv) {
				continue
			}
			contract := generatedContract{}
			for _, field := range fn.Type.Params.List {
				if isContextType(field.Type) {
					continue
				}
				contract.Parameters = append(contract.Parameters, expandAPIField(field, structs)...)
			}
			if fn.Type.Results != nil {
				for _, field := range fn.Type.Results.List {
					if isErrorType(field.Type) {
						continue
					}
					results, orderSignificant := expandResult(field.Type, structs)
					contract.Results = append(contract.Results, results...)
					contract.ResultOrderSignificant = contract.ResultOrderSignificant || orderSignificant
				}
			}
			contract.BindSites, contract.BindSitesKnown = generatedBindSites(fn.Body, querySQL, engine)
			out[fn.Name.Name] = contract
		}
	}
	return out, nil
}

func fieldsFromStruct(st *ast.StructType) []apiField {
	var fields []apiField
	for _, field := range st.Fields.List {
		for _, name := range field.Names {
			fields = append(fields, apiField{Name: normalizeAPIName(name.Name), Type: expressionString(field.Type)})
		}
	}
	return fields
}

func expandAPIField(field *ast.Field, structs map[string][]apiField) []apiField {
	if ident, ok := field.Type.(*ast.Ident); ok {
		if fields, found := structs[ident.Name]; found {
			return slices.Clone(fields)
		}
	}
	var out []apiField
	for _, name := range field.Names {
		out = append(out, apiField{Name: normalizeAPIName(name.Name), Type: expressionString(field.Type)})
	}
	return out
}

func expandResult(expr ast.Expr, structs map[string][]apiField) ([]apiField, bool) {
	if list, ok := expr.(*ast.ArrayType); ok {
		return expandResult(list.Elt, structs)
	}
	if ident, ok := expr.(*ast.Ident); ok {
		if fields, found := structs[ident.Name]; found {
			return expandModelFields(fields, structs, nil), strings.HasSuffix(ident.Name, "Row")
		}
	}
	return []apiField{{Name: "$value", Type: expressionString(expr)}}, true
}

// sqlc.embed carries a named model inside a query row. Include its fields in
// the API pin so schema changes cannot hide behind an unchanged type name.
func expandModelFields(fields []apiField, structs map[string][]apiField, parents map[string]bool) []apiField {
	var out []apiField
	for _, field := range fields {
		nested, model := structs[field.Type]
		if !model || parents[field.Type] {
			out = append(out, field)
			continue
		}
		path := make(map[string]bool, len(parents)+1)
		for name := range parents {
			path[name] = true
		}
		path[field.Type] = true
		for _, child := range expandModelFields(nested, structs, path) {
			child.Name = field.Name + "." + child.Name
			out = append(out, child)
		}
	}
	return out
}

func generatedBindSites(body *ast.BlockStmt, querySQL map[string]string, engine string) ([]string, bool) {
	var sql string
	var bindArgs []string
	ast.Inspect(body, func(node ast.Node) bool {
		if sql != "" {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) < 2 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !generatedDBCalls[selector.Sel.Name] {
			return true
		}
		queryIdent, ok := call.Args[1].(*ast.Ident)
		if !ok {
			return true
		}
		query, ok := querySQL[queryIdent.Name]
		if !ok {
			return true
		}
		sql = query
		for _, arg := range call.Args[2:] {
			switch expr := arg.(type) {
			case *ast.SelectorExpr:
				bindArgs = append(bindArgs, normalizeAPIName(expr.Sel.Name))
			case *ast.Ident:
				bindArgs = append(bindArgs, normalizeAPIName(expr.Name))
			default:
				bindArgs = append(bindArgs, expressionString(arg))
			}
		}
		return false
	})
	if sql == "" {
		if engine == "sqlite" {
			return generatedSliceBindSites(body, querySQL)
		}
		return nil, false
	}
	ordinals := bindOrdinals(sql, engine)
	sites := make([]string, len(ordinals))
	for i, ordinal := range ordinals {
		if ordinal < 1 || ordinal > len(bindArgs) {
			sites[i] = fmt.Sprintf("$missing%d", ordinal)
			continue
		}
		sites[i] = bindArgs[ordinal-1]
	}
	return sites, true
}

var generatedDBCalls = map[string]bool{
	"Exec": true, "ExecContext": true,
	"Query": true, "QueryContext": true,
	"QueryRow": true, "QueryRowContext": true,
}

var (
	postgresBindOrdinalRe = regexp.MustCompile(`\$(\d+)`)
	sqliteBindOrdinalRe   = regexp.MustCompile(`\?(\d*)`)
)

func bindOrdinals(sql, engine string) []int {
	masked := maskSQLContractLiteralsAndComments(sql)
	if engine == "postgres" {
		matches := postgresBindOrdinalRe.FindAllStringSubmatch(masked, -1)
		out := make([]int, 0, len(matches))
		for _, match := range matches {
			ordinal, err := strconv.Atoi(match[1])
			if err == nil {
				out = append(out, ordinal)
			}
		}
		return out
	}
	matches := sqliteBindOrdinalRe.FindAllStringSubmatch(masked, -1)
	out := make([]int, 0, len(matches))
	next := 1
	for _, match := range matches {
		ordinal := next
		if match[1] != "" {
			parsed, err := strconv.Atoi(match[1])
			if err == nil {
				ordinal = parsed
			}
		}
		out = append(out, ordinal)
		if ordinal >= next {
			next = ordinal + 1
		}
	}
	return out
}

func maskSQLContractLiteralsAndComments(sql string) string {
	b := []byte(sql)
	for i := 0; i < len(sql); {
		end, kind, closed := sqlLexeme(sql, i, true)
		if kind == 0 {
			i++
			continue
		}
		from, to := i, end
		if kind == '\'' || kind == '"' {
			from++
			if closed {
				to--
			}
		}
		for j := from; j < to; j++ {
			b[j] = ' '
		}
		i = end
	}
	return string(b)
}

func queriesReceiver(fields *ast.FieldList) bool {
	if len(fields.List) != 1 {
		return false
	}
	expr := fields.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "Queries"
}

func isContextType(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	base, baseOK := selectorBase(selector)
	return baseOK && base == "context" && selector.Sel.Name == "Context"
}

func isErrorType(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "error"
}

func selectorBase(selector *ast.SelectorExpr) (string, bool) {
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

func expressionString(expr ast.Expr) string {
	var buf bytes.Buffer
	if err := format.Node(&buf, token.NewFileSet(), expr); err != nil {
		return "<invalid>"
	}
	return buf.String()
}

func normalizeAPIName(name string) string {
	if name == "" {
		return name
	}
	if name == "id" {
		return "ID"
	}
	name = strings.ToUpper(name[:1]) + name[1:]
	for _, prefix := range []string{"ChainOrg", "ChainProject", "ChainEnvironment", "ChainEnv"} {
		if strings.HasPrefix(name, prefix) {
			return strings.TrimPrefix(name, "Chain")
		}
	}
	return name
}

func parameterNames(fields []apiField) []string {
	names := fieldNames(fields)
	for i, name := range names {
		if name == "EnvironmentID" {
			names[i] = "EnvID"
		}
	}
	return names
}

func equalParameterNames(queryName string, sqlite, postgres []apiField) bool {
	return matchParameterFields(queryName, sqlite, postgres, false)
}

func equalBindSites(queryName string, sqlite, postgres []string) bool {
	if len(sqlite) != len(postgres) {
		return false
	}
	for i := range sqlite {
		sqName, pgName := sqlite[i], postgres[i]
		if sqName == "EnvironmentID" {
			sqName = "EnvID"
		}
		if pgName == "EnvironmentID" {
			pgName = "EnvID"
		}
		if !parameterNameCompatible(queryName, sqName, pgName) {
			return false
		}
	}
	return true
}

func equalResultNames(sqlite, postgres generatedContract) bool {
	sqNames := fieldNames(sqlite.Results)
	pgNames := fieldNames(postgres.Results)
	if !sqlite.ResultOrderSignificant && !postgres.ResultOrderSignificant {
		slices.Sort(sqNames)
		slices.Sort(pgNames)
	}
	return slices.Equal(sqNames, pgNames)
}

func fieldNames(fields []apiField) []string {
	out := make([]string, len(fields))
	for i, field := range fields {
		out[i] = field.Name
	}
	return out
}

func compatibleParameterTypes(queryName string, sqlite, postgres []apiField) bool {
	return matchParameterFields(queryName, sqlite, postgres, true)
}

func matchParameterFields(queryName string, sqlite, postgres []apiField, checkTypes bool) bool {
	if len(sqlite) != len(postgres) {
		return false
	}
	sqNames := parameterNames(sqlite)
	pgNames := parameterNames(postgres)
	usedSQLite := make([]bool, len(sqlite))
	usedPostgres := make([]bool, len(postgres))
	for sqIndex, sqName := range sqNames {
		for pgIndex, pgName := range pgNames {
			if usedPostgres[pgIndex] || sqName != pgName || checkTypes && !compatibleType(queryName, sqlite[sqIndex].Name, sqlite[sqIndex].Type, postgres[pgIndex].Type) {
				continue
			}
			usedSQLite[sqIndex] = true
			usedPostgres[pgIndex] = true
			break
		}
	}
	for sqIndex, sqName := range sqNames {
		if usedSQLite[sqIndex] {
			continue
		}
		matched := false
		for pgIndex, pgName := range pgNames {
			if usedPostgres[pgIndex] || !parameterNameCompatible(queryName, sqName, pgName) {
				continue
			}
			if checkTypes && !compatibleType(queryName, sqlite[sqIndex].Name, sqlite[sqIndex].Type, postgres[pgIndex].Type) {
				continue
			}
			usedPostgres[pgIndex] = true
			matched = true
			break
		}
		if !matched {
			return false
		}
	}
	return true
}

func parameterNameCompatible(queryName, sqlite, postgres string) bool {
	if sqlite == postgres {
		return true
	}
	return approvedParameterNames[queryName][sqlite] == postgres
}

func compatibleResultTypes(queryName string, sqlite, postgres generatedContract) bool {
	if sqlite.ResultOrderSignificant || postgres.ResultOrderSignificant {
		if len(sqlite.Results) != len(postgres.Results) {
			return false
		}
		for i := range sqlite.Results {
			fieldName := sqlite.Results[i].Name
			if fieldName == "$value" {
				fieldName = queryName
			}
			if !compatibleType(queryName, fieldName, sqlite.Results[i].Type, postgres.Results[i].Type) {
				return false
			}
		}
		return true
	}
	pgTypes := map[string]string{}
	for _, field := range postgres.Results {
		pgTypes[field.Name] = field.Type
	}
	for _, field := range sqlite.Results {
		pgType, ok := pgTypes[field.Name]
		if !ok || !compatibleType(queryName, field.Name, field.Type, pgType) {
			return false
		}
	}
	return true
}

func compatibleType(queryName, name, sqlite, postgres string) bool {
	if at := strings.LastIndexByte(name, '.'); at >= 0 {
		name = name[at+1:]
	}
	if sqlite == postgres {
		return true
	}
	if booleanContractFields[name] {
		if (queryName == "AdapterWorkerLoadExecutionQuery" || queryName == "AdapterWorkerLoadActivationQuery") && (name == "AllowPersonalToken" || name == "VariableProtected" || name == "VariableHidden" || name == "VariableExpand") {
			return sqlite == "int64" && postgres == "int32" // SQL CASE projections return exactly 0 or 1
		}
		return sqlite == "int64" && postgres == "bool"
	}
	if isTimestampContractField(name) || timestampQueryFields[queryName][name] {
		return (sqlite == "string" || sqlite == "sql.NullString" || sqlite == "interface{}") && postgres == "pgtype.Timestamptz"
	}
	if jsonContractFields[name] || jsonQueryFields[queryName][name] {
		return sqlite == "string" && postgres == "[]byte"
	}
	allowed := map[string]map[string]bool{
		"sql.NullString":  {"pgtype.Text": true},
		"int64":           {"int32": true},
		"sql.NullInt64":   {"pgtype.Int8": true, "pgtype.Int4": true},
		"float64":         {"pgtype.Float8": true, "pgtype.Numeric": true},
		"sql.NullFloat64": {"pgtype.Float8": true, "pgtype.Numeric": true},
	}
	return allowed[sqlite][postgres]
}

var booleanContractFields = map[string]bool{
	"Active": true, "Additive": true, "AllowIndefinite": true, "AllowSelfApproval": true,
	"Applied": true, "Browser": true, "Deprecated": true, "DriftAttention": true,
	"Enabled": true, "HistoryAuthorized": true, "Inert": true, "KeepRemote": true,
	"LastDrillOk": true, "MachineReveal": true, "MaterialSecret": true, "Missing": true,
	"NameidQualifierPresent": true, "NameidSpQualifierPresent": true, "OccurredAsserted": true,
	"EnrolmentRequired": true,                                                        // the sign-in enrolment gate (#760): sqlite INTEGER, postgres BOOLEAN
	"Prepared":          true, "Suspended": true, "ConfirmRestoredCredentials": true, // runtime configuration booleans map INTEGER to BOOLEAN
	"PayloadPresent": true, "Protected": true, "SchemaOverride": true, "Secret": true,
	"LocalEnabled":       true, // registration_policies.local_enabled (#606): sqlite INTEGER, postgres BOOLEAN
	"Bypassed":           true, // access_requests.bypassed (#152): sqlite INTEGER, postgres BOOLEAN
	"AllowPersonalToken": true, "VariableProtected": true, "VariableHidden": true, "VariableExpand": true,
}

func isTimestampContractField(name string) bool {
	return strings.HasSuffix(name, "At") || strings.HasSuffix(name, "Time") ||
		name == "Now" || name == "Ceiling" || name == "MetadataValidUntil" || name == "WindowStart" ||
		strings.Contains(name, "LastPruneSuccess")
}

func (c generatedContract) parameterKey() string { return fieldsKey(c.Parameters) }
func (c generatedContract) resultKey() string    { return fieldsKey(c.Results) }

func fieldsKey(fields []apiField) string {
	parts := make([]string, len(fields))
	for i, field := range fields {
		parts[i] = field.Name + ":" + field.Type
	}
	return strings.Join(parts, ",")
}

func (c generatedContract) hash() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("parameters=%s\nresults=%s\nresult_order_significant=%t\nbind_sites=%s\nbind_sites_known=%t", c.parameterKey(), c.resultKey(), c.ResultOrderSignificant, strings.Join(c.BindSites, ","), c.BindSitesKnown)))
	return hex.EncodeToString(sum[:])
}

// Both engines store these fields as JSON documents; generated PostgreSQL
// exposes jsonb bytes while SQLite exposes the identical serialized text.
var jsonContractFields = map[string]bool{
	"ApprovedWindows": true, "SelectedRepositoryIds": true,
	"FailureNames": true, "Warnings": true,
	"OrphanedNames": true,
}
