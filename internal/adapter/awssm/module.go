package awssm

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

// Module converges one target into AWS Secrets Manager. Ownership lives in the
// Hikyo ledger; on the AWS side every owned secret carries the tag
// MANAGED_BY_HIKYO=<target id> and every Hikyo-written version carries the
// HIKYO_CURRENT staging label. Neither is ever used to read a value.
type Module struct {
	API API
}

var _ adapter.Module = (*Module)(nil)

// regional is implemented by clients that know their signing region, so ARN
// checks can prove the endpoint did not move regions.
type regional interface{ Region() string }

func (m *Module) ValidateConfig(cfg adapter.Config) error {
	return configError(ValidateOrigin(cfg.Origin))
}

func (m *Module) TestConnection(ctx context.Context, req adapter.ConnectionRequest) (adapter.Connection, error) {
	if m.API == nil {
		return adapter.Connection{}, errors.New("aws-secrets-manager: API is not configured")
	}
	if req.Gate == nil {
		return adapter.Connection{}, adapter.ErrUnauthorized
	}
	if req.AllowEnvironmentCreate {
		return adapter.Connection{}, errors.New("aws-secrets-manager: environment creation does not apply")
	}
	if err := adapter.ValidateAWSSecretsManagerDestination(req.Destination); err != nil {
		return adapter.Connection{}, configError(err)
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	account, err := m.verifyAccount(ctx, req.Destination)
	if err != nil {
		return adapter.Connection{}, err
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	// The probe proves Secrets Manager reachability and permission on the
	// exact route without touching a value.
	if req.Destination.Kind == adapter.JSONObject {
		if _, _, err := m.describe(ctx, req.Destination, req.Destination.Name); err != nil {
			return adapter.Connection{}, err
		}
	} else if _, err := m.API.ListSecretNames(ctx, req.Destination.Name, 1); err != nil {
		return adapter.Connection{}, err
	}
	version := "secretsmanager"
	if r, ok := m.API.(regional); ok && r.Region() != "" {
		version += ":" + r.Region()
	}
	return adapter.Connection{Version: version, DestinationID: account}, nil
}

// verifyAccount proves the credential still acts in the configured account.
// A different account is a destination move and never silently followed.
func (m *Module) verifyAccount(ctx context.Context, destination adapter.Destination) (int64, error) {
	identity, err := m.API.ResolveIdentity(ctx)
	if err != nil {
		return 0, err
	}
	if identity.Account != destination.Owner {
		return 0, fmt.Errorf("%w: configured account %s, credential acts in %s", adapter.ErrDestinationID, destination.Owner, identity.Account)
	}
	id, err := strconv.ParseInt(identity.Account, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("%w: account id %q is not numeric", adapter.ErrDestinationID, identity.Account)
	}
	if destination.NumericID != 0 && destination.NumericID != id {
		return 0, fmt.Errorf("%w: configured %d, resolved %d", adapter.ErrDestinationID, destination.NumericID, id)
	}
	return id, nil
}

// describe reads metadata for one name and proves its ARN sits in the
// configured account and the client's region.
func (m *Module) describe(ctx context.Context, destination adapter.Destination, name string) (SecretMetadata, bool, error) {
	meta, err := m.API.DescribeSecret(ctx, name)
	if IsNotFound(err) {
		return SecretMetadata{}, false, nil
	}
	if err != nil {
		return SecretMetadata{}, false, err
	}
	region := ""
	if r, ok := m.API.(regional); ok {
		region = r.Region()
	}
	if err := verifyARN(meta.ARN, destination.Owner, region); err != nil {
		return SecretMetadata{}, false, err
	}
	return meta, true, nil
}

func verifyARN(arn, account, region string) error {
	parts := strings.SplitN(arn, ":", 7)
	if len(parts) != 7 || parts[0] != "arn" || parts[2] != serviceName || parts[5] != "secret" {
		return fmt.Errorf("%w: provider returned an unrecognized secret ARN", adapter.ErrDestinationID)
	}
	if parts[4] != account {
		return fmt.Errorf("%w: secret lives in account %s, configured %s", adapter.ErrDestinationID, parts[4], account)
	}
	if region != "" && parts[3] != region {
		return fmt.Errorf("%w: secret lives in region %s, configured %s", adapter.ErrDestinationID, parts[3], region)
	}
	return nil
}

func (m *Module) Plan(ctx context.Context, req adapter.PlanRequest) (adapter.Plan, error) {
	if m.API == nil {
		return adapter.Plan{}, errors.New("aws-secrets-manager: API is not configured")
	}
	destination := req.Target.Destination
	if err := adapter.ValidateAWSSecretsManagerManifest(destination, req.Target.NamePrefix, req.Manifest, false); err != nil {
		return adapter.Plan{}, configError(err)
	}
	if req.Gate == nil {
		return adapter.Plan{}, adapter.ErrUnauthorized
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Plan{}, err
	}
	if _, err := m.verifyAccount(ctx, destination); err != nil {
		return adapter.Plan{}, err
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Plan{}, err
	}
	existing := map[string]bool{}
	if destination.Kind == adapter.JSONObject {
		_, found, err := m.describe(ctx, destination, destination.Name)
		if err != nil {
			return adapter.Plan{}, err
		}
		existing[destination.Name] = found
	} else {
		names, err := m.API.ListSecretNames(ctx, destination.Name+req.Target.NamePrefix, 0)
		if err != nil {
			return adapter.Plan{}, err
		}
		for _, name := range names {
			existing[name] = true
		}
	}
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.Plan{}, err
	}
	desired, err := adapter.AWSDesiredRows(destination, req.Target.NamePrefix, req.Manifest)
	if err != nil {
		return adapter.Plan{}, err
	}
	return adapter.Plan{Changes: adapter.PlanChanges(desired, ledger, existing)}, nil
}

// rowStatus says how one name's failure affects the rest of the sync.
type rowStatus int

const (
	rowDone rowStatus = iota
	rowConflict
	rowFailed
	rowFatal
)

func (m *Module) Sync(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	if journal == nil {
		return adapter.SyncResult{}, errors.New("aws-secrets-manager: durable journal is required")
	}
	if m.API == nil {
		return adapter.SyncResult{}, errors.New("aws-secrets-manager: API is not configured")
	}
	destination := req.Target.Destination
	if err := adapter.ValidateAWSSecretsManagerManifest(destination, req.Target.NamePrefix, req.Manifest, !req.Teardown); err != nil {
		return adapter.SyncResult{}, err
	}
	inspect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "*", Disposition: adapter.Update}
	if err := journal.Gate(ctx, inspect); err != nil {
		return adapter.SyncResult{}, err
	}
	if _, err := m.verifyAccount(ctx, destination); err != nil {
		return adapter.SyncResult{}, err
	}
	if err := journal.Gate(ctx, inspect); err != nil {
		return adapter.SyncResult{}, err
	}
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	var desired []adapter.DesiredRow
	if !req.Teardown {
		if desired, err = adapter.AWSDesiredRows(destination, req.Target.NamePrefix, req.Manifest); err != nil {
			return adapter.SyncResult{}, err
		}
	}
	completed := adapter.CompletedNames(req.Completed)
	result := adapter.SyncResult{}
	// Per-key outcomes are independent: one refused or ambiguous name is
	// reported and the rest still converge. Only failures that would repeat
	// for every name (authority, credential, rate limit, destination move)
	// stop the loop.
	var failures []error
	for _, row := range desired {
		key := adapter.NewLedgerKey(adapter.Secret, row.EffectiveName)
		if completed[key] {
			continue
		}
		change, status, err := m.converge(ctx, req, journal, ledger, row)
		switch status {
		case rowDone:
			ledger[key] = adapter.LedgerEntry{Surface: adapter.Secret, EffectiveName: row.EffectiveName, State: adapter.Owned}
			result.Changes = append(result.Changes, change)
		case rowConflict:
			result.Conflicts = append(result.Conflicts, change)
			failures = append(failures, err)
		case rowFailed:
			result.Failed = append(result.Failed, change)
			failures = append(failures, err)
		default:
			if change.EffectiveName != "" {
				result.Failed = append(result.Failed, change)
			}
			return result, errors.Join(append(failures, err)...)
		}
	}
	reservations, prunes := adapter.Undesired(desired, ledger)
	for _, row := range reservations {
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
		if err := journal.Gate(ctx, effect); err != nil {
			return result, errors.Join(append(failures, err)...)
		}
		if err := journal.ReleaseReservation(ctx, effect); err != nil {
			return result, errors.Join(append(failures, err)...)
		}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete})
	}
	// Prune only once every desired name converged: a degraded target keeps
	// its owned names until the retry that can also finish the writes.
	if len(failures) != 0 {
		return result, errors.Join(failures...)
	}
	for _, row := range prunes {
		change, status, err := m.prune(ctx, req, journal, row)
		switch status {
		case rowDone:
			result.Changes = append(result.Changes, change)
		case rowConflict:
			result.Conflicts = append(result.Conflicts, change)
			failures = append(failures, err)
		case rowFailed:
			result.Failed = append(result.Failed, change)
			failures = append(failures, err)
		default:
			result.Failed = append(result.Failed, change)
			return result, errors.Join(append(failures, err)...)
		}
	}
	return result, errors.Join(failures...)
}

