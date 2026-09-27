package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

// Module converges one Workers script or one Pages project environment.
//
// Cloudflare has a single encrypted surface per destination, so every row,
// including config-classified keys and the management sentinel, is owned on
// the secret surface and written as secret_text.
type Module struct {
	API API
	// Now is the clock the token-expiry check uses; nil means time.Now.
	Now func() time.Time
}

var _ adapter.Module = (*Module)(nil)

func (m *Module) now() time.Time {
	if m.Now != nil {
		return m.Now().UTC()
	}
	return time.Now().UTC()
}

func (m *Module) ValidateConfig(cfg adapter.Config) error {
	_, err := canonicalOrigin(cfg.Origin)
	return err
}

// ValidateDestination is the static shape check shared by configure and sync.
func ValidateDestination(d adapter.Destination) error {
	switch d.Kind {
	case adapter.WorkersScript:
		_, err := scriptPath(d)
		return err
	case adapter.PagesProject:
		_, err := projectPath(d)
		return err
	default:
		return fmt.Errorf("cloudflare: destination kind must be %s or %s", adapter.WorkersScript, adapter.PagesProject)
	}
}

// TestConnection verifies the token is active, unexpired and confined to the
// configured account, then resolves the destination and lists its names. It
// writes nothing: the first sync writes the sentinel before any key.
func (m *Module) TestConnection(ctx context.Context, req adapter.ConnectionRequest) (adapter.Connection, error) {
	if m.API == nil {
		return adapter.Connection{}, errors.New("cloudflare: API is not configured")
	}
	if err := validateCredential(req.Access.Credential); err != nil {
		return adapter.Connection{}, err
	}
	if err := ValidateDestination(req.Destination); err != nil {
		return adapter.Connection{}, err
	}
	if req.AllowEnvironmentCreate {
		return adapter.Connection{}, errors.New("cloudflare: Hikyo never creates Workers scripts or Pages projects; create the destination first")
	}
	if req.Gate == nil {
		return adapter.Connection{}, adapter.ErrUnauthorized
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	token, err := m.checkToken(ctx, req.Destination.Owner)
	if err != nil {
		return adapter.Connection{}, err
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	accounts, err := m.API.ListAccountIDs(ctx)
	if err != nil {
		return adapter.Connection{}, capabilityError(req.Destination, err)
	}
	if len(accounts) != 1 || accounts[0] != req.Destination.Owner {
		return adapter.Connection{}, fmt.Errorf("cloudflare: token reaches %d accounts; refused as overbroad, scope the token to exactly account %s", len(accounts), req.Destination.Owner)
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	id, _, err := m.resolve(ctx, req.Destination)
	if err != nil {
		return adapter.Connection{}, capabilityError(req.Destination, err)
	}
	if req.Destination.NumericID != 0 && req.Destination.NumericID != id {
		return adapter.Connection{}, fmt.Errorf("%w: configured %d, resolved %d", adapter.ErrDestinationID, req.Destination.NumericID, id)
	}
	return adapter.Connection{Version: "cloudflare", DestinationID: id, CredentialExpiresAt: token.ExpiresAt}, nil
}

func (m *Module) checkToken(ctx context.Context, account string) (TokenStatus, error) {
	token, err := m.API.VerifyToken(ctx, account)
	if err != nil {
		return TokenStatus{}, fmt.Errorf("cloudflare: token verification failed: %w", err)
	}
	if token.Status != "active" {
		return TokenStatus{}, fmt.Errorf("%w: token status is %q", adapter.ErrProviderAuth, token.Status)
	}
	if !token.ExpiresAt.IsZero() && !token.ExpiresAt.After(m.now()) {
		return TokenStatus{}, fmt.Errorf("%w: token expired at %s", adapter.ErrProviderAuth, token.ExpiresAt.Format(time.RFC3339))
	}
	return token, nil
}

func capabilityError(d adapter.Destination, err error) error {
	if errors.Is(err, adapter.ErrDestinationID) {
		return fmt.Errorf("cloudflare: destination identity changed; re-configure the target: %w", err)
	}
	switch d.Kind {
	case adapter.WorkersScript:
		if IsStatus(err, http.StatusNotFound) {
			return fmt.Errorf("cloudflare: Workers script %q not found in account %s (renamed or deleted?): %w", d.Name, d.Owner, err)
		}
		if errors.Is(err, adapter.ErrProviderAuth) {
			return fmt.Errorf("cloudflare: Workers script destination requires an account-scoped token with Workers Scripts: Edit: %w", err)
		}
		return fmt.Errorf("cloudflare: Workers script %q: %w", d.Name, err)
	case adapter.PagesProject:
		if IsStatus(err, http.StatusNotFound) {
			return fmt.Errorf("cloudflare: Pages project %q not found in account %s (renamed or deleted?): %w", d.Name, d.Owner, err)
		}
		if errors.Is(err, adapter.ErrProviderAuth) {
			return fmt.Errorf("cloudflare: Pages project destination requires an account-scoped token with Cloudflare Pages: Edit: %w", err)
		}
		return fmt.Errorf("cloudflare: Pages project %q: %w", d.Name, err)
	default:
		return err
	}
}

// resolve returns the destination fingerprint and the set of names present
// at the destination, upper-cased. Names of every binding or variable type
// count: a plain_text binding Hikyo does not own is as much in the way as a
// secret, and a secret upsert must never collide with it.
func (m *Module) resolve(ctx context.Context, d adapter.Destination) (int64, map[string]bool, error) {
	switch d.Kind {
	case adapter.WorkersScript:
		tag, err := m.API.ResolveScript(ctx, d)
		if err != nil {
			return 0, nil, err
		}
		names, err := m.API.ListBindingNames(ctx, d)
		if err != nil {
			return 0, nil, err
		}
		return Fingerprint(d, tag), adapter.NameSet(names), nil
	case adapter.PagesProject:
		shape, err := m.API.ResolveProject(ctx, d)
		if err != nil {
			return 0, nil, err
		}
		names := make([]string, 0, len(shape.Names))
		for name := range shape.Names {
			names = append(names, name)
		}
		return Fingerprint(d, shape.ID), adapter.NameSet(names), nil
	default:
		return 0, nil, ValidateDestination(d)
	}
}

func (m *Module) verify(ctx context.Context, target adapter.Target) (map[string]bool, error) {
	id, names, err := m.resolve(ctx, target.Destination)
	if err != nil {
		return nil, capabilityError(target.Destination, err)
	}
	if id != target.Destination.NumericID {
		return nil, fmt.Errorf("%w: configured %d, resolved %d", adapter.ErrDestinationID, target.Destination.NumericID, id)
	}
	return names, nil
}

// desiredRows puts every entry on the secret surface. Only the secret
// sentinel is desired: there is no plaintext surface to mark.
func desiredRows(prefix string, manifest []adapter.ManifestEntry, sentinel bool) []adapter.DesiredRow {
	rows := make([]adapter.DesiredRow, 0, len(manifest)+1)
	for _, entry := range manifest {
		rows = append(rows, adapter.DesiredRow{ManifestEntry: entry, Surface: adapter.Secret, EffectiveName: prefix + entry.CanonicalName})
	}
	slices.SortFunc(rows, func(a, b adapter.DesiredRow) int { return strings.Compare(a.EffectiveName, b.EffectiveName) })
	if sentinel {
		marker := adapter.DesiredRow{ManifestEntry: adapter.ManifestEntry{Classification: adapter.SecretClassification, Value: adapter.SentinelName}, Surface: adapter.Secret, EffectiveName: prefix + adapter.SentinelName}
		rows = append([]adapter.DesiredRow{marker}, rows...)
	}
	return rows
}

func (m *Module) Plan(ctx context.Context, req adapter.PlanRequest) (adapter.Plan, error) {
	if m.API == nil {
		return adapter.Plan{}, errors.New("cloudflare: API is not configured")
	}
	if err := adapter.ValidateCloudflareManifest(req.Target.NamePrefix, req.Manifest, false); err != nil {
		return adapter.Plan{}, err
	}
	if err := ValidateDestination(req.Target.Destination); err != nil {
		return adapter.Plan{}, err
	}
	if req.Gate == nil {
		return adapter.Plan{}, adapter.ErrUnauthorized
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Plan{}, err
	}
	names, err := m.verify(ctx, req.Target)
	if err != nil {
		return adapter.Plan{}, err
	}
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.Plan{}, err
	}
	desired := desiredRows(req.Target.NamePrefix, req.Manifest, true)
	return adapter.Plan{Changes: adapter.PlanChanges(desired, ledger, names), Warnings: sideEffectWarnings(req.Target.Destination, len(desired))}, nil
}

func sideEffectWarnings(d adapter.Destination, writes int) []string {
	if writes == 0 {
		return nil
	}
	switch d.Kind {
	case adapter.WorkersScript:
		return []string{fmt.Sprintf("cloudflare: each Workers secret write or delete deploys a new version of script %q; up to %d writes per sync", d.Name, writes)}
	case adapter.PagesProject:
		return []string{fmt.Sprintf("cloudflare: Pages %s variables apply from the next deployment of project %q; existing deployments keep their values", d.Environment, d.Name)}
	}
	return nil
}

func (m *Module) Sync(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	if m.API == nil || journal == nil {
		return adapter.SyncResult{}, errors.New("cloudflare: API and durable journal are required")
	}
	if err := adapter.ValidateCloudflareManifest(req.Target.NamePrefix, req.Manifest, true); err != nil {
		return adapter.SyncResult{}, err
	}
	if err := ValidateDestination(req.Target.Destination); err != nil {
		return adapter.SyncResult{}, err
	}
	inspect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "*", Disposition: adapter.Update}
	if err := journal.Gate(ctx, inspect); err != nil {
		return adapter.SyncResult{}, err
	}
	if _, err := m.checkToken(ctx, req.Target.Destination.Owner); err != nil {
		return adapter.SyncResult{}, err
	}
	if err := journal.Gate(ctx, inspect); err != nil {
		return adapter.SyncResult{}, err
	}
	if _, err := m.verify(ctx, req.Target); err != nil {
		return adapter.SyncResult{}, err
	}
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	rows := desiredRows(req.Target.NamePrefix, req.Manifest, !req.Teardown)
	completed := adapter.CompletedNames(req.Completed)
	result := adapter.SyncResult{Warnings: sideEffectWarnings(req.Target.Destination, len(rows))}
	for _, row := range rows {
		key := adapter.NewLedgerKey(row.Surface, row.EffectiveName)
		if completed[key] {
			continue
		}
		record, claimed := ledger[key]
		state := record.State
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Create, KeyID: row.KeyID}
		if claimed && (state == adapter.Owned || state == adapter.Dispatched) && !record.Missing {
			effect.Disposition = adapter.Update
		}
		if !claimed {
			state, err = journal.Reserve(ctx, effect)
			if err != nil {
				return result, err
			}
			ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: state}
		}
		// Both Cloudflare writes are upserts, so an unowned name is refused
		// by name before any write. Identity and names are re-read right
		// before every write to keep the capture window as short as possible.
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		present, err := m.verify(ctx, req.Target)
		if err != nil {
			return result, err
		}
		if state == adapter.Reserved && present[strings.ToUpper(row.EffectiveName)] {
			if err := journal.Refuse(ctx, effect); err != nil {
				return result, err
			}
			result.Conflicts = append(result.Conflicts, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Conflict})
			return result, fmt.Errorf("%w: %s already exists at the destination", adapter.ErrConflict, row.EffectiveName)
		}
		if err := m.prepare(ctx, journal, effect, state); err != nil {
			return result, err
		}
		err = m.write(ctx, req.Target.Destination, row)
		if err != nil {
			outcome, finalState := failureCompletion(state, err)
			completion := adapter.Completion{Outcome: outcome, State: finalState, ProviderStatus: providerStatus(err)}
			if finalState == "" {
				completion.ReleaseLedger = true
			}
			if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
				return result, finishErr
			}
			result.Failed = append(result.Failed, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
			if outcome == adapter.OutcomeUnknown {
				return result, fmt.Errorf("%w: %s %s", adapter.ErrIndeterminate, row.Surface, row.EffectiveName)
			}
			return result, capabilityError(req.Target.Destination, err)
		}
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned, ProviderStatus: http.StatusOK}); err != nil {
			return result, err
		}
		ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: adapter.Owned}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
	}
	return m.prune(ctx, req, journal, rows, ledger, result)
}

