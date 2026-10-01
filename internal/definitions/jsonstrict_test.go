package definitions

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDecodeStrictDistinguishesMapKeysAndStructFields(t *testing.T) {
	type entry struct {
		Hash string `json:"hash"`
	}
	type artifact struct {
		Helpers map[string]entry `json:"helpers"`
	}
	var got artifact
	valid := `{"helpers":{"Owner.X":{"hash":"a"},"Owner.x":{"hash":"b"}}}`
	if err := DecodeStrict([]byte(valid), &got); err != nil {
		t.Fatal(err)
	}
	if got.Helpers["Owner.X"].Hash != "a" || got.Helpers["Owner.x"].Hash != "b" {
		t.Fatal(got)
	}
	for _, raw := range []string{
		`{"helpers":{"Owner.X":{"hash":"a"},"Owner.X":{"hash":"b"}}}`,
		`{"helpers":{"Owner.X":{"hash":"a","HASH":"b"}}}`,
		`{"helpers":{},"HELPERS":{}}`,
	} {
		var duplicate *DuplicateMemberError
		if err := DecodeStrict([]byte(raw), &got); !errors.As(err, &duplicate) {
			t.Fatalf("duplicate escaped: %s: %v", raw, err)
		}
	}
	for _, raw := range []string{`{"helpers":{"Owner.X":{"hash":"a","unknown":1}}}`, valid + `{}`, `{"helpers":`} {
		if err := DecodeStrict([]byte(raw), &got); err == nil {
			t.Fatalf("invalid input accepted: %s", raw)
		}
	}
}

func TestDecodeStrictFollowsNestedArrayMapAndPromotedSchemas(t *testing.T) {
	type Entry struct {
		Fields map[string]string `json:"fields"`
	}
	type artifact struct {
		Entry
		Entries []map[string]Entry `json:"entries"`
	}
	var got artifact
	valid := `{"fields":{"X":"a","x":"b"},"entries":[{"X":{"fields":{"A":"a","a":"b"}},"x":{"fields":{}}}]}`
	if err := DecodeStrict([]byte(valid), &got); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"fields":{},"FIELDS":{}}`,
		`{"entries":[{"X":{"fields":{},"FIELDS":{}}}]}`,
		`{"entries":[{"X":{"fields":{"A":"a","A":"b"}}}]}`,
	} {
		if err := DecodeStrict([]byte(raw), &got); err == nil {
			t.Fatalf("duplicate escaped: %s", raw)
		}
	}
}

func TestDecodeStrictPreservesNestedDynamicMapKeys(t *testing.T) {
	var got struct {
		Values map[string]any `json:"values"`
	}
	valid := `{"values":{"nested":{"X":1,"x":2},"array":[{"A":1,"a":2}]}}`
	if err := DecodeStrict([]byte(valid), &got); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"values":{"nested":{"X":1,"X":2}}}`, `{"values":{"array":[{"A":1,"A":2}]}}`} {
		if err := DecodeStrict([]byte(raw), &got); err == nil {
			t.Fatal("duplicate dynamic map key escaped")
		}
	}
}

func TestDecodeStrictDirectFieldShadowsAnonymousSchema(t *testing.T) {
	type Inner struct{ Value map[string]int }
	type Outer struct {
		Inner
		Value struct{ ID string }
	}
	var got Outer
	raw := `{"Value":{"id":"first","ID":"second"}}`
	var duplicate *DuplicateMemberError
	if err := DecodeStrict([]byte(raw), &got); !errors.As(err, &duplicate) {
		t.Fatalf("shadowed struct duplicate escaped: %v", err)
	}
}

func TestDecodeStrictTaggedFieldDominatesSameDepthName(t *testing.T) {
	type artifact struct {
		Value  map[string]int
		Tagged struct{ ID string } `json:"Value"`
	}
	var got artifact
	var duplicate *DuplicateMemberError
	if err := DecodeStrict([]byte(`{"Value":{"id":"first","ID":"second"}}`), &got); !errors.As(err, &duplicate) {
		t.Fatalf("tagged struct duplicate escaped: %v", err)
	}
}

func TestDecodeStrictRecursiveAnonymousSchemaRefusesUnknownField(t *testing.T) {
	type recursive struct {
		*recursive
		Known string
	}
	var got recursive
	var unknown *UnknownFieldError
	if err := DecodeStrict([]byte(`{"unknown":{"X":1}}`), &got); !errors.As(err, &unknown) {
		t.Fatalf("recursive schema refused incorrectly: %v", err)
	}
	if err := DecodeStrict([]byte(`{"Known":"valid"}`), &got); err != nil || got.Known != "valid" {
		t.Fatalf("direct member in recursive schema: %v", err)
	}
}

func TestDecodeStrictDirectMapShadowsAnonymousStruct(t *testing.T) {
	type Inner struct{ Value struct{ ID string } }
	type Outer struct {
		Inner
		Value map[string]int
	}
	var got Outer
	if err := DecodeStrict([]byte(`{"Value":{"ID":1,"id":2}}`), &got); err != nil {
		t.Fatalf("direct map identities conflated: %v", err)
	}
}

type opaqueJSONMap map[string]json.RawMessage

func (value *opaqueJSONMap) UnmarshalJSON(raw []byte) error {
	type plain opaqueJSONMap
	var decoded plain
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*value = opaqueJSONMap(decoded)
	return nil
}

func TestDecodeStrictOpaqueJSONUsesConservativeMemberChecks(t *testing.T) {
	for name, target := range map[string]any{"raw message": new(json.RawMessage), "custom map": new(opaqueJSONMap), "double-pointer raw": new(*json.RawMessage), "double-pointer custom": new(*opaqueJSONMap)} {
		t.Run(name, func(t *testing.T) {
			for _, raw := range []string{`{"ID":1,"id":2}`, `{"nested":{"HASH":1,"hash":2}}`} {
				var duplicate *DuplicateMemberError
				if err := DecodeStrict([]byte(raw), target); !errors.As(err, &duplicate) {
					t.Fatalf("opaque alias duplicate escaped: %s: %v", raw, err)
				}
			}
			if err := DecodeStrict([]byte(`{"ID":1,"other":2}`), target); err != nil {
				t.Fatal(err)
			}
		})
	}
	var wrapped struct {
		Bundle json.RawMessage `json:"bundle"`
	}
	if err := DecodeStrict([]byte(`{"bundle":{"ID":1,"id":2}}`), &wrapped); err == nil {
		t.Fatal("nested opaque aliases accepted")
	}
	var ordinary map[string]any
	if err := DecodeStrict([]byte(`{"ID":1,"id":2,"nested":{"HASH":1,"hash":2}}`), &ordinary); err != nil {
		t.Fatalf("ordinary map identities conflated: %v", err)
	}
}