// writePlan is the value-blind decision for one desired name.
type writePlan struct {
	conflict        string
	create          bool
	restore         bool
	tag             bool
	put             bool
	promote         bool
	markCurrent     bool
	previousCurrent string
	previousHikyo   string
	versionTag      bool
}

// decide inspects metadata only: existence, ownership tag, deletion state,
// the labels on the version holding AWSCURRENT, and the HIKYO_VERSION tag.
//
// HIKYO_PENDING proves a staged version only for this exact job token. Once
// promoted, HIKYO_CURRENT and HIKYO_VERSION provide lasting ownership evidence.
func decide(meta SecretMetadata, found bool, state adapter.LedgerState, targetID, token string) writePlan {
	if !found {
		return writePlan{create: true, put: true, promote: true, markCurrent: true, versionTag: true}
	}
	owner, tagged := meta.Tags[adapter.SentinelName]
	plan := writePlan{put: true, promote: true, markCurrent: true, versionTag: true}
	switch {
	case tagged && owner != targetID:
		return writePlan{conflict: "tagged as owned by another Hikyo target"}
	case !tagged && state == adapter.Reserved:
		return writePlan{conflict: "exists, unowned; adopt it from a plan to let Hikyo overwrite it"}
	case !tagged && state != adapter.Owned:
		// A dispatched create always tags atomically, so an untagged secret
		// here was created by someone else in the crash window.
		return writePlan{conflict: "exists without the " + adapter.SentinelName + " tag; tag it " + adapter.SentinelName + "=" + targetID + " to let Hikyo overwrite it"}
	case !tagged:
		// Owned without the tag is an explicit adoption: tag before writing.
		plan.tag = true
	}
	if meta.Deleted {
		plan.restore = true
	}
	current, written := "", meta.Tags[VersionTag] != ""
	for version, stages := range meta.Stages {
		if slices.Contains(stages, awsCurrent) {
			current = version
		}
		if slices.Contains(stages, CurrentStage) {
			written = true
			plan.previousHikyo = version
		}
		if slices.Contains(stages, PendingStage) {
			written = true
		}
	}
	plan.previousCurrent = current
	ours := current != "" && (slices.Contains(meta.Stages[current], CurrentStage) || meta.Tags[VersionTag] == current || (current == token && slices.Contains(meta.Stages[current], PendingStage)))
	// Once Hikyo has written a secret, or created it, any AWSCURRENT that is
	// not Hikyo's was written by someone else. An adopted secret Hikyo never
	// wrote is the one case where a foreign AWSCURRENT is expected, including
	// when its ownership tag landed but the response was lost. Owned authority
	// alone cannot override evidence of an earlier Hikyo value version.
	if current != "" && !ours && (written || meta.Tags[VersionTag] != "" || (tagged && state != adapter.Owned)) {
		return writePlan{conflict: "AWSCURRENT version " + current + " was written outside Hikyo; tag the secret " + VersionTag + "=" + current + " to let Hikyo overwrite it"}
	}
	if ours && current == token {
		plan.put = false
		plan.promote = false
		plan.markCurrent = plan.previousHikyo != token
		plan.versionTag = meta.Tags[VersionTag] != token
	}
	return plan
}

