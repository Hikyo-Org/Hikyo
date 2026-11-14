package service

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func TestValidIncidentObjectID(t *testing.T) {
	for id, want := range map[string]bool{
		"adp_01":                true,
		"A-b_9":                 true,
		"":                      false,
		"adp bad":               false,
		"adp;drop":              false,
		"adp/../x":              false,
		strings.Repeat("a", 80): true,
		strings.Repeat("a", 81): false,
		"adp\x00":               false,
		"ädp":                   false,
	} {
		if got := validIncidentObjectID(id); got != want {
			t.Errorf("validIncidentObjectID(%q) = %t, want %t", id, got, want)
		}
	}
}

func TestIncidentAccessSameOwnership(t *testing.T) {
	base := incidentAccess{kind: domain.IncidentScopeAccount, scopeID: "usr_a", org: "org_a", footprint: []domain.OrgID{"org_a"}}
	same := incidentAccess{kind: domain.IncidentScopeAccount, scopeID: "usr_a", org: "org_a", footprint: []domain.OrgID{"org_a"}}
	if !base.sameOwnership(same) {
		t.Error("identical access reported as changed")
	}
	for name, other := range map[string]incidentAccess{
		"kind":    {kind: domain.IncidentScopeOrg, scopeID: "usr_a", org: "org_a", footprint: []domain.OrgID{"org_a"}},
		"scope":   {kind: domain.IncidentScopeAccount, scopeID: "usr_b", org: "org_a", footprint: []domain.OrgID{"org_a"}},
		"org":     {kind: domain.IncidentScopeAccount, scopeID: "usr_a", org: "", footprint: []domain.OrgID{"org_a"}},
		"grown":   {kind: domain.IncidentScopeAccount, scopeID: "usr_a", org: "org_a", footprint: []domain.OrgID{"org_a", "org_b"}},
		"swapped": {kind: domain.IncidentScopeAccount, scopeID: "usr_a", org: "org_a", footprint: []domain.OrgID{"org_b"}},
		"shrunk":  {kind: domain.IncidentScopeAccount, scopeID: "usr_a", org: "org_a"},
	} {
		if base.sameOwnership(other) {
			t.Errorf("a changed %s was reported as the same ownership", name)
		}
	}
}

func TestAttestable(t *testing.T) {
	for name, tc := range map[string]struct {
		item authz.IncidentItem
		want bool
	}{
		"observable session":         {authz.IncidentItem{OwnerFunction: domain.OwnerSessionRevoke, Outcome: domain.ItemPending}, false},
		"observable developer cred":  {authz.IncidentItem{OwnerFunction: domain.OwnerDeveloperRevoke, Outcome: domain.ItemUnknown}, false},
		"blocked factor":             {authz.IncidentItem{OwnerFunction: domain.OwnerFactorRemoval, Outcome: domain.ItemBlocked}, false},
		"blocked upstream":           {authz.IncidentItem{OwnerFunction: domain.OwnerDynamicUpstream, Outcome: domain.ItemBlocked, Reason: domain.ReasonUpstreamNotObservable}, false},
		"upstream by reason":         {authz.IncidentItem{OwnerFunction: domain.OwnerAdapterTargetPause, Outcome: domain.ItemUnknown, Reason: domain.ReasonUpstreamNotObservable}, true},
		"upstream by owner":          {authz.IncidentItem{OwnerFunction: domain.OwnerAdapterUpstream, Outcome: domain.ItemUnknown}, true},
		"consumer adoption":          {authz.IncidentItem{OwnerFunction: domain.OwnerConsumerAdoption, Outcome: domain.ItemPending}, true},
		"factor Hikyo cannot remove": {authz.IncidentItem{OwnerFunction: domain.OwnerFactorRemoval, Outcome: domain.ItemUnknown, Reason: domain.ReasonNoRevokeOwner}, true},
		"linked identity":            {authz.IncidentItem{OwnerFunction: domain.OwnerExternalIdentity, Outcome: domain.ItemUnknown, Reason: domain.ReasonNoRevokeOwner}, true},
	} {
		if got := attestable(tc.item); got != tc.want {
			t.Errorf("%s: attestable = %t, want %t", name, got, tc.want)
		}
	}
}
