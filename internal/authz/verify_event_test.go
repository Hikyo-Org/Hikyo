package authz

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestVerifyEventRetainsCanonicalBoundaryAndEventAuthorization(t *testing.T) {
	tok := NewTxToken()
	chain := domain.Scope{Org: "org_a", Project: "prj_a"}
	valid := &proof{kind: kindTenant, op: OpProjectCreate, chain: chain, tok: tok}
	ended := NewTxToken()
	ended.Invalidate()
	var typedNil *proof
	for _, tc := range []struct {
		name  string
		p     Proof
		token *TxToken
		op    StoreOp
		event audit.EventType
	}{
		{"nil", nil, tok, StoreAuditTenantInsert, audit.EventProjectCreated},
		{"typed nil", typedNil, tok, StoreAuditTenantInsert, audit.EventProjectCreated},
		{"forged", embeddedProof{Proof: valid}, tok, StoreAuditTenantInsert, audit.EventProjectCreated},
		{"foreign transaction", valid, NewTxToken(), StoreAuditTenantInsert, audit.EventProjectCreated},
		{"nil transaction", valid, nil, StoreAuditTenantInsert, audit.EventProjectCreated},
		{"ended transaction", &proof{kind: kindTenant, op: OpProjectCreate, chain: chain, tok: ended}, ended, StoreAuditTenantInsert, audit.EventProjectCreated},
		{"operation mismatch", valid, tok, StoreEnvironmentsUpdateNote, audit.EventProjectCreated},
		{"unlicensed event", valid, tok, StoreAuditTenantInsert, audit.EventEnvNoteChanged},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := VerifyEvent(tc.p, tc.op, tc.token, tc.event); err == nil {
				t.Fatal("invalid audit proof accepted")
			}
		})
	}
	got, err := VerifyEvent(valid, StoreAuditTenantInsert, tok, audit.EventProjectCreated)
	if err != nil || got != chain {
		t.Fatalf("licensed event chain=%+v err=%v", got, err)
	}
}