func (m *Module) converge(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal, ledger map[adapter.LedgerKey]adapter.LedgerEntry, row adapter.DesiredRow) (adapter.Change, rowStatus, error) {
	key := adapter.NewLedgerKey(adapter.Secret, row.EffectiveName)
	record, claimed := ledger[key]
	state := record.State
	effect := adapter.Effect{Surface: adapter.Secret, EffectiveName: row.EffectiveName, Disposition: adapter.Create, KeyID: row.KeyID}
	if claimed && (state == adapter.Owned || state == adapter.Dispatched) && !record.Missing {
		effect.Disposition = adapter.Update
	}
	change := adapter.Change{Surface: adapter.Secret, EffectiveName: row.EffectiveName, Disposition: effect.Disposition}
	if !claimed {
		var err error
		if state, err = journal.Reserve(ctx, effect); err != nil {
			return change, rowFatal, err
		}
		ledger[key] = adapter.LedgerEntry{Surface: adapter.Secret, EffectiveName: row.EffectiveName, State: state}
	}
	if err := journal.Gate(ctx, effect); err != nil {
		return change, rowFatal, err
	}
	meta, found, err := m.describe(ctx, req.Target.Destination, row.EffectiveName)
	if err != nil {
		return change, statusFor(err), err
	}
	token := idempotencyToken(req.JobID, req.Target.ID, req.Target.Generation, row.EffectiveName)
	plan := decide(meta, found, state, req.Target.ID, token)
	if plan.conflict != "" {
		change.Disposition = adapter.Conflict
		if state == adapter.Reserved {
			if err := journal.Refuse(ctx, effect); err != nil {
				return change, rowFatal, err
			}
		}
		return change, rowConflict, fmt.Errorf("%w: secret %s: %s", adapter.ErrConflict, row.EffectiveName, plan.conflict)
	}
	if err := journal.Gate(ctx, effect); err != nil {
		return change, rowFatal, err
	}
	if err := journal.Prepare(ctx, effect, state); err != nil {
		return change, rowFatal, err
	}
	if gateErr := journal.Gate(ctx, effect); gateErr != nil {
		if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeFailure, State: state}); finishErr != nil {
			return change, rowFatal, finishErr
		}
		return change, rowFatal, gateErr
	}
	if mutated, err := m.apply(ctx, req, row, plan, state, token); err != nil {
		status, err := m.finishFailure(ctx, journal, effect, state, mutated, err)
		if status == rowConflict {
			change.Disposition = adapter.Conflict
		}
		return change, status, err
	}
	if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned}); err != nil {
		return change, rowFatal, err
	}
	return change, rowDone, nil
}

