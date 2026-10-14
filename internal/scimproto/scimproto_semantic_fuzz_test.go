package scimproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// FuzzFilterSemanticContract generates quoted values independently of the
// production recognizer. Logical-looking bytes inside a value remain data;
// adding a real compound expression or crossing resource kinds is refused.
func FuzzFilterSemanticContract(f *testing.F) {
	f.Add("Sales and Marketing (EU)", byte(0))
	f.Add("\" or userName pr", byte(1))
	f.Add("Kelvin\n\\", byte(2))
	f.Fuzz(func(t *testing.T, value string, selector byte) {
		encoded, err := json.Marshal(value)
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
	f.Fuzz(func(t *testing.T, value string, selector byte) {
		encoded, err := json.Marshal(value)
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
