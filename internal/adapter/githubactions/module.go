package githubactions

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
)

type Sealer func([]byte, PublicKey) (string, error)

type Module struct {
	API  API
	Seal Sealer
}

var _ adapter.Module = (*Module)(nil)

func SealSecret(value []byte, key PublicKey) (string, error) {
	sealed, err := crypto.SealAnonymousBox(value, key.Key)
	if err != nil {
		return "", fmt.Errorf("github-actions: seal secret: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (m *Module) sealer() Sealer {
	if m.Seal != nil {
		return m.Seal
	}
	return SealSecret
}

func (m *Module) ValidateConfig(cfg adapter.Config) error {
	_, err := canonicalOrigin(cfg.Origin)
	return err
}

func (m *Module) TestConnection(ctx context.Context, req adapter.ConnectionRequest) (adapter.Connection, error) {
	if m.API == nil {
		return adapter.Connection{}, errors.New("github-actions: API is not configured")
	}
	if err := validateCredential(req.Access.Credential); err != nil {
		return adapter.Connection{}, err
	}
	if err := validateDestination(req.Destination); err != nil {
		return adapter.Connection{}, err
	}
	if req.Gate == nil {
		return adapter.Connection{}, adapter.ErrUnauthorized
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	identity, err := m.API.ResolveDestination(ctx, req.Destination)
	if err != nil && req.Destination.Kind == adapter.Environment && req.AllowEnvironmentCreate && IsStatus(err, http.StatusNotFound) {
		if err := req.Gate(ctx); err != nil {
			return adapter.Connection{}, err
		}
		if req.BeforeEnvironmentCreate != nil {
			if err := req.BeforeEnvironmentCreate(ctx); err != nil {
				return adapter.Connection{}, err
			}
		}
		createErr := m.API.CreateEnvironment(ctx, req.Destination)
		if req.AfterEnvironmentCreate != nil {
			if auditErr := req.AfterEnvironmentCreate(ctx, createErr); auditErr != nil {
				return adapter.Connection{}, auditErr
			}
		}
		if err := createErr; err != nil {
			if IsStatus(err, http.StatusForbidden) {
				return adapter.Connection{}, fmt.Errorf("github-actions: environment creation requires Administration write permission; grant it for pre-create then narrow the token, or widen the token for Hikyo-managed creation: %w", err)
			}
			return adapter.Connection{}, destinationCapabilityError(req.Destination, err)
		}
		identity, err = m.API.ResolveDestination(ctx, req.Destination)
	}
	if err != nil {
		return adapter.Connection{}, destinationCapabilityError(req.Destination, err)
	}
	if req.Destination.NumericID != 0 && req.Destination.NumericID != identity.ID {
		return adapter.Connection{}, fmt.Errorf("%w: configured %d, resolved %d", adapter.ErrDestinationID, req.Destination.NumericID, identity.ID)
	}
	if err := m.API.VerifySelectedRepositories(ctx, req.Destination); err != nil {
		return adapter.Connection{}, destinationCapabilityError(req.Destination, err)
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	if _, err := m.API.ListSecretNames(ctx, req.Destination); err != nil {
		return adapter.Connection{}, destinationCapabilityError(req.Destination, err)
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	if _, err := m.API.PublicKey(ctx, req.Destination); err != nil {
		return adapter.Connection{}, destinationCapabilityError(req.Destination, err)
	}
	connection := adapter.Connection{Version: "github-actions", DestinationID: identity.ID, RepositoryID: identity.RepositoryID}
	if source, ok := m.API.(interface{ CredentialExpiresAt() time.Time }); ok {
		connection.CredentialExpiresAt = source.CredentialExpiresAt()
	}
	return connection, nil
}

func destinationCapabilityError(destination adapter.Destination, err error) error {
	if errors.Is(err, adapter.ErrDestinationID) {
		return fmt.Errorf("github-actions: destination identity changed; re-configure the target: %w", err)
	}
	switch destination.Kind {
	case adapter.Repository:
		return fmt.Errorf("github-actions: repository destination requires fine-grained repository Secrets: write and Variables: write permissions: %w", err)
	case adapter.Organization:
		return fmt.Errorf("github-actions: organization destination requires fine-grained organization Secrets: write and Variables: write permissions: %w", err)
	case adapter.Environment:
		return fmt.Errorf("github-actions: environment %q unavailable or unauthorized; requires fine-grained repository Environments: write and Actions: read permissions, subject to plan availability; or explicitly reconfigure to repository scope with a prefix: %w", destination.Environment, err)
	default:
		return err
	}
}

func validateManifest(prefix string, entries []adapter.ManifestEntry, values bool) error {
	return adapter.ValidateGitHubActionsManifest(prefix, entries, values)
}

func validateDestination(destination adapter.Destination) error {
	if destination.Kind != adapter.Organization {
		return nil
	}
	switch destination.Visibility {
	case "all", "private":
		if len(destination.SelectedRepositoryIDs) != 0 {
			return errors.New("github-actions: selected repository ids require selected visibility")
		}
	case "selected":
		if len(destination.SelectedRepositoryIDs) == 0 {
			return errors.New("github-actions: selected visibility requires immutable repository ids")
		}
	default:
		return errors.New("github-actions: organization destination requires all, private, or selected visibility")
	}
	return nil
}

func (m *Module) Plan(ctx context.Context, req adapter.PlanRequest) (adapter.Plan, error) {
	if m.API == nil {
		return adapter.Plan{}, errors.New("github-actions: API is not configured")
	}
	if err := validateManifest(req.Target.NamePrefix, req.Manifest, false); err != nil {
		return adapter.Plan{}, err
	}
	if err := validateDestination(req.Target.Destination); err != nil {
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
	names, err := m.API.ListSecretNames(ctx, req.Target.Destination)
	if err != nil {
		return adapter.Plan{}, err
	}
	providerSecrets := adapter.NameSet(names)
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.Plan{}, err
	}
	desired := adapter.DesiredRows(req.Target.NamePrefix, req.Manifest, true)
	return adapter.Plan{
		Changes:  adapter.PlanChanges(desired, ledger, providerSecrets),
		Warnings: capacityWarnings(req.Target.Destination, desired, req.Ledger, providerSecrets),
	}, nil
}

func (m *Module) Sync(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	if m.API == nil || journal == nil {
		return adapter.SyncResult{}, errors.New("github-actions: API and durable journal are required")
	}
	if err := validateManifest(req.Target.NamePrefix, req.Manifest, true); err != nil {
		return adapter.SyncResult{}, err
	}
	if err := validateDestination(req.Target.Destination); err != nil {
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
	names, err := m.API.ListSecretNames(ctx, req.Target.Destination)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	providerSecrets := adapter.NameSet(names)
	desiredRows := adapter.DesiredRows(req.Target.NamePrefix, req.Manifest, !req.Teardown)
	completed := adapter.CompletedNames(req.Completed)
	var publicKey PublicKey
	if slices.ContainsFunc(desiredRows, func(row adapter.DesiredRow) bool { return row.Surface == adapter.Secret }) {
		if err := journal.Gate(ctx, inspect); err != nil {
			return adapter.SyncResult{}, err
		}
		publicKey, err = m.API.PublicKey(ctx, req.Target.Destination)
		if err != nil {
			return adapter.SyncResult{}, err
		}
	}
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	result := adapter.SyncResult{Warnings: capacityWarnings(req.Target.Destination, desiredRows, req.Ledger, providerSecrets)}
	for _, row := range desiredRows {
		if completed[adapter.NewLedgerKey(row.Surface, row.EffectiveName)] {
			continue
		}
		key := adapter.NewLedgerKey(row.Surface, row.EffectiveName)
		record, claimed := ledger[key]
		state := record.State
		ownedMissing := record.Missing
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Create, KeyID: row.KeyID}
		if claimed && (state == adapter.Owned || state == adapter.Dispatched) && !ownedMissing {
			effect.Disposition = adapter.Update
		}
		if !claimed {
			state, err = journal.Reserve(ctx, effect)
			if err != nil {
				return result, err
			}
			ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: state}
		}
		freshReservation := state == adapter.Reserved
		if freshReservation && row.Surface == adapter.Secret && providerSecrets[row.EffectiveName] {
			if err := journal.Refuse(ctx, effect); err != nil {
				return result, err
			}
			return addConflict(result, row), fmt.Errorf("%w: secret %s", adapter.ErrConflict, row.EffectiveName)
		}
		if err := m.prepare(ctx, journal, effect, state, req.Target); err != nil {
			return result, err
		}
		var providerStatus int
		primaryLanded := false
		if row.Surface == adapter.Secret {
			var write WriteResult
			write, err = m.writeSecret(ctx, journal, effect, req.Target.Destination, row, publicKey)
			if err != nil && definiteNonRate4xx(err) {
				firstState := stateAfterFailure(state, err)
				if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: "failure", State: firstState}); finishErr != nil {
					return result, finishErr
				}
				if gateErr := journal.Gate(ctx, inspect); gateErr != nil {
					return result, gateErr
				}
				publicKey, err = m.API.PublicKey(ctx, req.Target.Destination)
				if err != nil {
					return result, err
				}
				if err = m.prepare(ctx, journal, effect, firstState, req.Target); err != nil {
					return result, err
				}
				write, err = m.writeSecret(ctx, journal, effect, req.Target.Destination, row, publicKey)
			}
			if err == nil && write.Status != http.StatusCreated && write.Status != http.StatusNoContent {
				err = fmt.Errorf("github-actions: secret %s returned unexpected status %d", row.EffectiveName, write.Status)
			}
			providerStatus = write.Status
			primaryLanded = err == nil
			if err == nil && freshReservation && write.Status == http.StatusNoContent {
				if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: "success", Conflict: true, ProviderStatus: write.Status, Finding: "possible_capture"}); finishErr != nil {
					return result, finishErr
				}
				return addConflict(result, row), fmt.Errorf("%w: possible_capture secret %s", adapter.ErrConflict, row.EffectiveName)
			}
		} else {
			var write WriteResult
			write, err = m.writeVariable(ctx, journal, effect, req.Target.Destination, row, state, ownedMissing)
			providerStatus = write.Status
			if IsStatus(err, http.StatusNotFound) && (state == adapter.Owned || state == adapter.Dispatched) && !ownedMissing {
				if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: "failure", State: adapter.Owned, Missing: true, ProviderStatus: http.StatusNotFound, Finding: "owned_missing"}); finishErr != nil {
					return result, finishErr
				}
				ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: adapter.Owned, Missing: true}
				ownedMissing = true
				effect.Disposition = adapter.Create
				if err = m.prepare(ctx, journal, effect, adapter.Owned, req.Target); err != nil {
					return result, err
				}
				write, err = m.writeVariable(ctx, journal, effect, req.Target.Destination, row, adapter.Owned, true)
				providerStatus = write.Status
			}
			if IsStatus(err, http.StatusConflict) {
				completion := adapter.Completion{Outcome: "failure", Conflict: true, ProviderStatus: http.StatusConflict}
				if ownedMissing {
					completion.State = adapter.Owned
					completion.Missing = true
					completion.Finding = "owned_missing"
				}
				if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
					return result, finishErr
				}
				return addConflict(result, row), fmt.Errorf("%w: variable %s", adapter.ErrConflict, row.EffectiveName)
			}
			primaryLanded = err == nil
		}
		if err == nil && req.Target.Destination.Kind == adapter.Organization && req.Target.Destination.Visibility == "selected" {
			if gateErr := journal.Gate(ctx, effect); gateErr != nil {
				err = gateErr
			} else {
				err = m.API.ReplaceSelectedRepositories(ctx, req.Target.Destination, row.Surface, row.EffectiveName)
			}
		}
		if err != nil {
			outcome, finalState := failureCompletion(state, err)
			completion := adapter.Completion{Outcome: outcome, State: finalState, ProviderStatus: providerStatus}
			if ownedMissing {
				completion.Missing = true
				completion.Finding = "owned_missing"
			}
			if primaryLanded {
				completion.Outcome = "unknown"
				completion.State = adapter.Dispatched
			}
			if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
				return result, finishErr
			}
			result.Failed = append(result.Failed, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
			if outcome == "unknown" {
				return result, fmt.Errorf("%w: %s %s", adapter.ErrIndeterminate, row.Surface, row.EffectiveName)
			}
			return result, err
		}
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: "success", State: adapter.Owned, ProviderStatus: providerStatus}); err != nil {
			return result, err
		}
		ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: adapter.Owned}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
	}
	return m.prune(ctx, req, journal, desiredRows, ledger, result)
}

