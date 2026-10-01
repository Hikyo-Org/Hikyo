package lint

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"regexp"
	"strings"
)

// sqlc's SQLite list form rewrites its constant and constructs a variadic
// argument list. Accept only its canonical single-slice prelude. Unknown
// rewrites remain unproved; swapped scalar fields retain their actual order.
func generatedSliceBindSites(body *ast.BlockStmt, querySQL map[string]string) ([]string, bool) {
	if len(body.List) < 4 {
		return nil, false
	}
	assignment, ok := body.List[0].(*ast.AssignStmt)
	if !ok || len(assignment.Lhs) != 1 || len(assignment.Rhs) != 1 || assignment.Tok != token.DEFINE {
		return nil, false
	}
	variable, ok := assignment.Lhs[0].(*ast.Ident)
	if !ok || variable.Name != "query" {
		return nil, false
	}
	constant, ok := assignment.Rhs[0].(*ast.Ident)
	if !ok {
		return nil, false
	}
	sql, ok := querySQL[constant.Name]
	if !ok {
		return nil, false
	}
	markers := sliceMarkerRe.FindAllStringSubmatch(sql, -1)
	if len(markers) != 1 {
		return nil, false
	}
	var fields []string
	index := 2
	for ; index < len(body.List); index++ {
		match := sliceAppendRe.FindStringSubmatch(statementString(body.List[index]))
		if match == nil {
			break
		}
		fields = append(fields, match[1])
	}
	if index >= len(body.List)-1 {
		return nil, false
	}
	branch, ok := body.List[index].(*ast.IfStmt)
	if !ok {
		return nil, false
	}
	match := sliceConditionRe.FindStringSubmatch(expressionString(branch.Cond))
	if match == nil {
		return nil, false
	}
	sliceField := match[1]
	statement := body.List[index+1]
	callAssignment, ok := statement.(*ast.AssignStmt)
	if !ok || len(callAssignment.Rhs) != 1 {
		return nil, false
	}
	call, ok := callAssignment.Rhs[0].(*ast.CallExpr)
	if !ok || !call.Ellipsis.IsValid() || len(call.Args) != 3 {
		return nil, false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !generatedDBCalls[selector.Sel.Name] {
		return nil, false
	}
	if expressionString(call.Args[0]) != "ctx" || expressionString(call.Args[1]) != "query" || expressionString(call.Args[2]) != "queryParams" {
		return nil, false
	}
	var expected strings.Builder
	fmt.Fprintf(&expected, "package fixture\nfunc queryPrelude(){\nquery := %s\nvar queryParams []interface{}\n", constant.Name)
	for _, field := range fields {
		fmt.Fprintf(&expected, "queryParams = append(queryParams, arg.%s)\n", field)
	}
	marker := "/*SLICE:" + markers[0][1] + "*/?"
	fmt.Fprintf(&expected, `if len(arg.%s) > 0 {
 for _, v := range arg.%s { queryParams = append(queryParams, v) }
 query = strings.Replace(query, %q, strings.Repeat(",?", len(arg.%s))[1:], 1)
 } else { query = strings.Replace(query, %q, "NULL", 1) }
 }
 `, sliceField, sliceField, marker, sliceField, marker)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "slice.go", expected.String(), 0)
	if err != nil {
		return nil, false
	}
	expectedBody := parsed.Decls[0].(*ast.FuncDecl).Body
	actualBody := &ast.BlockStmt{List: body.List[:index+1]}
	var actualBytes, expectedBytes bytes.Buffer
	if format.Node(&actualBytes, token.NewFileSet(), actualBody) != nil || format.Node(&expectedBytes, fset, expectedBody) != nil {
		return nil, false
	}
	// Positions from another file can affect formatting. Compare normalized
	// token spelling, retaining every identifier/operator/literal.
	if compactGo(actualBytes.String()) != compactGo(expectedBytes.String()) {
		return nil, false
	}
	fields = append(fields, sliceField)
	ordinals := bindOrdinals(sql, "sqlite")
	markerOffset := strings.Index(sql, marker)
	if markerOffset < 0 || len(ordinals) == 0 || ordinals[len(ordinals)-1] != len(fields) || len(bindOrdinals(sql[:markerOffset], "sqlite")) != len(ordinals)-1 {
		return nil, false // this canonical prelude appends the final bind last
	}
	for _, ordinal := range ordinals[:len(ordinals)-1] {
		if ordinal >= len(fields) {
			return nil, false
		}
	}
	sites := make([]string, len(ordinals))
	for i, ordinal := range ordinals {
		if ordinal < 1 || ordinal > len(fields) {
			return nil, false
		}
		sites[i] = normalizeAPIName(fields[ordinal-1])
	}
	return sites, true
}

func compactGo(source string) string {
	var scan scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile("tokens.go", -1, len(source))
	scan.Init(file, []byte(source), nil, 0)
	var out strings.Builder
	for {
		_, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		fmt.Fprintf(&out, "%d:%q;", kind, literal)
	}
	return out.String()
}

var (
	sliceMarkerRe    = regexp.MustCompile(`/\*SLICE:(\w+)\*/\?`)
	sliceAppendRe    = regexp.MustCompile(`^queryParams = append\(queryParams, arg\.(\w+)\)$`)
	sliceConditionRe = regexp.MustCompile(`^len\(arg\.(\w+)\) > 0$`)
)

func statementString(statement ast.Stmt) string {
	var out bytes.Buffer
	if format.Node(&out, token.NewFileSet(), statement) != nil {
		return ""
	}
	return out.String()
}
