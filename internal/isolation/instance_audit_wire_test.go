package isolation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/cli"
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
	srv := httptest.NewTLSServer(server.New(&server.API{Auth: wire.auth, Audits: &service.Audits{DB: db}, Grants: &service.Grants{DB: db}, Workspace: wire.workspace, Version: "wire"}, nil))
	t.Cleanup(srv.Close)
	wire.srv = srv

	queryPath := api.PathPrefix + "/instance/audit"
	exportPath := queryPath + "/export"
	admin := string(wire.admin)
	const otherOrg = "org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0aa0"
	const otherProject = "prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0aa1"
	const siblingProject = "prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0aa2"
	execRaw(t, db, "INSERT INTO orgs (id, name, active, metadata, created_at) VALUES ('"+otherOrg+"', 'other-audit-org', TRUE, '{}', "+ts+")")
	execRaw(t, db, "INSERT INTO projects (id, org_id, name, created_at) VALUES ('"+otherProject+"', '"+otherOrg+"', 'other-audit-project', "+ts+"), ('"+siblingProject+"', '"+wire.org+"', 'sibling-audit-project', "+ts+")")
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

	t.Run("missing_and_invalid_artifacts", func(t *testing.T) {
		for _, token := range []string{"", "invalid-token"} {
			for _, path := range []string{queryPath, exportPath} {
				code, body := wire.callAs(t, token, http.MethodGet, path, nil)
				if code != http.StatusUnauthorized {
					t.Fatalf("GET %s = %d %s", path, code, body)
				}
			}
		}
	})

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

	t.Run("project_automation_remains_tenant_confined", func(t *testing.T) {
		own := api.PathPrefix + "/orgs/" + wire.org + "/projects/" + wire.project + "/audit"
		if code, body := wire.callAs(t, minted.Value, http.MethodGet, own, nil); code != http.StatusOK {
			t.Fatalf("project collector cannot query its own trail: %d %s", code, body)
		}
		for _, path := range []string{own, api.PathPrefix + "/orgs/" + wire.org + "/projects/" + wire.project + "/environments/" + wire.env + "/audit"} {
			got := getAuditExport(t, srv, minted.Value, path+"/export?receipt=v1")
			if got.receipt == nil || got.receipt.Outcome != apigen.AuditExportReceiptOutcomeSuccess || !got.receipt.OutcomeRecorded {
				t.Fatalf("own-subtree collection did not complete: %+v", got.receipt)
			}
		}
		// Both outside scopes really exist and use schema-valid HTTP IDs.
		for _, path := range []string{
			api.PathPrefix + "/orgs/" + otherOrg + "/projects/" + otherProject + "/audit",
			api.PathPrefix + "/orgs/" + wire.org + "/projects/" + siblingProject + "/audit",
			api.PathPrefix + "/orgs/org_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0bb0/projects/prj_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0bb1/audit",
		} {
			for _, suffix := range []string{"", "/export"} {
				code, body := wire.callAs(t, minted.Value, http.MethodGet, path+suffix, nil)
				if code != http.StatusNotFound || decodeWireError(t, body) != apigen.ErrorCodeNotFound {
					t.Fatalf("collector escaped its project: %d %s", code, body)
				}
			}
		}
	})

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

	t.Run("actual_cli_query_and_export", func(t *testing.T) {
		stateDir, workDir := t.TempDir(), t.TempDir()
		var stdout, stderr bytes.Buffer
		ios := cli.IO{Stdout: &stdout, Stderr: &stderr, Workdir: workDir, Env: cli.Env{Getenv: func(key string) string {
			if key == "HIKYO_STATE_DIR" {
				return stateDir
			}
			return ""
		}}}
		state, err := cli.NewState(ios.Env)
		if err != nil {
			t.Fatal(err)
		}
		if err := state.Trust().Put(cli.TrustEntry{Name: "fixture", Origin: srv.URL, SPKIPin: cli.SPKIFingerprint(srv.Certificate())}); err != nil {
			t.Fatal(err)
		}
		if err := state.PutSession(cli.SessionArtifact{Instance: "fixture", Origin: srv.URL, Token: wire.token, SessionID: "fixture-cli", Principal: admin, ExpiresAt: "2030-01-01T00:00:00Z"}); err != nil {
			t.Fatal(err)
		}
		if code := cli.Run(t.Context(), ios, []string{"audit", "query", "--instance", "fixture", "--instance-scope", "--operation", "auth.login", "-o", "json"}); code != cli.ExitOK {
			t.Fatalf("CLI query exit=%d: %s", code, stderr.String())
		}
		var page apigen.AuditPage
		if err := json.Unmarshal(stdout.Bytes(), &page); err != nil || page.Count == 0 {
			t.Fatalf("CLI page=%s err=%v", stdout.String(), err)
		}
		stdout.Reset()
		stderr.Reset()
		if code := cli.Run(t.Context(), ios, []string{"audit", "export", "--instance", "fixture", "--instance-scope", "--operation", "auth.login"}); code != cli.ExitOK {
			t.Fatalf("CLI export exit=%d: %s", code, stderr.String())
		}
		if stdout.Len() == 0 || strings.Contains(stdout.String(), "export_receipt") {
			t.Fatalf("CLI export did not publish validated event JSONL: %s", stdout.String())
		}
		get := ios.Env.Getenv
		ios.Env.Getenv = func(key string) string {
			if key == "HIKYO_TOKEN" {
				return minted.Value
			}
			return get(key)
		}
		stdout.Reset()
		stderr.Reset()
		if code := cli.Run(t.Context(), ios, []string{"audit", "export", "--instance", "fixture", "--org", wire.org, "--project", wire.project, "--auth", "machine"}); code != cli.ExitOK || stdout.Len() == 0 {
			t.Fatalf("project automation CLI exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
		stdout.Reset()
		stderr.Reset()
		if code := cli.Run(t.Context(), ios, []string{"audit", "export", "--instance", "fixture", "--org", otherOrg, "--project", otherProject, "--auth", "machine"}); code != cli.ExitNotFound || stdout.Len() != 0 {
			t.Fatalf("cross-org CLI exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
	})

	t.Run("failed_local_login_is_visible_with_source", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+api.PathPrefix+"/auth/local/login", strings.NewReader(`{"username":"`+waAdmin+`","password":"incorrect synthetic password"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "hikyo-instance-audit-test")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("failed local login status = %d", resp.StatusCode)
		}
		page := getAuditPage(t, wire, queryPath+"?operation=auth.login&outcomes=failure")
		found := false
		for _, event := range page.Items {
			if event.UserAgent != nil && *event.UserAgent == "hikyo-instance-audit-test" {
				found = true
				if event.SourceIp == nil || *event.SourceIp == "" || event.ActorClass == "" || event.Origin == "" {
					t.Fatalf("source metadata missing: %+v", event)
				}
				if event.ActorId != nil || event.ActorClass != "unauthenticated" {
					t.Fatalf("failed login was attributed to an authenticated actor: %+v", event)
				}
			}
		}
		if !found {
			t.Fatalf("failed local login absent from instance trail: %+v", page.Items)
		}
	})

	t.Run("instance_security_mutation_is_visible", func(t *testing.T) {
		const target = "usr_0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0cc0"
		execRaw(t, db, "INSERT INTO principals (id, kind, created_at) VALUES ('"+target+"', 'human', "+ts+")")
		path := api.PathPrefix + "/instance/grants"
		code, body := wire.call(t, http.MethodPost, path, map[string]string{"principal": target, "capability": "read"})
		if code != http.StatusOK {
			t.Fatalf("grant mutation = %d %s", code, body)
		}
		page := getAuditPage(t, wire, queryPath+"?operation=grant.created&actor="+admin)
		found := false
		for _, event := range page.Items {
			if event.ActorId != nil && *event.ActorId == admin && event.ScopeClass == "instance" && event.Outcome == "success" {
				found = true
			}
		}
		if !found {
			t.Fatal("instance grant mutation is absent or misattributed")
		}
		code, body = wire.call(t, http.MethodDelete, path+"?principal="+target+"&capability=read", nil)
		if code != http.StatusNoContent {
			t.Fatalf("revoke synthetic grant = %d %s", code, body)
		}
	})

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

	t.Run("instance_export_reauthorizes_bearer_at_page_boundary", func(t *testing.T) {
		// Read the real instance export owner with the transport's bearer. A
		// one-row page makes the revocation boundary deterministic on both engines.
		w := &hookWriter{onFirst: func() {
			execRaw(t, db, "DELETE FROM grant_origins WHERE grant_id = 'g_ia_admin_inst_ar'")
			execRaw(t, db, "DELETE FROM grants WHERE id = 'g_ia_admin_inst_ar'")
		}}
		err := (&service.Audits{DB: db}).InstanceExportActor(t.Context(), service.Bearer(wire.token), service.AuditFilter{To: time.Now().UTC()}, 1, w)
		if !errors.Is(err, service.ErrExportUnpaired) || w.buf.Len() == 0 {
			t.Fatalf("revoked instance export = %v, bytes = %d", err, w.buf.Len())
		}
		startedID := queryString(t, db, "SELECT id FROM audit_instance_events WHERE type = 'audit.export_started' ORDER BY seq DESC LIMIT 1")
		if n := countInstance("type = 'audit.export_completed' AND correlation_id = '" + startedID + "'"); n != 0 {
			t.Fatal("revocation fabricated an authorized terminal outcome")
		}
		if n := countInstance("type = 'grant.denied' AND payload LIKE '%audit.instance-export%'"); n == 0 {
			t.Fatal("revocation lacks durable denial")
		}
		grant("g_ia_admin_inst_ar", admin, "", "")
	})

	t.Run("audit_reader_needs_no_instance_configuration_grant", func(t *testing.T) {
		execRaw(t, db, "DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE principal_id='"+admin+"' AND capability='instance-config')")
		execRaw(t, db, "DELETE FROM grants WHERE principal_id='"+admin+"' AND capability='instance-config'")
		code, body := wire.call(t, http.MethodGet, api.PathPrefix+"/auth/whoami", nil)
		if code != http.StatusOK {
			t.Fatalf("audit-only whoami = %d %s", code, body)
		}
		var identity apigen.WhoAmI
		if err := json.Unmarshal(body, &identity); err != nil {
			t.Fatal(err)
		}
		if identity.Capabilities.InstanceOperator || identity.Capabilities.InstanceAuditRead == nil || !*identity.Capabilities.InstanceAuditRead {
			t.Fatalf("audit-only hints = %+v", identity.Capabilities)
		}
		_ = getAuditPage(t, wire, queryPath+"?limit=1")
	})

	t.Run("instance_export_reauthorizes_a_revoked_session", func(t *testing.T) {
		w := &hookWriter{onFirst: func() {
			if err := wire.auth.Logout(t.Context(), wire.token); err != nil {
				t.Fatal(err)
			}
		}}
		receipt, err := (&service.Audits{DB: db}).InstanceExportActorReceipt(t.Context(), service.Bearer(wire.token), service.AuditFilter{}, 1, w)
		if !errors.Is(err, service.ErrExportUnpaired) || receipt.Outcome != service.ExportRevoked || receipt.OutcomeRecorded || receipt.RowsStreamed != 1 {
			t.Fatalf("session-revoked instance export: receipt=%+v err=%v", receipt, err)
		}
		if n := countInstance("type='audit.export_completed' AND correlation_id='" + receipt.StartedEventID + "'"); n != 0 {
			t.Fatal("session revocation wrote a terminal outcome without authority")
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

// The instance reader has separate SQL from the tenant reader. Keep its
// commit-order and fixed-snapshot promise under the same adversarial schedule.
func TestPostgresInstanceAuditExportCommitOrder(t *testing.T) {
	db := seededDB(t, openPostgres)
	insert := `INSERT INTO audit_instance_events (id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_id,actor_class,outcome,origin,payload)
	VALUES ($1,'auth.login',1,clock_timestamp(),FALSE,clock_timestamp(),'usr_root','human','success','cli','{}') RETURNING seq`
	low, err := db.PG().Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = low.Rollback(context.Background()) })
	var lowSeq, highSeq int64
	if err := low.QueryRow(t.Context(), insert, "instance_gap_low").Scan(&lowSeq); err != nil {
		t.Fatal(err)
	}
	if err := db.PG().QueryRow(t.Context(), insert, "instance_gap_high").Scan(&highSeq); err != nil {
		t.Fatal(err)
	}
	if lowSeq >= highSeq {
		t.Fatal("fixture did not reverse allocation and commit order")
	}
	firstByte := make(chan struct{})
	w := &hookWriter{onFirst: func() { close(firstByte) }}
	done := make(chan error, 1)
	go func() {
		done <- (&service.Audits{DB: db}).InstanceExport(t.Context(), root, service.AuditFilter{}, 1, w)
	}()
	deadline := time.After(5 * time.Second)
	for queryInt(t, db, `SELECT COUNT(*) FROM pg_locks WHERE locktype='advisory' AND classid=1464159830 AND objid=85 AND NOT granted`) == 0 {
		select {
		case err := <-done:
			t.Fatalf("instance export ended before the barrier: %v", err)
		case <-deadline:
			t.Fatal("instance export never reached the in-flight barrier")
		case <-time.After(10 * time.Millisecond):
		}
	}
	select {
	case <-firstByte:
		t.Fatal("instance export streamed before settling its fixed snapshot")
	default:
	}
	if err := low.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("instance export did not finish after commit")
	}
	var gap []exportLineForTest
	for _, line := range strings.Split(strings.TrimSpace(w.buf.String()), "\n") {
		var event exportLineForTest
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event.ID == "instance_gap_low" || event.ID == "instance_gap_high" {
			gap = append(gap, event)
		}
	}
	if len(gap) != 2 || gap[0].ID != "instance_gap_high" || gap[0].Seq != highSeq || gap[1].ID != "instance_gap_low" || gap[1].Seq != lowSeq {
		t.Fatalf("instance commit-order export = %+v", gap)
	}
}
