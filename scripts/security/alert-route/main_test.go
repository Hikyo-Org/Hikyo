package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const orgID = "00000000-0000-7000-8000-000000000001"
const actorID = "00000000-0000-7000-8000-000000000002"
const credentialID = "00000000-0000-7000-8000-000000000003"

var fixtureTime = time.Date(2026, 10, 28, 12, 1, 0, 0, time.UTC)

func fixtureConfig(t *testing.T) config {
	t.Helper()
	dir := t.TempDir()
	return config{Enabled: true, Destination: "https://operator.invalid/alerts", Receiver: "synthetic-receiver", TokenEnv: "HIKYO_ALERT_TEST_TOKEN", EscalationOwner: "synthetic-oncall", Sources: []source{{ID: "instance", Instance: "synthetic-hikyo", Trail: "instance", Database: filepath.Join(dir, "collector.db")}}}
}

func fixtureEvent(n int, kind, outcome string) event {
	return event{ID: fmt.Sprintf("00000000-0000-7000-8000-%012d", n+100), Type: kind, Version: 1, RecordedAt: fixtureTime, Actor: actorID, Credential: credentialID, Outcome: outcome, Origin: "cli", SourceIP: "192.0.2.10"}
}

func writeInput(t *testing.T, s source, events []event, now time.Time) {
	t.Helper()
	var b strings.Builder
	for _, e := range events {
		b.WriteString(wireFixture(e))
		b.WriteByte('\n')
	}
	writeRaw(t, s, []byte(b.String()), len(events), now)
}

func wireFixture(e event) string {
	return strings.TrimSuffix(mustJSON(e), "}") + `,"seq":1,"occurred_at":"` + e.RecordedAt.Format(time.RFC3339Nano) + `","occurred_asserted":false,"actor_class":"human","scope_class":"instance","payload":{}}`
}

