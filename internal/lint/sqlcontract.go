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

// approvedContractDifferences is deliberately exact. An exception is accepted
// only while both normalized SQL statements and both generated Go APIs retain
// their reviewed hashes. A query or schema change therefore invalidates its pin.
var approvedContractDifferences = map[string]approvedContractDifference{
	"InsertInstanceAuditEvent":     pin("b9bd450a26225955cf2d260d48f252e6c0a73caff301e24a3b08ff0bc1f338e7", "8119dce5ff473013f5ad809d47f786a6f06b91543fb0e75e78599a789c9d9792", "a506185e78e1db6eed9af13b67f5443b83243b5aacb29613cbc5bf49d4017488", "439f4681a6b157d7d47d4f87d4815c742d0b12b02e5276961189c0642d3478b7", "SQLite binds recorded_at; PostgreSQL's deferred appender assigns it"),
	"InsertTenantAuditEvent":       pin("ed8f8618d0f4289d6735a2ba7ab5d1135a8a29f4a570bcd5368c38baf4d4e3e1", "7ab43dd3f56957d51768bcfb03c1622612fdda971718c143abd3d1877094d625", "a9f31dfe7af196bd0c6e44ba946ae0aec2f1c505e5ed8d34f236cd07b6c7495b", "0d53582db06d063c08cd66639c9ca15feb16bbc2c4deea54ba0d0eb44fc84f36", "SQLite binds recorded_at; PostgreSQL's deferred appender assigns it"),
	"PageInstanceAudit":            pin("3c5b8ca14a82fa1ee3fd7c466769092b28f72e29c372f2639c3a6283bd1b5489", "0c2dcb6144d1bf394607a3027abd9e94ea1d64954049735e319e8a56af31ae22", "3ade01183bb0bb0ac66aff4d57abc6838620d0bc38644f6245e9c87fe1b04abb", "b67d8e4894d2c36ac255a9939392d9e0c1095a246d4f938e4969ca91b36a5114", "PostgreSQL returns commit_seq; SQLite uses seq as its commit cursor"),
	"PageInstanceAuditExport":      pin("8ba95a02b908bd8c05ee7a7baa4629c6fb6d553202543bbbabb364e460480a66", "d447876aa505a7a589bf1b1400ab9c57381932f414e0e207c3dc5ee794427f98", "bc3685ff499a432ee936a43a73683a7bd81682aa2777ffd9c06cb4e4ceace2c2", "c7d2d00577f69e149058f4667825081d17d5dd0b8862db17db0ad1d8e7881cad", "PostgreSQL returns commit_seq; SQLite uses seq as its commit cursor"),
	"PageTenantAuditEnv":           pin("7934810705135463755ae4f3760d64df00f69047d57e069036be723a5a44c0a3", "310ba728f5f11dfac0742102f38dcf786daeb95eec9c2914eb66f00ea470dc18", "46cb9f667e8cd31a9530079c41c2e02fa56c8e7f2ef480ff6a89d9c4f616956f", "ee121e269adae270f6e83582437f884d7e4c708d73cdb31134b89e8366aed50d", "PostgreSQL returns commit_seq; SQLite uses seq as its commit cursor"),
	"PageTenantAuditExportEnv":     pin("03bc0d2e5620a8c7117874f6a61dd5f236b28ec4ccc69cdc6a313d5aaa6a186b", "2ec85cdaedf0bd13bb80f0258eae29a30487c4c3c9e4e5266da7a4b37c7ebb59", "727cd90a0ca610c276efcb4421d87a2434c2e5b6d1d19dbaed2dba55d90bb708", "da054fb2e568803a53d6cbda842c97dcf9d4ea09deb1c664165e25d9bf694a0b", "PostgreSQL returns commit_seq; SQLite uses seq as its commit cursor"),
	"PageTenantAuditExportOrg":     pin("16c023aa0137312a27b95e513a19501cc7bc7f9096a856ddf61d97db9ac4e746", "9ea07a23563f661942839dfd4feae44f6bc28e61fd2f4e5944a61efdd2c8522c", "66033c1e9f86b179b057e7854784e5fbe0c1b5e1aadad9d310e2f24bf66d45e9", "4790c1ee97917dfb8c145e6ee1329dd4305055a1f6b951526de962cdc2797d6d", "PostgreSQL returns commit_seq; SQLite uses seq as its commit cursor"),
	"PageTenantAuditExportProject": pin("beba5db5086d77b54bef39c1a2de629db08b23da28b751bedd2388eb2618abcd", "07c09aa90fd3517f5d403fbbff54722510c27e08b3177d70378bd71133011caf", "2b4f62deaa146894d80372f7778f88e787a7dcfc23cdba9b9e8bc01a2f000e99", "704e0855171fea40d7c8a414a129287c55579773c6a2967cc7387b720fa1f485", "PostgreSQL returns commit_seq; SQLite uses seq as its commit cursor"),
	"PageTenantAuditOrg":           pin("12abef7deec7e5000db1cebe96c994264f98a7015fa289b74b82b04a13952628", "51ed60d74792458ee97a7fd19f28b9c358c8e61ea6bcc384063b075fbb0d5dc7", "2f9fae1eb821bc2f0abea61210d1b3827d5548f95b764a51d4ac82a0d2a65194", "d46205810c04e00743482483d7daeb81b869f70e132e6c6c7694bbed93551d57", "PostgreSQL returns commit_seq; SQLite uses seq as its commit cursor"),
	"PageTenantAuditProject":       pin("3916c1b1edd4669b15dee7678de74758d96a2f7a1ecf00d652e86d17388b94df", "8cebdbd9008a96589eaa0a045f33a7f6c0cbf539cb59fb1540ea779a6d908697", "669f2af73a4b3fa16d24ee59a93717c70e9a6a6ab09c615571428fb29e75f2fd", "e377b4d0a316aad0fea10e87b9846ad13293b02c6cecf695249823e50b88b877", "PostgreSQL returns commit_seq; SQLite uses seq as its commit cursor"),
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
	"ListOidcProvidersForReencrypt":        aliases("ID", "Cursor", "Limit", "PageLimit"),
	"ListPasswordCredsForReencrypt":        aliases("AccountID", "Cursor", "Limit", "PageLimit"),
	"ListPendingForReencrypt":              aliases("ID", "Cursor", "Limit", "PageLimit"),
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

func pin(sqliteSQL, postgresSQL, sqliteAPI, postgresAPI, reason string) approvedContractDifference {
	return approvedContractDifference{SQLiteSQLHash: sqliteSQL, PostgresSQLHash: postgresSQL, SQLiteAPIHash: sqliteAPI, PostgresAPIHash: postgresAPI, Reason: reason}
}

func compareQueryContracts(name string, sqlite, postgres Query, sqliteAPI, postgresAPI generatedContract) []string {
	var findings []string
	if sqlite.Cmd != postgres.Cmd {
		findings = append(findings, fmt.Sprintf("sqlpredicate: query %q command differs between engines: sqlite :%s, postgres :%s", name, sqlite.Cmd, postgres.Cmd))
	}
	if sqlite.Annotation != postgres.Annotation {
		findings = append(findings, fmt.Sprintf("sqlpredicate: query %q annotation differs between engines: sqlite %q, postgres %q", name, sqlite.Annotation, postgres.Annotation))
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
			return slices.Clone(fields), strings.HasSuffix(ident.Name, "Row")
		}
	}
	return []apiField{{Name: "$value", Type: expressionString(expr)}}, true
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
	for i := 0; i < len(b); {
		switch {
		case b[i] == '\'' || b[i] == '"':
			quote := b[i]
			backslashEscapes := quote == '\'' && i > 0 && (b[i-1] == 'E' || b[i-1] == 'e') && (i == 1 || !isSQLIdentifierByte(b[i-2]))
			i++
			for i < len(b) {
				if backslashEscapes && b[i] == '\\' && i+1 < len(b) {
					b[i], b[i+1] = ' ', ' '
					i += 2
					continue
				}
				if b[i] == quote {
					if i+1 < len(b) && b[i+1] == quote {
						b[i], b[i+1] = ' ', ' '
						i += 2
						continue
					}
					i++
					break
				}
				b[i] = ' '
				i++
			}
		case i+1 < len(b) && b[i] == '-' && b[i+1] == '-':
			for i < len(b) && b[i] != '\n' {
				b[i] = ' '
				i++
			}
		case i+1 < len(b) && b[i] == '/' && b[i+1] == '*':
			b[i], b[i+1] = ' ', ' '
			i += 2
			depth := 1
			for i < len(b) && depth > 0 {
				switch {
				case i+1 < len(b) && b[i] == '/' && b[i+1] == '*':
					b[i], b[i+1] = ' ', ' '
					i += 2
					depth++
				case i+1 < len(b) && b[i] == '*' && b[i+1] == '/':
					b[i], b[i+1] = ' ', ' '
					i += 2
					depth--
				default:
					b[i] = ' '
					i++
				}
			}
		case b[i] == '$':
			end := i + 1
			for end < len(b) && isDollarTagByte(b[end], end == i+1) {
				end++
			}
			if end >= len(b) || b[end] != '$' {
				i++
				continue
			}
			tag := string(b[i : end+1])
			closeAt := strings.Index(string(b[end+1:]), tag)
			if closeAt < 0 {
				i++
				continue
			}
			stop := end + 1 + closeAt + len(tag)
			for ; i < stop; i++ {
				b[i] = ' '
			}
		default:
			i++
		}
	}
	return string(b)
}

