// Command gen compiles reviewed authority metadata into static Go.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"golang.org/x/tools/go/packages"
)

type forwarder struct {
	Name      string `json:"name"`
	Target    string `json:"target"`
	Signature string `json:"signature"`
	Doc       string `json:"doc,omitempty"`
}
type forwarders struct {
	Version    int               `json:"version"`
	Imports    map[string]string `json:"imports"`
	Forwarders []forwarder       `json:"forwarders"`
}
type wireRow struct {
	Key       string   `json:"key"`
	Class     string   `json:"class,omitempty"`
	Ops       []string `json:"operations,omitempty"`
	Events    []string `json:"events,omitempty"`
	Index     int      `json:"primary_index,omitempty"`
	NoPrimary string   `json:"no_primary,omitempty"`
}
type wireExtras struct {
	Version    int       `json:"version"`
	Extensions []wireRow `json:"extensions"`
	Entries    []wireRow `json:"entries"`
}
type catalog struct {
	Operations map[string]string // const name -> wire identifier
	Classes    map[string]string // const name -> authorization class
	Events     map[string]bool
}

const module = "github.com/Hikyo-Org/hikyo"

func main() {
	check := flag.Bool("check", false, "validate inputs and reject missing or stale static outputs")
	flag.Parse()
	if err := run(".", *check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func readJSON(path string, dest any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := definitions.DecodeStrict(b, dest); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// The Go interface is the reviewed allowlist and signature owner.
// A target annotation is needed only for a renamed resolver method.
func readForwarders(path string, dest *forwarders) error {
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, source, parser.ParseComments)
	if err != nil {
		return err
	}
	*dest = forwarders{Version: 1, Imports: map[string]string{}}
	directives := map[*ast.Comment]bool{}
	for _, group := range file.Comments {
		for _, comment := range group.List {
			text := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(comment.Text, "//"), "/*"))
			if strings.HasPrefix(text, "hikyo:forward") {
				directives[comment] = true
			}
		}
	}
	for _, imp := range file.Imports {
		value, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return err
		}
		name := filepath.Base(value)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		dest.Imports[name] = value
	}
	found := false
	for _, decl := range file.Decls {
		g, ok := decl.(*ast.GenDecl)
		if !ok {
			return fmt.Errorf("forwarders: allowlist file contains executable declarations")
		}
		if g.Tok == token.IMPORT {
			continue
		}
		if g.Tok != token.TYPE {
			return fmt.Errorf("forwarders: unsupported declaration")
		}
		for _, spec := range g.Specs {
			typ := spec.(*ast.TypeSpec)
			if typ.Name.Name != "txForwarded" {
				return fmt.Errorf("forwarders: unexpected type %s", typ.Name.Name)
			}
			if found {
				return fmt.Errorf("forwarders: duplicate interface")
			}
			found = true
			iface, ok := typ.Type.(*ast.InterfaceType)
			if !ok {
				return fmt.Errorf("forwarders: allowlist is not an interface")
			}
			for _, field := range iface.Methods.List {
				if len(field.Names) != 1 {
					return fmt.Errorf("forwarders: embedded interfaces are forbidden")
				}
				if _, ok := field.Type.(*ast.FuncType); !ok {
					return fmt.Errorf("forwarders: non-method entry")
				}
				entry := forwarder{Name: field.Names[0].Name, Target: field.Names[0].Name, Signature: "func (a *TxAuthorizer) " + string(source[fset.Position(field.Pos()).Offset:fset.Position(field.End()).Offset])}
				var docs []string
				annotated := false
				for _, group := range []*ast.CommentGroup{field.Doc, field.Comment} {
					if group == nil {
						continue
					}
					for _, comment := range group.List {
						if directives[comment] {
							if group != field.Doc || !strings.HasPrefix(comment.Text, "//hikyo:forward ") {
								return fmt.Errorf("forwarders: invalid target annotation %s", entry.Name)
							}
							if annotated {
								return fmt.Errorf("forwarders: duplicate target annotation %s", entry.Name)
							}
							entry.Target = strings.TrimPrefix(comment.Text, "//hikyo:forward ")
							if !token.IsIdentifier(entry.Target) {
								return fmt.Errorf("forwarders: invalid target annotation %s", entry.Name)
							}
							annotated = true
							delete(directives, comment)
						} else if group == field.Doc {
							docs = append(docs, comment.Text)
						}
					}
				}
				entry.Doc = strings.Join(docs, "\n")
				dest.Forwarders = append(dest.Forwarders, entry)
			}
		}
	}
	if !found {
		return fmt.Errorf("forwarders: txForwarded interface missing")
	}
	if len(directives) != 0 {
		return fmt.Errorf("forwarders: target annotation must directly document an allowlisted method")
	}
	return nil
}

