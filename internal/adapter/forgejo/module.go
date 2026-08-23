package forgejo

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

type Module struct {
	API API
}

var _ adapter.Module = (*Module)(nil)

func (m *Module) ValidateConfig(cfg adapter.Config) error {
	_, err := canonicalOrigin(cfg.Origin)
	return err
}

func (m *Module) TestConnection(ctx context.Context, req adapter.ConnectionRequest) (adapter.Connection, error) {
	if m.API == nil {
		return adapter.Connection{}, errors.New("forgejo: API is not configured")
	}
	if req.Gate == nil {
		return adapter.Connection{}, adapter.ErrUnauthorized
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	version, err := m.API.Version(ctx)
	if err != nil {
		return adapter.Connection{}, err
	}
	if !supportedVersion(version) {
		return adapter.Connection{}, fmt.Errorf("%w: this Forgejo lacks the variables API (%s)", adapter.ErrVersionFloor, version)
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	id, err := m.API.ResolveDestination(ctx, req.Destination)
	if err != nil {
		return adapter.Connection{}, err
	}
	if req.Destination.NumericID != 0 && req.Destination.NumericID != id {
		return adapter.Connection{}, fmt.Errorf("%w: configured %d, resolved %d", adapter.ErrDestinationID, req.Destination.NumericID, id)
	}
	return adapter.Connection{Version: version, DestinationID: id}, nil
}

func supportedVersion(raw string) bool {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "v")
	parts := strings.SplitN(raw, ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, errMajor := strconv.Atoi(parts[0])
	minor, errMinor := strconv.Atoi(parts[1])
	return errMajor == nil && errMinor == nil && (major > 1 || major == 1 && minor >= 21)
}

func (m *Module) Plan(ctx context.Context, req adapter.PlanRequest) (adapter.Plan, error) {
	if err := adapter.ValidateManifest(req.Target.NamePrefix, req.Manifest); err != nil {
		return adapter.Plan{}, err
	}
	if req.Gate == nil {
		return adapter.Plan{}, adapter.ErrUnauthorized
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Plan{}, err
	}
	if err := m.verifyDestination(ctx, req.Target); err != nil {
		return adapter.Plan{}, err
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Plan{}, err
	}
	secretNames, err := m.API.ListSecretNames(ctx, req.Target.Destination)
	if err != nil {
		return adapter.Plan{}, err
	}
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.Plan{}, err
	}
	desired := adapter.DesiredRows(req.Target.NamePrefix, req.Manifest, true)
	return adapter.Plan{Changes: adapter.PlanChanges(desired, ledger, adapter.NameSet(secretNames))}, nil
}

func (m *Module) Sync(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	if journal == nil {
		return adapter.SyncResult{}, errors.New("forgejo: durable journal is required")
	}
	if err := adapter.ValidateManifest(req.Target.NamePrefix, req.Manifest); err != nil {
		return adapter.SyncResult{}, err
	}
	inspect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "*", Disposition: adapter.Update}
	if err := journal.Gate(ctx, inspect); err != nil {
		return adapter.SyncResult{}, err
	}
	if err := m.verifyDestination(ctx, req.Target); err != nil {
		return adapter.SyncResult{}, err
	}
	if err := journal.Gate(ctx, inspect); err != nil {
		return adapter.SyncResult{}, err
	}
	secretNames, err := m.API.ListSecretNames(ctx, req.Target.Destination)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	providerSecrets := adapter.NameSet(secretNames)
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	desiredRows := adapter.DesiredRows(req.Target.NamePrefix, req.Manifest, !req.Teardown)
	completed := adapter.CompletedNames(req.Completed)
	result := adapter.SyncResult{}
	for _, row := range desiredRows {
		if completed[adapter.NewLedgerKey(row.Surface, row.EffectiveName)] {
			continue
		}
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Create, KeyID: row.KeyID}
		key := adapter.NewLedgerKey(row.Surface, row.EffectiveName)
		record, claimed := ledger[key]
		state := record.State
		if claimed && (state == adapter.Owned || state == adapter.Dispatched) && !record.Missing {
			effect.Disposition = adapter.Update
		}
		if !claimed {
			state, err = journal.Reserve(ctx, effect)
			if err != nil {
				return result, err
			}
			record = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: state}
			ledger[key] = record
		}
		if state == adapter.Reserved && row.Surface == adapter.Secret && providerSecrets[row.EffectiveName] {
			if err := journal.Refuse(ctx, effect); err != nil {
				return result, err
			}
			conflict := adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Conflict}
			result.Conflicts = append(result.Conflicts, conflict)
			return result, fmt.Errorf("%w: secret %s", adapter.ErrConflict, row.EffectiveName)
		}
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		if err := m.verifyDestination(ctx, req.Target); err != nil {
			return result, err
		}
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		if err := journal.Prepare(ctx, effect, state); err != nil {
			return result, err
		}
		if gateErr := journal.Gate(ctx, effect); gateErr != nil {
			completion := adapter.Completion{Outcome: "failure", State: state}
			if record.Missing {
				completion.Missing = true
				completion.Finding = "owned_missing"
			}
			if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
				return result, finishErr
			}
			return result, gateErr
		}
		err := m.write(ctx, req.Target.Destination, row, state, record.Missing)
		absenceProven := row.Surface == adapter.Variable && !record.Missing && (state == adapter.Owned || state == adapter.Dispatched) && IsNotFound(err)
		if err != nil {
			if row.Surface == adapter.Variable && (state == adapter.Reserved || !claimed || record.Missing) && IsConflict(err) {
				completion := adapter.Completion{Outcome: "failure", Conflict: true}
				if record.Missing {
					completion.State = state
					completion.Missing = true
					completion.Finding = "owned_missing"
				}
				if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
					return result, finishErr
				}
				conflict := adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Conflict}
				result.Conflicts = append(result.Conflicts, conflict)
				return result, fmt.Errorf("%w: variable %s", adapter.ErrConflict, row.EffectiveName)
			}
			outcome := "unknown"
			var response *ResponseError
			if errors.As(err, &response) && response.Status >= 400 && response.Status < 500 {
				outcome = "failure"
			}
			finalState := adapter.Dispatched
			if outcome == "failure" {
				if state == adapter.Reserved || absenceProven {
					finalState = ""
				} else {
					finalState = state
				}
			}
			completion := adapter.Completion{Outcome: outcome, State: finalState}
			if record.Missing {
				completion.Missing = true
				completion.Finding = "owned_missing"
			}
			if completeErr := journal.Finish(ctx, effect, completion); completeErr != nil {
				return result, completeErr
			}
			result.Failed = append(result.Failed, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
			if outcome == "unknown" {
				return result, fmt.Errorf("%w: %s %s", adapter.ErrIndeterminate, row.Surface, row.EffectiveName)
			}
			return result, err
		}
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: "success", State: adapter.Owned}); err != nil {
			return result, err
		}
		ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: adapter.Owned}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
	}

	reservations, prunes := adapter.Undesired(desiredRows, ledger)
	for _, row := range reservations {
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		if err := journal.ReleaseReservation(ctx, effect); err != nil {
			return result, err
		}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete})
	}
	for _, row := range prunes {
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		if err := m.verifyDestination(ctx, req.Target); err != nil {
			return result, err
		}
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		if err := journal.Prepare(ctx, effect, row.State); err != nil {
			return result, err
		}
		if gateErr := journal.Gate(ctx, effect); gateErr != nil {
			if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: "failure", State: row.State}); finishErr != nil {
				return result, finishErr
			}
			return result, gateErr
		}
		err := m.delete(ctx, req.Target.Destination, row.Surface, row.EffectiveName)
		if err != nil && !IsNotFound(err) {
			outcome := "unknown"
			var response *ResponseError
			if errors.As(err, &response) && response.Status >= 400 && response.Status < 500 {
				outcome = "failure"
			}
			if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: outcome, State: row.State}); finishErr != nil {
				return result, finishErr
			}
			result.Failed = append(result.Failed, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete})
			return result, err
		}
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: "success", State: adapter.Released}); err != nil {
			return result, err
		}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete})
	}
	return result, nil
}

func (m *Module) verifyDestination(ctx context.Context, target adapter.Target) error {
	id, err := m.API.ResolveDestination(ctx, target.Destination)
	if err != nil {
		return err
	}
	if id != target.Destination.NumericID {
		return fmt.Errorf("%w: configured %d, resolved %d", adapter.ErrDestinationID, target.Destination.NumericID, id)
	}
	return nil
}

func (m *Module) write(ctx context.Context, destination adapter.Destination, row adapter.DesiredRow, prior adapter.LedgerState, forceCreate bool) error {
	if row.Surface == adapter.Secret {
		return m.API.PutSecret(ctx, destination, row.EffectiveName, row.Value)
	}
	if !forceCreate && (prior == adapter.Dispatched || prior == adapter.Owned) {
		return m.API.UpdateVariable(ctx, destination, row.EffectiveName, row.Value)
	}
	return m.API.CreateVariable(ctx, destination, row.EffectiveName, row.Value)
}

func (m *Module) delete(ctx context.Context, destination adapter.Destination, surface adapter.Surface, name string) error {
	if surface == adapter.Secret {
		return m.API.DeleteSecret(ctx, destination, name)
	}
	return m.API.DeleteVariable(ctx, destination, name)
}
