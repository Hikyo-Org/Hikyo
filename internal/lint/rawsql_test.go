package lint

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

type rawSQLContextImporter struct{}

func (rawSQLContextImporter) Import(path string) (*types.Package, error) {
	if path == "github.com/jackc/pgx/v5/pgconn" {
		pkg := types.NewPackage(path, "pgconn")
		obj := types.NewTypeName(token.NoPos, pkg, "CommandTag", nil)
		types.NewNamed(obj, types.NewStruct(nil, nil), nil)
		pkg.Scope().Insert(obj)
		batchObj := types.NewTypeName(token.NoPos, pkg, "Batch", nil)
		batch := types.NewNamed(batchObj, types.NewStruct(nil, nil), nil)
		pkg.Scope().Insert(batchObj)
		params := types.NewTuple(types.NewParam(token.NoPos, pkg, "sql", types.Typ[types.String]))
		batch.AddMethod(types.NewFunc(token.NoPos, pkg, "ExecParams", types.NewSignatureType(types.NewVar(token.NoPos, pkg, "b", types.NewPointer(batch)), nil, nil, params, nil, false)))
		pkg.MarkComplete()
		return pkg, nil
	}
	if path == "github.com/jackc/pgx/v5" || path == "github.com/jackc/pgx/v5/pgproto3" {
		name := "pgx"
		if strings.HasSuffix(path, "pgproto3") {
			name = "pgproto3"
		}
		pkg := types.NewPackage(path, name)
		for _, name := range []string{"Identifier", "Batch", "QueuedQuery", "Frontend"} {
			obj := types.NewTypeName(token.NoPos, pkg, name, nil)
			underlying := types.NewStruct(nil, nil)
			if name == "QueuedQuery" {
				underlying = types.NewStruct([]*types.Var{types.NewField(token.NoPos, pkg, "SQL", types.Typ[types.String], false)}, nil)
			}
			types.NewNamed(obj, underlying, nil)
			pkg.Scope().Insert(obj)
		}
		pkg.MarkComplete()
		return pkg, nil
	}
	if path == "database/sql" {
		pkg := types.NewPackage(path, "sql")
		obj := types.NewTypeName(token.NoPos, pkg, "Result", nil)
		types.NewNamed(obj, types.NewInterfaceType(nil, nil).Complete(), nil)
		pkg.Scope().Insert(obj)
		stmt := types.NewTypeName(token.NoPos, pkg, "Stmt", nil)
		types.NewNamed(stmt, types.NewStruct(nil, nil), nil)
		pkg.Scope().Insert(stmt)
		pkg.MarkComplete()
		return pkg, nil
	}
	if path == "database/sql/driver" {
		pkg := types.NewPackage(path, "driver")
		for _, name := range []string{"Result", "Rows"} {
			obj := types.NewTypeName(token.NoPos, pkg, name, nil)
			types.NewNamed(obj, types.NewInterfaceType(nil, nil).Complete(), nil)
			pkg.Scope().Insert(obj)
		}
		pkg.MarkComplete()
		return pkg, nil
	}
	pkg := types.NewPackage(path, "context")
	obj := types.NewTypeName(token.NoPos, pkg, "Context", nil)
	types.NewNamed(obj, types.NewInterfaceType(nil, nil).Complete(), nil)
	pkg.Scope().Insert(obj)
	pkg.MarkComplete()
	return pkg, nil
}

