package api_test

import (
	"bytes"
	"encoding/json/v2"
	"go/types"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// Under encoding/json/v2, omitempty drops a non-nil pointer to "", [] or {};
// omitzero drops only nil, matching v1 omitempty, except that it also drops a
// pointer to a value whose IsZero() is true. The overlay
// (api/omitzero-overlay.yaml) must therefore leave no omitempty on any json tag
// of a generated field, and omitzero must stay on pointers and interfaces (an
// omitzero slice or map would keep an empty non-nil value v1 omitted). The
// only pointee allowed an IsZero method, own or promoted, through an alias or
// a defined type, is the reviewed time.Time.
func TestApigenUsesOmitzeroNotOmitempty(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedTypes}, "github.com/Hikyo-Org/hikyo/api/apigen")
	if err != nil || len(pkgs) != 1 || len(pkgs[0].Errors) > 0 {
		t.Fatalf("load apigen: %v %v", err, pkgs)
	}
	var timeType types.Type
	for _, imp := range pkgs[0].Types.Imports() {
		if imp.Path() == "time" {
			timeType = imp.Scope().Lookup("Time").Type()
		}
	}
	if timeType == nil {
		t.Fatal("apigen does not import time; update this test")
	}
	zeroFields := 0
	seen := map[*types.Struct]bool{}
	var walk func(where string, typ types.Type)
	walk = func(where string, typ types.Type) {
		switch u := typ.Underlying().(type) {
		case *types.Pointer:
			walk(where, u.Elem())
		case *types.Slice:
			walk(where, u.Elem())
		case *types.Map:
			walk(where, u.Elem())
		case *types.Struct:
			if seen[u] {
				return
			}
			seen[u] = true
			for i := range u.NumFields() {
				f := u.Field(i)
				name := where + "." + f.Name()
				tag := reflect.StructTag(u.Tag(i)).Get("json")
				for _, o := range strings.Split(tag, ",")[1:] {
					switch o {
					case "omitempty":
						t.Errorf("%s: json tag %q uses omitempty; run `go run ./api/omitzero` and regenerate apigen", name, tag)
					case "omitzero":
						zeroFields++
						switch ft := f.Type().Underlying().(type) {
						case *types.Interface:
						case *types.Pointer:
							method, _, _ := types.LookupFieldOrMethod(f.Type(), true, nil, "IsZero")
							if method != nil && !types.Identical(ft.Elem(), timeType) {
								t.Errorf("%s: omitzero on *%s, which has IsZero; a non-nil zero value would be dropped where v1 sent it", name, ft.Elem())
							}
						default:
							t.Errorf("%s: omitzero on %s, want a pointer or interface (it keeps or drops values v1 omitempty did not)", name, f.Type())
						}
					}
				}
				walk(name, f.Type())
			}
		}
	}
	scope := pkgs[0].Types.Scope()
	for _, n := range scope.Names() {
		if obj, ok := scope.Lookup(n).(*types.TypeName); ok {
			walk(n, obj.Type())
		}
	}
	if zeroFields == 0 {
		t.Fatal("no omitzero fields found; the overlay is not applied")
	}
}

func TestApigenEncodesPresentEmptyValues(t *testing.T) {
	empty := ""
	bypassers := []apigen.ID{}
	for name, tc := range map[string]struct {
		in   any
		want string
	}{
		"empty string pointer": {apigen.UpdateKeyMetadataRequest{Description: &empty}, `"description":""`},
		"empty slice pointer":  {apigen.AccessPolicyInput{Bypassers: &bypassers}, `"bypassers":[]`},
	} {
		got, err := json.Marshal(tc.in, jsonwire.Stable)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Contains(got, []byte(tc.want)) {
			t.Errorf("%s: %s lacks %s", name, got, tc.want)
		}
	}
	got, err := json.Marshal(apigen.UpdateKeyMetadataRequest{}, jsonwire.Stable)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("description")) {
		t.Errorf("nil pointer must stay omitted: %s", got)
	}
}