func writeRaw(t *testing.T, s source, raw []byte, rows int, now time.Time) {
	t.Helper()
	terminal := map[string]interface{}{"export_receipt": map[string]interface{}{"version": 1, "started_event_id": actorID, "snapshot_time": now.Format(time.RFC3339Nano), "rows_streamed": rows, "outcome": "success", "outcome_recorded": true}}
	packet := append([]byte("Hikyo-Audit-Receipt: v1\nHikyo-Audit-Export-ID: "+actorID+"\nHikyo-Audit-Snapshot-Time: "+now.Format(time.RFC3339Nano)+"\n\n"), raw...)
	packet = append(packet, []byte(mustJSON(terminal)+"\n")...)
	db, err := sql.Open("sqlite", s.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{"CREATE TABLE IF NOT EXISTS stream(binding TEXT NOT NULL)", "CREATE TABLE IF NOT EXISTS batches(id TEXT PRIMARY KEY,digest TEXT NOT NULL,rows INTEGER NOT NULL,received_ns INTEGER NOT NULL,packet BLOB NOT NULL)", "CREATE TABLE IF NOT EXISTS failure(at_ns INTEGER NOT NULL,cause TEXT NOT NULL)", "DELETE FROM stream", "DELETE FROM batches", "DELETE FROM failure"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO stream VALUES (?)", mustJSON(binding(s))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO batches VALUES (?,?,?,?,?)", actorID, digest(packet), rows, now.UnixNano(), packet); err != nil {
		t.Fatal(err)
	}
}

func mutateCollector(t *testing.T, s source, q string, args ...interface{}) {
	t.Helper()
	db, err := sql.Open("sqlite", s.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

func assertRule(t *testing.T, st state, rule string, count int) {
	t.Helper()
	got := 0
	for _, d := range st.Outbox {
		if d.Alert.Rule == rule {
			got++
		}
	}
	if got != count {
		t.Fatalf("rule %s: got %d alerts, want %d", rule, got, count)
	}
}

func evaluate(t *testing.T, c config, st *state, now time.Time) []string {
	t.Helper()
	health, err := st.evaluate(c, now)
	if err != nil {
		t.Fatal(err)
	}
	return health
}

func TestRulesDuplicatesOutOfOrderAndBenign(t *testing.T) {
	c := fixtureConfig(t)
	events := []event{}
	for i := range 5 {
		events = append(events, fixtureEvent(i, "auth.login", "failure"))
	}
	events = append(events, events[0]) // transport duplicate cannot cross a threshold
	events[0].RecordedAt = fixtureTime.Add(-10 * time.Second)
	events = append(events, fixtureEvent(10, "auth.login", "success"), fixtureEvent(11, "audit.query", "success"))
	// An unsupported replay claim is deliberately silent in the current ADR.
	events = append(events, fixtureEvent(12, "auth.token_revoked_replay", "failure"))
	writeInput(t, c.Sources[0], events, fixtureTime)
	st := newState(c)
	if health := evaluate(t, c, &st, fixtureTime); len(health) != 0 {
		t.Fatal(health)
	}
	assertRule(t, st, "failed-login-burst", 1)
	if len(st.Outbox) != 1 {
		t.Fatal("benign or unsupported input alerted")
	}
	for _, d := range st.Outbox {
		if d.Alert.Count != 5 {
			t.Fatalf("duplicate counted: %d", d.Alert.Count)
		}
	}
	// Re-export, restart and a later row cannot re-notify this window.
	path := filepath.Join(t.TempDir(), "state.json")
	if err := save(path, st); err != nil {
		t.Fatal(err)
	}
	b, err := readBounded(path)
	if err != nil {
		t.Fatal(err)
	}
	var restarted state
	if err := decode(b, &restarted, true); err != nil {
		t.Fatal(err)
	}
	evaluate(t, c, &restarted, fixtureTime.Add(time.Second))
	if len(restarted.Outbox) != 1 {
		t.Fatal("restart duplicated alert")
	}
}

func TestDisclosureSensitiveAndScopeBoundaries(t *testing.T) {
	c := fixtureConfig(t)
	c.Sources[0].Trail = "tenant"
	c.Sources[0].Org = orgID
	events := []event{}
	for i := range 19 {
		e := fixtureEvent(i, "identity.disclosure", "success")
		e.Org = orgID
		events = append(events, e)
	}
	writeInput(t, c.Sources[0], events, fixtureTime)
	st := newState(c)
	evaluate(t, c, &st, fixtureTime)
	if len(st.Outbox) != 0 {
		t.Fatal("below-threshold disclosure alerted")
	}
	e := fixtureEvent(20, "identity.disclosure", "success")
	e.Org = orgID
	events = append(events, e)
	e = fixtureEvent(21, "grant.created", "success")
	e.Org = orgID
	events = append(events, e)
	writeInput(t, c.Sources[0], events, fixtureTime)
	evaluate(t, c, &st, fixtureTime)
	assertRule(t, st, "disclosure-burst", 1)
	assertRule(t, st, "sensitive-change", 1)
	for _, d := range st.Outbox {
		for _, ref := range d.Alert.Events {
			if ref.Actor != actorID || ref.Credential != credentialID || ref.Org != orgID {
				t.Fatal("lost attribution")
			}
		}
	}
	events[0].Org = actorID
	writeInput(t, c.Sources[0], events, fixtureTime)
	if health := evaluate(t, c, &st, fixtureTime); len(health) != 1 {
		t.Fatal("cross-scope input accepted")
	}
}

func TestWatchedDisclosureAndOfflineNegative(t *testing.T) {
	c := fixtureConfig(t)
	c.WatchedCredentials = []string{credentialID}
	e := fixtureEvent(1, "disclosure.value_revealed", "success")
	writeInput(t, c.Sources[0], []event{e}, fixtureTime)
	st := newState(c)
	evaluate(t, c, &st, fixtureTime)
	assertRule(t, st, "watched-disclosure", 1)
	e.ID = actorID
	e.Origin = "offline-reconciled"
	writeInput(t, c.Sources[0], []event{e}, fixtureTime)
	st = newState(c)
	evaluate(t, c, &st, fixtureTime)
	if len(st.Outbox) != 0 {
		t.Fatal("offline assertion treated as current online disclosure")
	}
}

func TestCollectorFailureStalePartialAndDigest(t *testing.T) {
	for _, fault := range []string{"missing", "stale", "future", "partial", "digest", "scope", "invalid", "old-event"} {
		t.Run(fault, func(t *testing.T) {
			c := fixtureConfig(t)
			s := c.Sources[0]
			now := fixtureTime
			e := fixtureEvent(1, "grant.created", "success")
			if fault == "old-event" {
				e.RecordedAt = now.Add(-time.Hour)
			}
			writeInput(t, s, []event{e}, now)
			switch fault {
			case "missing":
				if err := os.Remove(s.Database); err != nil {
					t.Fatal(err)
				}
			case "stale":
				now = now.Add(maxLag + time.Second)
			case "future":
				mutateCollector(t, s, "UPDATE batches SET received_ns=?", now.Add(time.Minute).UnixNano())
			case "partial":
				mutateCollector(t, s, "UPDATE batches SET packet=substr(packet,1,length(packet)-10)")
			case "digest":
				mutateCollector(t, s, "UPDATE batches SET digest=?", strings.Repeat("0", 64))
			case "scope":
				mutateCollector(t, s, "UPDATE stream SET binding=?", `{"source_id":"other","trail":"instance","scope":{}}`)
			case "invalid":
				mutateCollector(t, s, "UPDATE batches SET rows=2")
			}

			st := newState(c)
			health := evaluate(t, c, &st, now)
			if fault == "old-event" {
				if len(st.Outbox) != 0 {
					t.Fatal("historical event triggered current alert")
				}
				return
			}
			if len(health) != 1 {
				t.Fatal("source failure presented as healthy")
			}
			assertRule(t, st, "collector-unavailable", 1)
			assertRule(t, st, "sensitive-change", 0)
		})
	}
}

func TestSyntheticTLSReceiptRetryRestartAndAcknowledgment(t *testing.T) {
	c := fixtureConfig(t)
	writeInput(t, c.Sources[0], []event{fixtureEvent(1, "grant.created", "success")}, fixtureTime)
	st := newState(c)
	evaluate(t, c, &st, fixtureTime)
	var id string
	for key := range st.Outbox {
		id = key
	}
	requests := 0
	path := filepath.Join(t.TempDir(), "state.json")
	sink := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer synthetic-token" || r.Header.Get("Idempotency-Key") != id {
			t.Error("lost auth or idempotency")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var a alert
		if err := decode(body, &a, true); err != nil {
			t.Error(err)
		}
		if a.ID != id || len(a.Events) != 1 || a.Events[0].ID != fixtureEvent(1, "", "").ID {
			t.Error("event-to-alert link lost")
		}
		persisted, err := readBounded(path)
		var intent state
		if err != nil || decode(persisted, &intent, true) != nil || intent.Outbox[id] == nil || intent.Outbox[id].Status != "uncertain" || intent.Outbox[id].Attempts != requests {
			t.Error("network effect preceded durable attempt intent")
		}
		if requests == 1 {
			w.WriteHeader(503)
			return
		}
		fmt.Fprint(w, mustJSON(destinationReceipt{AlertID: id, Receiver: c.Receiver, ReceiptID: "synthetic-receipt-1"}))
	}))
	defer sink.Close()
	c.Destination = sink.URL
	if err := deliver(c, &st, path, fixtureTime, sink.Client(), "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	if st.Outbox[id].Status != "uncertain" || requests != 1 {
		t.Fatal("failed request claimed as delivery")
	}
	b, err := readBounded(path)
	if err != nil {
		t.Fatal(err)
	}
	var restarted state
	if err := decode(b, &restarted, true); err != nil {
		t.Fatal(err)
	}
	if err := deliver(c, &restarted, path, fixtureTime.Add(29*time.Second), sink.Client(), "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatal("retry ignored backoff")
	}
	if err := deliver(c, &restarted, path, fixtureTime.Add(30*time.Second), sink.Client(), "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	if restarted.Outbox[id].Status != "received" || requests != 2 {
		t.Fatal("matching receiver receipt not retained")
	}
	if err := deliver(c, &restarted, path, fixtureTime.Add(time.Minute), sink.Client(), "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	if requests != 2 {
		t.Fatal("received alert redelivered")
	}
	r, err := makeReport(c, restarted, fixtureTime.Add(6*time.Minute), nil, nil, "synthetic")
	if err != nil || len(r.Escalate) != 1 {
		t.Fatal("receipt mistaken for human acknowledgment")
	}
	ack := acknowledgment{AlertID: id, ReceiptID: "synthetic-receipt-1", Operator: "synthetic-operator", At: fixtureTime.Add(time.Minute)}
	r, err = makeReport(c, restarted, fixtureTime.Add(6*time.Minute), nil, []acknowledgment{ack}, "synthetic")
	if err != nil || len(r.Escalate) != 0 {
		t.Fatal("matching acknowledgment not recognized")
	}
	ack.ReceiptID = "wrong"
	if _, err := makeReport(c, restarted, fixtureTime, nil, []acknowledgment{ack}, "synthetic"); err == nil {
		t.Fatal("wrong receipt acknowledged")
	}
}