func (m *Module) apply(ctx context.Context, req adapter.SyncRequest, row adapter.DesiredRow, plan writePlan, state adapter.LedgerState, token string) (bool, error) {
	mutated := false
	tags := map[string]string{adapter.SentinelName: req.Target.ID}
	if plan.create {
		// The tag is created with the secret, so ownership is never absent on
		// a Hikyo-created name, even across a crash before the first value.
		err := m.API.CreateSecret(ctx, CreateSecretInput{Name: row.EffectiveName, KMSKeyID: req.Target.Destination.Environment, Tags: tags})
		if IsExists(err) {
			// Someone else won the race between DescribeSecret and create, or
			// a replay of our own create landed. Re-evaluate the complete
			// ownership AND version policy, not merely the ownership tag.
			meta, found, describeErr := m.describe(ctx, req.Target.Destination, row.EffectiveName)
			if describeErr != nil {
				return mutated, describeErr
			}
			if !found || meta.Tags[adapter.SentinelName] != req.Target.ID {
				return mutated, fmt.Errorf("%w: secret %s was created outside Hikyo", adapter.ErrConflict, row.EffectiveName)
			}
			plan = decide(meta, true, state, req.Target.ID, token)
			if plan.conflict != "" {
				return mutated, fmt.Errorf("%w: secret %s: %s", adapter.ErrConflict, row.EffectiveName, plan.conflict)
			}
			mutated = true
		} else if err != nil {
			return mutated, err
		} else {
			mutated = true
		}
	}
	if plan.restore {
		if err := m.API.RestoreSecret(ctx, row.EffectiveName); err != nil {
			return mutated, err
		}
		mutated = true
	}
	if plan.tag {
		if err := m.API.TagSecret(ctx, row.EffectiveName, tags); err != nil {
			return mutated, errors.Join(errOwnershipTag, err)
		}
		mutated = true
	}
	if plan.put {
		if err := m.API.PutSecretValue(ctx, row.EffectiveName, token, row.Value); err != nil {
			return mutated, err
		}
		mutated = true
	}
	if plan.promote {
		if err := m.API.UpdateSecretVersionStage(ctx, row.EffectiveName, awsCurrent, token, plan.previousCurrent); err != nil {
			// A conditional refusal must not advance ownership markers. Preserve
			// both the old Hikyo version and the concurrent external AWSCURRENT.
			var response *ResponseError
			if errors.As(err, &response) && (response.Code == "InvalidParameterException" || response.Code == "InvalidRequestException") {
				return mutated, fmt.Errorf("%w: secret %s: conditional AWSCURRENT promotion refused; review its current version before consenting to overwrite", adapter.ErrConflict, row.EffectiveName)
			}
			return mutated, err
		}
		mutated = true
	}
	if plan.markCurrent {
		if err := m.API.UpdateSecretVersionStage(ctx, row.EffectiveName, CurrentStage, token, plan.previousHikyo); err != nil {
			return mutated, err
		}
		mutated = true
	}
	if plan.versionTag {
		// Records the version Hikyo wrote independently of staging-label
		// behaviour. If this tag fails after the write, HIKYO_CURRENT still
		// marks the version, so the replay recognises it and retags.
		if err := m.API.TagSecret(ctx, row.EffectiveName, map[string]string{VersionTag: token}); err != nil {
			return mutated, err
		}
		mutated = true
	}
	return mutated, nil
}

