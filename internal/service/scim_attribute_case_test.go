package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestSCIMAttributePatchMatchesStoredCase(t *testing.T) {
	stored := DesiredUser{Attributes: map[string]any{"title": "Engineer", "displayName": "Before"}}
	got, err := ReduceUserPatch(stored, []UserPatchCommand{
		UserPatchMergeAttributes{Attributes: map[string]any{"TITLE": "Lead", "DISPLAYNAME": nil}},
		UserPatchMergeAttributes{Attributes: map[string]any{"Title": "Director"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Attributes, map[string]any{"title": "Director"}) {
		t.Fatalf("case-variant patch left stale or duplicate attributes: %#v", got.Attributes)
	}
	if !reflect.DeepEqual(stored.Attributes, map[string]any{"title": "Engineer", "displayName": "Before"}) {
		t.Fatal("patch mutated stored attributes")
	}
}

func TestSCIMAttributePatchRejectsDuplicateExtensionLeaves(t *testing.T) {
	_, err := ReduceUserPatch(DesiredUser{}, []UserPatchCommand{UserPatchMergeAttributes{Attributes: map[string]any{
		"urn:example:user": map[string]any{"employeeNumber": "one", "EMPLOYEENUMBER": "two"},
	}}})
	if !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("ambiguous extension leaf accepted: %v", err)
	}
}

func TestSCIMAttributeUniquenessMatchesUnicodeFold(t *testing.T) {
	if err := checkSCIMAttributeNames(map[string]any{"scope": "one", "ſcope": "two"}, 0); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Unicode case-equivalent names accepted: %v", err)
	}
}

func TestSCIMPreserveSubjectSourceKeepsOneCaseVariantLeaf(t *testing.T) {
	stored := map[string]any{"urn:example:user": map[string]any{"employeeNumber": "subject"}}
	desired := map[string]any{"URN:EXAMPLE:USER": map[string]any{"EMPLOYEENUMBER": "subject", "department": "Ops"}}
	got := preserveSubjectSource(desired, stored, "urn:example:user:employeeNumber")
	if err := checkSCIMAttributeNames(got, 0); err != nil {
		t.Fatalf("preserved subject created duplicate leaves: %v", err)
	}
	if want := map[string]any{"URN:EXAMPLE:USER": map[string]any{"employeeNumber": "subject", "department": "Ops"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("preserved attributes = %#v, want %#v", got, want)
	}
	if len(desired["URN:EXAMPLE:USER"].(map[string]any)) != 2 {
		t.Fatal("preserving the subject mutated incoming attributes")
	}
}
