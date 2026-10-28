package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic("cannot encode alert contract")
	}
	return string(b)
}

// Replace is durable before returning. A single scheduler owns the directory;
// the exclusive lock refuses concurrent runs rather than racing outbox writes.
func save(path string, st state) error {
	b, err := json.Marshal(st)
	if err != nil {
		return errors.New("cannot encode state")
	}
	if len(b) > maxFile {
		return errors.New("state size bound reached; independent escalation required")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".alert-state-")
	if err != nil {
		return errors.New("cannot create state")
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return errors.New("cannot persist state")
	}
	if err = os.Rename(name, path); err != nil {
		return errors.New("cannot replace state")
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return errors.New("cannot open state directory")
	}
	defer d.Close()
	return d.Sync()
}

type destinationReceipt struct {
	AlertID   string `json:"alert_id"`
	Receiver  string `json:"receiver"`
	ReceiptID string `json:"receipt_id"`
}

func send(client *http.Client, c config, a alert, token string) (string, string) {
	req, err := http.NewRequest(http.MethodPost, c.Destination, strings.NewReader(mustJSON(a)))
	if err != nil {
		return "terminal", ""
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Idempotency-Key", a.ID)
	resp, err := client.Do(req)
	if err != nil {
		return "uncertain", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != 408 && resp.StatusCode != 429 {
		return "terminal", ""
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return "uncertain", ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	var r destinationReceipt
	if err != nil || len(b) > 4096 || decode(b, &r, true) != nil || r.AlertID != a.ID || r.Receiver != c.Receiver || !labelPattern.MatchString(r.ReceiptID) {
		return "uncertain", ""
	}
	return "received", r.ReceiptID
}

func deliver(c config, st *state, path string, now time.Time, client *http.Client, token string) error {
	ids := make([]string, 0, len(st.Outbox))
	for id := range st.Outbox {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := st.Outbox[ids[i]].Alert.CreatedAt, st.Outbox[ids[j]].Alert.CreatedAt
		if a.Equal(b) {
			return ids[i] < ids[j]
		}
		return a.Before(b)
	})
	attempted := 0
	for _, id := range ids {
		d := st.Outbox[id]
		if d.Status == "received" || d.Status == "terminal" || d.Status == "exhausted-uncertain" || now.Before(d.Next) {
			continue
		}
		if d.Attempts >= 3 {
			d.Status = "exhausted-uncertain"
			continue
		}
		if attempted >= 4 {
			break
		}
		attempted++
		d.Attempts++
		d.Status = "uncertain" // Persist intent before any network effect, including crash ambiguity.
		d.Next = now.Add(30 * time.Second)
		if d.Attempts >= 2 {
			d.Next = now.Add(2 * time.Minute)
		}
		if err := save(path, *st); err != nil {
			return err
		}
		d.Status, d.Receipt = send(client, c, d.Alert, token)
		if d.Status == "uncertain" && d.Attempts >= 3 {
			d.Status = "exhausted-uncertain"
		}
		if err := save(path, *st); err != nil {
			return err
		}
	}
	return save(path, *st)
}

type acknowledgment struct {
	AlertID   string    `json:"alert_id"`
	ReceiptID string    `json:"receipt_id"`
	Operator  string    `json:"operator"`
	At        time.Time `json:"acknowledged_at"`
}

type report struct {
	Mode             string      `json:"mode"`
	EvaluatedAt      time.Time   `json:"evaluated_at"`
	UnhealthySources []string    `json:"unhealthy_sources"`
	Deliveries       []*delivery `json:"deliveries"`
	Escalate         []string    `json:"escalate"`
	EscalationOwner  string      `json:"escalation_owner"`
}

func makeReport(c config, st state, now time.Time, health []string, acks []acknowledgment, mode string) (report, error) {
	acknowledged := map[string]bool{}
	for _, a := range acks {
		d, ok := st.Outbox[a.AlertID]
		if !ok && now.Sub(a.At) > retention {
			continue
		}
		if !ok || d.Status != "received" || d.Receipt != a.ReceiptID || !labelPattern.MatchString(a.Operator) || a.At.Before(d.Alert.CreatedAt) || a.At.After(now.Add(30*time.Second)) {
			return report{}, errors.New("acknowledgment must match a received alert, receipt, operator and time")
		}
		acknowledged[a.AlertID] = true
	}
	r := report{Mode: mode, EvaluatedAt: now, UnhealthySources: health, Deliveries: []*delivery{}, Escalate: []string{}, EscalationOwner: c.EscalationOwner}
	for _, d := range st.Outbox {
		r.Deliveries = append(r.Deliveries, d)
		if d.Status == "terminal" || d.Status == "exhausted-uncertain" || (!acknowledged[d.Alert.ID] && now.Sub(d.Alert.CreatedAt) >= 5*time.Minute) {
			r.Escalate = append(r.Escalate, d.Alert.ID)
		}
	}
	sort.Slice(r.Deliveries, func(i, j int) bool { return r.Deliveries[i].Alert.ID < r.Deliveries[j].Alert.ID })
	sort.Strings(r.Escalate)
	return r, nil
}

func run(args []string, out io.Writer) error {
	f := flag.NewFlagSet("alert-route", flag.ContinueOnError)
	configPath := f.String("config", "", "operator configuration JSON")
	stateDir := f.String("state", "", "private durable state directory, required for -send")
	ackPath := f.String("ack-file", "", "operator acknowledgments JSON array")
	sending := f.Bool("send", false, "send to the explicitly configured destination; requires separate operator authorization")
	enroll := f.Bool("enroll", false, "explicitly enroll a new private state directory without sending")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *configPath == "" || (*sending && *enroll) {
		return errors.New("require -config and no positional arguments")
	}
	b, err := readBounded(*configPath)
	if err != nil {
		return err
	}
	var c config
	if err := decode(b, &c, true); err != nil {
		return err
	}
	if err := c.validate(); err != nil {
		return err
	}
	now := time.Now().UTC()
	st := newState(c)
	path := ""
	mode := "plan-no-delivery"
	if *sending || *enroll {
		mode = "send"
		if *stateDir == "" {
			return errors.New("-send requires a private durable -state directory")
		}
		info, err := os.Lstat(*stateDir)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
			return errors.New("state directory must exist with mode 0700 and cannot be a symlink")
		}
		lock := filepath.Join(*stateDir, "run.lock")
		lf, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return errors.New("state locked; inspect the owning process before recovery")
		}
		defer os.Remove(lock)
		if err := lf.Close(); err != nil {
			return err
		}
		path = filepath.Join(*stateDir, "state.json")
		if *enroll {
			// Exclusive enrollment leaves an invalid sentinel on interruption.
			// A later send must never silently recreate lost or partial state.
			f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				return errors.New("enrollment refuses existing state; preserve custody")
			}
			if err := f.Close(); err != nil {
				return err
			}
			if err := save(path, st); err != nil {
				return err
			}
			return json.NewEncoder(out).Encode(map[string]string{"mode": "enrolled-no-delivery", "configuration": st.Config})
		}
		b, err := readBounded(path)
		if err != nil {
			return errors.New("state missing or unreadable; explicit first enrollment or custody recovery required")
		} else if decode(b, &st, true) != nil || !st.valid(c) {
			return errors.New("state invalid or configuration changed; preserve state and reconcile before restart")
		}
	}
	health, err := st.evaluate(c, now)
	if err != nil {
		return err
	}
	acks := []acknowledgment{}
	if *ackPath != "" {
		b, err := readBounded(*ackPath)
		if err != nil {
			return err
		}
		if err := decode(b, &acks, true); err != nil {
			return err
		}
	}
	if _, err := makeReport(c, st, now, health, acks, mode); err != nil {
		return err
	}
	if *sending {
		token := os.Getenv(c.TokenEnv)
		if token == "" || len(token) > 4096 || bytes.ContainsAny([]byte(token), "\r\n") {
			return errors.New("configured notification credential unavailable or invalid")
		}
		// Dedicated transport: no ambient proxy, redirects, cookies or credential forwarding.
		client := &http.Client{Transport: &http.Transport{TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 5 * time.Second}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		defer client.CloseIdleConnections()
		if err := deliver(c, &st, path, now, client, token); err != nil {
			return err
		}
	}
	r, err := makeReport(c, st, now, health, acks, mode)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(out).Encode(r); err != nil {
		return err
	}
	if len(health) > 0 || len(r.Escalate) > 0 {
		return errors.New("independent operator escalation required; inspect report")
	}
	for _, d := range r.Deliveries {
		if *sending && d.Status != "received" {
			return errors.New("notification delivery pending or uncertain; inspect report")
		}
	}
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "alert-route:", err)
		os.Exit(1)
	}
}