func (m *Module) prepare(ctx context.Context, journal adapter.Journal, effect adapter.Effect, state adapter.LedgerState, target adapter.Target) error {
	if err := journal.Gate(ctx, effect); err != nil {
		return err
	}
	if err := m.verifyDestination(ctx, target); err != nil {
		return err
	}
	if err := journal.Gate(ctx, effect); err != nil {
		return err
	}
	if err := journal.Prepare(ctx, effect, state); err != nil {
		return err
	}
	if err := journal.Gate(ctx, effect); err != nil {
		if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: "failure", State: state}); finishErr != nil {
			return finishErr
		}
		return err
	}
	return nil
}

func (m *Module) writeSecret(ctx context.Context, journal adapter.Journal, effect adapter.Effect, destination adapter.Destination, row adapter.DesiredRow, key PublicKey) (WriteResult, error) {
	sealed, err := m.sealer()([]byte(row.Value), key)
	if err != nil {
		return WriteResult{}, err
	}
	if err := journal.Gate(ctx, effect); err != nil {
		return WriteResult{}, err
	}
	return m.API.PutSecret(ctx, destination, row.EffectiveName, sealed, key.ID)
}

func (m *Module) writeVariable(ctx context.Context, journal adapter.Journal, effect adapter.Effect, destination adapter.Destination, row adapter.DesiredRow, state adapter.LedgerState, forceCreate bool) (WriteResult, error) {
	if err := journal.Gate(ctx, effect); err != nil {
		return WriteResult{}, err
	}
	if !forceCreate && (state == adapter.Owned || state == adapter.Dispatched) {
		result, err := m.API.UpdateVariable(ctx, destination, row.EffectiveName, row.Value)
		if err == nil && result.Status != http.StatusNoContent {
			return result, fmt.Errorf("github-actions: variable %s PATCH returned unexpected status %d", row.EffectiveName, result.Status)
		}
		return result, err
	}
	result, err := m.API.CreateVariable(ctx, destination, row.EffectiveName, row.Value)
	if err == nil && result.Status != http.StatusCreated {
		return result, fmt.Errorf("github-actions: variable %s POST returned unexpected status %d", row.EffectiveName, result.Status)
	}
	return result, err
}

