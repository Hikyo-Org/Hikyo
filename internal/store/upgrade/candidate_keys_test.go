package upgrade

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

func TestCandidateKeyInventoryIsReadOnlyAndBoundToItsOperation(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		var retained, healthyReader *candidateKeys
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			state := prepareSessionMigration(t, s, "testdata/session-key-inventory")
			if _, err := s.CandidateKeys(t.Context(), state); !errors.Is(err, ErrConflict) {
				return fmt.Errorf("prepared candidate keys: %v", err)
			}
			var err error
			state, err = s.Advance(t.Context(), state, SchemaWriteStarted)
			if err != nil {
				return err
			}
			if err := s.ApplyMigrations(t.Context(), state, sessionMigrations, "testdata/session-key-inventory"); err != nil {
				return err
			}
			state, err = s.Advance(t.Context(), state, SchemaApplied)
			if err != nil {
				return err
			}
			if _, err := s.HealthyKeys(t.Context(), state); !errors.Is(err, ErrConflict) {
				return fmt.Errorf("maintenance got healthy reader: %v", err)
			}
			retained, err = s.CandidateKeys(t.Context(), state)
			if err != nil {
				return err
			}
			master, err := retained.ActiveMasterWrappers(t.Context())
			if err != nil || len(master) != 0 {
				return fmt.Errorf("empty inventory changed: %v %+v", err, master)
			}
			tier3, err := retained.AllOpenableTier3(t.Context())
			if err != nil || len(tier3) != 0 {
				return fmt.Errorf("empty tier-3 inventory changed: %v %+v", err, tier3)
			}
			var count int
			if err := s.conn.QueryRowContext(t.Context(), "SELECT count(*) FROM master_keys").Scan(&count); err != nil {
				return err
			}
			if count != 0 {
				return errors.New("candidate inventory initialized hierarchy")
			}
			stamp := "2026-09-05T00:00:00Z"
			if _, err := s.conn.ExecContext(t.Context(), "INSERT INTO master_keys VALUES(1,2,'active',$1,$2),(0,1,'retired',$1,$2)", []byte{1, 2}, stamp); err != nil {
				return err
			}
			if _, err := s.conn.ExecContext(t.Context(), "INSERT INTO tier3_keys VALUES('instance','instance','','',1,1,'active',$1,$2),('token','token','','',2,1,'retiring',$1,$2),('retired','token','','',1,1,'retired',$1,$2)", []byte{3, 4}, stamp); err != nil {
				return err
			}
			// A caller may mutate its input; that must not rewrite retained authority.
			state.Pending.Phase = Prepared
			master, err = retained.ActiveMasterWrappers(t.Context())
			if err != nil {
				return err
			}
			if len(master) != 1 || master[0].Version != 1 || master[0].RootKeyEpoch != 2 || string(master[0].Blob) != string([]byte{1, 2}) || master[0].CreatedAt.Format("2006-01-02T15:04:05Z07:00") != stamp {
				return errors.New("candidate master metadata differs")
			}
			tier3, err = retained.AllOpenableTier3(t.Context())
			if err != nil {
				return err
			}
			if len(tier3) != 2 || tier3[0].Purpose != crypto.PurposeInstance || tier3[1].Purpose != crypto.PurposeToken || tier3[1].Version != 2 {
				return errors.New("candidate tier-3 inventory differs")
			}
			if _, err := s.conn.ExecContext(t.Context(), "UPDATE master_keys SET version=4294967296 WHERE state='active'"); err != nil {
				return err
			}
			if _, err := healthyReader.AllOpenableTier3(t.Context()); err == nil {
				t.Fatal("closed owner retained healthy inventory authority")
			}
			if _, err := retained.ActiveMasterWrappers(t.Context()); err == nil {
				return errors.New("overflowed candidate key version accepted")
			}
			current, err := s.Read(t.Context())
			if err != nil {
				return err
			}
			current, err = s.Advance(t.Context(), current, Healthy)
			if err != nil {
				return err
			}
			healthyReader, err = s.HealthyKeys(t.Context(), current)
			if err != nil {
				return err
			}
			healthyTier3, err := healthyReader.AllOpenableTier3(t.Context())
			if err != nil || len(healthyTier3) != 2 {
				return fmt.Errorf("healthy inventory differs: %v", err)
			}
			if _, err := s.CandidateKeys(t.Context(), current); !errors.Is(err, ErrConflict) {
				return fmt.Errorf("healthy state got candidate reader: %v", err)
			}
			if _, err := retained.AllOpenableTier3(t.Context()); !errors.Is(err, ErrConflict) {
				return fmt.Errorf("completed operation retained candidate authority: %v", err)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := healthyReader.AllOpenableTier3(t.Context()); err == nil {
			t.Fatal("closed owner retained healthy inventory authority")
		}
		if _, err := retained.ActiveMasterWrappers(t.Context()); err == nil {
			t.Fatal("closed owner retained candidate authority")
		}
	})
}
