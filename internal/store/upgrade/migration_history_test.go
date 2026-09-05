package upgrade

import (
	"embed"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"testing"
)

//go:embed testdata/history-*/*.sql
var historyMigrations embed.FS

func prepareHistoryRoute(t *testing.T, s *Session) State {
	t.Helper()
	source, err := releaseidentity.BuildMigrationManifest(historyMigrations, "testdata/history-source", s.engine)
	if err != nil {
		t.Fatal(err)
	}
	target, err := releaseidentity.BuildMigrationManifest(historyMigrations, "testdata/history-target", s.engine)
	if err != nil {
		t.Fatal(err)
	}
	empty := emptyManifest(s.engine)
	first := operation(Source{Genesis: FreshGenesis}, empty)
	first.TargetMigrationDigest, _ = source.Digest()
	state, err := s.Bootstrap(t.Context(), empty, first, Production)
	if err != nil {
		t.Fatal(err)
	}
	state, err = s.Advance(t.Context(), state, SchemaWriteStarted)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyMigrations(t.Context(), state, historyMigrations, "testdata/history-source"); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []Phase{SchemaApplied, Healthy} {
		state, err = s.Advance(t.Context(), state, phase)
		if err != nil {
			t.Fatal(err)
		}
	}
	next := nextOperation(state)
	next.TargetMigrationDigest, _ = target.Digest()
	state, err = s.Prepare(t.Context(), state, next)
	if err != nil {
		t.Fatal(err)
	}
	state, err = s.Advance(t.Context(), state, SchemaWriteStarted)
	if err != nil {
		t.Fatal(err)
	}
	return state
}
func TestMigrationResumeRejectsCorruptHistoryBeforeTargetEffects(t *testing.T) {
	for _, test := range []struct{ name, mutation string }{
		{"unknown", `INSERT INTO goose_db_version(version_id,is_applied) VALUES(55,true)`},
		{"duplicate", `INSERT INTO goose_db_version(version_id,is_applied) VALUES(1,true)`},
		{"down", `INSERT INTO goose_db_version(version_id,is_applied) VALUES(2,false)`},
		{"missing source", `DELETE FROM goose_db_version WHERE version_id=1`},
		{"missing bookkeeping", `DELETE FROM goose_db_version WHERE version_id=0`},
		{"missing history", `DROP TABLE goose_db_version`},
	} {
		t.Run(test.name, func(t *testing.T) {
			both(t, func(t *testing.T, cfg Config) {
				err := WithLock(t.Context(), cfg, func(s *Session) error {
					state := prepareHistoryRoute(t, s)
					if _, err := s.conn.ExecContext(t.Context(), test.mutation); err != nil {
						return err
					}
					if err := s.ApplyMigrations(t.Context(), state, historyMigrations, "testdata/history-target"); err == nil {
						t.Fatal("corrupt applied history resumed")
					}
					var effects int
					if err := s.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM migration_history_probe WHERE value='target-effect'`).Scan(&effects); err != nil {
						return err
					}
					if effects != 0 {
						t.Fatal("history refusal occurred after target SQL effects")
					}
					actual, err := s.Read(t.Context())
					if err != nil {
						return err
					}
					if !equalRecord(actual, state) {
						t.Fatal("history refusal changed durable pending state")
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}
func TestMigrationResumeAcceptsCompleteTargetWithoutReplayingEffects(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			state := prepareHistoryRoute(t, s)
			for range 2 {
				if err := s.ApplyMigrations(t.Context(), state, historyMigrations, "testdata/history-target"); err != nil {
					return err
				}
			}
			var effects int
			if err := s.conn.QueryRowContext(t.Context(), `SELECT count(*) FROM migration_history_probe WHERE value='target-effect'`).Scan(&effects); err != nil {
				return err
			}
			if effects != 1 {
				t.Fatalf("complete target migration replayed: %d", effects)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}