func TestTimeoutMissingReceiptAndTerminal(t *testing.T) {
	for _, fault := range []string{"timeout", "empty-200", "wrong-receiver", "terminal", "redirect"} {
		t.Run(fault, func(t *testing.T) {
			c := fixtureConfig(t)
			writeInput(t, c.Sources[0], []event{fixtureEvent(1, "grant.created", "success")}, fixtureTime)
			st := newState(c)
			evaluate(t, c, &st, fixtureTime)
			release := make(chan struct{})
			sink := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch fault {
				case "timeout":
					<-release
				case "terminal":
					w.WriteHeader(403)
				case "redirect":
					w.Header().Set("Location", "https://never.invalid/")
					w.WriteHeader(302)
				case "wrong-receiver":
					fmt.Fprint(w, `{"alert_id":"wrong","receiver":"wrong","receipt_id":"fake"}`)
				}
			}))
			defer sink.Close()
			defer close(release)
			client := sink.Client()
			client.Timeout = 20 * time.Millisecond
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			c.Destination = sink.URL
			path := filepath.Join(t.TempDir(), "state.json")
			for i := range 4 {
				if err := deliver(c, &st, path, fixtureTime.Add(time.Duration(i)*3*time.Minute), client, "synthetic-token"); err != nil {
					t.Fatal(err)
				}
			}
			for _, d := range st.Outbox {
				want := "exhausted-uncertain"
				attempts := 3
				if fault == "terminal" {
					want = "terminal"
					attempts = 1
				}
				if d.Status != want || d.Attempts != attempts || d.Receipt != "" {
					t.Fatalf("wrong terminal state: %+v", d)
				}
			}
		})
	}
}