func isDollarTagByte(c byte, first bool) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || !first && c >= '0' && c <= '9'
}

func isSQLIdentifierByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
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

func compatibleFieldTypes(sqlite, postgres []apiField) bool {
	if len(sqlite) != len(postgres) {
		return false
	}
	for i := range sqlite {
		if !compatibleType(sqlite[i].Name, sqlite[i].Type, postgres[i].Type) {
			return false
		}
	}
	return true
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
			if usedPostgres[pgIndex] || sqName != pgName || checkTypes && !compatibleType(sqlite[sqIndex].Name, sqlite[sqIndex].Type, postgres[pgIndex].Type) {
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
			if checkTypes && !compatibleType(sqlite[sqIndex].Name, sqlite[sqIndex].Type, postgres[pgIndex].Type) {
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
			if !compatibleType(fieldName, sqlite.Results[i].Type, postgres.Results[i].Type) {
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
		if !ok || !compatibleType(field.Name, field.Type, pgType) {
			return false
		}
	}
	return true
}

func compatibleType(name, sqlite, postgres string) bool {
	if sqlite == postgres {
		return true
	}
	if booleanContractFields[name] {
		return sqlite == "int64" && postgres == "bool"
	}
	if isTimestampContractField(name) {
		return (sqlite == "string" || sqlite == "sql.NullString" || sqlite == "interface{}") && postgres == "pgtype.Timestamptz"
	}
	if name == "ApprovedWindows" {
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
	"Prepared": true, "Suspended": true, "ConfirmRestoredCredentials": true, // runtime configuration booleans map INTEGER to BOOLEAN
	"PayloadPresent": true, "Protected": true, "SchemaOverride": true, "Secret": true,
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
