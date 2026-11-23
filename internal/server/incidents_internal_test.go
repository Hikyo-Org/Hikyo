package server

import (
	"math"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestClamp32BoundsALedgerCount(t *testing.T) {
	for in, want := range map[int64]int32{-5: 0, 0: 0, 7: 7, math.MaxInt32: math.MaxInt32, math.MaxInt32 + 1: math.MaxInt32, math.MaxInt64: math.MaxInt32} {
		if got := clamp32(in); got != want {
			t.Errorf("clamp32(%d) = %d, want %d", in, got, want)
		}
	}
}

// The item wire carries the server's own verdicts: refused and attestable travel
// as sent, the rotation's live state only when the service read one.
func TestWireIncidentItemCarriesTheVerdicts(t *testing.T) {
	at := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	view := service.IncidentItemView{
		IncidentItem: authz.IncidentItem{ID: "itm_1", OwnerFunction: "account.contain", ObjectKind: "account", ObjectID: "usr_1", Outcome: domain.ItemPending, Reason: domain.ReasonOwnerFailed, UpdatedAt: at},
		Refused:      true, Attestable: false,
	}
	got := wireIncidentItem(view)
	if !got.Refused || got.Attestable || got.Outcome != apigen.IncidentItemOutcome("pending") || got.Reason == nil || *got.Reason != "owner_failed" {
		t.Fatalf("a refused item: %+v", got)
	}
	if got.ObjectState != nil || got.AttestedBy != nil || got.AttestedAt != nil {
		t.Fatalf("a refused item carries state it does not have: %+v", got)
	}

	view = service.IncidentItemView{
		IncidentItem: authz.IncidentItem{ID: "itm_2", ObjectKind: "rotation", Outcome: domain.ItemUnknown, AttestedBy: "usr_2", AttestedAt: at},
		ObjectState:  "gone", Attestable: true,
	}
	got = wireIncidentItem(view)
	if got.ObjectState == nil || *got.ObjectState != "gone" || !got.Attestable || got.Refused || got.AttestedBy == nil || got.AttestedAt == nil {
		t.Fatalf("a rotation item: %+v", got)
	}
}

func TestWireIncidentCountsAndReopen(t *testing.T) {
	v := service.IncidentView{
		Incident: authz.Incident{ID: "inc_1", ScopeKind: domain.IncidentScopeAccount, ScopeID: "usr_1", Status: domain.IncidentOpen, OrgID: "org_1"},
		Counts:   map[domain.ItemOutcome]int64{domain.ItemPending: 2, domain.ItemDone: 3, domain.ItemOutOfScope: math.MaxInt64},
		Reopened: true, Added: 4,
	}
	got := wireIncident(v)
	if got.Counts.Pending != 2 || got.Counts.Done != 3 || got.Counts.Unknown != 0 || got.Counts.OutOfScope != math.MaxInt32 {
		t.Fatalf("counts: %+v", got.Counts)
	}
	if got.Org == nil || *got.Org != "org_1" || got.Reopened == nil || !*got.Reopened || got.Added == nil || *got.Added != 4 || got.ClosedBy != nil || got.ClosedAt != nil {
		t.Fatalf("incident: %+v", got)
	}
}

func TestIncidentTargetMapsEveryScopeField(t *testing.T) {
	p, o, pr, ad := "usr_1", "org_1", "prj_1", "adp_1"
	got := incidentTarget(apigen.IncidentOpenRequest{ScopeKind: "project", Principal: &p, Org: &o, Project: &pr, Adapter: &ad})
	if got.Kind != domain.IncidentScopeProject || got.Principal != "usr_1" || got.Scope.Org != "org_1" || got.Scope.Project != "prj_1" || got.AdapterID != "adp_1" {
		t.Fatalf("target: %+v", got)
	}
}
