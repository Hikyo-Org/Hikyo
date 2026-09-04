package store

import (
	"testing"
	"time"
)

// TestDialectSelection pins the single per-engine dialect (sqliteDialect /
// pgDialect) now embedded in every adapter tx/db shim: the single-arg SQL
// placeholder rewrite, the two-arg SQLPerEngine selector for divergent pairs,
// placeholder style, and the engine-appropriate timestamp encoding.
func TestDialectSelection(t *testing.T) {
	at := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		d          adapterDialect
		wantSQL    string
		wantPerEng string
		wantPlace  string
		wantStamp  any
	}{
		{"sqlite", sqliteDialect{}, "a=? AND b=?", "sqlite", "?,?,?", "2026-08-23T12:00:00.000000Z"},
		{"postgres", pgDialect{}, "a=$1 AND b=$2", "postgres", "$2,$3,$4", at},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.SQL("a=? AND b=?"); got != tt.wantSQL {
				t.Fatalf("SQL() = %q, want %q", got, tt.wantSQL)
			}
			if got := tt.d.SQLPerEngine("sqlite", "postgres"); got != tt.wantPerEng {
				t.Fatalf("SQLPerEngine() = %q, want %q", got, tt.wantPerEng)
			}
			if got := tt.d.Placeholders(3, 2); got != tt.wantPlace {
				t.Fatalf("Placeholders(3, 2) = %q, want %q", got, tt.wantPlace)
			}
			if got := tt.d.Stamp(at); got != tt.wantStamp {
				t.Fatalf("Stamp() = %#v, want %#v", got, tt.wantStamp)
			}
		})
	}
}
