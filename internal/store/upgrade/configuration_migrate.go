package upgrade

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/google/uuid"
)

// MigrateConfiguration is the only managed-data writer admitted before runtime.
// It is restricted to the exact schema-applied candidate, existing owner and
// existing project keys. Source snapshots are immutable; all writes, the binding
// CAS, migration version and audit event commit together under migration exclusion.
func (s *Session) MigrateConfiguration(ctx context.Context, expected State, root []byte, check func(context.Context, *CandidateConfiguration, map[string]string) error) error {
	defer crypto.Zero(root)
	if check == nil {
		return nil
	}
	keys, err := s.CandidateKeys(ctx, expected)
	if err != nil {
		return err
	}
	return s.transaction(ctx, func() error {
		if err := keys.check(ctx); err != nil {
			return err
		}
		projection, err := keys.Configuration(ctx)
		if err != nil || projection == nil {
			return err
		}
		var version, generation, revision int64
		var snapshot, environment, incarnation string
		var suspended bool
		if err := s.conn.QueryRowContext(ctx, "SELECT migration_version,generation,desired_revision,desired_snapshot_id,environment_id,incarnation,suspended FROM self_config_binding WHERE id=1 AND owner_instance_id=$1 AND org_id=$2 AND project_id=$3", expected.InstanceID, projection.OrgID, projection.ProjectID).Scan(&version, &generation, &revision, &snapshot, &environment, &incarnation, &suspended); err != nil {
			return err
		}
		expectedIncarnation, err := expected.RecoveryIncarnation.MarshalText()
		if err != nil {
			return err
		}
		if suspended || incarnation != string(expectedIncarnation) {
			return errors.New("managed configuration recovery is unfinished; recover and Apply it before upgrading")
		}
		if version == runtimeconfig.MigrationVersion {
			return nil
		}
		values, err := crypto.OpenExistingProjectFields(ctx, keys, bytes.Clone(root), projection.OrgID, projection.ProjectID, projection.Fields)
		if err != nil {
			return err
		}
		defer clear(values)
		originalCount := len(values)
		additions, err := planConfigurationMigrations(projection, values, version)
		if err != nil {
			return err
		}
		// Validate the complete effective candidate before persisting even one key.
		if err := check(ctx, projection, values); err != nil {
			return err
		}
		if len(additions) == 0 && len(values) == originalCount {
			return s.configurationVersionCAS(ctx, expected, version, generation, snapshot)
		}
		if generation == math.MaxInt64 || revision == math.MaxInt64 {
			return ErrConflict
		}
		var latest int64
		if err := s.conn.QueryRowContext(ctx, "SELECT max(revision) FROM snapshots WHERE org_id=$1 AND project_id=$2 AND environment_id=$3", projection.OrgID, projection.ProjectID, environment).Scan(&latest); err != nil {
			return err
		}
		// A pending published configuration must never be implicitly activated or
		// replaced by a revision cloned from the older desired configuration.
		if latest != revision {
			return errors.New("managed configuration has a published revision awaiting Apply; apply or discard it on the source release before upgrading")
		}
		var jobs int
		if err := s.conn.QueryRowContext(ctx, "SELECT count(*) FROM self_config_jobs WHERE status IN ('preparing','applying','partial')").Scan(&jobs); err != nil {
			return err
		}
		if jobs != 0 {
			return errors.New("managed configuration Apply is unfinished; complete it on the source release before upgrading")
		}
		at := time.Now().UTC()
		stamp := at.Format("2006-01-02T15:04:05.000000Z")
		scope := []any{projection.OrgID, projection.ProjectID, environment}
		ids := make(map[string]string)
		for _, field := range projection.Fields {
			ids[field.Name] = field.AAD.KeyID
		}
		for _, key := range projection.Catalogue {
			if ids[key.Name] != "" {
				continue
			}
			var id string
			err := s.conn.QueryRowContext(ctx, "SELECT id FROM keys WHERE org_id=$1 AND project_id=$2 AND name=$3", projection.OrgID, projection.ProjectID, key.Name).Scan(&id)
			if err == nil {
				ids[key.Name] = id
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		for _, key := range additions {
			id, err := configurationID("key")
			if err != nil {
				return err
			}
			ids[key.Name] = id
			compiled, err := schema.CompileClassified(key.Classification, key.Declaration)
			if err != nil {
				return err
			}
			declaration, err := compiled.Canonical()
			if err != nil {
				return err
			}
			_, err = s.conn.ExecContext(ctx, "INSERT INTO keys(id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,group_id,created_at) VALUES($1,$2,$3,$4,'',$5,$6,FALSE,'',$7,'none','none',NULL,$8)", id, projection.OrgID, projection.ProjectID, key.Name, string(key.Classification), key.Description, string(declaration), stamp)
			if err != nil {
				return err
			}
		}
		if len(additions) > 0 {
			result, err := s.conn.ExecContext(ctx, "UPDATE project_schema_revisions SET revision=revision+1 WHERE org_id=$1 AND project_id=$2", scope[:2]...)
			if err := configurationOneRow(result, err); err != nil {
				return err
			}
		}
		var schemaRevision int64
		if err := s.conn.QueryRowContext(ctx, "SELECT revision FROM project_schema_revisions WHERE org_id=$1 AND project_id=$2", scope[:2]...).Scan(&schemaRevision); err != nil {
			return err
		}
		nextSnapshot, err := configurationID("snp")
		if err != nil {
			return err
		}
		nextRevision := revision + 1
		_, err = s.conn.ExecContext(ctx, "INSERT INTO snapshots(id,org_id,project_id,environment_id,revision,schema_revision,published_by,published_at,parameter_contract) SELECT $1,org_id,project_id,environment_id,$2,$3,'', $4,parameter_contract FROM snapshots WHERE org_id=$5 AND project_id=$6 AND environment_id=$7 AND id=$8", nextSnapshot, nextRevision, schemaRevision, stamp, projection.OrgID, projection.ProjectID, environment, snapshot)
		if err != nil {
			return err
		}
		// Ciphertext is never copied between AAD owners. Existing live value rows and
		// historical payloads retain their original bytes; the new snapshot is sealed.
		err = crypto.WithExistingProjectSealer(ctx, keys, bytes.Clone(root), projection.OrgID, projection.ProjectID, func(sealer *crypto.ProjectSealer) error {
			for _, key := range projection.Catalogue {
				value, present := values[key.Name]
				if !present {
					continue
				}
				var valueID string
				err := s.conn.QueryRowContext(ctx, "SELECT value_entry_id FROM snapshot_entries WHERE org_id=$1 AND project_id=$2 AND environment_id=$3 AND snapshot_id=$4 AND key_id=$5", projection.OrgID, projection.ProjectID, environment, snapshot, ids[key.Name]).Scan(&valueID)
				if errors.Is(err, sql.ErrNoRows) {
					// An existing live value is an operator choice. Never replace it to fill
					// a missing desired snapshot value.
					var liveID string
					err = s.conn.QueryRowContext(ctx, "SELECT id FROM value_entries WHERE org_id=$1 AND project_id=$2 AND environment_id=$3 AND key_id=$4", projection.OrgID, projection.ProjectID, environment, ids[key.Name]).Scan(&liveID)
					if err == nil {
						return fmt.Errorf("managed configuration %s has a newer stored value; publish and Apply it before upgrading", key.Name)
					}
					if !errors.Is(err, sql.ErrNoRows) {
						return err
					}
					valueID, err = configurationID("val")
					if err != nil {
						return err
					}
					ciphertext, err := sealer.SealValue(crypto.ValueAAD{OrgID: projection.OrgID, ProjectID: projection.ProjectID, EnvID: environment, KeyID: ids[key.Name], RowID: valueID, FieldTag: "value"}, []byte(value))
					if err != nil {
						return err
					}
					_, err = s.conn.ExecContext(ctx, "INSERT INTO value_entries(id,org_id,project_id,environment_id,key_id,ciphertext,updated_at,updated_by) VALUES($1,$2,$3,$4,$5,$6,$7,'')", valueID, projection.OrgID, projection.ProjectID, environment, ids[key.Name], ciphertext, stamp)
					if err != nil {
						return err
					}
					_, err = s.conn.ExecContext(ctx, "INSERT INTO revision_key_changes(org_id,project_id,environment_id,revision,key_id,key_name,change) VALUES($1,$2,$3,$4,$5,$6,'added')", projection.OrgID, projection.ProjectID, environment, nextRevision, ids[key.Name], key.Name)
					if err != nil {
						return err
					}
				} else if err != nil {
					return err
				}
				entryID, err := configurationID("sne")
				if err != nil {
					return err
				}
				ciphertext, err := sealer.SealField(crypto.ProjectFieldAAD{OrgID: projection.OrgID, ProjectID: projection.ProjectID, EnvironmentID: environment, SnapshotID: nextSnapshot, KeyID: ids[key.Name], OwnerTable: "snapshot_entries", OwnerRowID: entryID, FieldTag: "snapshot_value"}, []byte(value))
				if err != nil {
					return err
				}
				_, err = s.conn.ExecContext(ctx, "INSERT INTO snapshot_entries(id,org_id,project_id,environment_id,snapshot_id,key_id,key_name,classification,ciphertext,value_entry_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", entryID, projection.OrgID, projection.ProjectID, environment, nextSnapshot, ids[key.Name], key.Name, key.Classification, ciphertext, valueID)
				if err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		result, err := s.conn.ExecContext(ctx, "UPDATE self_config_binding SET previous_snapshot_id=desired_snapshot_id,desired_snapshot_id=$1,desired_revision=$2,generation=generation+1,migration_version=$3,updated_at=$4 WHERE id=1 AND owner_instance_id=$5 AND org_id=$6 AND project_id=$7 AND environment_id=$8 AND migration_version=$9 AND generation=$10 AND desired_snapshot_id=$11", nextSnapshot, nextRevision, runtimeconfig.MigrationVersion, stamp, expected.InstanceID, projection.OrgID, projection.ProjectID, environment, version, generation, snapshot)
		if err := configurationOneRow(result, err); err != nil {
			return err
		}
		for slot, id := range map[string]string{"desired": nextSnapshot, "previous": snapshot} {
			if _, err := s.conn.ExecContext(ctx, "INSERT INTO self_config_retention(slot,snapshot_id) VALUES($1,$2) ON CONFLICT(slot) DO UPDATE SET snapshot_id=excluded.snapshot_id", slot, id); err != nil {
				return err
			}
		}
		eventID, err := audit.NewEventID()
		if err != nil {
			return err
		}
		event := audit.Event{ID: eventID, Type: audit.EventSelfConfigMigrated, SchemaVersion: 1, OccurredAt: at, Actor: audit.Actor{Class: audit.ActorSystem}, Outcome: audit.OutcomeSuccess, Origin: audit.OriginSystem, Payload: audit.Payload{"migration_version": runtimeconfig.MigrationVersion, "owner_instance_id": expected.InstanceID, "revision": nextRevision, "generation": generation + 1}}
		if err := audit.Validate(event, audit.TrailInstance, domain.Scope{}); err != nil {
			return err
		}
		payload, err := json.Marshal(event.Payload)
		if err != nil {
			return err
		}
		_, err = s.conn.ExecContext(ctx, "INSERT INTO audit_instance_events(id,type,schema_version,occurred_at,occurred_asserted,recorded_at,actor_class,outcome,origin,payload) VALUES($1,$2,1,$3,FALSE,$3,'system','success','system',$4)", event.ID, string(event.Type), stamp, string(payload))
		if err != nil {
			return err
		}
		return keys.check(ctx)
	})
}

func configurationOneRow(result sql.Result, err error) error {
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return nil
}
func (s *Session) configurationVersionCAS(ctx context.Context, expected State, version, generation int64, snapshot string) error {
	result, err := s.conn.ExecContext(ctx, "UPDATE self_config_binding SET migration_version=$1 WHERE id=1 AND owner_instance_id=$2 AND migration_version=$3 AND generation=$4 AND desired_snapshot_id=$5", runtimeconfig.MigrationVersion, expected.InstanceID, version, generation, snapshot)
	return configurationOneRow(result, err)
}

func configurationID(prefix string) (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return prefix + "_" + id.String(), nil
}
