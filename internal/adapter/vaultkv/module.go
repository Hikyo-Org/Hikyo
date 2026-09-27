package vaultkv

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

// Custom metadata keys. They are names and version numbers only; no value,
// digest, or verifier of a value is ever written to destination metadata.
const (
	// MarkerKey names the owning Hikyo target on every managed path.
	MarkerKey = "managed_by_hikyo"
	// VersionKey records the KV version Hikyo last wrote. A current_version
	// that differs is external movement.
	VersionKey = "hikyo_version"
	// PendingKey records the version an in-flight write will produce, written
	// before the CAS request so a crash or ambiguous response replays safely.
	PendingKey = "hikyo_pending_version"
)

type Module struct {
	API API
}

var _ adapter.Module = (*Module)(nil)

// ValidateConfig accepts only the canonical origin spelling, so one server
// and namespace cannot be configured twice under different spellings of the
// same origin.
func (m *Module) ValidateConfig(cfg adapter.Config) error {
	canonical, err := CanonicalOrigin(cfg.Origin)
	if err != nil {
		return err
	}
	if canonical != cfg.Origin {
		return fmt.Errorf("vault-kv: origin must be spelled canonically as %s", canonical)
	}
	return nil
}

var mountSegment = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// ValidateDestination checks the KV addressing carried in the generic
// destination record: kind repository, Owner is the KV v2 mount path, Name is
// the path prefix under which every key becomes one secret.
func ValidateDestination(destination adapter.Destination) error {
	if destination.Kind != adapter.Repository || destination.Environment != "" || destination.RepositoryID != 0 || destination.Visibility != "" || len(destination.SelectedRepositoryIDs) != 0 {
		return errors.New("vault-kv: destination must be kind repository with the KV mount as owner and the path prefix as name")
	}
	if err := validatePath("mount", destination.Owner); err != nil {
		return err
	}
	if strings.HasPrefix(destination.Owner, "sys") && (destination.Owner == "sys" || strings.HasPrefix(destination.Owner, "sys/")) || destination.Owner == "auth" || strings.HasPrefix(destination.Owner, "auth/") || destination.Owner == "cubbyhole" || destination.Owner == "identity" {
		return errors.New("vault-kv: mount names a reserved system path")
	}
	return validatePath("path prefix", destination.Name)
}

func validatePath(label, path string) error {
	if path == "" || len(path) > 255 {
		return fmt.Errorf("vault-kv: %s must be 1..255 bytes", label)
	}
	for _, segment := range strings.Split(path, "/") {
		if !mountSegment.MatchString(segment) || segment == "." || segment == ".." {
			return fmt.Errorf("vault-kv: %s %q must be '/'-separated segments of letters, digits, '.', '-' or '_'", label, path)
		}
	}
	return nil
}

// DestinationID binds a target to the live mount identity and its path
// prefix. A mount that is disabled and re-enabled, or moved, gets a new UUID,
// so the id changes and every write refuses with ErrDestinationID.
func DestinationID(mount Mount, pathPrefix string) (int64, error) {
	identity := mount.UUID
	if identity == "" {
		identity = mount.Accessor
	}
	if identity == "" {
		return 0, errors.New("vault-kv: the mount reported neither uuid nor accessor")
	}
	sum := sha256.Sum256([]byte("hikyo-vault-kv\x00" + identity + "\x00" + pathPrefix))
	id := int64(binary.BigEndian.Uint64(sum[:8]) & math.MaxInt64)
	if id == 0 {
		id = 1
	}
	return id, nil
}