func (m *Module) prune(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal, desiredRows []adapter.DesiredRow, ledger map[adapter.LedgerKey]adapter.LedgerEntry, result adapter.SyncResult) (adapter.SyncResult, error) {
	reservations, prunes := adapter.Undesired(desiredRows, ledger)
	for _, row := range reservations {
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		if err := journal.ReleaseReservation(ctx, effect); err != nil {
			return result, err
		}
	}
	for _, row := range prunes {
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
		if err := m.prepare(ctx, journal, effect, row.State, req.Target); err != nil {
			return result, err
		}
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		var err error
		if row.Surface == adapter.Secret {
			err = m.API.DeleteSecret(ctx, req.Target.Destination, row.EffectiveName)
		} else {
			err = m.API.DeleteVariable(ctx, req.Target.Destination, row.EffectiveName)
		}
		if err != nil && !IsStatus(err, http.StatusNotFound) {
			outcome, state := failureCompletion(row.State, err)
			if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: outcome, State: state}); finishErr != nil {
				return result, finishErr
			}
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
	identity, err := m.API.ResolveDestination(ctx, target.Destination)
	if err != nil {
		return destinationCapabilityError(target.Destination, err)
	}
	if identity.ID != target.Destination.NumericID {
		return fmt.Errorf("%w: configured %d, resolved %d", adapter.ErrDestinationID, target.Destination.NumericID, identity.ID)
	}
	if target.Destination.Kind == adapter.Environment && identity.RepositoryID != target.Destination.RepositoryID {
		return fmt.Errorf("%w: configured repository %d, resolved %d", adapter.ErrDestinationID, target.Destination.RepositoryID, identity.RepositoryID)
	}
	return m.API.VerifySelectedRepositories(ctx, target.Destination)
}