func run(root string, check bool) error {
	var forwards forwarders
	var extras wireExtras
	if err := readForwarders(filepath.Join(root, "internal/authz/forwarders.go"), &forwards); err != nil {
		return err
	}
	if err := readJSON(filepath.Join(root, "internal/authz/wire_extras.json"), &extras); err != nil {
		return err
	}
	forward, err := renderForwarders(forwards)
	if err != nil {
		return err
	}
	cat, err := readCatalog(root)
	if err != nil {
		return err
	}
	ops, err := api.Operations()
	if err != nil {
		return err
	}
	wire, err := renderWire(ops, extras, cat)
	if err != nil {
		return err
	}
	outputs := map[string][]byte{
		filepath.Join(root, "internal/authz/forwarders_gen.go"):    forward,
		filepath.Join(root, "internal/authz/wire_registry_gen.go"): wire,
	}
	if err := validateTypes(root, outputs, forwards); err != nil {
		return err
	}
	for path, generated := range outputs {
		if err := updateOutput(path, generated, check); err != nil {
			return err
		}
	}
	return nil
}

func updateOutput(path string, generated []byte, check bool) error {
	if !check {
		return os.WriteFile(path, generated, 0o644)
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("generated output %s missing: %w", path, err)
	}
	if !bytes.Equal(existing, generated) {
		return fmt.Errorf("generated output %s is stale", path)
	}
	return nil
}

