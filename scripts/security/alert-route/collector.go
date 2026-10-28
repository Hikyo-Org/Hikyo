package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // The independently owned #1037 receiver uses SQLite.
)

type streamBinding struct {
	Source string            `json:"source_id"`
	Trail  string            `json:"trail"`
	Scope  map[string]string `json:"scope"`
}

func binding(s source) streamBinding {
	b := streamBinding{Source: s.Instance, Trail: s.Trail, Scope: map[string]string{}}
	if s.Org != "" {
		b.Scope["org_id"] = s.Org
	}
	if s.Project != "" {
		b.Scope["project_id"] = s.Project
	}
	return b
}

func loadSource(s source, now time.Time) ([]event, error) {
	path, err := filepath.Abs(s.Database)
	if err != nil {
		return nil, errors.New("invalid collector path")
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, errors.New("collector unavailable")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.New("collector snapshot unavailable")
	}
	defer tx.Rollback()
	var encoded string
	var actual streamBinding
	if tx.QueryRowContext(ctx, "SELECT binding FROM stream").Scan(&encoded) != nil || decode([]byte(encoded), &actual, true) != nil || mustJSON(actual) != mustJSON(binding(s)) {
		return nil, errors.New("collector binding mismatch")
	}
	var received int64
	var packet []byte
	var hash string
	var rows int
	if tx.QueryRowContext(ctx, "SELECT received_ns, CASE WHEN length(packet) <= ? THEN packet ELSE NULL END, digest, rows FROM batches ORDER BY received_ns DESC LIMIT 1", maxFile).Scan(&received, &packet, &hash, &rows) != nil {
		return nil, errors.New("collector has no complete bounded batch")
	}
	var failed sql.NullInt64
	if tx.QueryRowContext(ctx, "SELECT MAX(at_ns) FROM failure").Scan(&failed) != nil || (failed.Valid && failed.Int64 >= received) {
		return nil, errors.New("collector receive failure")
	}
	if now.Sub(time.Unix(0, received)) > maxLag || time.Unix(0, received).After(now) || digest(packet) != hash || rows < 0 || rows > maxEvents {
		return nil, errors.New("collector stale, oversized or corrupt")
	}
	// Independently recheck the negotiated header and terminal record. A 200,
	// packet prefix, or collector heartbeat alone is insufficient evidence.
	framing, body, ok := bytes.Cut(packet, []byte("\n\n"))
	if !ok {
		return nil, errors.New("missing response-bound collector framing")
	}
	headers := map[string]string{}
	for _, line := range strings.Split(string(framing), "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok || headers[key] != "" {
			return nil, errors.New("duplicate or invalid collector framing")
		}
		headers[key] = value
	}
	if len(headers) != 3 || headers["Hikyo-Audit-Receipt"] != "v1" {
		return nil, errors.New("invalid collector framing")
	}
	lines := bytes.Split(body, []byte{'\n'})
	if len(lines) != rows+2 || len(lines[len(lines)-1]) != 0 {
		return nil, errors.New("partial collector packet")
	}
	var terminal struct {
		Receipt struct {
			Version  int    `json:"version"`
			Started  string `json:"started_event_id"`
			Snapshot string `json:"snapshot_time"`
			Rows     int    `json:"rows_streamed"`
			Outcome  string `json:"outcome"`
			Recorded bool   `json:"outcome_recorded"`
		} `json:"export_receipt"`
	}
	if decode(lines[len(lines)-2], &terminal, true) != nil || terminal.Receipt.Version != 1 || !uuidPattern.MatchString(terminal.Receipt.Started) || terminal.Receipt.Rows != rows || terminal.Receipt.Outcome != "success" || !terminal.Receipt.Recorded {
		return nil, errors.New("incomplete export receipt")
	}
	if terminal.Receipt.Started != headers["Hikyo-Audit-Export-ID"] || terminal.Receipt.Snapshot != headers["Hikyo-Audit-Snapshot-Time"] {
		return nil, errors.New("export response binding mismatch")
	}
	if _, err := time.Parse(time.RFC3339Nano, terminal.Receipt.Snapshot); err != nil {
		return nil, errors.New("invalid export snapshot")
	}
	events := make([]event, 0, rows)
	for _, line := range lines[:len(lines)-2] {
		var e event
		if len(line) > 1<<20 || decode(line, &e, false) != nil {
			return nil, errors.New("invalid export event")
		}
		// Some object locators are names. Only opaque UUID references may leave.
		if !uuidPattern.MatchString(e.Object) {
			e.Object = ""
		}
		if !e.valid(s, now) {
			return nil, errors.New("invalid audit envelope or scope")
		}
		events = append(events, e)
	}
	return events, nil
}
