package gitlab

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

const (
	// expiryWarning is how far ahead a token expiry becomes a target warning.
	expiryWarning = 14 * 24 * time.Hour
	versionFloor  = "16.0"
	hiddenFloor   = "17.4"
)

// ErrPersonalToken is the documented refusal for a personal access token.
var ErrPersonalToken = errors.New("gitlab: personal access tokens are refused; use a project or group access token with the api scope and the Maintainer role, or recreate the adapter with allow_personal_token to accept a token that can act as its human owner everywhere they have access")

type Module struct {
	API API
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
	if _, err := CanonicalOrigin(cfg.Origin); err != nil {
		return err
	}
	_, err := TLSConfig(cfg.SPKIPin, cfg.CABundlePEM)
	return err
}

func validateDestination(d adapter.Destination) error {
	switch d.Kind {
	case adapter.Repository:
		if d.Owner == "" || d.Name == "" {
			return errors.New("gitlab: project destination requires namespace and project path")
		}
	case adapter.Organization:
		if d.Owner == "" || d.Name != "" {
			return errors.New("gitlab: group destination requires only the group full path")
		}
	default:
		return errors.New("gitlab: destination must be a project (repository) or group (organization)")
	}
	if d.Environment != "" || d.Visibility != "" || len(d.SelectedRepositoryIDs) != 0 || d.RepositoryID != 0 {
		return errors.New("gitlab: destination does not take GitHub routing fields")
	}
	return ValidateScope(d.Scope)
}

// ValidateScope checks a GitLab environment_scope value.
func ValidateScope(scope string) error {
	if scope == "" || len(scope) > 255 || strings.ContainsAny(scope, "\r\n\x00") || strings.TrimSpace(scope) != scope {
		return errors.New("gitlab: environment scope must be 1-255 characters without surrounding whitespace (use * for every environment)")
	}
	return nil
}

// versionAtLeast compares GitLab "major.minor" prefixes.
func versionAtLeast(raw, floor string) bool {
	parse := func(v string) (int, int, bool) {
		parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".", 3)
		if len(parts) < 2 {
			return 0, 0, false
		}
		major, errMajor := strconv.Atoi(parts[0])
		minor, errMinor := strconv.Atoi(strings.SplitN(parts[1], "-", 2)[0])
		return major, minor, errMajor == nil && errMinor == nil
	}
	major, minor, ok := parse(raw)
	wantMajor, wantMinor, _ := parse(floor)
	return ok && (major > wantMajor || major == wantMajor && minor >= wantMinor)
}

// checkToken enforces the least-privilege token contract and returns the
// expiry and any expiry warning. It never inspects token contents.
func (m *Module) checkToken(ctx context.Context, cfg adapter.Config) (time.Time, []string, error) {
	info, err := m.API.Token(ctx)
	if err != nil {
		return time.Time{}, nil, tokenError(err)
	}
	now := m.now()
	switch {
	case info.Revoked || !info.Active:
		return time.Time{}, nil, fmt.Errorf("%w: gitlab token is revoked or inactive", adapter.ErrProviderAuth)
	case !info.ExpiresAt.IsZero() && !now.Before(info.ExpiresAt):
		return time.Time{}, nil, fmt.Errorf("%w: gitlab token expired at %s", adapter.ErrProviderAuth, info.ExpiresAt.Format(time.DateOnly))
	case !slices.Contains(info.Scopes, "api"):
		return time.Time{}, nil, errors.New("gitlab: token requires the api scope to write CI/CD variables")
	case slices.Contains(info.Scopes, "sudo"), slices.Contains(info.Scopes, "admin_mode"):
		return time.Time{}, nil, errors.New("gitlab: token with sudo or admin_mode scope is refused; mint a project or group access token with only the api scope")
	case !info.Bot && !cfg.AllowPersonalToken:
		return time.Time{}, nil, ErrPersonalToken
	}
	var warnings []string
	if !info.ExpiresAt.IsZero() && info.ExpiresAt.Sub(now) <= expiryWarning {
		warnings = append(warnings, "gitlab: access token expires on "+info.ExpiresAt.Format(time.DateOnly)+"; rotate the adapter credential")
	}
	return info.ExpiresAt, warnings, nil
}

func tokenError(err error) error {
	if IsStatus(err, http.StatusNotFound) {
		return fmt.Errorf("gitlab: this GitLab cannot describe the presented token (requires GitLab >= %s): %w", versionFloor, err)
	}
	return err
}