func renderForwarders(m forwarders) ([]byte, error) {
	if m.Version != 1 || len(m.Forwarders) == 0 {
		return nil, fmt.Errorf("forwarders: unsupported version or empty allowlist")
	}
	var b strings.Builder
	b.WriteString("// Code generated by go run ./internal/authz/gen; DO NOT EDIT.\n\npackage authz\n\n")
	names := make([]string, 0, len(m.Imports))
	for n := range m.Imports {
		names = append(names, n)
	}
	slices.Sort(names)
	b.WriteString("import (\n")
	for group := 0; group < 2; group++ {
		if group > 0 {
			b.WriteString("\n")
		}
		for _, n := range names {
			path := m.Imports[n]
			if !token.IsIdentifier(n) || n == "_" || n == "." || path == "" {
				return nil, fmt.Errorf("forwarders: invalid import %q", n)
			}
			external := strings.Contains(strings.Split(path, "/")[0], ".")
			if external != (group == 1) {
				continue
			}
			if n == filepath.Base(path) {
				fmt.Fprintf(&b, "%q\n", path)
			} else {
				fmt.Fprintf(&b, "%s %q\n", n, path)
			}
		}
	}
	b.WriteString(")\n\nvar _ txForwarded = (*TxAuthorizer)(nil)\n\n")
	list := slices.Clone(m.Forwarders)
	slices.SortFunc(list, func(a, b forwarder) int { return strings.Compare(a.Name, b.Name) })
	seen := map[string]bool{}
	for _, entry := range list {
		if !ast.IsExported(entry.Name) || !token.IsIdentifier(entry.Target) || seen[entry.Name] {
			return nil, fmt.Errorf("forwarders: invalid or duplicate entry %q", entry.Name)
		}
		seen[entry.Name] = true
		file, err := parser.ParseFile(token.NewFileSet(), "signature.go", "package authz\n"+entry.Signature+" {}", 0)
		if err != nil {
			return nil, fmt.Errorf("forwarders %s: %w", entry.Name, err)
		}
		if len(file.Decls) != 1 {
			return nil, fmt.Errorf("forwarders %s: multiple declarations", entry.Name)
		}
		fn, ok := file.Decls[0].(*ast.FuncDecl)
		if !ok || fn.Name.Name != entry.Name || fn.Recv == nil || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 || fn.Recv.List[0].Names[0].Name != "a" {
			return nil, fmt.Errorf("forwarders %s: unsupported receiver or name", entry.Name)
		}
		star, ok := fn.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			return nil, fmt.Errorf("forwarders %s: non-pointer receiver", entry.Name)
		}
		id, ok := star.X.(*ast.Ident)
		if !ok || id.Name != "TxAuthorizer" {
			return nil, fmt.Errorf("forwarders %s: wrong receiver", entry.Name)
		}
		var args []string
		for _, p := range fn.Type.Params.List {
			if len(p.Names) == 0 {
				return nil, fmt.Errorf("forwarders %s: unnamed argument", entry.Name)
			}
			for _, n := range p.Names {
				if n.Name == "_" {
					return nil, fmt.Errorf("forwarders %s: blank argument", entry.Name)
				}
				args = append(args, n.Name)
			}
		}
		if len(fn.Type.Params.List) > 0 {
			if _, ok := fn.Type.Params.List[len(fn.Type.Params.List)-1].Type.(*ast.Ellipsis); ok {
				args[len(args)-1] += "..."
			}
		}
		if entry.Doc != "" {
			for _, line := range strings.Split(entry.Doc, "\n") {
				if !strings.HasPrefix(strings.TrimSpace(line), "//") {
					return nil, fmt.Errorf("forwarders %s: documentation must contain only comments", entry.Name)
				}
			}
			b.WriteString(entry.Doc + "\n")
		}
		b.WriteString(entry.Signature + " {\n")
		if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
			b.WriteString("return ")
		}
		fmt.Fprintf(&b, "a.r.%s(%s)\n}\n\n", entry.Target, strings.Join(args, ", "))
	}
	return format.Source([]byte(b.String()))
}

func readCatalog(root string) (catalog, error) {
	cat := catalog{Operations: map[string]string{}, Classes: map[string]string{}, Events: map[string]bool{}}
	for _, scope := range []string{"internal/authz/registry.go", "internal/audit/registry.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, scope), nil, 0)
		if err != nil {
			return cat, err
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, s := range g.Specs {
				v, ok := s.(*ast.ValueSpec)
				if !ok {
					continue
				}
				if g.Tok == token.CONST {
					for i, n := range v.Names {
						if !strings.HasPrefix(n.Name, "Op") {
							continue
						}
						if len(v.Values) <= i {
							return cat, fmt.Errorf("catalog: unsupported operation %s", n.Name)
						}
						lit, ok := v.Values[i].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							return cat, fmt.Errorf("catalog: nonliteral operation %s", n.Name)
						}
						value, err := strconv.Unquote(lit.Value)
						if err != nil {
							return cat, err
						}
						cat.Operations[n.Name] = value
					}
				}
				if len(v.Names) != 1 || len(v.Values) != 1 {
					continue
				}
				name := v.Names[0].Name
				if name != "operationTable" && !(scope == "internal/audit/registry.go" && name == "registry") {
					continue
				}
				lit, ok := v.Values[0].(*ast.CompositeLit)
				if !ok {
					return cat, fmt.Errorf("catalog: unsupported %s", name)
				}
				for _, el := range lit.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						return cat, fmt.Errorf("catalog: unkeyed %s", name)
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok {
						return cat, fmt.Errorf("catalog: unsupported key in %s", name)
					}
					if name == "registry" {
						cat.Events[key.Name] = true
						continue
					}
					row, ok := kv.Value.(*ast.CompositeLit)
					if !ok {
						return cat, fmt.Errorf("catalog: unsupported row %s", key.Name)
					}
					for _, field := range row.Elts {
						kv, ok := field.(*ast.KeyValueExpr)
						if !ok {
							return cat, fmt.Errorf("catalog: unkeyed operation %s", key.Name)
						}
						n, ok := kv.Key.(*ast.Ident)
						if !ok {
							return cat, fmt.Errorf("catalog: invalid operation field")
						}
						if n.Name == "class" {
							value, ok := kv.Value.(*ast.Ident)
							if !ok {
								return cat, fmt.Errorf("catalog: nonliteral class %s", key.Name)
							}
							cat.Classes[key.Name] = value.Name
						}
					}
					if cat.Classes[key.Name] == "" {
						return cat, fmt.Errorf("catalog: missing class %s", key.Name)
					}
				}
			}
		}
	}
	if len(cat.Classes) == 0 || len(cat.Events) == 0 {
		return cat, fmt.Errorf("catalog: missing operation or event table")
	}
	return cat, nil
}