func addConflict(result adapter.SyncResult, row adapter.DesiredRow) adapter.SyncResult {
	result.Conflicts = append(result.Conflicts, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Conflict})
	return result
}

func definiteNonRate4xx(err error) bool {
	var response *ResponseError
	return !errors.Is(err, adapter.ErrRateLimited) && errors.As(err, &response) && response.Status >= 400 && response.Status < 500
}

func stateAfterFailure(prior adapter.LedgerState, err error) adapter.LedgerState {
	if definiteNonRate4xx(err) {
		if prior == adapter.Reserved {
			return adapter.Dispatched
		}
		return prior
	}
	return adapter.Dispatched
}

func failureCompletion(prior adapter.LedgerState, err error) (string, adapter.LedgerState) {
	if definiteNonRate4xx(err) || errors.Is(err, adapter.ErrRateLimited) {
		if prior == adapter.Reserved {
			return "failure", ""
		}
		return "failure", prior
	}
	return "unknown", adapter.Dispatched
}

func capacityWarnings(destination adapter.Destination, desiredRows []adapter.DesiredRow, ledger []adapter.LedgerEntry, providerSecrets map[string]bool) []string {
	secretCap, variableCap := 100, 500
	scope := "repository"
	switch destination.Kind {
	case adapter.Organization:
		secretCap, variableCap, scope = 1000, 1000, "organization"
	case adapter.Environment:
		secretCap, variableCap, scope = 100, 100, "environment"
	}
	secrets := make(map[string]bool, len(providerSecrets))
	for name := range providerSecrets {
		secrets[strings.ToUpper(name)] = true
	}
	variables := map[string]bool{}
	variableBytes := 0
	for _, row := range desiredRows {
		switch row.Surface {
		case adapter.Secret:
			secrets[strings.ToUpper(row.EffectiveName)] = true
		case adapter.Variable:
			variables[strings.ToUpper(row.EffectiveName)] = true
			variableBytes += len([]byte(row.Value))
		}
	}
	for _, row := range ledger {
		if row.State == adapter.Released {
			continue
		}
		if row.Surface == adapter.Secret {
			secrets[strings.ToUpper(row.EffectiveName)] = true
		} else if row.Surface == adapter.Variable {
			variables[strings.ToUpper(row.EffectiveName)] = true
		}
	}
	warnings := []string{}
	if len(secrets) > secretCap {
		warnings = append(warnings, fmt.Sprintf("github-actions: secret count %d exceeds %s cap %d", len(secrets), scope, secretCap))
	}
	if len(variables) > variableCap {
		warnings = append(warnings, fmt.Sprintf("github-actions: variable count lower bound %d exceeds %s cap %d; unreadable unowned variables may increase it", len(variables), scope, variableCap))
	}
	if destination.Kind == adapter.Organization && len(secrets) > 100 {
		warnings = append(warnings, fmt.Sprintf("github-actions: organization workflows see only the first 100 secrets alphabetically; current count is at least %d", len(secrets)))
	}
	if destination.Kind != adapter.Environment && variableBytes > 256*1024 {
		warnings = append(warnings, fmt.Sprintf("github-actions: workflow variable bytes lower bound %d exceeds the combined organization and repository 256 KB delivery cap", variableBytes))
	}
	return warnings
}
