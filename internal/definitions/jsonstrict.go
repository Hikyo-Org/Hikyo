package definitions

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Strict JSON decoding shared by every closed artifact schema. It lives here
// because internal/importer already depends on this package (the canonical
// bundle type), so the dependency direction forces these neutral helpers down
// rather than duplicating the token walk. They return typed, taxonomy-free
// errors; each caller maps them to its own refusal vocabulary (this package to
// domain sentinels with caller-safe detail, the importer to its Code set).

// DuplicateMemberError names an object member that appeared more than once.
// encoding/json otherwise accepts duplicates with last-one-wins semantics,
// which is unsafe for a reviewed artifact.
type DuplicateMemberError struct{ Member string }

func (e *DuplicateMemberError) Error() string {
	return "object member " + e.Member + " appears more than once"
}

// UnknownFieldError names a field the target schema does not know — for a
// closed, versioned artifact that always means a version mismatch.
type UnknownFieldError struct{ Field string }

func (e *UnknownFieldError) Error() string { return "unknown field " + e.Field }

// ErrMalformed is a JSON syntax or shape failure; ErrTrailing is content after
// the top-level document. Both are content-free on purpose.
var (
	ErrMalformed = errors.New("not a well-formed document")
	ErrTrailing  = errors.New("trailing content after the document")
)

// DecodeStrict rejects duplicate object members and unknown fields, decodes
// into `into`, and refuses trailing content. It is the closed-schema decode
// every artifact parser funnels through.
func DecodeStrict(raw []byte, into any) error {
	if err := rejectDuplicateMembers(raw, reflect.TypeOf(into)); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		if field, ok := unknownField(err); ok {
			return &UnknownFieldError{Field: field}
		}
		return ErrMalformed
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ErrTrailing
	}
	return nil
}

// unknownField extracts the field name from encoding/json's unknown-field
// error, whose message is `json: unknown field "x"`.
func unknownField(err error) (string, bool) {
	msg := err.Error()
	const marker = "unknown field "
	i := strings.Index(msg, marker)
	if i < 0 {
		return "", false
	}
	field, err := strconv.Unquote(msg[i+len(marker):])
	return field, err == nil
}

// RejectDuplicateMembers walks the raw token stream before any decode.
func RejectDuplicateMembers(raw []byte) error {
	return rejectDuplicateMembers(raw, nil)
}

func rejectDuplicateMembers(raw []byte, schema reflect.Type) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := walkJSONValue(dec, schema); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return ErrTrailing
	}
	return nil
}

func walkJSONValue(dec *json.Decoder, schema reflect.Type) error {
	// A custom decoder controls its own shape. Do not infer map permissions
	// from the Go representation of RawMessage or another opaque JSON type.
	unmarshaler := reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()
	for schema != nil {
		if schema.Implements(unmarshaler) || schema.Kind() != reflect.Pointer && reflect.PointerTo(schema).Implements(unmarshaler) {
			schema = nil
			break
		}
		if schema.Kind() != reflect.Pointer {
			break
		}
		schema = schema.Elem()
	}
	tok, err := dec.Token()
	if err != nil {
		return ErrMalformed
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]struct{}{}
		for dec.More() {
			member, err := dec.Token()
			if err != nil {
				return ErrMalformed
			}
			key, ok := member.(string)
			if !ok {
				return ErrMalformed
			}
			folded := key
			if schema == nil || schema.Kind() != reflect.Map && schema.Kind() != reflect.Interface {
				folded = foldJSONMember(key)
			}
			if _, dup := seen[folded]; dup {
				return &DuplicateMemberError{Member: key}
			}
			seen[folded] = struct{}{}
			if err := walkJSONValue(dec, jsonMemberType(schema, key)); err != nil {
				return err
			}
		}
		if end, err := dec.Token(); err != nil || end != json.Delim('}') {
			return ErrMalformed
		}
	case '[':
		if schema != nil && (schema.Kind() == reflect.Array || schema.Kind() == reflect.Slice) {
			schema = schema.Elem()
		} else if schema == nil || schema.Kind() != reflect.Interface {
			schema = nil
		}
		for dec.More() {
			if err := walkJSONValue(dec, schema); err != nil {
				return err
			}
		}
		if end, err := dec.Token(); err != nil || end != json.Delim(']') {
			return ErrMalformed
		}
	default:
		return ErrMalformed
	}
	return nil
}

// foldJSONMember mirrors encoding/json's case-insensitive struct-field match, so
// exact and case-variant spellings occupy one logical member slot before the
// struct decode can apply last-value-wins to them.
func foldJSONMember(name string) string {
	return strings.Map(func(r rune) rune {
		for {
			next := unicode.SimpleFold(r)
			if next <= r {
				return next
			}
			r = next
		}
	}, name)
}

// Map keys are source identities, not struct field names. Follow the destination
// schema so a map may contain both X and x, while Hash/hash cannot overwrite one
// struct field. Anonymous fields follow encoding/json's promoted-field shape.
func jsonMemberType(schema reflect.Type, key string) reflect.Type {
	if schema == nil {
		return nil
	}
	if schema.Kind() == reflect.Map {
		return schema.Elem()
	}
	if schema.Kind() == reflect.Interface {
		return schema
	}
	if schema.Kind() != reflect.Struct {
		return nil
	}
	// Resolve the same JSON name by shallowest depth, then an explicit tag.
	// Equal candidates are ambiguous and encoding/json ignores them. Exact
	// names precede folded names; folded ties follow original field order.
	type member struct {
		typ               reflect.Type
		index             []int
		tagged, ambiguous bool
	}
	members := map[string]member{}
	parents := map[reflect.Type]bool{}
	var visit func(reflect.Type, []int)
	visit = func(typ reflect.Type, path []int) {
		if parents[typ] {
			return
		}
		parents[typ] = true
		defer delete(parents, typ)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			nested := field.Type
			for nested.Kind() == reflect.Pointer {
				nested = nested.Elem()
			}
			if field.PkgPath != "" && (!field.Anonymous || nested.Kind() != reflect.Struct) {
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			index := append(slices.Clone(path), i)
			if field.Anonymous && name == "" && nested.Kind() == reflect.Struct {
				visit(nested, index)
				continue
			}
			tagged := name != ""
			if name == "" {
				name = field.Name
			}
			candidate := member{typ: field.Type, index: index, tagged: tagged}
			if previous, exists := members[name]; exists {
				if len(previous.index) < len(index) || len(previous.index) == len(index) && previous.tagged && !tagged {
					continue
				}
				if len(previous.index) == len(index) && previous.tagged == tagged {
					previous.ambiguous = true
					members[name] = previous
					continue
				}
			}
			members[name] = candidate
		}
	}
	visit(schema, nil)
	if exact, exists := members[key]; exists && !exact.ambiguous {
		return exact.typ
	}
	var folded member
	for name, candidate := range members {
		if candidate.ambiguous || foldJSONMember(name) != foldJSONMember(key) {
			continue
		}
		if folded.typ == nil || slices.Compare(candidate.index, folded.index) < 0 {
			folded = candidate
		}
	}
	return folded.typ
}
