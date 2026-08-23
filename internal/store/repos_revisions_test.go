package store

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
)

func TestRevisionSnapshotMappersEnforceCollectionTriple(t *testing.T) {
	stamp := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	policy := "keep-if-either(max_age=720h0m0s,last_revisions=2)"

	tests := []struct {
		name           string
		payloadPresent bool
		collectedAt    bool
		policy         string
		wantCollected  bool
		wantErr        bool
	}{
		{name: "live", payloadPresent: true},
		{name: "collected", collectedAt: true, policy: policy, wantCollected: true},
		{name: "present with collection stamp", payloadPresent: true, collectedAt: true, policy: policy, wantErr: true},
		{name: "present with collection policy", payloadPresent: true, policy: policy, wantErr: true},
		{name: "collected without stamp", policy: policy, wantErr: true},
		{name: "collected without policy", collectedAt: true, wantErr: true},
	}

	for _, engine := range []struct {
		name   string
		mapRow func(bool, bool, string) (Snapshot, error)
	}{
		{
			name: "sqlite",
			mapRow: func(payloadPresent, collectedAt bool, policy string) (Snapshot, error) {
				row := sqlitegen.Snapshot{
					ID: "snp_mapper", PublishedAt: stamp.Format(timeFormat),
					PayloadPresent: boolInt(payloadPresent), CollectedPolicy: policy,
				}
				if collectedAt {
					row.CollectedAt = sql.NullString{String: stamp.Format(timeFormat), Valid: true}
				}
				return revisionSnapshotFromSQLite(row)
			},
		},
		{
			name: "postgres",
			mapRow: func(payloadPresent, collectedAt bool, policy string) (Snapshot, error) {
				row := pggen.Snapshot{
					ID:             "snp_mapper",
					PublishedAt:    pgtype.Timestamptz{Time: stamp, Valid: true},
					PayloadPresent: payloadPresent, CollectedPolicy: policy,
				}
				if collectedAt {
					row.CollectedAt = pgtype.Timestamptz{Time: stamp, Valid: true}
				}
				return revisionSnapshotFromPG(row)
			},
		},
	} {
		t.Run(engine.name, func(t *testing.T) {
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					got, err := engine.mapRow(test.payloadPresent, test.collectedAt, test.policy)
					if test.wantErr {
						if err == nil || !strings.Contains(err.Error(), "snp_mapper") {
							t.Fatalf("mapper error = %v, want snapshot-specific collection-triple refusal", err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if (got.Collected != nil) != test.wantCollected {
						t.Fatalf("collected = %+v, want present %t", got.Collected, test.wantCollected)
					}
					if got.PayloadPresent() == test.wantCollected || got.CollectionPolicy() != test.policy {
						t.Fatalf("collection behavior = present %t policy %q, want present %t policy %q",
							got.PayloadPresent(), got.CollectionPolicy(), !test.wantCollected, test.policy)
					}
					if test.wantCollected && (got.Collected.At != stamp || got.Collected.Policy != policy) {
						t.Fatalf("collection = %+v, want at %s with policy %q", got.Collected, stamp, policy)
					}
				})
			}
		})
	}
}
