// Package reflectx keeps reflection out of packages that handle authz.Proof
// values: the forgery guard bans reflect there, and this package never sees a
// proof it could assemble. It is for tests. The boundary test
// TestReflectxCannotForgeUnexportedFields rejects unsafe, reflect.NewAt and the
// Unsafe*/Pointer() accessors here; that syntactic check is the guard, not a
// proof that no other route to unexported fields exists.
package reflectx

import (
	"reflect"
	"strings"
	"testing"
)

// Equal reports whether a and b are deeply equal (reflect.DeepEqual).
func Equal(a, b any) bool { return reflect.DeepEqual(a, b) }

// FieldCount is the number of fields of struct type T.
func FieldCount[T any]() int { return reflect.TypeFor[T]().NumField() }

// HasField reports whether struct type T has a field with the given name.
func HasField[T any](name string) bool {
	_, ok := reflect.TypeFor[T]().FieldByName(name)
	return ok
}

// CollectionTakers lists the exported methods of v whose name contains
// substr (case-insensitively) and that take a slice or map parameter, as
// "Method takes a slice|map".
func CollectionTakers(v any, substr string) []string {
	typ := reflect.TypeOf(v)
	var out []string
	for i := range typ.NumMethod() {
		m := typ.Method(i)
		if !strings.Contains(strings.ToLower(m.Name), strings.ToLower(substr)) {
			continue
		}
		for j := range m.Type.NumIn() {
			if kind := m.Type.In(j).Kind(); kind == reflect.Slice || kind == reflect.Map {
				out = append(out, m.Name+" takes a "+kind.String())
			}
		}
	}
	return out
}

// AggregateMethods maps "accessor.method" (lower-cased accessor) for every
// method of every interface a no-argument accessor of interface type T returns.
func AggregateMethods[T any]() map[string]bool {
	out := map[string]bool{}
	bundle := reflect.TypeFor[T]()
	for i := range bundle.NumMethod() {
		acc := bundle.Method(i)
		if acc.Type.NumIn() != 0 || acc.Type.NumOut() != 1 {
			continue
		}
		agg := acc.Type.Out(0)
		if agg.Kind() != reflect.Interface {
			continue
		}
		for j := range agg.NumMethod() {
			out[strings.ToLower(acc.Name)+"."+agg.Method(j).Name] = true
		}
	}
	return out
}

// Sentinel is the value Plant writes into a leaf.
const Sentinel = "AKIAIOSFODNN7EXAMPLE_sentinel"

// Leaf is one author-controlled content leaf discovered by the walk: its
// dotted path from the root type (for diagnostics and the anti-vacuity guard)
// and a setter that plants the sentinel at exactly that leaf in a fresh,
// addressable root value — allocating pointers and one-element slices along the
// way so the containing entry actually exists for the extractor to see.
type Leaf struct {
	Path string
	set  func(root reflect.Value)
}

// navFn returns the addressable slot for the current position, given the
// addressable root. Composing navFns is how the walk threads pointer allocation
// and slice materialisation down to a deep leaf.
type navFn func(root reflect.Value) reflect.Value

func isContentLeafType(typ reflect.Type) bool {
	if typ.Kind() == reflect.String {
		return true
	}
	return typ.Kind() == reflect.Slice && (typ.Elem().Kind() == reflect.String || typ.Elem().Kind() == reflect.Uint8)
}

// collect recursively enumerates the author-controlled content
// leaves of typ. It FAILS CLOSED: any reflect kind it does not recognise is a
// t.Errorf, not a silent skip — "prove by construction" means an unhandled shape
// cannot pass as a no-op. Numeric and boolean scalars are the only silent skips,
// because a credential is a string and a number/bool field cannot carry one.
func collect(t testing.TB, typ reflect.Type, path string, excluded map[string]string, nav navFn) []Leaf {
	t.Helper()
	switch typ.Kind() {
	case reflect.String:
		return []Leaf{{path, func(root reflect.Value) {
			nav(root).SetString(Sentinel)
		}}}

	case reflect.Pointer:
		return collect(t, typ.Elem(), path, excluded, func(root reflect.Value) reflect.Value {
			slot := nav(root)
			if slot.IsNil() {
				slot.Set(reflect.New(typ.Elem()))
			}
			return slot.Elem()
		})

	case reflect.Slice:
		elem := typ.Elem()
		switch {
		case elem.Kind() == reflect.Uint8:
			// []byte / json.RawMessage is a NAMED TERMINAL-BLOB boundary: the
			// scanner runs one rule pass over the whole blob (declarationLeaves
			// emits it verbatim) and never parses its interior, so the walk stops
			// here rather than descending into the JSON structure.
			return []Leaf{{path, func(root reflect.Value) {
				nav(root).SetBytes([]byte(Sentinel))
			}}}
		case elem.Kind() == reflect.String:
			return []Leaf{{path, func(root reflect.Value) {
				slot := nav(root)
				slot.Set(reflect.MakeSlice(typ, 1, 1))
				slot.Index(0).SetString(Sentinel)
			}}}
		case elem.Kind() == reflect.Struct || (elem.Kind() == reflect.Pointer && elem.Elem().Kind() == reflect.Struct):
			// A slice of structs (e.g. any_of []Rule, bundle Keys []Key): recurse
			// into element 0, materialising a one-element slice so it exists.
			return collect(t, elem, path, excluded, func(root reflect.Value) reflect.Value {
				slot := nav(root)
				if slot.Len() == 0 {
					slot.Set(reflect.MakeSlice(typ, 1, 1))
				}
				return slot.Index(0)
			})
		default:
			t.Errorf("coverage walk: unhandled slice element kind %s at %s", elem.Kind(), path)
			return nil
		}

	case reflect.Struct:
		var out []Leaf
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if reason, isExcluded := excluded[typ.Name()+"."+f.Name]; isExcluded {
				if !isContentLeafType(f.Type) {
					t.Errorf("coverage walk: exclusion %s.%s is an aggregate, not a leaf (%s): %s", typ.Name(), f.Name, f.Type, reason)
				} else {
					continue // named terminal-leaf exclusion, justified above
				}
			}
			idx := i
			childNav := func(root reflect.Value) reflect.Value { return nav(root).Field(idx) }
			out = append(out, collect(t, f.Type, path+"."+f.Name, excluded, childNav)...)
		}
		return out

	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return nil // scalar constraint field: structurally cannot carry a credential string

	default:
		t.Errorf("coverage walk: unhandled kind %s at %s", typ.Kind(), path)
		return nil
	}
}

// ContentLeaves walks the type of root and returns its content leaves, with the
// named OwnerType.Field leaves in excluded left out.
func ContentLeaves(t testing.TB, root any, excluded map[string]string) []Leaf {
	t.Helper()
	typ := reflect.TypeOf(root)
	return collect(t, typ, typ.Name(), excluded, func(r reflect.Value) reflect.Value { return r })
}

// Plant returns a fresh T with the sentinel set at exactly one leaf.
func Plant[T any](l Leaf) T {
	root := reflect.New(reflect.TypeFor[T]()).Elem()
	l.set(root)
	return root.Interface().(T)
}
