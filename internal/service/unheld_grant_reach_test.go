package service

import (
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
)

func grantRow(capability domain.Capability, scope domain.Scope) authz.GrantRow {
	return authz.GrantRow{Grant: domain.Grant{Capability: capability, Scope: scope}}
}

// The whoami report-delivery-status hint and the grant path's own gate are one
// predicate: for every target org, the reach answers exactly what
// mayGrantUnheld answers, so the dialog never offers a grant the server
// refuses nor hides one it allows.
func TestUnheldGrantReachMatchesMayGrantUnheld(t *testing.T) {
	const orgA, orgB, orgC domain.OrgID = "org_a", "org_b", "org_c"
	for _, tc := range []struct {
		name string
		rows []authz.GrantRow
		want UnheldGrantReach
	}{
		{name: "none"},
		{
			name: "project and environment manage-members are not enough",
			rows: []authz.GrantRow{
				grantRow(domain.CapManageMembers, domain.Scope{Org: orgA, Project: "prj_a"}),
				grantRow(domain.CapManageMembers, domain.Scope{Org: orgA, Project: "prj_a", Env: "env_a"}),
			},
		},
		{
			name: "org scope reaches that org only, deduplicated and sorted",
			rows: []authz.GrantRow{
				grantRow(domain.CapManageMembers, domain.Scope{Org: orgB}),
				grantRow(domain.CapManageMembers, domain.Scope{Org: orgA}),
				grantRow(domain.CapManageMembers, domain.Scope{Org: orgB}),
				grantRow(domain.CapRead, domain.Scope{Org: orgC}),
			},
			want: UnheldGrantReach{Orgs: []domain.OrgID{orgA, orgB}},
		},
		{
			name: "instance scope reaches every org",
			rows: []authz.GrantRow{grantRow(domain.CapManageMembers, domain.Scope{})},
			want: UnheldGrantReach{Instance: true},
		},
		{
			name: "other capabilities at instance scope reach nothing",
			rows: []authz.GrantRow{grantRow(domain.CapInstanceConfig, domain.Scope{})},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := unheldGrantReach(tc.rows)
			if got.Instance != tc.want.Instance || !slices.Equal(got.Orgs, tc.want.Orgs) {
				t.Fatalf("reach = %+v, want %+v", got, tc.want)
			}
			for _, org := range []domain.OrgID{orgA, orgB, orgC} {
				hint := got.Instance || slices.Contains(got.Orgs, org)
				if gate := mayGrantUnheld(tc.rows, domain.Scope{Org: org, Project: "prj_x", Env: "env_x"}); hint != gate {
					t.Fatalf("org %s: hint %v, grant gate %v", org, hint, gate)
				}
			}
		})
	}
}
