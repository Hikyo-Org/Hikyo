package scimproto

import "testing"

// RFC 7643 §2.1 makes attribute names case-insensitive and nothing more: a
// name that differs by '_' or '-' is a different (unknown) attribute.
func TestAttributeNamesFoldCaseOnly(t *testing.T) {
	u, e := DecodeUser([]byte(`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"USERNAME":"alice","user_name":"mallory","externalId":"A","external_id":"B"}`))
	if e != nil {
		t.Fatalf("DecodeUser refused: %v", e)
	}
	if u.UserName != "alice" || u.ExternalID != "A" {
		t.Fatalf("UserName=%q ExternalID=%q, want alice and A", u.UserName, u.ExternalID)
	}
	if u.Extra["user_name"] != "mallory" || u.Extra["external_id"] != "B" {
		t.Fatalf("unknown attributes not kept as opaque metadata: %v", u.Extra)
	}

	// A name differing by '_' alone must not set the attribute (v2's
	// MatchCaseInsensitiveNames would).
	u, e = DecodeUser([]byte(`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"user_name":"mallory"}`))
	if e != nil {
		t.Fatalf("user_name: DecodeUser refused: %v", e)
	}
	if u.UserName != "" || u.Extra["user_name"] != "mallory" {
		t.Fatalf("user_name: UserName=%q Extra=%v, want empty UserName and the member kept opaque", u.UserName, u.Extra)
	}

	// Unicode folding matches the PATCH code that acts on the decoded names
	// (U+017F folds to 's'), and a second spelling next to the canonical one
	// is a duplicate, not a smuggled value.
	if u, e := DecodeUser([]byte(`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"uſerName":"bob"}`)); e != nil || u.UserName != "bob" {
		t.Fatalf("uſerName: UserName=%q err=%v, want bob", u.UserName, e)
	}
	if _, e := DecodeUser([]byte(`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:User"],"userName":"alice","uſerName":"bob"}`)); e == nil || e.SCIMType != TypeInvalidSyntax {
		t.Fatalf("userName plus uſerName: want invalidSyntax, got %v", e)
	}
	// The pathless PATCH replace path treats `memberſ` as members, so the
	// decode must fill Members from it rather than leave it nil (which would
	// replace the group's members with none).
	if g, e := DecodeGroup([]byte(`{"schemas":["urn:ietf:params:scim:schemas:core:2.0:Group"],"memberſ":[{"value":"u1"}]}`)); e != nil || len(g.Members) != 1 || g.Members[0].Value != "u1" {
		t.Fatalf("memberſ: Members=%v err=%v, want [u1]", g.Members, e)
	}

	// Nested names fold too: members inside a PATCH value and the PatchOp envelope.
	const patch = `{"SCHEMAS":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"operations":[{"OP":"replace","Path":"members","VALUE":[{"VALUE":"u1","Type":"User","member_id":"x"}]}]}`
	ops, e := ParsePatch([]byte(patch), ResourceGroup)
	if e != nil {
		t.Fatalf("ParsePatch refused: %v", e)
	}
	set, ok := ops[0].Payload.(PatchMemberSetPayload)
	if !ok || len(set.Members) != 1 || set.Members[0].Value != "u1" || set.Members[0].Type != "User" {
		t.Fatalf("payload = %#v", ops[0].Payload)
	}

	// Two spellings of one attribute are refused, not resolved by position.
	for name, body := range map[string]string{
		"envelope": `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[],"operations":[{"op":"remove","path":"title"}]}`,
		"trailing": `{"schemas":["urn:ietf:params:scim:api:messages:2.0:PatchOp"],"Operations":[{"op":"remove","path":"title"}]} {}`,
	} {
		if _, e := ParsePatch([]byte(body), ResourceUser); e == nil || e.SCIMType != TypeInvalidSyntax {
			t.Errorf("%s: want invalidSyntax, got %v", name, e)
		}
	}
}
