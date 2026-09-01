package authz

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
)

func rejectsWireRegistry(t *testing.T, name string, entry wireEntry) {
	t.Helper()
	if _, err := newWireRegistry(map[string]wireEntry{name: entry}); err == nil {
		t.Fatalf("newWireRegistry accepted malformed entry %q", name)
	}
}

// TestWireRegistrySnapshot pins the table transcription while the single-owner
// representation replaces the three former maps. Count changes require an
// explicit review of the wire, operation-linkage, and direct-event surfaces.
func TestWireRegistrySnapshot(t *testing.T) {
	facts := RegistryFacts{}
	if got := len(facts.Wire()); got != 292 {
		t.Fatalf("wire entries = %d, want 292", got)
	}
	if got := len(facts.WireRoutes()); got != 202 {
		t.Fatalf("operation-linked entries = %d, want 202", got)
	}
	if got := len(facts.WireEvents()); got != 62 {
		t.Fatalf("direct-event entries = %d, want 62", got)
	}
}

func TestWireRegistryRejectsInvalidClass(t *testing.T) {
	rejectsWireRegistry(t, "http:GET /invalid", wireEntry{Class: ClassStub - 1})
}

func TestWireRegistryRejectsStubWithOperations(t *testing.T) {
	rejectsWireRegistry(t, "cli:stub", wireEntry{Class: ClassStub, Ops: []Operation{OpOrgGet}})
}

func TestWireRegistryRejectsStubWithEvents(t *testing.T) {
	rejectsWireRegistry(t, "cli:stub", wireEntry{Class: ClassStub, Events: []audit.EventType{audit.EventOrgRead}})
}