func rawSQLFixture(t *testing.T, banner, extra string) (string, *packages.Package, *ast.File) {
	t.Helper()
	root := t.TempDir()
	source := banner + `package fixture
import "context"
import "database/sql"
import "github.com/jackc/pgx/v5/pgconn"
import sqldriver "database/sql/driver"
import "github.com/jackc/pgx/v5"
import "github.com/jackc/pgx/v5/pgproto3"
type batchDriver interface { SendBatch(context.Context,*pgx.Batch)(int,error); ExecBatch(context.Context,*pgconn.Batch)(int,error); Frontend()*pgproto3.Frontend; Copy(context.Context,pgx.Identifier,[]string,any)(int,error) }
func batchSend(ctx context.Context,d batchDriver) { run:=d.SendBatch; run(ctx,nil) }
func batchExecute(ctx context.Context,d batchDriver) { d.ExecBatch(ctx,nil) }
func batchStage(b *pgconn.Batch) { b.ExecParams("DELETE FROM items") }
func batchStageAlias(b *pgconn.Batch) { run:=b.ExecParams; run("DELETE FROM items") }
func queuedLiteral() { _=pgx.QueuedQuery{SQL:"DELETE FROM items"} }
func frontend(d batchDriver) { run:=d.Frontend; run() }
func identifierCopy(ctx context.Context,d batchDriver) { d.Copy(ctx,pgx.Identifier{},nil,nil) }
type SQLText string
type lowDriver interface { Exec(SQLText,[]any)(sqldriver.Result,error); QueryContext(context.Context,SQLText,[]any)(sqldriver.Rows,error) }
func namedString(d lowDriver) { d.Exec(SQLText("DELETE FROM items"),nil) }
func driverRows(ctx context.Context,d lowDriver) { d.QueryContext(ctx,SQLText("SELECT * FROM items"),nil) }
type copyDriver interface { CopyFrom(context.Context,any,string)(pgconn.CommandTag,error); CopyTo(context.Context,any,string)(pgconn.CommandTag,error) }
func copyFrom(ctx context.Context,d copyDriver) { d.CopyFrom(ctx,nil,"COPY items FROM STDIN") }
func copyAlias(ctx context.Context,d copyDriver) { run := d.CopyTo; run(ctx,nil,"COPY items TO STDOUT") }
type plainDriver interface { Exec(string,...any)(sql.Result,error); PrepareContext(context.Context,string)(*sql.Stmt,error) }
func prepare(ctx context.Context,d plainDriver) { d.PrepareContext(ctx,"DELETE FROM items") }
func contextFree(d plainDriver) { d.Exec("DELETE FROM items") }
type logger interface { ErrorContext(context.Context,string,...any) }
func logging(ctx context.Context,l logger) { l.ErrorContext(ctx,"message",1) }
type driver interface { Exec(context.Context, string, ...any) (int64, error) }
func direct(ctx context.Context, d driver) { d.Exec(ctx, "DELETE FROM items") }
func methodValue(ctx context.Context, d driver) { run := d.Exec; run(ctx, "DELETE FROM items") }
func interfaceCall(ctx context.Context, d any) { d.(driver).Exec(ctx, "DELETE FROM items") }
func protocol(ctx context.Context, d driver) { d.Exec(ctx, "PRAGMA foreign_keys=ON") }
type params struct { ID string }
type queries struct{}
func (queries) Get(context.Context, params) (string,error) { return "",nil }
func generatedQuery(ctx context.Context, q queries) { q.Get(ctx, params{ID:"one"}) }
` + extra
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, "protocol.go"), source, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	configuration := types.Config{Importer: rawSQLContextImporter{}}
	checked, err := configuration.Check(Module+"/internal/store", fset, []*ast.File{file}, info)
	if err != nil {
		t.Fatal(err)
	}
	return root, &packages.Package{PkgPath: checked.Path(), Fset: fset, Types: checked, TypesInfo: info, Syntax: []*ast.File{file}}, file
}

func TestRawSQLRejectsMethodValuesInterfacesAndFakeGeneratedBanner(t *testing.T) {
	for _, banner := range []string{"", "// Code generated by pretend. DO NOT EDIT.\n"} {
		root, pkg, _ := rawSQLFixture(t, banner, "")
		findings := CheckRawSQL([]*packages.Package{pkg}, root, Module+"/internal/store", nil)
		for _, name := range []string{"direct", "methodValue", "interfaceCall", "protocol", "contextFree", "prepare", "copyFrom", "copyAlias", "namedString", "driverRows", "batchSend", "batchExecute", "batchStage", "batchStageAlias", "queuedLiteral", "frontend", "identifierCopy"} {
			if !strings.Contains(strings.Join(findings, "\n"), ":"+name+" executes handwritten SQL") {
				t.Errorf("raw %s escaped: %v", name, findings)
			}
		}
		if strings.Contains(strings.Join(findings, "\n"), "logging") {
			t.Fatalf("logger mistaken for SQL: %v", findings)
		}
		if strings.Contains(strings.Join(findings, "\n"), "generatedQuery") {
			t.Fatalf("typed parameter carrier mistaken for raw SQL: %v", findings)
		}
	}
}

func TestRawSQLChecksEveryInitDeclaration(t *testing.T) {
	root, pkg, _ := rawSQLFixture(t, "", `var d plainDriver
func init() {}
func init() { d.Exec("DELETE FROM items") }
`)
	findings := strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module+"/internal/store", nil), "\n")
	if !strings.Contains(findings, ":init executes handwritten SQL") {
		t.Fatal("second init escaped: " + findings)
	}
}

func TestRawSQLProtocolCallerPinsRejectNewReferencesAndDrift(t *testing.T) {
	root, pkg, file := rawSQLFixture(t, "", `
func approved(ctx context.Context,d driver) { protocol(ctx,d) }
func bypass(ctx context.Context,d driver) { alias := protocol; alias(ctx,d) }
`)
	hashes := map[string]string{}
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Body != nil {
			var body bytes.Buffer
			if err := format.Node(&body, pkg.Fset, fn.Body); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(body.Bytes())
			hashes[fn.Name.Name] = hex.EncodeToString(hash[:])
		}
	}
	pins := map[string]RawSQLProtocol{"protocol.go:protocol": {Hash: hashes["protocol"], Reason: "Closed protocol", Callers: map[string]string{"protocol.go:approved": hashes["approved"]}}}
	findings := strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module+"/internal/store", pins), "\n")
	if !strings.Contains(findings, "protocol.go:bypass references protocol") || strings.Contains(findings, "protocol.go:approved references protocol") {
		t.Fatal("caller ownership not preserved: " + findings)
	}
	pins["protocol.go:protocol"].Callers["protocol.go:approved"] = "changed"
	findings = strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module+"/internal/store", pins), "\n")
	if !strings.Contains(findings, "protocol.go:approved references protocol") {
		t.Fatal("caller drift escaped: " + findings)
	}
	pins["protocol.go:protocol"].Callers["protocol.go:deleted"] = "old"
	findings = strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module+"/internal/store", pins), "\n")
	if !strings.Contains(findings, "retire its caller pin") {
		t.Fatal("stale caller escaped: " + findings)
	}
}

