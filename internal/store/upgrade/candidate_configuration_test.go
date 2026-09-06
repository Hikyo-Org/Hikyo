package upgrade

import (
	"errors"
	"fmt"
	"testing"
)

func TestCandidateConfigurationProjectionBindsOwnerScopeAndSession(t *testing.T) {
	both(t, func(t *testing.T, cfg Config) {
		var retained *candidateKeys
		err := WithLock(t.Context(), cfg, func(s *Session) error {
			state := prepareSessionMigration(t, s, "testdata/session-key-inventory")
			state, err := s.Advance(t.Context(), state, SchemaWriteStarted)
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
			retained, err = s.CandidateKeys(t.Context(), state)
			if err != nil {
				return err
			}
			if got, err := retained.Configuration(t.Context()); err != nil || got != nil {
				return fmt.Errorf("pre-feature projection: %v", err)
			}
			for _, query := range []string{
				"CREATE TABLE self_config_binding(id INTEGER,owner_instance_id TEXT,org_id TEXT,project_id TEXT,environment_id TEXT,desired_snapshot_id TEXT,desired_revision INTEGER,schema_version INTEGER,generation INTEGER)",
				"CREATE TABLE snapshots(id TEXT,org_id TEXT,project_id TEXT,environment_id TEXT,revision INTEGER,payload_present BOOLEAN)",
				"CREATE TABLE keys(id TEXT,org_id TEXT,project_id TEXT,name TEXT,classification TEXT,declaration TEXT,required_mode TEXT,forbidden_mode TEXT,group_id TEXT,folder_path TEXT)",
				"CREATE TABLE snapshot_entries(id TEXT,org_id TEXT,project_id TEXT,environment_id TEXT,snapshot_id TEXT,key_id TEXT,key_name TEXT,classification TEXT,ciphertext BYTEA)",
				"INSERT INTO snapshots VALUES('snapshot','org','project','env',1,TRUE),('other','remote','remote','env',1,TRUE)",
				"INSERT INTO keys VALUES('key','org','project','SETTING','secret','{}','none','none',NULL,'')",
			} {
				if _, err := s.conn.ExecContext(t.Context(), query); err != nil {
					return err
				}
			}
			if _, err := s.conn.ExecContext(t.Context(), "INSERT INTO self_config_binding VALUES(1,$1,'org','project','env','snapshot',1,1,1)", state.InstanceID); err != nil {
				return err
			}
			if _, err := s.conn.ExecContext(t.Context(), "INSERT INTO snapshot_entries VALUES('entry','org','project','env','snapshot','key','SETTING','secret',$1),('remote-entry','remote','remote','env','other','key','SETTING','secret',$1)", []byte("sealed")); err != nil {
				return err
			}
			projection, err := retained.Configuration(t.Context())
			if err != nil {
				return err
			}
			if len(projection.Fields) != 1 || projection.Fields[0].AAD.OwnerRowID != "entry" || len(projection.Catalogue) != 1 {
				return errors.New("projection escaped fixed desired scope")
			}
			for _, mutation := range []struct{ change, restore string }{
				{"UPDATE self_config_binding SET owner_instance_id='remote'", "UPDATE self_config_binding SET owner_instance_id=$1"},
				{"UPDATE self_config_binding SET desired_snapshot_id='other'", "UPDATE self_config_binding SET desired_snapshot_id='snapshot'"},
				{"UPDATE snapshots SET payload_present=FALSE WHERE id='snapshot'", "UPDATE snapshots SET payload_present=TRUE WHERE id='snapshot'"},
				{"UPDATE snapshot_entries SET classification='config' WHERE id='entry'", "UPDATE snapshot_entries SET classification='secret' WHERE id='entry'"},
			} {
				if _, err := s.conn.ExecContext(t.Context(), mutation.change); err != nil {
					return err
				}
				if _, err := retained.Configuration(t.Context()); !errors.Is(err, ErrConflict) {
					return fmt.Errorf("invalid projection accepted: %v", err)
				}
				var err error
				if mutation.restore == "UPDATE self_config_binding SET owner_instance_id=$1" {
					_, err = s.conn.ExecContext(t.Context(), mutation.restore, state.InstanceID)
				} else {
					_, err = s.conn.ExecContext(t.Context(), mutation.restore)
				}
				if err != nil {
					return err
				}
			}
			if _, err := s.Advance(t.Context(), state, Healthy); err != nil {
				return err
			}
			if _, err := retained.Configuration(t.Context()); !errors.Is(err, ErrConflict) {
				return errors.New("phase change retained configuration authority")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := retained.Configuration(t.Context()); err == nil {
			t.Fatal("closed session retained configuration authority")
		}
	})
}