func (m *Module) TestConnection(ctx context.Context, req adapter.ConnectionRequest) (adapter.Connection, error) {
	if m.API == nil {
		return adapter.Connection{}, errors.New("vault-kv: API is not configured")
	}
	if err := ValidateDestination(req.Destination); err != nil {
		return adapter.Connection{}, err
	}
	if req.Gate == nil {
		return adapter.Connection{}, adapter.ErrUnauthorized
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	health, err := m.API.Health(ctx)
	if err != nil {
		return adapter.Connection{}, err
	}
	if !health.Initialized {
		return adapter.Connection{}, errors.New("vault-kv: server is not initialized")
	}
	if health.Sealed {
		return adapter.Connection{}, &ResponseError{Status: 503, Sealed: true}
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	id, err := m.resolve(ctx, req.Destination)
	if err != nil {
		return adapter.Connection{}, err
	}
	if req.Destination.NumericID != 0 && req.Destination.NumericID != id {
		return adapter.Connection{}, fmt.Errorf("%w: the KV mount or its identity changed", adapter.ErrDestinationID)
	}
	// Expiry is informational. The mount probe above already proved the token
	// authenticates, so a refusal here only means the policy omits
	// lookup-self (a least-privilege token without the default policy).
	info, err := m.API.LookupSelf(ctx)
	if err != nil && !errors.Is(err, adapter.ErrProviderAuth) {
		return adapter.Connection{}, err
	}
	return adapter.Connection{Version: health.Version, DestinationID: id, CredentialExpiresAt: info.ExpireTime}, nil
}

func (m *Module) resolve(ctx context.Context, destination adapter.Destination) (int64, error) {
	mount, err := m.API.MountInfo(ctx, destination.Owner)
	if err != nil {
		if IsNotFound(err) {
			return 0, fmt.Errorf("%w: KV mount %q is not present", adapter.ErrDestinationID, destination.Owner)
		}
		return 0, err
	}
	if mount.Type != "kv" && mount.Type != "generic" {
		return 0, fmt.Errorf("vault-kv: mount %q is a %q engine, not KV", destination.Owner, mount.Type)
	}
	if mount.Version != "2" {
		return 0, fmt.Errorf("vault-kv: mount %q is not KV version 2; KV v1 has no check-and-set", destination.Owner)
	}
	return DestinationID(mount, destination.Name)
}

func (m *Module) verifyDestination(ctx context.Context, target adapter.Target) error {
	id, err := m.resolve(ctx, target.Destination)
	if err != nil {
		return err
	}
	if id != target.Destination.NumericID {
		return fmt.Errorf("%w: the KV mount or its identity changed", adapter.ErrDestinationID)
	}
	return nil
}

func secretPath(target adapter.Target, effectiveName string) string {
	return target.Destination.Name + "/" + effectiveName
}

// desiredRows keeps one management sentinel. Both classifications share one
// KV tree, so the generic variable-surface sentinel would address the same
// path as the secret-surface one.
func desiredRows(prefix string, manifest []adapter.ManifestEntry, sentinels bool) []adapter.DesiredRow {
	rows := adapter.DesiredRows(prefix, manifest, sentinels)
	out := rows[:0]
	for _, row := range rows {
		if row.KeyID == "" && row.Surface == adapter.Variable {
			continue
		}
		out = append(out, row)
	}
	return out
}

type pathKind int

const (
	pathAbsent pathKind = iota
	// pathClean: marked for this target and current_version equals the
	// recorded version (or the pending write provably did not land).
	pathClean
	// pathLanded: the pending write landed; finalize before anything else.
	pathLanded
	// pathUnmarked: present with no Hikyo marker. Unowned unless the ledger
	// holds it (an explicit adoption).
	pathUnmarked
	// pathForeign: marked by another Hikyo target.
	pathForeign
	// pathMoved: marked for this target but versions moved externally.
	pathMoved
)

type pathState struct {
	kind    pathKind
	version int64
	// released: the current version is soft-deleted or no version exists, so
	// no live value would be overwritten.
	released bool
}

func parseVersion(raw string) (int64, bool) {
	if raw == "" {
		return 0, true
	}
	version, err := strconv.ParseInt(raw, 10, 64)
	return version, err == nil && version >= 0
}

func classify(meta Metadata, targetID string) pathState {
	marker := meta.CustomMetadata[MarkerKey]
	current := meta.CurrentVersion
	released := current == 0 || meta.Versions[current].Deleted || meta.Versions[current].Destroyed
	switch {
	case marker == "":
		return pathState{kind: pathUnmarked, version: current, released: released}
	case marker != targetID:
		return pathState{kind: pathForeign, version: current}
	}
	recorded, okRecorded := parseVersion(meta.CustomMetadata[VersionKey])
	pending, okPending := parseVersion(meta.CustomMetadata[PendingKey])
	switch {
	case !okRecorded || !okPending:
		return pathState{kind: pathMoved, version: current}
	case pending != 0 && current == pending:
		return pathState{kind: pathLanded, version: current, released: released}
	case pending != 0 && current == pending-1:
		return pathState{kind: pathClean, version: current, released: released}
	case pending == 0 && current == recorded:
		return pathState{kind: pathClean, version: current, released: released}
	}
	return pathState{kind: pathMoved, version: current}
}

func (m *Module) inspect(ctx context.Context, target adapter.Target, name string) (pathState, error) {
	meta, err := m.API.ReadMetadata(ctx, target.Destination.Owner, secretPath(target, name))
	if IsNotFound(err) {
		return pathState{kind: pathAbsent}, nil
	}
	if err != nil {
		return pathState{}, err
	}
	return classify(meta, target.ID), nil
}

// writable decides whether a desired row may be written given its ledger
// claim and live path state. Unclaimed paths are writable only when absent or
// when this target's own marker shows a released (soft-deleted) earlier
// delivery; everything else is `exists, unowned`. Claimed paths refuse
// another target's marker and external version movement.
func writable(claimed bool, state pathState) bool {
	switch state.kind {
	case pathAbsent:
		return true
	case pathClean, pathLanded:
		return claimed || state.released
	case pathUnmarked:
		return claimed
	default:
		return false
	}
}

func claimedState(record adapter.LedgerEntry, claimed bool) bool {
	return claimed && (record.State == adapter.Owned || record.State == adapter.Dispatched)
}

func (m *Module) Plan(ctx context.Context, req adapter.PlanRequest) (adapter.Plan, error) {
	if m.API == nil {
		return adapter.Plan{}, errors.New("vault-kv: API is not configured")
	}
	if err := adapter.ValidateVaultKVManifest(req.Target.NamePrefix, req.Manifest, false); err != nil {
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
	if err := m.verifyDestination(ctx, req.Target); err != nil {
		return adapter.Plan{}, err
	}
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.Plan{}, err
	}
	desired := desiredRows(req.Target.NamePrefix, req.Manifest, true)
	desiredSet := make(map[adapter.LedgerKey]bool, len(desired))
	changes := make([]adapter.Change, 0, len(desired)+len(ledger))
	for _, row := range desired {
		if err := req.Gate(ctx); err != nil {
			return adapter.Plan{}, err
		}
		key := adapter.NewLedgerKey(row.Surface, row.EffectiveName)
		desiredSet[key] = true
		record, claimed := ledger[key]
		owned := claimedState(record, claimed)
		state, err := m.inspect(ctx, req.Target, row.EffectiveName)
		if err != nil {
			return adapter.Plan{}, err
		}
		disposition := adapter.Create
		switch {
		case !writable(owned, state):
			disposition = adapter.Conflict
		case owned && state.kind != pathAbsent:
			disposition = adapter.Update
		}
		changes = append(changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: disposition})
	}
	for key, record := range ledger {
		if desiredSet[key] || record.State == adapter.Reserved {
			continue
		}
		changes = append(changes, adapter.Change{Surface: record.Surface, EffectiveName: record.EffectiveName, Disposition: adapter.Delete})
	}
	adapter.SortChanges(changes)
	return adapter.Plan{Changes: changes}, nil
}

func (m *Module) Sync(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	if m.API == nil || journal == nil {
		return adapter.SyncResult{}, errors.New("vault-kv: API and durable journal are required")
	}
	if err := adapter.ValidateVaultKVManifest(req.Target.NamePrefix, req.Manifest, true); err != nil {
		return adapter.SyncResult{}, err
	}
	if err := ValidateDestination(req.Target.Destination); err != nil {
		return adapter.SyncResult{}, err
	}
	inspect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "*", Disposition: adapter.Update}
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
	rows := desiredRows(req.Target.NamePrefix, req.Manifest, !req.Teardown)
	completed := adapter.CompletedNames(req.Completed)
	result := adapter.SyncResult{}
	for _, row := range rows {
		key := adapter.NewLedgerKey(row.Surface, row.EffectiveName)
		if completed[key] {
			continue
		}
		if err := m.syncRow(ctx, req.Target, row, ledger, journal, &result); err != nil {
			return result, err
		}
	}
	reservations, prunes := adapter.Undesired(rows, ledger)
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
		if row.Surface == adapter.Variable && strings.EqualFold(row.EffectiveName, req.Target.NamePrefix+adapter.SentinelName) {
			// Never written by this provider; drop any stray claim without a
			// provider request.
			if err := m.releaseWithoutRequest(ctx, row, journal); err != nil {
				return result, err
			}
			continue
		}
		if err := m.pruneRow(ctx, req.Target, row, journal, &result); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (m *Module) releaseWithoutRequest(ctx context.Context, row adapter.LedgerEntry, journal adapter.Journal) error {
	effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
	if err := journal.Gate(ctx, effect); err != nil {
		return err
	}
	if err := journal.Prepare(ctx, effect, row.State); err != nil {
		return err
	}
	return journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Released})
}