func TestRawSQLProtocolIsNamedReasonedAndBodyPinned(t *testing.T) {
	root, pkg, file := rawSQLFixture(t, "", "")
	var body bytes.Buffer
	for _, declaration := range file.Decls {
		if fn, ok := declaration.(*ast.FuncDecl); ok && fn.Name.Name == "protocol" {
			if err := format.Node(&body, pkg.Fset, fn.Body); err != nil {
				t.Fatal(err)
			}
		}
	}
	hash := sha256.Sum256(body.Bytes())
	owner := "protocol.go:protocol"
	pins := map[string]RawSQLProtocol{owner: {Hash: hex.EncodeToString(hash[:]), Reason: "Named pragma boot protocol"}}
	for _, finding := range CheckRawSQL([]*packages.Package{pkg}, root, Module+"/internal/store", pins) {
		if strings.Contains(finding, ":protocol ") {
			t.Fatalf("reviewed protocol refused: %s", finding)
		}
	}
	pins[owner] = RawSQLProtocol{Hash: "changed", Reason: "Named pragma boot protocol"}
	if findings := strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module+"/internal/store", pins), "\n"); !strings.Contains(findings, "changed its reviewed engine protocol") {
		t.Fatalf("protocol drift escaped: %s", findings)
	}
	pins[owner] = RawSQLProtocol{Hash: hex.EncodeToString(hash[:])}
	if findings := strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module+"/internal/store", pins), "\n"); !strings.Contains(findings, owner+" executes handwritten SQL") {
		t.Fatalf("unjustified protocol accepted: %s", findings)
	}
}

func TestRawSQLRejectsUnownedInitializationAndStaleProtocol(t *testing.T) {
	root, pkg, _ := rawSQLFixture(t, "", `var initializer = func(ctx context.Context,d driver) { d.Exec(ctx,"DELETE FROM items") }`)
	findings := strings.Join(CheckRawSQL([]*packages.Package{pkg}, root, Module+"/internal/store", map[string]RawSQLProtocol{"deleted.go:protocol": {Hash: "old", Reason: "retired protocol"}}), "\n")
	if !strings.Contains(findings, "outside a named function owner") {
		t.Fatal("initializer escaped: " + findings)
	}
	if !strings.Contains(findings, "absent from analyzed production source") {
		t.Fatal("stale protocol escaped: " + findings)
	}
}

func TestRawSQLUnownedSourceStillRejectsExecution(t *testing.T) {
	_, pkg, _ := rawSQLFixture(t, "", "")
	findings := strings.Join(CheckRawSQL([]*packages.Package{pkg}, t.TempDir(), Module+"/internal/store", nil), "\n")
	if !strings.Contains(findings, "cannot identify source owner") {
		t.Fatal("unowned execution escaped: " + findings)
	}
}

func TestGeneratedSQLSourceRequiresRealNamedQuery(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "internal/store/sqlitegen/fake.sql.go")
	source := filepath.Join(root, "internal/store/queries/sqlite/fake.sql")
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), output, "// Code generated by sqlc. DO NOT EDIT.\npackage sqlitegen", parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if generatedSQLSource(root, output, file) {
		t.Fatal("missing query source accepted")
	}
	if err := os.WriteFile(source, []byte("-- empty file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if generatedSQLSource(root, output, file) {
		t.Fatal("empty query source accepted")
	}
	if err := os.WriteFile(source, []byte("-- name: Real :one\nSELECT 1;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if !generatedSQLSource(root, output, file) {
		t.Fatal("canonical named source refused")
	}
	fake, err := parser.ParseFile(token.NewFileSet(), output, "// Code generated by pretend. DO NOT EDIT.\npackage sqlitegen", parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	if generatedSQLSource(root, output, fake) {
		t.Fatal("fake generator header accepted inside canonical output")
	}
	if generatedSQLSource(root, filepath.Join(root, "fake.sql.go"), file) {
		t.Fatal("generated banner escaped source owner")
	}
	linked := filepath.Join(filepath.Dir(source), "linked.sql")
	if err := os.Symlink(source, linked); err != nil {
		t.Fatal(err)
	}
	if generatedSQLSource(root, filepath.Join(filepath.Dir(output), "linked.sql.go"), file) {
		t.Fatal("symlink query source accepted")
	}
}
