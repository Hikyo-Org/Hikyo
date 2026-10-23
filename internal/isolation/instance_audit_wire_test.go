package isolation

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The instance audit trail through the real stack, both engines (#1035). The
// service readers have their own suite; this pins what only the transport
// decides: the routes reach the instance readers, the formula is instance
// scope, and only a human session is eligible.
func TestInstanceAuditWire(t *testing.T) {
	forEngines(t, runInstanceAuditWire)
}

func runInstanceAuditWire(t *testing.T, db *store.DB) {
	wire := newAccessWireEnv(t, db)
	srv := httptest.NewServer(server.New(&server.API{Auth: wire.auth, Audits: &service.Audits{DB: db}}, nil))
	t.Cleanup(srv.Close)
	wire.srv = srv

	queryPath := api.PathPrefix + "/instance/audit"
	exportPath := queryPath + "/export"
	admin := string(wire.admin)
	countInstance := func(where string) int64 {
		return queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE "+where)
	}
	grant := func(id, principal, org, project string) {
		t.Helper()
		orgCol, projectCol := "NULL", "NULL"
		if org != "" {
			orgCol = "'" + org + "'"
		}
		if project != "" {
			projectCol = "'" + project + "'"
		}
		execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('%s', '%s', 'audit-read', %s, %s, NULL, %s)`, id, principal, orgCol, projectCol, ts))
		seedOrigins(t, db)
	}

	// Project automation that legitimately holds audit-read in its own project:
	// the scheduled tenant export the audit-model ADR allows.
	scope := domain.Scope{Org: domain.OrgID(wire.org), Project: domain.ProjectID(wire.project)}
	execRaw(t, db, fmt.Sprintf(`INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_ia_admin_mi', '%s', 'manage-identities', '%s', '%s', NULL, %s)`, admin, wire.org, wire.project, ts))
	seedOrigins(t, db)
	identities := &service.Identities{DB: db, Auth: wire.auth}
	sa, err := identities.CreateServiceAccount(t.Context(), service.LocalPrincipal(wire.admin), scope, "audit-collector", domain.ClassAutomation)
	if err != nil {
		t.Fatal(err)
	}
	minted, err := identities.MintCredential(t.Context(), service.LocalPrincipal(wire.admin), scope, sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatal(err)
	}
	grant("g_ia_sa_ar", string(sa.Principal), wire.org, wire.project)

	// The artifact gate refuses a machine credential before authorization, with
	// the uniform nonexistent shape and the cause on the instance trail.
	t.Run("project_automation_is_refused", func(t *testing.T) {
		for path, op := range map[string]string{queryPath: "queryInstanceAudit", exportPath: "exportInstanceAudit"} {
			code, body := wire.callAs(t, minted.Value, http.MethodGet, path, nil)
			if code != http.StatusNotFound {
				t.Fatalf("automation GET %s = %d %s, want 404", path, code, body)
			}
			if n := countInstance(fmt.Sprintf("type = 'auth.artifact_class_refused' AND actor_id = '%s' AND object_id = '%s'", sa.Principal, op)); n != 1 {
				t.Fatalf("%s artifact refusals for automation = %d, want 1", op, n)
			}
		}
		if n := countInstance(fmt.Sprintf("actor_id = '%s' AND type IN ('audit.query', 'audit.export_started')", sa.Principal)); n != 0 {
			t.Fatalf("automation reached an instance reader: %d trail reads recorded", n)
		}
	})

	// The tenant grant is real: the same session reads the org trail.
	grant("g_ia_admin_org_ar", admin, wire.org, "")
	if code, body := wire.call(t, http.MethodGet, api.PathPrefix+"/orgs/"+wire.org+"/audit?limit=1", nil); code != http.StatusOK {
		t.Fatalf("org audit with org audit-read = %d %s, want 200", code, body)
	}

	t.Run("tenant_audit_read_is_refused", func(t *testing.T) {
		for path, op := range map[string]string{queryPath: "audit.instance-query", exportPath: "audit.instance-export"} {
			before := countInstance("type = 'grant.denied' AND actor_id = '" + admin + "' AND payload LIKE '%" + op + "%'")
			code, body := wire.call(t, http.MethodGet, path, nil)
			if code != http.StatusForbidden {
				t.Fatalf("GET %s with only org audit-read = %d %s, want 403", path, code, body)
			}
			if got := decodeWireError(t, body); got != apigen.ErrorCodeForbidden {
				t.Fatalf("GET %s error code = %q, want forbidden", path, got)
			}
			if n := countInstance("type = 'grant.denied' AND actor_id = '" + admin + "' AND payload LIKE '%" + op + "%'"); n != before+1 {
				t.Fatalf("%s denial events = %d, want %d", op, n, before+1)
			}
		}
		if n := countInstance("type = 'audit.export_started' AND actor_id = '" + admin + "'"); n != 0 {
			t.Fatalf("refused export recorded %d started events, want 0", n)
		}
	})

	grant("g_ia_admin_inst_ar", admin, "", "")

	t.Run("query_pages_under_a_stable_ceiling", func(t *testing.T) {
		before := countInstance("type = 'audit.query' AND actor_id = '" + admin + "'")
		first := getAuditPage(t, wire, queryPath+"?limit=1")
		if first.Count != 1 || first.Exhausted || first.UpperSeq < first.NextAfterSeq {
			t.Fatalf("first page = %+v, want one row below a pinned ceiling", first)
		}
		q := url.Values{
			"limit":     {"100"},
			"after_seq": {fmt.Sprint(first.NextAfterSeq)},
			"to_seq":    {fmt.Sprint(first.UpperSeq)},
		}
		rest := getAuditPage(t, wire, queryPath+"?"+q.Encode())
		if rest.UpperSeq != first.UpperSeq {
			t.Fatalf("ceiling moved from %d to %d while paging", first.UpperSeq, rest.UpperSeq)
		}
		for _, e := range rest.Items {
			if e.Seq <= first.NextAfterSeq || e.Seq > first.UpperSeq {
				t.Fatalf("seq %d is outside the cursor window (%d, %d]", e.Seq, first.NextAfterSeq, first.UpperSeq)
			}
		}
		logins := getAuditPage(t, wire, queryPath+"?operation=auth.login&actor="+admin)
		if logins.Count == 0 {
			t.Fatal("the operator's own logins are not on the instance trail page")
		}
		if n := countInstance("type = 'audit.query' AND actor_id = '" + admin + "'"); n != before+3 {
			t.Fatalf("instance audit.query events = %d, want %d (one per page)", n, before+3)
		}
	})

	t.Run("export_streams_the_instance_trail", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+exportPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+wire.token)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("export = %d %s, want 200", resp.StatusCode, raw)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "application/x-ndjson" {
			t.Fatalf("content type = %q", ct)
		}
		if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "hikyo-audit-instance.jsonl") {
			t.Fatalf("content disposition = %q", cd)
		}
		lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
		for _, line := range lines {
			var row struct {
				OrgID string `json:"org_id"`
			}
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				t.Fatalf("export line is not JSON: %v", err)
			}
			if row.OrgID != "" {
				t.Fatalf("instance export carried a tenant row: %s", line)
			}
		}
		startedID := queryString(t, db, "SELECT id FROM audit_instance_events WHERE type = 'audit.export_started' AND outcome = 'intent' AND actor_id = '"+admin+"' ORDER BY seq DESC LIMIT 1")
		if n := countInstance(fmt.Sprintf("type = 'audit.export_completed' AND outcome = 'success' AND correlation_id = '%s' AND payload LIKE '%%\"rows_streamed\":%d%%'", startedID, len(lines))); n != 1 {
			t.Fatalf("completed events for %s with rows_streamed=%d: %d, want 1", startedID, len(lines), n)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type LIKE 'audit.export_%' AND actor_id = '"+admin+"'"); n != 0 {
			t.Fatalf("instance export wrote %d events to the tenant trail", n)
		}
	})
}

func getAuditPage(t *testing.T, wire accessWireEnv, path string) apigen.AuditPage {
	t.Helper()
	code, body := wire.call(t, http.MethodGet, path, nil)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d %s, want 200", path, code, body)
	}
	var page apigen.AuditPage
	if err := json.Unmarshal(body, &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func decodeWireError(t *testing.T, body []byte) apigen.ErrorCode {
	t.Helper()
	var payload apigen.Error
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("error body is not JSON: %v (%s)", err, body)
	}
	return payload.Error.Code
}
