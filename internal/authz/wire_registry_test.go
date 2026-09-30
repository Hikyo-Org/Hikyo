package authz

import (
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
)

func rejectsWireRegistry(t *testing.T, name string, entry wireEntry) {
	t.Helper()
	if _, err := newWireRegistry(map[string]wireEntry{name: entry}); err == nil {
		t.Fatalf("newWireRegistry accepted malformed entry %q", name)
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

// newWireRegistry validates and clones the static wire table in CI. Runtime
// projections read generated metadata without parsing or validating it at boot.
func newWireRegistry(table map[string]wireEntry) (map[string]wireEntry, error) {
	entries := make(map[string]wireEntry, len(table))
	for name, entry := range table {
		if name == "" {
			return nil, fmt.Errorf("authz wire registry: empty entry key")
		}
		switch entry.Class {
		case ClassTenant, ClassInstance, ClassUnauthenticated, ClassSystem:
		case ClassStub:
			if len(entry.Ops) > 0 || len(entry.Events) > 0 {
				return nil, fmt.Errorf("authz wire registry: stub entry %q carries operation or event linkage", name)
			}
		default:
			return nil, fmt.Errorf("authz wire registry: entry %q has invalid class %d", name, entry.Class)
		}

		seenOps := make(map[Operation]bool, len(entry.Ops))
		for _, op := range entry.Ops {
			if _, known := registry.ops[op]; !known {
				return nil, fmt.Errorf("authz wire registry: entry %q names unknown operation %q", name, op)
			}
			if seenOps[op] {
				return nil, fmt.Errorf("authz wire registry: entry %q repeats operation %q", name, op)
			}
			seenOps[op] = true
		}

		seenEvents := make(map[audit.EventType]bool, len(entry.Events))
		for _, event := range entry.Events {
			if _, known := audit.Spec(event); !known {
				return nil, fmt.Errorf("authz wire registry: entry %q names unknown event %q", name, event)
			}
			if seenEvents[event] {
				return nil, fmt.Errorf("authz wire registry: entry %q repeats event %q", name, event)
			}
			seenEvents[event] = true
		}

		entry.Ops = append([]Operation(nil), entry.Ops...)
		entry.Events = append([]audit.EventType(nil), entry.Events...)
		entries[name] = entry
	}
	return entries, nil
}
