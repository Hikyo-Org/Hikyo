package isolation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/auditexport"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The audit export completion receipt through the real stack: a stepped-up
// session, the real Audits service and router, and a live HTTP client. The
// response-writer hooks are the only test seam; they stand in for a dropped
// connection and for events that happen between two pages.

// firstWriteHook passes the first body write through and then runs after, so
// the first committed page is on the wire before the hook acts.
type firstWriteHook struct {
	http.ResponseWriter
	after func()
	fired bool
}

func (w *firstWriteHook) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if !w.fired {
		w.fired = true
		w.after()
	}
	return n, err
}

// deadAfterFirstWrite delivers the first body write and refuses every later
// one, as a client that disconnected mid-download does.
type deadAfterFirstWrite struct {
	http.ResponseWriter
	wrote bool
}

func (w *deadAfterFirstWrite) Write(p []byte) (int, error) {
	if w.wrote {
		return 0, errors.New("client gone")
	}
	w.wrote = true
	return w.ResponseWriter.Write(p)
}

func newAuditExportServer(t *testing.T, e accessWireEnv, wrap func(http.ResponseWriter) http.ResponseWriter) *httptest.Server {
	t.Helper()
	h := server.New(&server.API{
		Auth:         e.auth,
		Orgs:         &service.Orgs{DB: e.db},
		Projects:     &service.Projects{DB: e.db},
		Environments: &service.Environments{DB: e.db, Keyring: probeKeyring(t, e.db)},
		Folders:      &service.Folders{DB: e.db},
		Grants:       &service.Grants{DB: e.db},
		Rules:        &service.Rules{DB: e.db},
		Settings:     &service.ProjectSettings{DB: e.db, Auth: e.auth},
		Delivery:     &service.Delivery{DB: e.db, Keyring: e.auth.Keyring},
		SCIMWire:     &service.SCIM{DB: e.db},
		Workspace:    e.workspace,
		Audits:       &service.Audits{DB: e.db},
		Version:      "wire",
	}, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wrap != nil {
			w = wrap(w)
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

type exportResponse struct {
	header http.Header
	body   []byte
	events []string
	// receipt is the final line when it parses as a receipt line.
	receipt *apigen.AuditExportReceipt
}

func getAuditExport(t *testing.T, srv *httptest.Server, token, path string) exportResponse {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("export %s = %d %s, want 200", path, resp.StatusCode, body)
	}
	out := exportResponse{header: resp.Header, body: body}
	// Run the supported CLI/collector consumer against the actual wire bytes.
	// Only publish staged evidence after it confirms terminal success and EOF.
	if strings.Contains(path, "receipt=v1") {
		var staged bytes.Buffer
		receipt, consumeErr := auditexport.Consume(&http.Response{
			StatusCode: resp.StatusCode, Header: resp.Header,
			Body: io.NopCloser(bytes.NewReader(body)),
		}, &staged)
		var terminal struct {
			ExportReceipt apigen.AuditExportReceipt `json:"export_receipt"`
		}
		lines := bytes.Split(bytes.TrimSuffix(body, []byte("\n")), []byte("\n"))
		last := lines[len(lines)-1]
		_ = json.Unmarshal(last, &terminal)
		wantSuccess := terminal.ExportReceipt.Outcome == apigen.AuditExportReceiptOutcomeSuccess && terminal.ExportReceipt.OutcomeRecorded
		if (consumeErr == nil) != wantSuccess {
			t.Fatalf("supported consumer: receipt=%+v err=%v, wire success=%v", receipt, consumeErr, wantSuccess)
		}
		if consumeErr == nil && !bytes.Equal(staged.Bytes(), body[:len(body)-len(last)-1]) {
			t.Fatal("supported consumer did not stage exactly the event bytes")
		}
	}
	if len(body) > 0 && body[len(body)-1] != '\n' {
		t.Fatalf("export body does not end on a line boundary: %q", body[max(0, len(body)-80):])
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
		if line == "" {
			continue
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			t.Fatalf("export line is not JSON: %v: %q", err, line)
		}
		if _, ok := probe["export_receipt"]; !ok {
			if out.receipt != nil {
				t.Fatalf("event line after the receipt: %q", line)
			}
			out.events = append(out.events, line)
			continue
		}
		if out.receipt != nil {
			t.Fatalf("second receipt line: %q", line)
		}
		dec := json.NewDecoder(strings.NewReader(line))
		dec.DisallowUnknownFields()
		var parsed apigen.AuditExportReceiptLine
		if err := dec.Decode(&parsed); err != nil {
			t.Fatalf("receipt line does not match the contract: %v: %q", err, line)
		}
		out.receipt = &parsed.ExportReceipt
	}
	return out
}

func wantReceipt(t *testing.T, got exportResponse, outcome apigen.AuditExportReceiptOutcome, cause string, recorded bool) apigen.AuditExportReceipt {
	t.Helper()
	if h := got.header.Get("Hikyo-Audit-Receipt"); h != "v1" {
		t.Fatalf("Hikyo-Audit-Receipt header = %q, want v1", h)
	}
	if got.receipt == nil {
		t.Fatalf("stream ended without a receipt after %d event lines", len(got.events))
	}
	r := *got.receipt
	if id := got.header.Get("Hikyo-Audit-Export-ID"); id == "" || id != r.StartedEventId {
		t.Fatalf("response export identity = %q, receipt identity = %q", id, r.StartedEventId)
	}
	if r.SnapshotTime.IsZero() || got.header.Get("Hikyo-Audit-Snapshot-Time") != r.SnapshotTime.Format(time.RFC3339Nano) {
		t.Fatalf("response snapshot does not match receipt: %v", r.SnapshotTime)
	}
	if r.Version != apigen.AuditExportReceiptV1 || r.StartedEventId == "" {
		t.Fatalf("receipt version/started = %d/%q", r.Version, r.StartedEventId)
	}
	if r.Outcome != outcome || r.OutcomeRecorded != recorded {
		t.Fatalf("receipt outcome = %s recorded=%v, want %s recorded=%v", r.Outcome, r.OutcomeRecorded, outcome, recorded)
	}
	gotCause := ""
	if r.Cause != nil {
		gotCause = string(*r.Cause)
	}
	if gotCause != cause {
		t.Fatalf("receipt cause = %q, want %q", gotCause, cause)
	}
	if r.RowsStreamed != int64(len(got.events)) {
		t.Fatalf("receipt rows_streamed = %d, but the stream carried %d event lines", r.RowsStreamed, len(got.events))
	}
	return r
}

// completedFor counts the export_completed events paired with a started event.
func completedFor(t *testing.T, e accessWireEnv, startedID, outcome string) int64 {
	t.Helper()
	return queryInt(t, e.db, fmt.Sprintf(
		"SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'audit.export_completed' AND correlation_id = '%s' AND outcome = '%s'",
		startedID, outcome))
}

func TestAuditExportReceiptWire(t *testing.T) {
	forEngines(t, runAuditExportReceiptWire)
}

func runAuditExportReceiptWire(t *testing.T, db *store.DB) {
	e := newAccessWireEnv(t, db)
	execRaw(t, db, fmt.Sprintf(
		"INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_receipt_ar', '%s', 'audit-read', '%s', NULL, NULL, %s)",
		e.admin, e.org, ts))
	seedOrigins(t, db)
	orgPath := api.PathPrefix + "/orgs/" + e.org + "/audit/export"

	t.Run("empty_success_every_scope", func(t *testing.T) {
		srv := newAuditExportServer(t, e, nil)
		const nothing = "?correlation_id=matches-nothing"
		for _, path := range []string{
			orgPath,
			api.PathPrefix + "/orgs/" + e.org + "/projects/" + e.project + "/audit/export",
			api.PathPrefix + "/orgs/" + e.org + "/projects/" + e.project + "/environments/" + e.env + "/audit/export",
		} {
			plain := getAuditExport(t, srv, e.token, path+nothing)
			if plain.header.Get("Hikyo-Audit-Receipt") != "" || plain.receipt != nil || len(plain.events) != 0 {
				t.Fatalf("%s without opt-in: header=%q receipt=%v events=%d, want plain empty JSONL",
					path, plain.header.Get("Hikyo-Audit-Receipt"), plain.receipt, len(plain.events))
			}
			got := getAuditExport(t, srv, e.token, path+nothing+"&receipt=v1")
			r := wantReceipt(t, got, apigen.AuditExportReceiptOutcomeSuccess, "", true)
			if completedFor(t, e, r.StartedEventId, "success") != 1 {
				t.Fatalf("%s: no success export_completed paired with %s", path, r.StartedEventId)
			}
		}
	})

	t.Run("multi_page_success", func(t *testing.T) {
		// More rows than any page can hold, so the export must read past its
		// first page whatever page size the transport asks for.
		const fillers = store.AuditMaxPageSize + 1
		asserted := "0"
		if db.Engine() == store.EnginePostgres {
			asserted = "false"
		}
		var b strings.Builder
		b.WriteString("INSERT INTO audit_tenant_events (id, type, schema_version, occurred_at, occurred_asserted, recorded_at, actor_class, scope_class, org_id, outcome, origin, payload) VALUES ")
		for i := 0; i < fillers; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "('aud_receipt_fill_%d', 'grant.created', 1, '2026-01-01T00:00:00.000000Z', %s, '2026-01-01T00:00:00.000000Z', 'system', 'org', '%s', 'success', 'system', '{\"receipt_filler\":true}')", i, asserted, e.org)
		}
		execRaw(t, db, b.String())

		srv := newAuditExportServer(t, e, nil)
		plain := getAuditExport(t, srv, e.token, orgPath)
		if plain.receipt != nil {
			t.Fatal("a client that did not opt in received a receipt line")
		}
		got := getAuditExport(t, srv, e.token, orgPath+"?receipt=v1")
		r := wantReceipt(t, got, apigen.AuditExportReceiptOutcomeSuccess, "", true)
		if n := strings.Count(strings.Join(got.events, "\n"), `"receipt_filler":true`); n != fillers {
			t.Fatalf("export carried %d filler events, want %d", n, fillers)
		}
		if completedFor(t, e, r.StartedEventId, "success") != 1 {
			t.Fatalf("no success export_completed paired with %s", r.StartedEventId)
		}
	})

	t.Run("writer_disconnect", func(t *testing.T) {
		srv := newAuditExportServer(t, e, func(w http.ResponseWriter) http.ResponseWriter {
			return &deadAfterFirstWrite{ResponseWriter: w}
		})
		got := getAuditExport(t, srv, e.token, orgPath+"?receipt=v1")
		if got.receipt != nil {
			t.Fatalf("receipt %+v reached a client whose connection was gone", *got.receipt)
		}
		if len(got.events) != 1 {
			t.Fatalf("delivered %d event lines, want the 1 written before the disconnect", len(got.events))
		}
		startedID := queryString(t, db,
			"SELECT id FROM audit_tenant_events WHERE type = 'audit.export_started' ORDER BY seq DESC LIMIT 1")
		if completedFor(t, e, startedID, "disconnected") != 1 {
			t.Fatalf("no disconnected export_completed paired with %s", startedID)
		}
	})

	t.Run("internal_read_failure", func(t *testing.T) {
		now := store.CanonTime(time.Now())
		seedAuditExpiry(t, db, "tenant", "receipt-wire-expired", "grant.denied", "", now.Add(-91*24*time.Hour))
		retention := &service.Retention{DB: db, Now: func() time.Time { return now }}
		srv := newAuditExportServer(t, e, func(w http.ResponseWriter) http.ResponseWriter {
			return &firstWriteHook{ResponseWriter: w, after: func() {
				// Pruning between pages makes the next page refuse the
				// now-incomplete snapshot.
				if _, err := retention.Sweep(t.Context()); err != nil {
					t.Errorf("retention sweep: %v", err)
				}
			}}
		})
		got := getAuditExport(t, srv, e.token, orgPath+"?receipt=v1")
		r := wantReceipt(t, got, apigen.AuditExportReceiptOutcomeFailure, "page-read-failed", true)
		if r.RowsStreamed == 0 {
			t.Fatal("the read failed before the first page; this case needs a mid-stream failure")
		}
		if completedFor(t, e, r.StartedEventId, "failure") != 1 {
			t.Fatalf("no failure export_completed paired with %s", r.StartedEventId)
		}
	})

	t.Run("outcome_write_failure", func(t *testing.T) {
		if db.Engine() == store.EngineSQLite {
			execRaw(t, db, `CREATE TRIGGER refuse_export_completed BEFORE INSERT ON audit_tenant_events WHEN NEW.type='audit.export_completed' BEGIN SELECT RAISE(ABORT, 'export outcome refused'); END`)
		} else {
			execRaw(t, db, `CREATE FUNCTION refuse_export_completed() RETURNS TRIGGER LANGUAGE plpgsql AS $$ BEGIN IF NEW.type='audit.export_completed' THEN RAISE EXCEPTION 'export outcome refused'; END IF; RETURN NEW; END; $$; CREATE TRIGGER refuse_export_completed BEFORE INSERT ON audit_tenant_events FOR EACH ROW EXECUTE FUNCTION refuse_export_completed()`)
		}
		defer func() {
			if db.Engine() == store.EngineSQLite {
				execRaw(t, db, `DROP TRIGGER refuse_export_completed`)
			} else {
				execRaw(t, db, `DROP TRIGGER refuse_export_completed ON audit_tenant_events; DROP FUNCTION refuse_export_completed()`)
			}
		}()
		srv := newAuditExportServer(t, e, nil)
		got := getAuditExport(t, srv, e.token, orgPath+"?receipt=v1")
		r := wantReceipt(t, got, apigen.AuditExportReceiptOutcomeFailure, "outcome-write-failed", false)
		if r.RowsStreamed == 0 {
			t.Fatal("no rows streamed; this case needs a stream that read to its snapshot")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'audit.export_completed' AND correlation_id = '"+r.StartedEventId+"'"); n != 0 {
			t.Fatalf("export_completed events = %d despite the refusing trigger", n)
		}
	})

	t.Run("revocation", func(t *testing.T) {
		srv := newAuditExportServer(t, e, func(w http.ResponseWriter) http.ResponseWriter {
			return &firstWriteHook{ResponseWriter: w, after: func() { clearOrgGrants(t, db, e.org) }}
		})
		got := getAuditExport(t, srv, e.token, orgPath+"?receipt=v1")
		r := wantReceipt(t, got, apigen.AuditExportReceiptOutcomeRevoked, "", false)
		if r.RowsStreamed == 0 {
			t.Fatal("revocation stopped the stream before the first page; it must stop at the next page boundary")
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'audit.export_completed' AND correlation_id = '"+r.StartedEventId+"'"); n != 0 {
			t.Fatal("revoked export has a completed event; the unpaired started record is the reconciliation case")
		}
	})
}