// errOwnershipTag means the adopted secret's value has not been written.
// Whether its ownership tag landed or not, explicit adoption remains valid.
var errOwnershipTag = errors.New("aws-secrets-manager: recording adopted ownership tag")

// finishFailure settles an attempted write. A refusal AWS answered is a
// definite failure; anything else may have landed and stays dispatched so the
// replay reuses the same idempotency token.
func (m *Module) finishFailure(ctx context.Context, journal adapter.Journal, effect adapter.Effect, state adapter.LedgerState, mutated bool, err error) (rowStatus, error) {
	completion := adapter.Completion{Outcome: adapter.OutcomeUnknown, State: adapter.Dispatched}
	if errors.Is(err, errOwnershipTag) {
		completion.State = state
	}
	conflict := errors.Is(err, adapter.ErrConflict)
	if IsDefinite(err) || conflict {
		completion = adapter.Completion{Outcome: adapter.OutcomeFailure, State: state, Conflict: conflict}
		if state == adapter.Reserved && !mutated {
			completion.State, completion.ReleaseLedger = "", true
		} else if state == adapter.Reserved {
			completion.State = adapter.Dispatched
		}
	}
	if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
		return rowFatal, finishErr
	}
	if conflict {
		return rowConflict, err
	}
	if completion.Outcome == adapter.OutcomeUnknown {
		return rowFailed, errors.Join(fmt.Errorf("%w: secret %s", adapter.ErrIndeterminate, effect.EffectiveName), err)
	}
	return statusFor(err), err
}