func TestRedactionAndBrokenDetectorNegativeControl(t *testing.T) {
	c := fixtureConfig(t)
	e := fixtureEvent(1, "grant.created", "success")
	canary := "DO_NOT_EXPORT_SECRET_CANARY"
	raw := strings.TrimSuffix(mustJSON(e), "}") + `,"user_agent":"` + canary + `","payload":{"password":"` + canary + `","token":"ew_secret_canary"}}` + "\n"
	writeRaw(t, c.Sources[0], []byte(raw), 1, fixtureTime)
	st := newState(c)
	evaluate(t, c, &st, fixtureTime)
	r, err := makeReport(c, st, fixtureTime, nil, nil, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	output := mustJSON(st) + mustJSON(r)
	for _, secret := range []string{canary, "ew_secret_canary", "192.0.2.10"} {
		if strings.Contains(output, secret) {
			t.Fatal("sensitive bytes escaped")
		}
	}
	// Broken detector: a renamed event must fail the expected-alert oracle.
	e.Type = "broken.grant_created"
	writeInput(t, c.Sources[0], []event{e}, fixtureTime)
	broken := newState(c)
	evaluate(t, c, &broken, fixtureTime)
	if len(st.Outbox) != 1 || len(broken.Outbox) != 0 {
		t.Fatal("negative control cannot distinguish broken detector")
	}
}

func TestConfigAndCommandFailClosedWithoutNetwork(t *testing.T) {
	for _, fault := range []string{"disabled", "http", "query", "missing-owner", "missing-sources", "bad-credential", "duplicate-source"} {
		t.Run(fault, func(t *testing.T) {
			c := fixtureConfig(t)
			switch fault {
			case "disabled":
				c.Enabled = false
			case "http":
				c.Destination = "http://operator.invalid/"
			case "query":
				c.Destination += "?token=secret"
			case "missing-owner":
				c.EscalationOwner = ""
			case "missing-sources":
				c.Sources = nil
			case "bad-credential":
				c.WatchedCredentials = []string{"raw-secret"}
			case "duplicate-source":
				c.Sources = append(c.Sources, c.Sources[0])
			}
			if c.validate() == nil {
				t.Fatal("invalid enabled configuration accepted")
			}
		})
	}
	c := fixtureConfig(t)
	writeInput(t, c.Sources[0], nil, time.Now().UTC())
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(mustJSON(c)), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"-config", path}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "plan-no-delivery") {
		t.Fatal("plan claims delivery")
	}
	if err := run([]string{"-config", path, "-send"}, io.Discard); err == nil {
		t.Fatal("send allowed without durable state")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.lock"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-config", path, "-send", "-state", dir}, io.Discard); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatal("concurrent writer not refused")
	}
	if err := os.Remove(filepath.Join(dir, "run.lock")); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-config", path, "-send", "-state", dir}, io.Discard); err == nil || !strings.Contains(err.Error(), "state missing") {
		t.Fatal("missing custody state silently initialized")
	}
	if err := run([]string{"-config", path, "-enroll", "-state", dir}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-config", path, "-enroll", "-state", dir}, io.Discard); err == nil {
		t.Fatal("existing custody re-enrolled")
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-config", path, "-send", "-state", dir}, io.Discard); err == nil {
		t.Fatal("corrupt custody state accepted")
	}
}

