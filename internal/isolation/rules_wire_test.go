package isolation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The member access rule routes through the real stack: create, list at both
// depths (the project listing reads and shows only its own project), revoke,
// and the uniform refusal, where revoking a rule the caller cannot reach, a
// rule that does not exist and a rule under a missing org are byte-identical.
func TestRuleWire(t *testing.T) {
	forEngines(t, runRuleWire)
}

func runRuleWire(t *testing.T, db *store.DB) {
	e := newAccessWireEnv(t, db)
	const (
		holder      = "usr_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f71"
		sibling     = "prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f72"
		siblingEnv  = "env_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f73"
		missingRule = "rul_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f74"
		missingOrg  = "org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fee"
	)
	execRaw(t, db, fmt.Sprintf(`INSERT INTO principals (id, kind, created_at) VALUES ('%s', 'human', %s)`, holder, ts))
	execRaw(t, db, fmt.Sprintf(`INSERT INTO projects (id, org_id, name, created_at) VALUES ('%s', '%s', 'sibling', %s)`, sibling, e.org, ts))
	execRaw(t, db, fmt.Sprintf(`INSERT INTO environments (id, org_id, project_id, name, note, created_at, display_order) `+
		`VALUES ('%s', '%s', '%s', 'sibling-env', '', %s, 0)`, siblingEnv, e.org, sibling, ts))

	org := api.PathPrefix + "/orgs/" + e.org
	db0 := "db"
	body := apigen.CreateRuleRequest{Principal: holder, Capability: apigen.RuleCapabilityEdit, Where: apigen.RuleWhere{
		Projects: []apigen.ID{e.project, sibling},
		Environments: apigen.RuleEnvironmentAxis{Mode: apigen.RuleAxisModeAll, Items: []apigen.RuleEnvironmentItem{
			{Project: sibling, Environment: siblingEnv},
		}},
		Keys: apigen.RuleKeyAxis{Mode: apigen.RuleAxisModeOnly, Items: []apigen.RuleKeyItem{
			{Project: e.project, Folder: &db0}, {Project: sibling, Folder: &db0},
		}},
	}}
	code, raw := e.call(t, http.MethodPost, org+"/rules", body)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, raw)
	}
	var created apigen.Rule
	if err := json.Unmarshal(raw, &created); err != nil {
		t.Fatal(err)
	}
	if created.PrincipalId != holder || created.CreatedBy != string(e.admin) || len(created.Where.Keys.Items) != 2 {
		t.Fatalf("created %+v", created)
	}

	// Revoke works: a second rule created and revoked leaves the first alone.
	second := body
	second.Capability = apigen.RuleCapabilityPublish
	code, raw = e.call(t, http.MethodPost, org+"/rules", second)
	var doomed apigen.Rule
	if code != http.StatusCreated || json.Unmarshal(raw, &doomed) != nil {
		t.Fatalf("create second = %d %s", code, raw)
	}
	if code, raw := e.call(t, http.MethodDelete, org+"/rules/"+doomed.Id, nil); code != http.StatusNoContent {
		t.Fatalf("revoke = %d %s", code, raw)
	}

	// A key item naming both a folder and a key is refused on shape.
	bad := body
	key := missingRule
	bad.Where.Keys.Items = []apigen.RuleKeyItem{{Project: e.project, Folder: &db0, Key: &key}}
	if code, raw := e.call(t, http.MethodPost, org+"/rules", bad); code != http.StatusBadRequest {
		t.Fatalf("ambiguous key item = %d %s, want 400", code, raw)
	}

	var list apigen.RuleList
	code, raw = e.call(t, http.MethodGet, org+"/rules", nil)
	if code != http.StatusOK || json.Unmarshal(raw, &list) != nil || list.Count != 1 || list.Items[0].OtherProjects {
		t.Fatalf("org listing = %d %s", code, raw)
	}
	if got := list.Items[0].Where; len(got.Projects) != 2 || len(got.Environments.Items) != 1 {
		t.Fatalf("org listing where = %+v", got)
	}
	code, raw = e.call(t, http.MethodGet, org+"/projects/"+e.project+"/rules", nil)
	if code != http.StatusOK || json.Unmarshal(raw, &list) != nil || list.Count != 1 {
		t.Fatalf("project listing = %d %s", code, raw)
	}
	got := list.Items[0]
	if !got.OtherProjects || len(got.Where.Projects) != 1 || got.Where.Projects[0] != e.project ||
		len(got.Where.Environments.Items) != 0 || len(got.Where.Keys.Items) != 1 {
		t.Fatalf("project listing shows %+v, want only its own project's part and other_projects", got)
	}
	if bytes.Contains(raw, []byte(sibling)) || bytes.Contains(raw, []byte(siblingEnv)) {
		t.Fatalf("project listing disclosed the sibling project: %s", raw)
	}

	// Uniform refusal: strip the caller's member management.
	clearOrgGrants(t, db, e.org)
	stripMemberManagement(t, db, e.admin)
	gone := api.PathPrefix + "/orgs/" + missingOrg
	for _, pair := range []struct {
		name             string
		method           string
		refused, missing string
		body             any
	}{
		{"list_org_rules", http.MethodGet, org + "/rules", gone + "/rules", nil},
		{"list_project_rules", http.MethodGet, org + "/projects/" + e.project + "/rules", gone + "/projects/" + e.project + "/rules", nil},
		{"create_rule", http.MethodPost, org + "/rules", gone + "/rules", body},
		{"revoke_unreachable_vs_missing_rule", http.MethodDelete, org + "/rules/" + created.Id, org + "/rules/" + missingRule, nil},
		{"revoke_unreachable_vs_missing_org", http.MethodDelete, org + "/rules/" + created.Id, gone + "/rules/" + created.Id, nil},
	} {
		t.Run(pair.name, func(t *testing.T) {
			refusedCode, refusedBody := e.call(t, pair.method, pair.refused, pair.body)
			missingCode, missingBody := e.call(t, pair.method, pair.missing, pair.body)
			if refusedCode != http.StatusNotFound || missingCode != http.StatusNotFound {
				t.Fatalf("statuses %d and %d, want both 404\n  %s\n  %s", refusedCode, missingCode, refusedBody, missingBody)
			}
			if !bytes.Equal(refusedBody, missingBody) {
				t.Fatalf("bodies differ:\n  refused: %s\n  missing: %s", refusedBody, missingBody)
			}
		})
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM rules WHERE id = '"+created.Id+"'"); n != 1 {
		t.Fatal("a refused revoke removed the rule")
	}
}
