package authz

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store/authn"
)

// This baseline captures the complete pre-generation public contract, rather
// than deriving expectations from the forwarding allowlist under test.
func TestTxAuthorizerPublicSurfaceIsPreserved(t *testing.T) {
	b, err := os.ReadFile("testdata/tx_authorizer_surface.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]string
	if err = json.Unmarshal(b, &expected); err != nil {
		t.Fatal(err)
	}
	actual := map[string]string{}
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		fs := token.NewFileSet()
		file, err := parser.ParseFile(fs, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !ast.IsExported(fn.Name.Name) {
				continue
			}
			star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
			if !ok {
				continue
			}
			name, ok := star.X.(*ast.Ident)
			if !ok || name.Name != "TxAuthorizer" {
				continue
			}
			var signature bytes.Buffer
			if err := format.Node(&signature, fs, fn.Type); err != nil {
				t.Fatal(err)
			}
			actual[fn.Name.Name] = signature.String()
		}
	}
	if !reflect.DeepEqual(actual, expected) {
		for name, signature := range expected {
			if actual[name] != signature {
				t.Errorf("public method %s changed: got %q want %q", name, actual[name], signature)
			}
		}
		for name := range actual {
			if _, known := expected[name]; !known {
				t.Errorf("unreviewed public method %s", name)
			}
		}
	}
	typ := reflect.TypeOf((*TxAuthorizer)(nil))
	resolver, exists := typ.Elem().FieldByName("r")
	if !exists || resolver.PkgPath == "" || resolver.Anonymous || resolver.Type != reflect.TypeOf((*authn.Resolver)(nil)) {
		t.Fatal("transaction authorizer must retain its private concrete resolver field")
	}
	if typ.NumMethod() != len(expected) {
		t.Fatalf("compiled method set has %d methods, baseline %d; possible promoted resolver exposure", typ.NumMethod(), len(expected))
	}
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		if _, known := expected[method.Name]; !known {
			t.Errorf("promoted unreviewed method %s", method.Name)
		}
		if containsResolver(method.Type, map[reflect.Type]bool{}) {
			t.Errorf("public method %s exposes concrete resolver", method.Name)
		}
	}
	if containsResolver(typ, map[reflect.Type]bool{}) {
		t.Fatal("public field exposes concrete resolver")
	}
}

func containsResolver(typ reflect.Type, seen map[reflect.Type]bool) bool {
	if typ == reflect.TypeOf(authn.Resolver{}) {
		return true
	}
	if seen[typ] {
		return false
	}
	seen[typ] = true
	switch typ.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
		return containsResolver(typ.Elem(), seen)
	case reflect.Map:
		return containsResolver(typ.Key(), seen) || containsResolver(typ.Elem(), seen)
	case reflect.Func:
		for i := 0; i < typ.NumIn(); i++ {
			if containsResolver(typ.In(i), seen) {
				return true
			}
		}
		for i := 0; i < typ.NumOut(); i++ {
			if containsResolver(typ.Out(i), seen) {
				return true
			}
		}
	case reflect.Struct:
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if (field.PkgPath == "" || field.Anonymous) && containsResolver(field.Type, seen) {
				return true
			}
		}
	case reflect.Interface:
		for i := 0; i < typ.NumMethod(); i++ {
			if containsResolver(typ.Method(i).Type, seen) {
				return true
			}
		}
	}
	return false
}

type resolverCarrier struct{ Resolver *authn.Resolver }

func TestResolverExposureGuardCatchesNestedSignaturesAndFields(t *testing.T) {
	for _, value := range []any{(*authn.Resolver)(nil), ([]*authn.Resolver)(nil), (map[string]*authn.Resolver)(nil), (func() *authn.Resolver)(nil), (*interface{ Get() *authn.Resolver })(nil), struct{ Resolver *authn.Resolver }{}, struct{ resolverCarrier }{}, struct{ *resolverCarrier }{}} {
		if !containsResolver(reflect.TypeOf(value), map[reflect.Type]bool{}) {
			t.Errorf("missed resolver exposure in %T", value)
		}
	}
	if containsResolver(reflect.TypeOf((*TxAuthorizer)(nil)), map[reflect.Type]bool{}) {
		t.Fatal("private resolver field incorrectly exposed")
	}
}

func TestGeneratedWireRegistryIsValid(t *testing.T) {
	if _, err := newWireRegistry(wireRegistry); err != nil {
		t.Fatal(err)
	}
}
