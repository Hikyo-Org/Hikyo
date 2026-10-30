package main

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
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

// readCollectorSnapshot is the exact read-only engine boundary for #1037's
// independently administered receiver. It cannot return a driver handle or
// open Hikyo's datastore through the application store. The reviewed raw-SQL
// protocol pins this body, its bindings and its sole parsing caller.
func readCollectorSnapshot(s source, now time.Time) ([]byte, int, error) {
	path, err := filepath.Abs(s.Database)
	if err != nil {
		return nil, 0, errors.New("invalid collector path")
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, 0, errors.New("collector unavailable")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, 0, errors.New("collector snapshot unavailable")
	}
	defer tx.Rollback()
	var encoded string
	var actual streamBinding
	if tx.QueryRowContext(ctx, "SELECT binding FROM stream").Scan(&encoded) != nil || decode([]byte(encoded), &actual, true) != nil || mustJSON(actual) != mustJSON(binding(s)) {
		return nil, 0, errors.New("collector binding mismatch")
	}
	var received int64
	var packet []byte
	var hash string
	var rows int
	if tx.QueryRowContext(ctx, "SELECT received_ns, CASE WHEN length(packet) <= ? THEN packet ELSE NULL END, digest, rows FROM batches ORDER BY received_ns DESC LIMIT 1", maxFile).Scan(&received, &packet, &hash, &rows) != nil {
		return nil, 0, errors.New("collector has no complete bounded batch")
	}
	var failed sql.NullInt64
	if tx.QueryRowContext(ctx, "SELECT MAX(at_ns) FROM failure").Scan(&failed) != nil || (failed.Valid && failed.Int64 >= received) {
		return nil, 0, errors.New("collector receive failure")
	}
	if now.Sub(time.Unix(0, received)) > maxLag || time.Unix(0, received).After(now) || digest(packet) != hash || rows < 0 || rows > maxEvents {
		return nil, 0, errors.New("collector stale, oversized or corrupt")
	}
	return packet, rows, nil
}
