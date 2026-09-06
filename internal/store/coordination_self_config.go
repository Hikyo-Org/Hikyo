package store

import (
	"context"
	"errors"
	"time"
)

// SelfConfigGeneration is nonsecret runtime admission metadata. Consumers can
// fence stale bundles without minting a runtime payload proof on an HTTP stack.
type SelfConfigGeneration struct {
	OwnerInstanceID, Incarnation            string
	Generation                              int64
	Suspended, Managed, DeploymentRestoring bool
}

func (c *Coordination) CurrentSelfConfigGeneration(ctx context.Context) (SelfConfigGeneration, error) {
	var out SelfConfigGeneration
	err := c.transaction(ctx, true, func(q *coordinationTx) error {
		const prefix = `SELECT b.owner_instance_id,b.generation,b.incarnation,b.suspended,EXISTS(SELECT 1 FROM self_config_jobs j JOIN self_config_rollouts r ON r.job_id=j.id WHERE j.generation=b.generation AND r.incarnation=b.incarnation AND `
		const suffix = `) FROM self_config_binding b WHERE b.id=1`
		var err error
		if q.db.engine == EnginePostgres {
			err = q.db.pool.QueryRow(ctx, prefix+`r.command_json::jsonb->'command'->>'action'='restore'`+suffix).Scan(&out.OwnerInstanceID, &out.Generation, &out.Incarnation, &out.Suspended, &out.DeploymentRestoring)
		} else {
			err = q.db.sqRead.QueryRowContext(ctx, prefix+`json_extract(r.command_json,'$.command.action')='restore'`+suffix).Scan(&out.OwnerInstanceID, &out.Generation, &out.Incarnation, &out.Suspended, &out.DeploymentRestoring)
		}
		if isNoRows(err) {
			return nil
		}
		if err != nil {
			return err
		}
		out.Managed = true
		return nil
	})
	return out, err
}

func (c *Coordination) SelfConfigSeedAttest(ctx context.Context, nodeID string, schemaVersion int64, fingerprint string, now time.Time) error {
	if nodeID == "" || schemaVersion < 1 || fingerprint == "" {
		return errors.New("store: invalid self-configuration seed attestation")
	}
	return c.transaction(ctx, false, func(q *coordinationTx) error {
		var err error
		if q.db.engine == EnginePostgres {
			_, err = q.db.pool.Exec(ctx, `INSERT INTO self_config_seed_attestations(node_id,schema_version,fingerprint,heartbeat_at) VALUES($1,$2,$3,$4) ON CONFLICT(node_id) DO UPDATE SET schema_version=excluded.schema_version,fingerprint=excluded.fingerprint,heartbeat_at=excluded.heartbeat_at`, nodeID, schemaVersion, fingerprint, now)
		} else {
			_, err = q.db.sqWrite.ExecContext(ctx, `INSERT INTO self_config_seed_attestations(node_id,schema_version,fingerprint,heartbeat_at) VALUES(?,?,?,?) ON CONFLICT(node_id) DO UPDATE SET schema_version=excluded.schema_version,fingerprint=excluded.fingerprint,heartbeat_at=excluded.heartbeat_at`, nodeID, schemaVersion, fingerprint, fixedStamp(now))
		}
		return err
	})
}

var ErrSelfConfigSeedDisagreement = errors.New("store: admitted replicas have missing, stale or different self-configuration seeds")