func (m *Module) TestConnection(ctx context.Context, req adapter.ConnectionRequest) (adapter.Connection, error) {
	if m.API == nil {
		return adapter.Connection{}, errors.New("gitlab: API is not configured")
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
	version, err := m.API.Version(ctx)
	if err != nil {
		return adapter.Connection{}, err
	}
	if !versionAtLeast(version, versionFloor) {
		return adapter.Connection{}, fmt.Errorf("%w: gitlab requires version >= %s for raw and scoped variable writes (%s)", adapter.ErrVersionFloor, versionFloor, version)
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	expires, _, err := m.checkToken(ctx, req.Config)
	if err != nil {
		return adapter.Connection{}, err
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	identity, err := m.API.ResolveDestination(ctx, req.Destination)
	if err != nil {
		return adapter.Connection{}, destinationError(req.Destination, err)
	}
	if req.Destination.NumericID != 0 && req.Destination.NumericID != identity.ID {
		return adapter.Connection{}, fmt.Errorf("%w: configured %d, resolved %d", adapter.ErrDestinationID, req.Destination.NumericID, identity.ID)
	}
	if !strings.EqualFold(identity.Path, FullPath(req.Destination)) {
		return adapter.Connection{}, fmt.Errorf("%w: %s now resolves to %s", adapter.ErrDestinationID, FullPath(req.Destination), identity.Path)
	}
	return adapter.Connection{Version: version, DestinationID: identity.ID, CredentialExpiresAt: expires}, nil
}

func destinationError(d adapter.Destination, err error) error {
	if errors.Is(err, adapter.ErrDestinationID) || errors.Is(err, adapter.ErrRateLimited) || errors.Is(err, adapter.ErrProviderAuth) {
		return err
	}
	kind := "project"
	if d.Kind == adapter.Organization {
		kind = "group"
	}
	if IsStatus(err, http.StatusNotFound) || IsStatus(err, http.StatusForbidden) {
		return fmt.Errorf("gitlab: %s %q unavailable or unauthorized; the token needs the api scope and the Maintainer role (Owner for group variables): %w", kind, FullPath(d), err)
	}
	return err
}

// verifyDestination re-resolves the immutable numeric id and refuses when
// the project or group moved or was renamed since configuration.
func (m *Module) verifyDestination(ctx context.Context, target adapter.Target) error {
	identity, err := m.API.ResolveDestination(ctx, target.Destination)
	if err != nil {
		return destinationError(target.Destination, err)
	}
	if identity.ID != target.Destination.NumericID {
		return fmt.Errorf("%w: configured %d, resolved %d", adapter.ErrDestinationID, target.Destination.NumericID, identity.ID)
	}
	if !strings.EqualFold(identity.Path, FullPath(target.Destination)) {
		return fmt.Errorf("%w: gitlab %d moved from %s to %s; re-configure the target", adapter.ErrDestinationID, identity.ID, FullPath(target.Destination), identity.Path)
	}
	return nil
}

// desiredRows puts every GitLab write on the one variable surface: GitLab has
// a single CI/CD variable namespace, so there is one sentinel and a
// classification change is an update of the same owned key.
func desiredRows(prefix string, manifest []adapter.ManifestEntry, sentinel bool) []adapter.DesiredRow {
	rows := adapter.DesiredRows(prefix, manifest, false)
	for i := range rows {
		rows[i].Surface = adapter.Variable
	}
	if sentinel {
		rows = append([]adapter.DesiredRow{{
			ManifestEntry: adapter.ManifestEntry{Classification: adapter.ConfigClassification, Value: adapter.SentinelName},
			Surface:       adapter.Variable, EffectiveName: prefix + adapter.SentinelName,
		}}, rows...)
	}
	return rows
}

func variableFor(target adapter.Target, row adapter.DesiredRow) Variable {
	secret := row.Classification == adapter.SecretClassification
	return Variable{
		Key: row.EffectiveName, Value: row.Value, Scope: target.Destination.Scope,
		Protected: target.Options.Protected && row.KeyID != "",
		Masked:    secret, Hidden: secret && target.Options.Hidden, Raw: !target.Options.Expand,
	}
}

func (m *Module) Plan(ctx context.Context, req adapter.PlanRequest) (adapter.Plan, error) {
	if m.API == nil {
		return adapter.Plan{}, errors.New("gitlab: API is not configured")
	}
	if err := adapter.ValidateGitLabManifest(req.Target.NamePrefix, req.Manifest, false); err != nil {
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
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.Plan{}, err
	}
	// Value-blind and read-free: unowned keys stay unknown until a create
	// either lands or is refused as already taken.
	desired := desiredRows(req.Target.NamePrefix, req.Manifest, true)
	return adapter.Plan{Changes: adapter.PlanChanges(desired, ledger, nil)}, nil
}

func (m *Module) Sync(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	if m.API == nil || journal == nil {
		return adapter.SyncResult{}, errors.New("gitlab: API and durable journal are required")
	}
	if err := adapter.ValidateGitLabManifest(req.Target.NamePrefix, req.Manifest, true); err != nil {
		return adapter.SyncResult{}, err
	}
	if err := validateDestination(req.Target.Destination); err != nil {
		return adapter.SyncResult{}, err
	}
	inspect := adapter.Effect{Surface: adapter.Variable, EffectiveName: "*", Disposition: adapter.Update}
	if err := journal.Gate(ctx, inspect); err != nil {
		return adapter.SyncResult{}, err
	}
	_, warnings, err := m.checkToken(ctx, req.Config)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	if req.Target.Options.Hidden && !req.Teardown {
		if err := journal.Gate(ctx, inspect); err != nil {
			return adapter.SyncResult{}, err
		}
		version, err := m.API.Version(ctx)
		if err != nil {
			return adapter.SyncResult{}, err
		}
		if !versionAtLeast(version, hiddenFloor) {
			return adapter.SyncResult{}, fmt.Errorf("%w: hidden variables require GitLab >= %s (%s); masking is never downgraded silently, so disable hidden on the target explicitly", adapter.ErrVersionFloor, hiddenFloor, version)
		}
	}
	if err := journal.Gate(ctx, inspect); err != nil {
		return adapter.SyncResult{}, err
	}
	if err := m.verifyDestination(ctx, req.Target); err != nil {
		return adapter.SyncResult{}, err
	}
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	desired := desiredRows(req.Target.NamePrefix, req.Manifest, !req.Teardown)
	completed := adapter.CompletedNames(req.Completed)
	result := adapter.SyncResult{Warnings: warnings}
	for _, row := range desired {
		key := adapter.NewLedgerKey(row.Surface, row.EffectiveName)
		if completed[key] {
			continue
		}
		record, claimed := ledger[key]
		state, ownedMissing := record.State, record.Missing
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
		if err := m.prepare(ctx, journal, effect, state, req.Target, ownedMissing); err != nil {
			return result, err
		}
		variable := variableFor(req.Target, row)
		write, err := m.write(ctx, journal, effect, req.Target.Destination, variable, state, ownedMissing)
		if IsStatus(err, http.StatusNotFound) && (state == adapter.Owned || state == adapter.Dispatched) && !ownedMissing {
			// Hikyo owns the key but it is gone: record owned_missing durably,
			// then recreate. A crash between the two replays as a create.
			if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeFailure, State: adapter.Owned, Missing: true, ProviderStatus: http.StatusNotFound, Finding: "owned_missing"}); finishErr != nil {
				return result, finishErr
			}
			ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: adapter.Owned, Missing: true}
			state, ownedMissing = adapter.Owned, true
			effect.Disposition = adapter.Create
			if err := m.prepare(ctx, journal, effect, state, req.Target, ownedMissing); err != nil {
				return result, err
			}
			write, err = m.write(ctx, journal, effect, req.Target.Destination, variable, state, ownedMissing)
		}
		if IsTaken(err) {
			// exists, unowned: the ledger never claimed this key here, or an
			// owned key was deleted and something else now holds it.
			completion := adapter.Completion{Outcome: adapter.OutcomeFailure, ReleaseLedger: true, Conflict: true, ProviderStatus: http.StatusBadRequest}
			if ownedMissing {
				completion = adapter.Completion{Outcome: adapter.OutcomeFailure, State: adapter.Owned, Missing: true, Conflict: true, ProviderStatus: http.StatusBadRequest, Finding: "owned_missing"}
			}
			if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
				return result, finishErr
			}
			result.Conflicts = append(result.Conflicts, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Conflict})
			return result, fmt.Errorf("%w: variable %s", adapter.ErrConflict, row.EffectiveName)
		}
		if err != nil {
			outcome, finalState := failureCompletion(state, err)
			completion := adapter.Completion{Outcome: outcome, State: finalState, ProviderStatus: write.Status}
			if finalState == "" {
				completion.ReleaseLedger = true
			}
			if ownedMissing && finalState != "" {
				completion.Missing = true
				completion.Finding = "owned_missing"
			}
			if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
				return result, finishErr
			}
			result.Failed = append(result.Failed, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
			if outcome == adapter.OutcomeUnknown {
				return result, fmt.Errorf("%w: variable %s", adapter.ErrIndeterminate, row.EffectiveName)
			}
			if IsStatus(err, http.StatusForbidden) {
				return result, destinationError(req.Target.Destination, err)
			}
			return result, fmt.Errorf("gitlab: variable %s: %w", row.EffectiveName, err)
		}
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned, ProviderStatus: write.Status}); err != nil {
			return result, err
		}
		ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: adapter.Owned}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
	}
	return m.prune(ctx, req, journal, desired, ledger, result)
}