func renderWire(ops map[string]api.Operation, extras wireExtras, cat catalog) ([]byte, error) {
	if extras.Version != 1 {
		return nil, fmt.Errorf("wire: unsupported manifest version")
	}
	classes := map[string]string{"tenant": "ClassTenant", "instance": "ClassInstance", "unauthenticated": "ClassUnauthenticated", "system": "ClassSystem"}
	byValue := map[string]string{}
	for name, value := range cat.Operations {
		if old := byValue[value]; old != "" {
			return nil, fmt.Errorf("wire: duplicate operation identifier %s", value)
		}
		byValue[value] = name
	}
	extensions := map[string]wireRow{}
	for _, row := range extras.Extensions {
		if row.Key == "" || extensions[row.Key].Key != "" || row.Class != "" {
			return nil, fmt.Errorf("wire: duplicate or conflicting extension %q", row.Key)
		}
		extensions[row.Key] = row
	}
	rows := map[string]wireRow{}
	ids := map[string]bool{}
	for _, op := range ops {
		key := "http:" + op.Method + " " + op.Path
		class := classes[op.Class]
		if class == "" || op.ID == "" || ids[op.ID] || rows[key].Key != "" || !strings.HasPrefix(op.Path, "/") || strings.Count(op.Path, "{") != strings.Count(op.Path, "}") {
			return nil, fmt.Errorf("wire: invalid/duplicate contract operation %q", key)
		}
		ids[op.ID] = true
		row := wireRow{Key: key, Class: class}
		extra, present := extensions[key]
		delete(extensions, key)
		if op.AuthzOp != "" {
			primary := byValue[op.AuthzOp]
			if primary == "" || cat.Classes[primary] != class {
				return nil, fmt.Errorf("wire: unknown or conflicting primary operation %q", op.AuthzOp)
			}
			if extra.NoPrimary != "" {
				return nil, fmt.Errorf("wire: stale no-primary exception %s", key)
			}
			if extra.Index < 0 || extra.Index > len(extra.Ops) {
				return nil, fmt.Errorf("wire: invalid primary position %s", key)
			}
			row.Ops = append(row.Ops, extra.Ops[:extra.Index]...)
			row.Ops = append(row.Ops, primary)
			row.Ops = append(row.Ops, extra.Ops[extra.Index:]...)
		} else {
			if (class == "ClassTenant" || class == "ClassInstance" || len(extra.Ops) > 0) && extra.NoPrimary == "" {
				return nil, fmt.Errorf("wire: missing primary metadata %s", key)
			}
			if extra.Index != 0 {
				return nil, fmt.Errorf("wire: primary position without primary %s", key)
			}
			row.Ops = slices.Clone(extra.Ops)
		}
		row.Events = slices.Clone(extra.Events)
		if present && len(extra.Ops) == 0 && len(extra.Events) == 0 && extra.NoPrimary == "" {
			return nil, fmt.Errorf("wire: empty extension %s", key)
		}
		rows[key] = row
	}
	if len(extensions) > 0 {
		return nil, fmt.Errorf("wire: stale contract extensions")
	}
	for _, row := range extras.Entries {
		if row.Key == "" || rows[row.Key].Key != "" || row.NoPrimary != "" || row.Index != 0 {
			return nil, fmt.Errorf("wire: duplicate/conflicting explicit row %q", row.Key)
		}
		// OpenAPI owns HTTP metadata except the operational listener's
		// three contract-external GET routes (server.NewOperational).
		if strings.HasPrefix(row.Key, "http:") {
			switch row.Key {
			case "http:GET /healthz", "http:GET /metrics", "http:GET /readyz":
			default:
				return nil, fmt.Errorf("wire: explicit HTTP row must be defined in OpenAPI %q", row.Key)
			}
		}
		rows[row.Key] = row
	}
	var b strings.Builder
	b.WriteString("// Code generated by go run ./internal/authz/gen; DO NOT EDIT.\n\npackage authz\n\nimport \"" + module + "/internal/audit\"\n\nvar wireRegistry = map[string]wireEntry{\n")
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		row := rows[key]
		valid := row.Class == "ClassStub"
		for _, c := range classes {
			valid = valid || row.Class == c
		}
		if !valid || (row.Class == "ClassStub" && (len(row.Ops) > 0 || len(row.Events) > 0)) {
			return nil, fmt.Errorf("wire: invalid class or stub %s", key)
		}
		seen := map[string]bool{}
		for _, op := range row.Ops {
			if cat.Classes[op] == "" || seen[op] {
				return nil, fmt.Errorf("wire: unknown or repeated operation %s in %s", op, key)
			}
			seen[op] = true
		}
		clear(seen)
		for _, event := range row.Events {
			if !cat.Events[event] || seen[event] {
				return nil, fmt.Errorf("wire: unknown or repeated event %s in %s", event, key)
			}
			seen[event] = true
		}
		fmt.Fprintf(&b, "%q: {Class: %s", key, row.Class)
		if len(row.Ops) > 0 {
			fmt.Fprintf(&b, ", Ops: []Operation{%s}", strings.Join(row.Ops, ", "))
		}
		if len(row.Events) > 0 {
			events := make([]string, len(row.Events))
			for i, e := range row.Events {
				events[i] = "audit." + e
			}
			fmt.Fprintf(&b, ", Events: []audit.EventType{%s}", strings.Join(events, ", "))
		}
		b.WriteString("},\n")
	}
	b.WriteString("}\n")
	return format.Source([]byte(b.String()))
}

