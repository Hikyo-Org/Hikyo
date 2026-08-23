package store

import (
	"testing"
	"time"
)

func TestAdapterTransactionOwnsDialectSelection(t *testing.T) {
	tests := []struct {
		name      string
		db        adapterDB
		wantSQL   string
		wantStamp any
	}{
		{
			name:      "sqlite",
			db:        sqliteAdapterTx{},
			wantSQL:   "sqlite",
			wantStamp: "2026-08-23T12:00:00.000000Z",
		},
		{
			name:      "postgres",
			db:        pgAdapterTx{},
			wantSQL:   "postgres",
			wantStamp: time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.db.SQL("sqlite", "postgres"); got != tt.wantSQL {
				t.Fatalf("SQL() = %q, want %q", got, tt.wantSQL)
			}
			at := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
			if got := tt.db.Stamp(at); got != tt.wantStamp {
				t.Fatalf("Stamp() = %#v, want %#v", got, tt.wantStamp)
			}
		})
	}
}