func (m *Module) prepare(ctx context.Context, journal adapter.Journal, effect adapter.Effect, state adapter.LedgerState, target adapter.Target, ownedMissing bool) error {
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
		completion := adapter.Completion{Outcome: adapter.OutcomeFailure, State: state}
		if ownedMissing {
			completion.Missing = true
			completion.Finding = "owned_missing"
		}
		if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
			return finishErr
		}
		return err
	}
	return nil
}

func (m *Module) write(ctx context.Context, journal adapter.Journal, effect adapter.Effect, destination adapter.Destination, variable Variable, state adapter.LedgerState, forceCreate bool) (WriteResult, error) {
	if err := journal.Gate(ctx, effect); err != nil {
		return WriteResult{}, err
	}
	if !forceCreate && (state == adapter.Owned || state == adapter.Dispatched) {
		result, err := m.API.UpdateVariable(ctx, destination, variable)
		if err == nil && result.Status != http.StatusOK {
			return result, fmt.Errorf("gitlab: variable %s PUT returned unexpected status %d", variable.Key, result.Status)
		}
		return result, err
	}
	result, err := m.API.CreateVariable(ctx, destination, variable)
	if err == nil && result.Status != http.StatusCreated {
		return result, fmt.Errorf("gitlab: variable %s POST returned unexpected status %d", variable.Key, result.Status)
	}
	return result, err
}

