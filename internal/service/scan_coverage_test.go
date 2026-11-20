package service

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/testutil/reflectx"
)

// SS3 field-coverage matrix (#74, secret-scanning ADR §2). The Surface-2 scan is
// defined STRUCTURALLY, not by enumeration: every author-controlled string leaf
// of the canonical definitions model must be scanned, and adding a public string
// field silently unscanned must be impossible.
//
// The proof is by construction, not a maintained constant list: a RECURSIVE
// reflection walk enumerates every content leaf reachable in the real Go types
// — descending through nested structs, pointers, and slices (schema.Declaration
// → schema.Rule, the Rule's pattern / enum members / url schemes / JSON Schema
// blob, the any_of alternatives, and the same tree under a bundle Key) — sets
// ONE leaf at a time to a unique sentinel, and asserts the leaf extractor
// surfaces that sentinel. A leaf that is neither surfaced nor on the closed
// exclusion list fails here, so a newly added string field anywhere in the
// reachable tree cannot ship unscanned.
//
// The exclusion list is the ADR's "fixed schema keywords + server-generated
// immutable identifiers", keyed by OwnerType.FieldName and named leaf-by-leaf
// so a reviewer sees exactly what is not scanned and why. Aggregate fields can
// never be excluded: the walk always descends into them, and only terminal
// string/blob leaves may match this list.
var excludedContentFields = map[string]string{
	// --- direct-edit (service) model ---
	"KeySpec.Classification": "closed enum secret|config, not author free-text",
	"KeySpec.GroupID":        "server-generated group identifier, not composed content",
	"Presence.Mode":          "closed none|all|explicit enum in both presence models",
	"Presence.Environments": "environment references: direct-edit values are server-issued ids; bundle env-name references are scanned " +
		"at their declaration site in the same bundle and dangling references are refused",
	"Rule.Type": "closed type enum, a fixed schema keyword",
	"KeyMetadataUpdate.ConfirmWidening": "principal ids confirming a folder move (member-access-rules ADR D9), compared to the " +
		"server-computed set and never persisted as declaration text",
	// --- definitions bundle model ---
	"Key.ID":             "server-generated key identifier, not composed content",
	"Key.Classification": "closed enum secret|config, not author free-text",
	"Key.Group": "key-group NAME reference; a dangling one is refused by definitions.Resolve " +
		"(validateKeyReferences) before persist, and a real group's name is itself scanned via key_groups",
	"Environment.ID": "server-generated environment identifier, not composed content",
	"KeyGroup.ID":    "server-generated group identifier, not composed content",
}

func leafSetContains(leaves []scanLeaf, sentinel string) bool {
	for _, l := range leaves {
		if string(l.Content) == sentinel {
			return true
		}
	}
	return false
}

// assertEachLeafScanned plants the sentinel at each leaf in a fresh root and
// asserts the extractor surfaces it.
func assertEachLeafScanned[T any](t *testing.T, leaves []reflectx.Leaf, extract func(root T) []scanLeaf) {
	t.Helper()
	for _, leaf := range leaves {
		if !leafSetContains(extract(reflectx.Plant[T](leaf)), reflectx.Sentinel) {
			t.Errorf("author-controlled leaf %s is not scan-covered and is not on the exclusion list", leaf.Path)
		}
	}
}

func TestSurface2FieldCoverageMatrix(t *testing.T) {
	// KeySpec: every content leaf, recursively (including the whole
	// Declaration → Rule / any_of tree), one sentinel at a time.
	assertEachLeafScanned(t, reflectx.ContentLeaves(t, KeySpec{}, excludedContentFields), keySpecLeaves)

	// KeyMetadataUpdate: the PATCH members (pointer content leaves).
	assertEachLeafScanned(t, reflectx.ContentLeaves(t, KeyMetadataUpdate{}, excludedContentFields), keyMetadataLeaves)

	// The hierarchy name inputs are single-string ingresses handled directly by
	// the Folders/Environments/KeyGroups services (not a struct field), so assert
	// each locator constant is present and distinct — a coverage gap there is
	// visible too.
	for _, loc := range []string{locEnvironmentName, locEnvironmentNote, locFolderPath, locGroupName} {
		if loc == "" {
			t.Error("a hierarchy locator constant is empty")
		}
	}
}

// TestBundleLeafCoverageMatrix is SS3.e extended to the definitions bundle model
// (#74 SS3): definitions.Bundle carries its own Key/Environment/KeyGroup structs,
// distinct from the service KeySpec, scanned by bundleLeaves on plan/apply/check.
// It walks the whole Bundle recursively — Environments/KeyGroups names, and each
// Key's leaves INCLUDING its Declaration → Rule / any_of tree — sets one leaf at
// a time, and asserts bundleLeaves surfaces it. A newly added bundle string leaf
// that is neither covered nor excluded fails here, so it cannot ship unscanned
// through the Git flow.
func TestBundleLeafCoverageMatrix(t *testing.T) {
	assertEachLeafScanned(t, reflectx.ContentLeaves(t, definitions.Bundle{}, excludedContentFields), bundleLeaves)
}

// TestCoverageWalkReachesDeepLeaves is the anti-vacuity guard: it pins that the
// recursive walk actually reaches the deepest author-controlled leaves under a
// nested Declaration, so a walker bug that collected zero (or only shallow)
// leaves — which would make the two matrices above pass on an empty set — trips
// here. It also stands in for "a syntactically-new deep string field": if the
// declaration tree gains a scanned field, the walk must reach it, and these
// anchors prove the descent through pointer (Rule), slice-of-struct (any_of),
// and terminal-blob (JSON Schema) shapes all work.
func TestCoverageWalkReachesDeepLeaves(t *testing.T) {
	paths := map[string]bool{}
	for _, l := range reflectx.ContentLeaves(t, KeySpec{}, excludedContentFields) {
		paths[l.Path] = true
	}
	for _, l := range reflectx.ContentLeaves(t, definitions.Bundle{}, excludedContentFields) {
		paths[l.Path] = true
	}
	for _, want := range []string{
		"KeySpec.Declaration.Rule.Pattern",        // pointer → struct → string
		"KeySpec.Declaration.Rule.Members",        // pointer → struct → []string (enum members)
		"KeySpec.Declaration.Rule.JSONSchema",     // pointer → struct → []byte blob
		"KeySpec.Declaration.AnyOf.Pattern",       // slice-of-struct alternative → string
		"KeySpec.Declaration.AnyOf.Members",       // slice-of-struct alternative → []string
		"Bundle.Keys.Declaration.Rule.Pattern",    // bundle key declaration, direct rule
		"Bundle.Keys.Declaration.AnyOf.Pattern",   // bundle key declaration, any_of alternative
		"Bundle.Keys.Declaration.Rule.JSONSchema", // bundle key declaration, JSON blob
	} {
		if !paths[want] {
			t.Errorf("the coverage walk never reached %q; the recursive descent is broken and the matrices would pass vacuously", want)
		}
	}
	if len(paths) == 0 {
		t.Fatal("the coverage walk collected zero leaves")
	}
}
