package authz

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestAdapterConfigurationForUpdateRegistryBoundary(t *testing.T) {
	for op, spec := range operationTable {
		if got := spec.storeOps[StoreAdaptersConfigurationForUpdate]; got != (op == OpAdapterConfigure) {
			t.Errorf("%s configuration writer permission = %t", op, got)
		}
	}
	if spec := operationTable[OpAdapterConfigure]; spec.class != ClassTenant || spec.level != domain.LevelProject {
		t.Fatal("configuration writer must retain tenant/project authority")
	}
	if readOnlyStoreOps[StoreAdaptersConfigurationForUpdate] {
		t.Fatal("locking configuration door registered as read-only")
	}
	for site, ops := range systemSites {
		if ops[StoreAdaptersConfigurationForUpdate] {
			t.Errorf("configuration writer reachable from system site %s", site)
		}
	}
	tok := NewTxToken()
	defer tok.Invalidate()
	chain := domain.Scope{Org: "org_a", Project: "prj_a"}
	for _, op := range []Operation{OpAdapterConfigure, OpAdapterInspect, OpAdapterPlan, OpAdapterDelete} {
		p := &proof{kind: kindTenant, op: op, chain: chain, tok: tok}
		got, err := Verify(p, StoreAdaptersConfigurationForUpdate, tok)
		if op == OpAdapterConfigure {
			if err != nil || got != chain {
				t.Fatalf("configure proof: chain=%+v err=%v", got, err)
			}
		} else if err == nil {
			t.Errorf("%s proof accepted by configuration writer", op)
		}
	}
}
