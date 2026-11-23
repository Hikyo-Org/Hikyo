package cli

import (
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func TestIncidentUsageErrorsComeBeforeAuthentication(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
		want string
	}{
		{"open without a scope", []string{"open"}, "--scope"},
		{"account scope without an account", []string{"open", "--scope", "account"}, "--account"},
		{"account scope with an org", []string{"open", "--scope", "account", "--account", "usr_one", "--org", "org_one"}, "--account only"},
		{"org scope without an org", []string{"open", "--scope", "org"}, "--org only"},
		{"project scope without a project", []string{"open", "--scope", "project", "--org", "org_one"}, "--project"},
		{"adapter scope without an adapter", []string{"open", "--scope", "adapter", "--org", "org_one", "--project", "prj_one"}, "--adapter"},
		{"show without an id", []string{"show"}, "<incident>"},
		{"items with two ids", []string{"items", "one", "two"}, "<incident>"},
		{"attest without an item", []string{"attest", "inc_one"}, "<item>"},
		{"close with two ids", []string{"close", "one", "two"}, "<incident>"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ios, _, stderr := composeIO(t.TempDir(), t.TempDir(), "", nil)
			code := Run(t.Context(), ios, append([]string{"incident"}, tt.args...))
			if code != ExitUsage || !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("exit=%d stderr=%q; want a usage error naming %q", code, stderr, tt.want)
			}
		})
	}
}

// A refused item and one that has not run yet are different states.
func TestIncidentItemTableSeparatesRefusedFromNotYetRun(t *testing.T) {
	t.Parallel()
	reason, state := "owner_failed", "halted"
	table := incidentItemTable(apigen.IncidentItemList{Items: []apigen.IncidentItem{
		{Id: "itm_a", ObjectKind: "account", ObjectId: "usr_a", Outcome: apigen.IncidentItemOutcomePending, Refused: true, Reason: &reason},
		{Id: "itm_b", ObjectKind: "session", ObjectId: "ses_b", Outcome: apigen.IncidentItemOutcomePending},
		{Id: "itm_c", ObjectKind: "rotation", ObjectId: "rot_c", Outcome: apigen.IncidentItemOutcomeDone, ObjectState: &state},
	}})
	got := []string{table.Rows[0][2], table.Rows[1][2], table.Rows[2][2], table.Rows[2][4]}
	want := []string{"refused", "not yet run", "done", "halted"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cells = %v, want %v", got, want)
		}
	}
}
