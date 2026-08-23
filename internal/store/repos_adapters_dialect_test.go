package store

import "testing"

func TestAdoptDBOwnsDialectPlaceholders(t *testing.T) {
	tests := []struct {
		name string
		db   adoptDB
		want string
	}{
		{name: "sqlite", db: sqliteAdoptDB{}, want: "?,?,?"},
		{name: "postgres", db: pgAdoptDB{}, want: "$2,$3,$4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.db.Placeholders(3, 2); got != tt.want {
				t.Fatalf("Placeholders(3, 2) = %q, want %q", got, tt.want)
			}
		})
	}
}
