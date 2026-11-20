package scimproto

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

// FuzzFilterSemanticContract generates quoted values independently of the
// production recognizer. Logical-looking bytes inside a value remain data;
// adding a real compound expression or crossing resource kinds is refused.
func FuzzFilterSemanticContract(f *testing.F) {
	f.Add("Sales and Marketing (EU)", byte(0))
	f.Add("\" or userName pr", byte(1))
	f.Add("Kelvin\n\\", byte(2))
	f.Add("Sales \xff\xfe Marketing", byte(3))
	f.Fuzz(func(t *testing.T, value string, selector byte) {
		// v2 refuses invalid UTF-8; replace it with U+FFFD as v1 did (though
		// unescaped). That loses nothing: the parsers refuse raw invalid UTF-8
		// (TestInvalidUTF8Refused).
		encoded, err := json.Marshal(value, jsonwire.Stable, jsontext.AllowInvalidUTF8(true))
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct {
			attr  string
			res   Resource
			shape FilterShape
		}{
			{"userName", ResourceUser, FilterUserNameEq},
			{"displayName", ResourceGroup, FilterDisplayNameEq},
			{"externalId", ResourceUser, FilterExternalIDEq},
			{"externalId", ResourceGroup, FilterExternalIDEq},
		}
		c := cases[int(selector)%len(cases)]
		raw := c.attr + " eq " + string(encoded)
		if len(raw)+30 > filterBound {
			return
		}
		var canonicalValue string
		if err := json.Unmarshal(encoded, &canonicalValue); err != nil {
			t.Fatal(err)
		}
		for _, spelling := range []string{raw, " \t" + strings.ToUpper(c.attr) + " EQ " + string(encoded) + "\n"} {
			got, e := ParseFilter(spelling, c.res)
			if e != nil || got.Shape != c.shape || got.Value != canonicalValue {
				t.Fatalf("literal value changed: %#v, %v", got, e)
			}
		}
		for _, suffix := range []string{" and externalId eq \"x\"", " or externalId eq \"x\""} {
			if _, e := ParseFilter(raw+suffix, c.res); e == nil || e.SCIMType != TypeInvalidFilter {
				t.Fatal("compound expression was not refused as invalidFilter")
			}
		}
		if c.attr != "externalId" {
			other := ResourceUser
			if c.res == ResourceUser {
				other = ResourceGroup
			}
			if _, e := ParseFilter(raw, other); e == nil {
				t.Fatal("resource-specific filter crossed resource kinds")
			}
		}
	})
}

// FuzzPatchAtomicRefusal appends a resource-invalid operation to a genuine
// accepted assignment. No prefix of the parsed plan may escape on failure.
func FuzzPatchAtomicRefusal(f *testing.F) {
	f.Add("external identity", byte(0))
	f.Add("\"}],\"op\":\"remove\"", byte(1))
	f.Add("external \xff identity", byte(1))
	f.Fuzz(func(t *testing.T, value string, selector byte) {
		// v2 refuses invalid UTF-8; replace it with U+FFFD as v1 did (though
		// unescaped). That loses nothing: the parsers refuse raw invalid UTF-8
		// (TestInvalidUTF8Refused).
		encoded, err := json.Marshal(value, jsonwire.Stable, jsontext.AllowInvalidUTF8(true))
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) > bodyBound/2 {
			return
		}
		res, forbidden := ResourceUser, "members"
		if selector%2 == 1 {
			res, forbidden = ResourceGroup, "active"
		}
		prefix := `{"schemas":["` + SchemaPatchOp + `"],"Operations":[{"op":"replace","path":"externalId","value":` + string(encoded)
		valid := []byte(prefix + `}]}`)
		ops, e := ParsePatch(valid, res)
		if e != nil || len(ops) != 1 || ops[0].Op != "replace" || ops[0].Payload == nil {
			t.Fatalf("positive assignment control failed: %v", e)
		}
		var canonicalValue string
		if err := json.Unmarshal(encoded, &canonicalValue); err != nil {
			t.Fatal(err)
		}
		plain, ok := ops[0].Payload.(PatchPlainPayload)
		if !ok || plain.Attribute != "externalId" || plain.Value != canonicalValue {
			t.Fatal("accepted assignment changed its attribute or literal value")
		}
		invalid := []byte(prefix + `},{"op":"replace","path":"` + forbidden + `","value":true}]}`)
		ops, e = ParsePatch(invalid, res)
		if e == nil || e.SCIMType != TypeInvalidPath || ops != nil {
			t.Fatalf("invalid suffix leaked an accepted prefix: %#v, %v", ops, e)
		}
	})
}

// TestInvalidUTF8Refused pins the refusal the fuzz targets above rely on:
// scimproto's parsers refuse raw invalid UTF-8 and unpaired surrogate escapes
// instead of replacing them, and still accept valid non-ASCII values.
func TestInvalidUTF8Refused(t *testing.T) {
	for _, c := range []struct {
		literal, want string // want is empty when the literal must be refused
	}{
		{"a\xffb", ""},
		{`a\ud800b`, ""},
		{`a\udc00b`, ""},
		{"a\u00e9b", "a\u00e9b"},
		{`a\u00e9b`, "a\u00e9b"},
	} {
		v, refuse := c.literal, c.want == ""
		f, e := ParseFilter(`userName eq "`+v+`"`, ResourceUser)
		if refuse && (e == nil || e.SCIMType != TypeInvalidFilter) || !refuse && (e != nil || f.Value != c.want) {
			t.Errorf("filter %q: got %#v, %v; want value %q (empty: invalidFilter)", v, f, e, c.want)
		}
		patch := `{"schemas":["` + SchemaPatchOp + `"],"Operations":[{"op":"replace","path":"externalId","value":"` + v + `"}]}`
		ops, e := ParsePatch([]byte(patch), ResourceUser)
		want := PatchPlainPayload{Attribute: "externalId", Value: c.want}
		if refuse && (e == nil || e.SCIMType != TypeInvalidSyntax || ops != nil) || !refuse && (e != nil || len(ops) != 1 || ops[0].Payload != want) {
			t.Errorf("patch %q: got %#v, %v; want value %q (empty: invalidSyntax)", v, ops, e, c.want)
		}
		u, e := DecodeUser([]byte(`{"schemas":["` + SchemaUser + `"],"userName":"` + v + `"}`))
		if refuse && (e == nil || e.SCIMType != TypeInvalidSyntax) || !refuse && (e != nil || u.UserName != c.want) {
			t.Errorf("user %q: got %#v, %v; want userName %q (empty: invalidSyntax)", v, u, e, c.want)
		}
	}
}