func TestWindowBoundaryAndUnknownAccountLimits(t *testing.T) {
	c := fixtureConfig(t)
	events := []event{}
	for i := range 5 {
		e := fixtureEvent(i, "auth.login", "failure")
		e.Actor = ""
		e.Credential = ""
		if i == 4 {
			e.RecordedAt = e.RecordedAt.Add(-2 * time.Minute)
		}
		events = append(events, e)
	}
	writeInput(t, c.Sources[0], events, fixtureTime)
	st := newState(c)
	evaluate(t, c, &st, fixtureTime)
	if len(st.Outbox) != 0 {
		t.Fatal("fixed windows silently treated as sliding")
	}
	e := fixtureEvent(9, "auth.login", "failure")
	e.Actor = ""
	e.Credential = ""
	events = append(events, e)
	writeInput(t, c.Sources[0], events, fixtureTime)
	evaluate(t, c, &st, fixtureTime)
	assertRule(t, st, "failed-login-burst", 1)
	for _, d := range st.Outbox {
		for _, ref := range d.Alert.Events {
			if ref.Actor != "" || ref.Credential != "" {
				t.Fatal("unknown account identity invented")
			}
		}
	}
}

func TestIndependentReceiveFailureAndBoundedOutbox(t *testing.T) {
	c := fixtureConfig(t)
	writeInput(t, c.Sources[0], nil, fixtureTime)
	mutateCollector(t, c.Sources[0], "INSERT INTO failure VALUES (?, 'receive-refused')", fixtureTime.UnixNano())
	st := newState(c)
	if h := evaluate(t, c, &st, fixtureTime); len(h) != 1 {
		t.Fatal("receiver failure masked by recent heartbeat")
	}
	st = newState(c)
	for i := range maxAlerts {
		st.Outbox[fmt.Sprint(i)] = &delivery{Status: "pending"}
	}
	if _, err := st.evaluate(c, fixtureTime); err == nil {
		t.Fatal("outbox overflow silently dropped evidence")
	}
}

// Opt-in cross-lane consumer check, run against the exact #1037 receiver source.
// CI still runs all self-contained contracts above without that sibling checkout.
func TestCollectorProducerCompatibility(t *testing.T) {
	script := os.Getenv("HIKYO_ALERT_COLLECTOR_SCRIPT")
	if script == "" {
		t.Skip("NOTRUN: set HIKYO_ALERT_COLLECTOR_SCRIPT to the reviewed #1037 receiver.py")
	}
	c := fixtureConfig(t)
	s := c.Sources[0]
	now := time.Now().UTC()
	e := fixtureEvent(1, "grant.created", "success")
	e.RecordedAt = now
	raw := wireFixture(e) + "\n"
	writeRaw(t, s, []byte(raw), 1, now)
	db, err := sql.Open("sqlite", s.Database)
	if err != nil {
		t.Fatal(err)
	}
	var packet []byte
	if err := db.QueryRow("SELECT packet FROM batches").Scan(&packet); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.Database); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "receiver.json")
	cfg := map[string]interface{}{"source_id": s.Instance, "trail": s.Trail, "scope": map[string]string{}, "database": s.Database, "max_bytes": maxFile, "max_rows": maxEvents}
	if err := os.WriteFile(path, []byte(mustJSON(cfg)), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"init", "receive"} {
		cmd := exec.Command("python3", script, command, path)
		cmd.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
		if command == "receive" {
			cmd.Stdin = bytes.NewReader(packet)
		}
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("synthetic receiver %s: %v: %s", command, err, output)
		}
	}
	st := newState(c)
	if health := evaluate(t, c, &st, time.Now().UTC()); len(health) != 0 {
		t.Fatal("collector consumer contract mismatch", health)
	}
	assertRule(t, st, "sensitive-change", 1)
}