func validateTypes(root string, outputs map[string][]byte, m forwarders) error {
	overlay := map[string][]byte{}
	for path, b := range outputs {
		abs, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		overlay[abs] = b
	}
	pkgs, err := packages.Load(&packages.Config{Dir: root, Mode: packages.NeedTypes | packages.NeedImports | packages.NeedName, Overlay: overlay}, module+"/internal/authz")
	if err != nil {
		return err
	}
	if len(pkgs) != 1 {
		return fmt.Errorf("types: expected authz package")
	}
	pkg := pkgs[0]
	for _, e := range pkg.Errors {
		return fmt.Errorf("types: %s", e)
	}
	authorizer := pkg.Types.Scope().Lookup("TxAuthorizer")
	resolver := pkg.Imports[module+"/internal/store/authn"]
	if authorizer == nil || resolver == nil {
		return fmt.Errorf("types: missing authority types")
	}
	target := resolver.Types.Scope().Lookup("Resolver")
	if target == nil {
		return fmt.Errorf("types: missing concrete resolver")
	}
	for _, entry := range m.Forwarders {
		a, _, _ := types.LookupFieldOrMethod(types.NewPointer(authorizer.Type()), false, pkg.Types, entry.Name)
		r, _, _ := types.LookupFieldOrMethod(types.NewPointer(target.Type()), false, resolver.Types, entry.Target)
		if a == nil || r == nil {
			return fmt.Errorf("types: missing forwarded method %s -> %s", entry.Name, entry.Target)
		}
		strip := func(obj types.Object) *types.Signature {
			sig := obj.Type().(*types.Signature)
			return types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
		}
		if !types.Identical(strip(a), strip(r)) {
			return fmt.Errorf("types: forwarding contract differs %s -> %s", entry.Name, entry.Target)
		}
	}
	return nil
}