func (m *Module) prepare(ctx context.Context, journal adapter.Journal, effect adapter.Effect, state adapter.LedgerState) error {
	if err := journal.Gate(ctx, effect); err != nil {
		return err
	}
	if err := journal.Prepare(ctx, effect, state); err != nil {
		return err
	}
	if err := journal.Gate(ctx, effect); err != nil {
		if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeFailure, State: state}); finishErr != nil {
			return finishErr
		}
		return err
	}
	return nil
}

func (m *Module) write(ctx context.Context, d adapter.Destination, row adapter.DesiredRow) error {
	if d.Kind == adapter.PagesProject {
		value := row.Value
		return m.API.PatchPagesSecret(ctx, d, row.EffectiveName, &value)
	}
	return m.API.PutSecret(ctx, d, row.EffectiveName, row.Value)
}

func (m *Module) remove(ctx context.Context, d adapter.Destination, name string) error {
	if d.Kind == adapter.PagesProject {
		return m.API.PatchPagesSecret(ctx, d, name, nil)
	}
	return m.API.DeleteSecret(ctx, d, name)
}

// prune deletes only ledger-owned names that are no longer desired, sentinel
// last. A name already gone at the destination counts as deleted.
func (m *Module) prune(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal, rows []adapter.DesiredRow, ledger map[adapter.LedgerKey]adapter.LedgerEntry, result adapter.SyncResult) (adapter.SyncResult, error) {
	reservations, prunes := adapter.Undesired(rows, ledger)
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
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		if _, err := m.verify(ctx, req.Target); err != nil {
			return result, err
		}
		if err := m.prepare(ctx, journal, effect, row.State); err != nil {
			return result, err
		}
		err := m.remove(ctx, req.Target.Destination, row.EffectiveName)
		if err != nil && !IsStatus(err, http.StatusNotFound) {
			outcome, state := failureCompletion(row.State, err)
			completion := adapter.Completion{Outcome: outcome, State: state, ProviderStatus: providerStatus(err)}
			if state == "" {
				completion.ReleaseLedger = true
			}
			if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
				return result, finishErr
			}
			result.Failed = append(result.Failed, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete})
			if outcome == adapter.OutcomeUnknown {
				return result, fmt.Errorf("%w: delete %s", adapter.ErrIndeterminate, row.EffectiveName)
			}
			return result, capabilityError(req.Target.Destination, err)
		}
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Released, ProviderStatus: providerStatus(err)}); err != nil {
			return result, err
		}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete})
	}
	return result, nil
}

func definite4xx(err error) bool {
	var response *ResponseError
	return errors.As(err, &response) && response.Status >= 400 && response.Status < 500
}

// failureCompletion keeps ownership honest: a definite refusal or rate limit
// did not land, so a fresh reservation is released; anything else (network
// error, 5xx, ambiguous envelope) may have landed and stays dispatched.
func failureCompletion(prior adapter.LedgerState, err error) (adapter.Outcome, adapter.LedgerState) {
	if definite4xx(err) || errors.Is(err, adapter.ErrRateLimited) {
		if prior == adapter.Reserved {
			return adapter.OutcomeFailure, ""
		}
		return adapter.OutcomeFailure, prior
	}
	return adapter.OutcomeUnknown, adapter.Dispatched
}

func providerStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	if errors.Is(err, adapter.ErrRateLimited) {
		return http.StatusTooManyRequests
	}
	var response *ResponseError
	if errors.As(err, &response) {
		return response.Status
	}
	return 0
}
