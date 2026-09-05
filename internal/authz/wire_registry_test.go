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
	// #568 added member invitations, #147 added dynamic providers and leases,
	// #151 added change approvals, #157 added adapter pause and resume, and
	// #628 added the two unauthenticated MCP metadata methods.
	// Ops diagnostics adds the CLI-only escrow verification event.
	if got := len(facts.Wire()); got != 318 {
		t.Fatalf("wire entries = %d, want 318", got)
	}
	if got := len(facts.WireRoutes()); got != 225 {
		t.Fatalf("operation-linked entries = %d, want 225", got)
	}
	if got := len(facts.WireEvents()); got != 68 {
		t.Fatalf("direct-event entries = %d, want 68", got)
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