func (m *Module) syncRow(ctx context.Context, target adapter.Target, row adapter.DesiredRow, ledger map[adapter.LedgerKey]adapter.LedgerEntry, journal adapter.Journal, result *adapter.SyncResult) error {
	key := adapter.NewLedgerKey(row.Surface, row.EffectiveName)
	effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Create, KeyID: row.KeyID}
	record, claimed := ledger[key]
	owned := claimedState(record, claimed)
	if owned && !record.Missing {
		effect.Disposition = adapter.Update
	}
	state := record.State
	if !claimed {
		var err error
		state, err = journal.Reserve(ctx, effect)
		if err != nil {
			return err
		}
		record = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: state}
		ledger[key] = record
	}
	if err := journal.Gate(ctx, effect); err != nil {
		return err
	}
	if err := m.verifyDestination(ctx, target); err != nil {
		return err
	}
	live, err := m.inspect(ctx, target, row.EffectiveName)
	if err != nil {
		return err
	}
	conflict := adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Conflict}
	if !writable(owned, live) {
		if state == adapter.Reserved {
			if err := journal.Refuse(ctx, effect); err != nil {
				return err
			}
			delete(ledger, key)
			result.Conflicts = append(result.Conflicts, conflict)
			return fmt.Errorf("%w: KV path %s", adapter.ErrConflict, row.EffectiveName)
		}
		// Owned but moved or re-marked externally: record the conflict for
		// operator attention and keep the claim. Nothing is written.
		if err := journal.Prepare(ctx, effect, state); err != nil {
			return err
		}
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeFailure, State: state, Conflict: true, Missing: record.Missing}); err != nil {
			return err
		}
		result.Conflicts = append(result.Conflicts, conflict)
		return fmt.Errorf("%w: KV path %s moved outside Hikyo", adapter.ErrConflict, row.EffectiveName)
	}
	missing := claimed && record.State == adapter.Owned && live.kind == pathAbsent
	if err := journal.Gate(ctx, effect); err != nil {
		return err
	}
	if err := journal.Prepare(ctx, effect, state); err != nil {
		return err
	}
	if gateErr := journal.Gate(ctx, effect); gateErr != nil {
		completion := adapter.Completion{Outcome: adapter.OutcomeFailure, State: state}
		if missing {
			completion.Missing, completion.Finding = true, "owned_missing"
		}
		if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
			return finishErr
		}
		return gateErr
	}
	writeErr := m.write(ctx, target, row, live)
	if writeErr != nil {
		completion := adapter.Completion{Outcome: adapter.OutcomeUnknown, State: adapter.Dispatched}
		casMoved := IsCASMismatch(writeErr)
		if casMoved || definitive(writeErr) {
			completion.Outcome = adapter.OutcomeFailure
			if state == adapter.Reserved {
				completion.State, completion.ReleaseLedger = "", true
			} else {
				completion.State = state
			}
			completion.Conflict = casMoved
		}
		if missing && !completion.ReleaseLedger {
			completion.Missing, completion.Finding = true, "owned_missing"
		}
		if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
			return finishErr
		}
		if completion.ReleaseLedger {
			delete(ledger, key)
		}
		if casMoved {
			result.Conflicts = append(result.Conflicts, conflict)
			// writeErr also carries any failed pending-marker withdrawal.
			return errors.Join(fmt.Errorf("%w: KV path %s moved during the write", adapter.ErrConflict, row.EffectiveName), writeErr)
		}
		result.Failed = append(result.Failed, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
		if completion.Outcome == adapter.OutcomeUnknown {
			return errors.Join(fmt.Errorf("%w: KV path %s", adapter.ErrIndeterminate, row.EffectiveName), writeErr)
		}
		return writeErr
	}
	if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned}); err != nil {
		return err
	}
	ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: adapter.Owned}
	result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
	return nil
}