// statusFor decides whether a failure is local to one name or would repeat
// for every name in the target.
func statusFor(err error) rowStatus {
	switch {
	case errors.Is(err, adapter.ErrProviderAuth), errors.Is(err, adapter.ErrRateLimited),
		errors.Is(err, adapter.ErrDestinationID), errors.Is(err, adapter.ErrUnauthorized),
		errors.Is(err, adapter.ErrSuperseded):
		return rowFatal
	case errors.Is(err, adapter.ErrConflict):
		return rowConflict
	}
	return rowFailed
}

func (m *Module) prune(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal, row adapter.LedgerEntry) (adapter.Change, rowStatus, error) {
	effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
	change := adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
	if err := journal.Gate(ctx, effect); err != nil {
		return change, rowFatal, err
	}
	meta, found, err := m.describe(ctx, req.Target.Destination, row.EffectiveName)
	if err != nil {
		return change, statusFor(err), err
	}
	if owner, tagged := meta.Tags[adapter.SentinelName]; found && tagged && owner != req.Target.ID {
		change.Disposition = adapter.Conflict
		return change, rowConflict, fmt.Errorf("%w: secret %s is tagged as owned by another Hikyo target; not deleting", adapter.ErrConflict, row.EffectiveName)
	}
	if err := journal.Gate(ctx, effect); err != nil {
		return change, rowFatal, err
	}
	if err := journal.Prepare(ctx, effect, row.State); err != nil {
		return change, rowFatal, err
	}
	if gateErr := journal.Gate(ctx, effect); gateErr != nil {
		if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeFailure, State: row.State}); finishErr != nil {
			return change, rowFatal, finishErr
		}
		return change, rowFatal, gateErr
	}
	// Already gone or already scheduled for deletion is the desired end state.
	if found && !meta.Deleted {
		if err := m.API.DeleteSecret(ctx, row.EffectiveName); err != nil && !IsNotFound(err) {
			outcome := adapter.OutcomeUnknown
			if IsDefinite(err) {
				outcome = adapter.OutcomeFailure
			}
			if finishErr := journal.Finish(ctx, effect, adapter.Completion{Outcome: outcome, State: row.State}); finishErr != nil {
				return change, rowFatal, finishErr
			}
			if outcome == adapter.OutcomeUnknown {
				return change, rowFailed, errors.Join(fmt.Errorf("%w: delete %s", adapter.ErrIndeterminate, row.EffectiveName), err)
			}
			return change, statusFor(err), err
		}
	}
	if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Released}); err != nil {
		return change, rowFatal, err
	}
	return change, rowDone, nil
}

// idempotencyToken is stable across every retry of one outbox job for one
// name, so replaying an ambiguous PutSecretValue cannot create a second
// version, while a new job always writes a new version.
func idempotencyToken(jobID, targetID string, generation int64, name string) string {
	if jobID == "" {
		var random [32]byte
		_, _ = rand.Read(random[:])
		return hex.EncodeToString(random[:])
	}
	sum := sha256.Sum256([]byte("hikyo/aws-secrets-manager/v1\x00" + jobID + "\x00" + targetID + "\x00" + strconv.FormatInt(generation, 10) + "\x00" + name))
	return hex.EncodeToString(sum[:])
}
