package main

import (
	"bytes"
	"errors"
	"strings"
	"time"
)

func loadSource(s source, now time.Time) ([]event, error) {
	packet, rows, err := readCollectorSnapshot(s, now)
	if err != nil {
		return nil, err
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
