package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
)

// SelfConfigGeneration is nonsecret runtime admission metadata. Consumers can
// fence stale bundles without minting a runtime payload proof on an HTTP stack.
type SelfConfigGeneration struct {
	TopologyStamp                           string
	OwnerInstanceID, Incarnation            string
	Generation                              int64
	Topology                                *domain.SingletonTopologyChange
	TopologyRestoring                       bool
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
		assignment, err := q.currentTopology(ctx)
		if err != nil {
			return err
		}
		if assignment != nil {
			out.Topology = assignment.Change
			out.TopologyStamp = assignment.Stamp
			out.TopologyRestoring = assignment.Action == "restore" && assignment.Change.Before != assignment.Change.After
			out.DeploymentRestoring = out.DeploymentRestoring || assignment.Restoring
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

// currentTopology selects the last committed correspondence and latest
// installed template independently. Ordinary applies retain both; a source
// rollout changes the template without discarding membership history.
type topologyAssignment struct {
	Change        *domain.SingletonTopologyChange
	Action, Stamp string
	Restoring     bool
}

func (c *coordinationTx) currentTopology(ctx context.Context) (*topologyAssignment, error) {
	const prefix = `SELECT r.command_json FROM self_config_rollouts r JOIN self_config_jobs j ON j.id=r.job_id JOIN self_config_binding b ON b.id=1 AND b.incarnation=r.incarnation WHERE j.generation<=b.generation AND j.status NOT IN ('preparing','aborted') AND `
	const suffix = ` ORDER BY j.generation DESC LIMIT 1`
	var raw string
	var err error
	if c.db.engine == EnginePostgres {
		err = c.db.pool.QueryRow(ctx, prefix+`jsonb_typeof(r.command_json::jsonb->'command'->'topology')='object'`+suffix).Scan(&raw)
	} else {
		err = c.db.sqRead.QueryRowContext(ctx, prefix+`json_type(r.command_json,'$.command.topology')='object'`+suffix).Scan(&raw)
	}
	if isNoRows(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	change, action, err := rolloutTopology(raw)
	if err != nil || change == nil {
		return nil, domain.ErrConflict
	}
	var latestCommand, latestResponse string
	var currentGeneration bool
	const latest = `SELECT r.command_json,r.response_json,j.generation=b.generation FROM self_config_rollouts r JOIN self_config_jobs j ON j.id=r.job_id JOIN self_config_binding b ON b.id=1 AND b.incarnation=r.incarnation WHERE j.generation<=b.generation AND j.status NOT IN ('preparing','aborted') ORDER BY j.generation DESC LIMIT 1`
	if c.db.engine == EnginePostgres {
		err = c.db.pool.QueryRow(ctx, latest).Scan(&latestCommand, &latestResponse, &currentGeneration)
	} else {
		err = c.db.sqRead.QueryRowContext(ctx, latest).Scan(&latestCommand, &latestResponse, &currentGeneration)
	}
	if err != nil {
		return nil, err
	}
	var command struct {
		Command struct {
			Action                string `json:"action"`
			PreviousTemplateStamp string `json:"previous_template_stamp"`
		} `json:"command"`
	}
	var response struct {
		TemplateStamp string `json:"template_stamp"`
	}
	if json.Unmarshal([]byte(latestCommand), &command) != nil || json.Unmarshal([]byte(latestResponse), &response) != nil {
		return nil, domain.ErrConflict
	}
	stamp := response.TemplateStamp
	if command.Command.Action == "restore" {
		stamp = command.Command.PreviousTemplateStamp
	}
	return &topologyAssignment{Change: change, Action: action, Stamp: stamp, Restoring: (action == "restore" && change.Before != change.After) || (currentGeneration && command.Command.Action == "restore")}, nil
}

// topologyLeaseAllowed shares the membership lock with final Apply. It cannot
// grant an obsolete process a heartbeat or HA lease after the cutover.
func (c *coordinationTx) topologyLeaseAllowed(ctx context.Context, nodeID string) error {
	if c.db.engine == EnginePostgres {
		if _, err := c.db.pool.Exec(ctx, `SELECT pg_advisory_xact_lock(1464159830,87)`); err != nil {
			return err
		}
	}
	assignment, err := c.currentTopology(ctx)
	if err != nil {
		return err
	}
	if assignment != nil && (assignment.Restoring || !assignment.Change.After.HA || assignment.Change.After.NodeID != nodeID || c.db.runtimeNodeID != nodeID || c.db.runtimeTemplateStamp != assignment.Stamp) {
		return domain.ErrConflict
	}
	return nil
}
