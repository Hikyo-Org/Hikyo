package service

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// classifyResetTarget is the org-bounded reachability test at the heart of the
// credential-reset rules (ADR - Recovery). These pin the outcomes plus the
// fail-closed footprint-less case.
func TestClassifyResetTarget(t *testing.T) {
	g := func(cap domain.Capability, org domain.OrgID, prj domain.ProjectID) domain.Grant {
		return domain.Grant{Capability: cap, Scope: domain.Scope{Org: org, Project: prj}}
	}
	cases := []struct {
		name        string
		grants      []domain.Grant
		footprint   []domain.OrgID
		wantOrg     string
		wantInst    bool
		wantOrgHits int
	}{
		{
			name:        "org-bounded to one org",
			grants:      []domain.Grant{g("read", "org_a", "prj_a1"), g("edit", "org_a", "")},
			footprint:   []domain.OrgID{"org_a"},
			wantOrg:     "org_a",
			wantOrgHits: 1,
		},
		{
			name:        "instance-scoped grant is an instance capability",
			grants:      []domain.Grant{g("read", "org_a", ""), g("instance-config", "", "")},
			footprint:   []domain.OrgID{"org_a", ""},
			wantInst:    true,
			wantOrgHits: 0,
		},
		{
			name:        "grants spanning two orgs are multi-org",
			grants:      []domain.Grant{g("read", "org_a", ""), g("read", "org_b", "")},
			footprint:   []domain.OrgID{"org_a", "org_b"},
			wantOrgHits: 2,
		},
		{
			name:        "a rules-only member is bounded to the rule's org",
			footprint:   []domain.OrgID{"org_b"},
			wantOrg:     "org_b",
			wantOrgHits: 1,
		},
		{
			name:        "a rule in a second org makes a grant holder multi-org",
			grants:      []domain.Grant{g("read", "org_a", "")},
			footprint:   []domain.OrgID{"org_a", "org_b"},
			wantOrgHits: 2,
		},
		{
			name:        "a footprint-less target is fail-closed to the instance path",
			wantOrgHits: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			org, inst, count := classifyResetTarget(c.grants, c.footprint)
			if inst != c.wantInst {
				t.Fatalf("instanceCap = %v, want %v", inst, c.wantInst)
			}
			if count != c.wantOrgHits {
				t.Fatalf("orgCount = %d, want %d", count, c.wantOrgHits)
			}
			if !inst && count == 1 && org != c.wantOrg {
				t.Fatalf("org = %q, want %q", org, c.wantOrg)
			}
		})
	}
}