func versionString(version int64) *string {
	value := strconv.FormatInt(version, 10)
	return &value
}

// write delivers one value with the crash-safe marker protocol:
//
//  1. an absent path is created with check-and-set 0 before any metadata is
//     touched, so a concurrent creator's metadata is never replaced or
//     stripped; any other path is marked with this target and the version the
//     write will produce;
//  2. the value is written with check-and-set on the observed version, so any
//     external movement since the metadata read is a conflict, never a blind
//     overwrite;
//  3. the marker, the produced version, and a cleared pending marker are
//     recorded in one metadata patch.
//
// A crash or ambiguous response between steps leaves a pending version that
// the next attempt resolves from metadata alone: current == pending means the
// write landed, current == pending-1 means it did not, anything else is
// external movement. A create that landed before its marker leaves an
// unmarked path that the durable dispatched claim still covers.
func (m *Module) write(ctx context.Context, target adapter.Target, row adapter.DesiredRow, live pathState) error {
	mount, path := target.Destination.Owner, secretPath(target, row.EffectiveName)
	cas := live.version
	switch live.kind {
	case pathAbsent:
		cas = 0
	case pathLanded:
		if err := m.finalize(ctx, target, path, live.version); err != nil {
			return err
		}
		fallthrough
	default:
		marker := target.ID
		if err := m.API.PatchCustomMetadata(ctx, mount, path, map[string]*string{MarkerKey: &marker, PendingKey: versionString(cas + 1)}); err != nil {
			return err
		}
	}
	version, err := m.API.WriteCAS(ctx, mount, path, row.Value, cas)
	if err != nil {
		if IsCASMismatch(err) && live.kind != pathAbsent {
			// The external write that beat this one holds version cas+1, so a
			// pending marker of cas+1 would replay as "our write landed" and
			// take the path over. Withdraw it; a failed withdrawal is reported
			// because the hazard would outlive this attempt.
			if cleanupErr := m.API.PatchCustomMetadata(ctx, mount, path, map[string]*string{PendingKey: nil}); cleanupErr != nil {
				return errors.Join(err, fmt.Errorf("vault-kv: withdrawing the pending marker after a lost check-and-set: %w", cleanupErr))
			}
		}
		return err
	}
	// The value is delivered. A failed finalize leaves either a pending
	// version equal to current_version, which the next attempt finalizes as
	// landed, or an unmarked create held by the dispatched claim.
	_ = m.finalize(ctx, target, path, version)
	return nil
}

