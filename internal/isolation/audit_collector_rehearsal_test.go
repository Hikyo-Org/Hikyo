package isolation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// TestAuditCollectorProductRehearsal is opt-in because it crosses the product's
// Go/Python process boundary and retains synthetic packets for the Linux custody
// rehearsal. The caller must supply a new, private output directory's parent.
// This is local fixture evidence, never protected-host or release acceptance.
func TestAuditCollectorProductRehearsal(t *testing.T) {
	out := os.Getenv("HIKYO_AUDIT_REHEARSAL_DIR")
	if out == "" {
		t.Skip("opt-in local collector rehearsal")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "scripts", "ops", "audit-collector", "receiver.py"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(out, 0o700); err != nil {
		t.Fatal("rehearsal output must not already exist:", err)
	}
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := newAccessWireEnv(t, db) // Real bootstrap, credentials and WebAuthn test authenticator.
		projects := &service.Projects{DB: db}
		if _, err := projects.Rename(t.Context(), service.Bearer(e.token), domain.Scope{Org: domain.OrgID(e.org), Project: domain.ProjectID(e.project)}, "collector-synthetic-project"); err != nil {
			t.Fatal(err)
		}
		// Authority rows are harness setup, not a new credential/export authority.
		// Product events below are produced only by the supported service writers.
		execRaw(t, db, fmt.Sprintf("INSERT INTO grants (id,principal_id,capability,created_at) VALUES ('g_collector_instance','%s','audit-read',%s)", e.admin, ts))
		execRaw(t, db, fmt.Sprintf("INSERT INTO grants (id,principal_id,capability,org_id,created_at) VALUES ('g_collector_org','%s','audit-read','%s',%s)", e.admin, e.org, ts))
		seedOrigins(t, db)
		if _, err := e.auth.LocalLogin(t.Context(), waAdmin, "deliberately wrong synthetic password", service.ArtifactCLI); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("supervised synthetic login refusal: %v", err)
		}
		srv := newAuditExportServer(t, e, nil)
		for _, trail := range []string{"tenant", "instance"} {
			t.Run(trail, func(t *testing.T) {
				name := string(db.Engine()) + "-" + trail
				dir := filepath.Join(out, name)
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				path := api.PathPrefix + "/instance/audit/export?receipt=v1"
				scope := map[string]string{}
				wantType, wantOutcome := "auth.login", "failure"
				if trail == "tenant" {
					path = api.PathPrefix + "/orgs/" + e.org + "/audit/export?receipt=v1"
					scope["org_id"] = e.org
					wantType, wantOutcome = "settings.project_renamed", "success"
				}
				got := getAuditExport(t, srv, e.token, path)
				wantReceipt(t, got, apigen.AuditExportReceiptOutcomeSuccess, "", true)
				var eventID string
				for _, line := range got.events {
					var row struct{ ID, Type, Outcome string }
					if err := json.Unmarshal([]byte(line), &row); err != nil {
						t.Fatal(err)
					}
					if row.Type == wantType && row.Outcome == wantOutcome {
						eventID = row.ID
						break
					}
				}
				if eventID == "" {
					t.Fatalf("real %s/%s event absent from export", wantType, wantOutcome)
				}
				var packet bytes.Buffer
				for _, header := range []string{"Hikyo-Audit-Receipt", "Hikyo-Audit-Export-ID", "Hikyo-Audit-Snapshot-Time"} {
					fmt.Fprintf(&packet, "%s: %s\n", header, got.header.Get(header))
				}
				packet.WriteByte('\n')
				packet.Write(got.body)
				cfg, err := json.Marshal(map[string]any{
					"source_id": "local-1037-" + string(db.Engine()), "trail": trail,
					"scope": scope, "database": "retained.sqlite", "max_bytes": 16777216, "max_rows": 20000,
				})
				if err != nil {
					t.Fatal(err)
				}
				config := filepath.Join(dir, "stream.json")
				if err := os.WriteFile(config, cfg, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "packet"), packet.Bytes(), 0o600); err != nil {
					t.Fatal(err)
				}
				run := func(command string, input []byte, wantOK bool) []byte {
					t.Helper()
					cmd := exec.CommandContext(t.Context(), "python3", "-I", "-B", script, command, config)
					cmd.Stdin = bytes.NewReader(input)
					result, err := cmd.CombinedOutput()
					if (err == nil) != wantOK {
						t.Fatalf("receiver %s success=%v, want %v: %s", command, err == nil, wantOK, result)
					}
					return result
				}
				run("init", nil, true)
				ack := run("receive", packet.Bytes(), true)
				if again := run("receive", packet.Bytes(), true); !bytes.Equal(ack, again) {
					t.Fatal("lost acknowledgment retry changed receipt")
				}
				// HTTP 200 bytes without the terminal receipt are incomplete.
				end := bytes.LastIndex(bytes.TrimSuffix(packet.Bytes(), []byte("\n")), []byte("\n"))
				run("receive", packet.Bytes()[:end+1], false)
				run("status", nil, false)
				// These SQL operations affect exclusively this disposable harness DB.
				// Verify the independently committed packet survives both rewrite and
				// deletion. No installed server or durable application data is touched.
				table := "audit_" + trail + "_events"
				execRaw(t, db, "UPDATE "+table+" SET payload='{}' WHERE id='"+eventID+"'")
				execRaw(t, db, "DELETE FROM "+table+" WHERE id='"+eventID+"'")
				if n := queryInt(t, db, "SELECT COUNT(*) FROM "+table+" WHERE id='"+eventID+"'"); n != 0 {
					t.Fatal("synthetic producer event was not removed")
				}
				read := exec.CommandContext(t.Context(), "python3", "-I", "-B", "-c", `import hashlib,pathlib,sqlite3,sys
p=pathlib.Path(sys.argv[1]); d=sqlite3.connect((p/'retained.sqlite').as_uri()+'?mode=ro',uri=True)
original=(p/'packet').read_bytes(); row=d.execute('SELECT packet,digest FROM batches').fetchone()
assert row[0]==original and row[1]==hashlib.sha256(original).hexdigest()
assert d.execute('SELECT COUNT(*) FROM events WHERE id=?',(sys.argv[2],)).fetchone()==(1,)
print('retained read after producer rewrite/removal: PASS')`, dir, eventID)
				if output, err := read.CombinedOutput(); err != nil {
					t.Fatalf("independent reader: %v %s", err, output)
				}
				if err := os.WriteFile(filepath.Join(dir, "ack.json"), ack, 0o600); err != nil {
					t.Fatal(err)
				}
				t.Logf("%s: product %s/%s, completed packet, exact retry, partial refusal, retained read PASS; separate-process rehearsal only", name, wantType, wantOutcome)
			})
		}
	})
}