func (m *Module) prune(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal, desired []adapter.DesiredRow, ledger map[adapter.LedgerKey]adapter.LedgerEntry, result adapter.SyncResult) (adapter.SyncResult, error) {
	reservations, prunes := adapter.Undesired(desired, ledger)
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
		if err := m.prepare(ctx, journal, effect, row.State, req.Target, false); err != nil {
			return result, err
		}
		if err := journal.Gate(ctx, effect); err != nil {
			if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeFailure, State: row.State}); finishErr != nil {
				return result, finishErr
			}
			return result, err
		}
		err := m.API.DeleteVariable(ctx, req.Target.Destination, row.EffectiveName, req.Target.Destination.Scope)
		if err != nil && !IsStatus(err, http.StatusNotFound) {
			outcome, state := failureCompletion(row.State, err)
			if state == "" {
				// A prune never releases on failure: ownership stays until the
				// delete is proven.
				state = row.State
			}
			if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: outcome, State: state}); finishErr != nil {
				return result, finishErr
			}
			result.Failed = append(result.Failed, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete})
			if outcome == adapter.OutcomeUnknown {
				return result, fmt.Errorf("%w: variable %s", adapter.ErrIndeterminate, row.EffectiveName)
			}
			return result, fmt.Errorf("gitlab: delete variable %s: %w", row.EffectiveName, err)
		}
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Released}); err != nil {
			return result, err
		}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete})
	}
	return result, nil
}

func definiteNonRate4xx(err error) bool {
	var response *ResponseError
	return !errors.Is(err, adapter.ErrRateLimited) && errors.As(err, &response) && response.Status >= 400 && response.Status < 500
}

// failureCompletion keeps ledger truth: a definite refusal leaves the prior
// state (releasing only a bare reservation); anything ambiguous is recorded
// as dispatched with an unknown outcome.
func failureCompletion(prior adapter.LedgerState, err error) (adapter.Outcome, adapter.LedgerState) {
	if definiteNonRate4xx(err) || errors.Is(err, adapter.ErrRateLimited) {
		if prior == adapter.Reserved {
			return adapter.OutcomeFailure, ""
		}
		return adapter.OutcomeFailure, prior
	}
	return adapter.OutcomeUnknown, adapter.Dispatched
}