func (m *Module) finalize(ctx context.Context, target adapter.Target, path string, version int64) error {
	marker := target.ID
	return m.API.PatchCustomMetadata(ctx, target.Destination.Owner, path, map[string]*string{MarkerKey: &marker, VersionKey: versionString(version), PendingKey: nil})
}

// pruneRow soft-deletes the current version of a ledger-owned path. It never
// destroys versions or deletes metadata: the value stays recoverable with
// `vault kv undelete` until the mount's own version retention removes it. A
// path that moved outside Hikyo is released, never deleted.
func (m *Module) pruneRow(ctx context.Context, target adapter.Target, row adapter.LedgerEntry, journal adapter.Journal, result *adapter.SyncResult) error {
	effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
	if err := journal.Gate(ctx, effect); err != nil {
		return err
	}
	if err := m.verifyDestination(ctx, target); err != nil {
		return err
	}
	live, err := m.inspect(ctx, target, row.EffectiveName)
	if err != nil {
		return err
	}
	if err := journal.Gate(ctx, effect); err != nil {
		return err
	}
	if err := journal.Prepare(ctx, effect, row.State); err != nil {
		return err
	}
	if gateErr := journal.Gate(ctx, effect); gateErr != nil {
		if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeFailure, State: row.State}); finishErr != nil {
			return finishErr
		}
		return gateErr
	}
	deleted := adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
	switch live.kind {
	case pathAbsent:
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Released}); err != nil {
			return err
		}
		result.Changes = append(result.Changes, deleted)
		return nil
	case pathForeign, pathMoved:
		// Someone else changed a path Hikyo no longer wants. Deleting it would
		// destroy their edit, and holding the claim would wedge every later
		// prune and teardown, so custody is released with a recorded conflict
		// and a target warning, and the path is left exactly as found.
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeFailure, ReleaseLedger: true, Conflict: true}); err != nil {
			return err
		}
		result.Conflicts = append(result.Conflicts, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Conflict})
		result.Warnings = append(result.Warnings, "vault-kv: "+row.EffectiveName+" moved outside Hikyo; released without pruning")
		return nil
	}
	mount, path := target.Destination.Owner, secretPath(target, row.EffectiveName)
	var deleteErr error
	if live.kind == pathLanded {
		deleteErr = m.finalize(ctx, target, path, live.version)
	}
	if deleteErr == nil && !live.released {
		deleteErr = m.API.DeleteLatest(ctx, mount, path)
	}
	if deleteErr != nil && !IsNotFound(deleteErr) {
		outcome := adapter.OutcomeUnknown
		if definitive(deleteErr) {
			outcome = adapter.OutcomeFailure
		}
		if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: outcome, State: row.State}); finishErr != nil {
			return finishErr
		}
		result.Failed = append(result.Failed, deleted)
		return deleteErr
	}
	if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Released}); err != nil {
		return err
	}
	result.Changes = append(result.Changes, deleted)
	return nil
}
