package service

import (
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/testutil/reflectx"
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
	if !reflectx.Equal(got.Attributes, map[string]any{"title": "Director"}) {
		t.Fatalf("case-variant patch left stale or duplicate attributes: %#v", got.Attributes)
	}
	if !reflectx.Equal(stored.Attributes, map[string]any{"title": "Engineer", "displayName": "Before"}) {
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
	if want := map[string]any{"URN:EXAMPLE:USER": map[string]any{"employeeNumber": "subject", "department": "Ops"}}; !reflectx.Equal(got, want) {
		t.Fatalf("preserved attributes = %#v, want %#v", got, want)
	}
	if len(desired["URN:EXAMPLE:USER"].(map[string]any)) != 2 {
		t.Fatal("preserving the subject mutated incoming attributes")
	}
}

// A patch with many commands must cost linear work in the attributes it
// touches. The old merge copied the whole map and scanned every stored key per
// command, so this took minutes; the bound is generous to stay unflaky.
func TestSCIMAttributePatchScalesLinearly(t *testing.T) {
	const commands = 20000
	patch := make([]UserPatchCommand, 0, commands+1)
	for i := range commands {
		patch = append(patch, UserPatchMergeAttributes{Attributes: map[string]any{fmt.Sprintf("Attr%d", i): i}})
	}
	patch = append(patch, UserPatchMergeAttributes{Attributes: map[string]any{"ATTR0": nil, "attr1": "again"}})

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	got, err := ReduceUserPatch(DesiredUser{}, patch)
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	// Deterministic guard: a clone per command allocates gigabytes here.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 256<<20 {
		t.Fatalf("%d-command patch allocated %d bytes; merge is no longer linear", commands, allocated)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("%d-command patch took %s; merge is no longer linear", commands, elapsed)
	}
	if _, ok := got.Attributes["Attr0"]; ok || len(got.Attributes) != commands-1 || got.Attributes["Attr1"] != "again" {
		t.Fatalf("merge result wrong: %d attributes, Attr1=%v", len(got.Attributes), got.Attributes["Attr1"])
	}
}
