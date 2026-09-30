package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/Hikyo-Org/hikyo/internal/store/sqlitegen"
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
		var err error
		if q.db.engine == EnginePostgres {
			var row pggen.CoordinationSelfConfigGenerationRow
			row, err = pggen.New(q.db.pool).CoordinationSelfConfigGeneration(ctx)
			out.OwnerInstanceID, out.Generation, out.Incarnation, out.Suspended, out.DeploymentRestoring = row.OwnerInstanceID, row.Generation, row.Incarnation, row.Suspended, row.DeploymentRestoring
		} else {
			var row sqlitegen.CoordinationSelfConfigGenerationRow
			row, err = sqlitegen.New(q.db.sqRead).CoordinationSelfConfigGeneration(ctx)
			out.OwnerInstanceID, out.Generation, out.Incarnation, out.Suspended, out.DeploymentRestoring = row.OwnerInstanceID, row.Generation, row.Incarnation, row.Suspended != 0, row.DeploymentRestoring
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
			err = pggen.New(q.db.pool).CoordinationSelfConfigSeedAttest(ctx, pggen.CoordinationSelfConfigSeedAttestParams{NodeID: nodeID, SchemaVersion: schemaVersion, Fingerprint: fingerprint, HeartbeatAt: pgtype.Timestamptz{Time: now, Valid: true}})
		} else {
			err = sqlitegen.New(q.db.sqWrite).CoordinationSelfConfigSeedAttest(ctx, sqlitegen.CoordinationSelfConfigSeedAttestParams{NodeID: nodeID, SchemaVersion: schemaVersion, Fingerprint: fingerprint, HeartbeatAt: fixedStamp(now)})
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
	var raw string
	var err error
	if c.db.engine == EnginePostgres {
		raw, err = pggen.New(c.db.pool).CoordinationCurrentTopology(ctx)
	} else {
		raw, err = sqlitegen.New(c.db.sqRead).CoordinationCurrentTopology(ctx)
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
	if c.db.engine == EnginePostgres {
		var row pggen.CoordinationLatestRolloutRow
		row, err = pggen.New(c.db.pool).CoordinationLatestRollout(ctx)
		latestCommand, latestResponse, currentGeneration = row.CommandJson, row.ResponseJson, row.CurrentGeneration
	} else {
		var row sqlitegen.CoordinationLatestRolloutRow
		row, err = sqlitegen.New(c.db.sqRead).CoordinationLatestRollout(ctx)
		latestCommand, latestResponse, currentGeneration = row.CommandJson, row.ResponseJson, row.CurrentGeneration
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
		if err := pggen.New(c.db.pool).CoordinationLockTopology(ctx); err != nil {
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
