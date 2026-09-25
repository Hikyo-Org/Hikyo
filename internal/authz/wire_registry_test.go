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
	// Self-configuration adds five owner-local HTTP routes and operation links.
	// #680 adds two revision-diff routes and one disclosure event surface.
	// Account profiles add two self-service routes and one audit event.
	// #723 adds two local parameter-declaration routes and their operation links.
	// Container maintenance adds one unauthenticated runtime status route.
	// #781 adds four unauthenticated, stateless Codex lifecycle methods.
	// #760 adds three unauthenticated login-challenge finish routes (two carrying
	// login/session/clone events, the webauthn start carrying none).
	// #606 adds six registration-policy routes (get/put/delete at org and
	// instance scope), each linked to its own operation.
	// #788 adds the delivery-target report, tombstone and list routes; the two
	// machine writes carry the federated pre-authentication refusal events.
	if got := len(facts.Wire()); got != 346 {
		t.Fatalf("wire entries = %d, want 346", got)
	}
	if got := len(facts.WireRoutes()); got != 243 {
		t.Fatalf("operation-linked entries = %d, want 243", got)
	}
	if got := len(facts.WireEvents()); got != 74 {
		t.Fatalf("direct-event entries = %d, want 74", got)
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
