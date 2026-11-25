package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// CredentialRotations owns scheduled credential rotation configuration (#148,
// slice S1). It records WHAT rotates and WHO is the rotation's authority; it
// executes nothing and holds no secret. The run state machine arrives with S2,
// which acts as the recorded authority principal, never as a system proof, and
// re-authorizes it in full before every transition that can still be rolled
// back; after the publish only fail-safe checks apply, so losing authority can
// never strand a live superseded credential. (The name avoids the existing
// Rotation service, which rotates Hikyo's own key hierarchy.)
//
// A rotation's publish is unattended, so it can never satisfy a per-publish
// ceremony. Creating or resuming one is therefore itself the human act that
// stands in for those ceremonies: both consume a publish ceremony over the
// rotation's two keys, with the environment, exactly as a values publish does.
type CredentialRotations struct {
	DB   *store.DB
	Auth *Auth
	Now  func() time.Time
}

func (s *CredentialRotations) now() time.Time { return nowOr(s.Now) }

// rotationRefusal is a named, caller-correctable rotation refusal. It renders
// its explanation to the caller (SafeDetail) and keeps the domain error class
// that decides its status.
type rotationRefusal struct {
	msg  string
	base error
}

func (e *rotationRefusal) Error() string      { return "service: " + e.msg }
func (e *rotationRefusal) Unwrap() error      { return e.base }
func (e *rotationRefusal) SafeDetail() string { return e.msg }

var (
	// ErrRotationKeyInUse refuses a second live rotation for a key, whichever
	// role the key plays, so two rotations never fight over one value.
	ErrRotationKeyInUse = &rotationRefusal{"one of these keys is already driven by another live rotation, as its password key or its username key", domain.ErrConflict}
	// ErrRotationStale refuses a state change whose expected generation is not
	// the rotation's current generation.
	ErrRotationStale = &rotationRefusal{"the rotation changed since it was read; read it again", domain.ErrConflict}
	// ErrRotationAuthorityLost refuses a resume while the recorded authority
	// principal no longer holds the access the rotation needs.
	ErrRotationAuthorityLost = &rotationRefusal{"the rotation's recorded authority principal no longer holds edit, publish and reveal on its keys; create the rotation again", domain.ErrConflict}
	// ErrRotationHumanOnly refuses a non-human caller: a rotation is a standing
	// delegation of a person's own access, and only a person can consume the
	// ceremony that authorizes it.
	ErrRotationHumanOnly = &rotationRefusal{"a rotation is created and resumed by a signed-in person who holds the access it will exercise", domain.ErrInvalid}
	// ErrRotationProtectedEnvironment refuses a protected environment: its
	// publishes need a per-publish ceremony an unattended run cannot give.
	ErrRotationProtectedEnvironment = &rotationRefusal{"a scheduled rotation publishes unattended, so it cannot target a protected environment, whose publishes need a per-publish ceremony", domain.ErrConflict}
	// ErrRotationApprovalPolicy refuses an environment covered by a secret-change
	// approval policy: the unattended publish would become an approval request
	// and the run's candidate would be lost every cycle.
	ErrRotationApprovalPolicy = &rotationRefusal{"a scheduled rotation publishes unattended, so it cannot target an environment covered by a secret-change approval policy", domain.ErrConflict}
	// ErrRotationProviderInactive refuses a resume while the provider can no
	// longer mint: deleted, or without an admin credential.
	ErrRotationProviderInactive = &rotationRefusal{"the rotation's dynamic provider does not exist, was deleted, or has no admin credential set, so it cannot mint; set the credential or create the rotation again", domain.ErrConflict}
	// ErrRotationKeyGrouped refuses a key that belongs to a key group: a group
	// publishes together, so an unattended publish would carry other keys with it.
	ErrRotationKeyGrouped = &rotationRefusal{"a scheduled rotation publishes exactly its two keys, unattended, and a key group publishes together, so neither key may belong to a key group; take the keys out of their group first", domain.ErrConflict}

	// ErrRotationDraftPending is the refusal for a rotation authority that holds a
	// pending draft on one of the rotation's keys: the scheduled publish stages its
	// own value into the same cell, which would replace that draft.
	ErrRotationDraftPending = &rotationRefusal{"the rotation's authority principal has a pending draft on one of the rotation's keys, and the scheduled publish would overwrite it; publish or discard that draft first", domain.ErrConflict}

	// ErrRotationKeyMissing refuses a resume while one of the rotation's keys is
	// gone or no longer fit to receive a generated password.
	ErrRotationKeyMissing = &rotationRefusal{"one of the rotation's keys no longer exists or is no longer secret-classified; create the rotation again", domain.ErrConflict}
	// ErrEnvironmentHasLiveRotation refuses an environment delete while a rotation
	// in it is not deleted or has a run that may hold a live role (any phase but
	// done, and a failed run that had already published): deleting the environment
	// would orphan a role nothing would revoke. The refusal names the rotations.
	ErrEnvironmentHasLiveRotation = &rotationRefusal{"the environment still has a live credential rotation or a run that may hold a live role; delete the rotation and let its runs finish first", domain.ErrConflict}
)

// environmentLiveRotationError is ErrEnvironmentHasLiveRotation naming the
// rotations that block the delete. Ids are not secret.
type environmentLiveRotationError struct{ detail string }

func (e *environmentLiveRotationError) Error() string      { return "service: " + e.detail }
func (e *environmentLiveRotationError) SafeDetail() string { return e.detail }
func (e *environmentLiveRotationError) Unwrap() error      { return ErrEnvironmentHasLiveRotation }

// purgeEnvironmentRotations removes a deleted environment's rotation rows in the
// delete's own transaction, refusing by name while a rotation is live or a run
// may hold a live role. The refusal carries the blocking rotation ids.
func purgeEnvironmentRotations(ctx context.Context, r store.Repos, proof authz.Proof) error {
	err := r.Rotations().PurgeEnvironment(ctx, proof)
	var live *store.RotationLiveError
	if !errors.As(err, &live) {
		return err
	}
	detail := "the environment still has a live credential rotation or a run that may hold a live role"
	if len(live.LiveRotationIDs) > 0 {
		detail += "; rotations not yet deleted: " + strings.Join(live.LiveRotationIDs, ", ")
	}
	if len(live.BlockingRunOwners) > 0 {
		detail += "; rotations whose runs may hold a live role: " + strings.Join(live.BlockingRunOwners, ", ")
	}
	if live.Truncated {
		detail += " (first " + fmt.Sprint(store.RotationBlockingIDsLimit) + " shown)"
	}
	return &environmentLiveRotationError{detail: detail + "; delete each rotation and let its runs finish first"}
}

// RotationView is the metadata surface of one rotation. It carries no secret.
// CredentialExpiresAt is when the credential currently delivered into its keys
// lapses, when a run has published one.
type RotationView struct {
	store.RotationRecord
	// HaltReason is why a halted rotation stopped, as one of the fixed words
	// (retries_exhausted, authority_lost, gate_failed, or unknown for any stored
	// value that is not one of them); empty unless the state is halted. Never the
	// stored text itself: an archive can carry anything in that column.
	HaltReason string
	// LastRunSummary is the rotation's newest run with its stamp parsed, nil when it
	// has never run. The owed-drop counts are the record's OwedDropRuns and
	// OwedDropRunsVersionsChanged.
	LastRunSummary *RotationLastRunView
}

// RotationLastRunView is the newest run of a rotation: metadata only, the fixed-word
// error code the runs list carries, and no role handle.
type RotationLastRunView struct {
	ID        string
	Phase     string
	ErrorCode string
	PhaseAt   time.Time
}

// newRotationView is the one place a stored record becomes the view a transport
// reads, so the halt reason is never passed on raw and the stamps are parsed once.
func newRotationView(rec store.RotationRecord) (RotationView, error) {
	view := RotationView{RotationRecord: rec, HaltReason: rotationHaltWord(rec.State, rec.RawHaltReason)}
	if rec.LastRun != nil {
		phaseAt, err := time.Parse(time.RFC3339Nano, rec.LastRun.PhaseAt)
		if err != nil {
			return RotationView{}, fmt.Errorf("service: parse rotation last run phase_at: %w", err)
		}
		view.LastRunSummary = &RotationLastRunView{ID: rec.LastRun.ID, Phase: rec.LastRun.Phase, ErrorCode: rotationErrorWord(rec.LastRun.ErrorCode), PhaseAt: phaseAt}
	}
	return view, nil
}

// summarizedRotationView returns the view of one rotation a response carries, with
// the run summary read for it. The summary is a response concern only: the stores'
// Get and List, the executor's gates and every existence check leave it empty.
func summarizedRotationView(ctx context.Context, rotations store.RotationReader, proof authz.Proof, rec store.RotationRecord) (RotationView, error) {
	one := []store.RotationRecord{rec}
	if err := rotations.Summarize(ctx, proof, one); err != nil {
		return RotationView{}, err
	}
	return newRotationView(one[0])
}

// RotationErrorUnknown is the error code a run shows when its stored one is not one
// of the fixed words the executor writes.
const RotationErrorUnknown = "unknown"

// rotationErrorWord maps a run's stored error code to the fixed words the wire, the
// CLI and the browser carry (the rotCode words and `abandoned`). The column is text,
// so a value outside that list, whatever wrote it, is shown as unknown and never
// passed on. Empty stays empty: the run has no code.
func rotationErrorWord(stored string) string {
	switch {
	case stored == "":
		return ""
	case slices.Contains(rotationRunErrorWords, stored):
		return stored
	}
	return RotationErrorUnknown
}

// rotationRunErrorWords is the one list of the words a run's error code may carry:
// every rotCode constant plus abandoned. A test ties it to the constants, so a new
// code cannot be added and silently shown as unknown.
var rotationRunErrorWords = []string{
	rotCodeInterrupted, rotCodeProvider, rotCodeMintFailed, rotCodeMintAmbiguous, rotCodePublishFailed,
	rotCodeBudget, rotCodePaused, rotCodeHeld, rotCodeDropFailed, rotCodeOwnsObjects, rotCodeVersionsChanged,
	rotCodeRevokeFailed, rotCodeExtendFailed, rotCodeExtendSkipped, rotCodeExpired, rotCodeInfra, "abandoned",
}

// RotationHaltUnknown is the halt reason of a halted rotation whose stored reason is
// not one of the words the executor writes (an empty or legacy value).
const RotationHaltUnknown = "unknown"

// rotationHaltWord maps the stored halt reason to the fixed words the wire, the CLI
// and the browser use. The reason is read only for a halted rotation: every state
// change clears it, and a column that disagrees with the state is not believed.
func rotationHaltWord(state, stored string) string {
	if state != store.RotationStateHalted {
		return ""
	}
	switch stored {
	case rotHaltExhausted, rotHaltAuthority, rotHaltGate:
		return stored
	}
	return RotationHaltUnknown
}

// CreateRotationRequest configures one rotation. The keys must already exist;
// KeyID receives the generated password and UsernameKeyID the generated role
// name, so a consumer always reads a matching pair.
type CreateRotationRequest struct {
	ProviderID             string
	KeyID                  string
	UsernameKeyID          string
	IntervalSeconds        int64
	RevocationDelaySeconds int64
}

// RotationStateRequest is a pause, resume or delete. ExpectedGeneration 0 means
// "the current generation"; a non-zero value must match it.
type RotationStateRequest struct {
	RotationID         string
	ExpectedGeneration int64
	// Abandon, on a delete only, closes the rotation's unsettled runs as
	// quarantine instead of refusing (#148, S2); the wire flag is --abandon.
	Abandon bool
}

// rotationAction is the closed set of state changes a rotation accepts.
type rotationAction string

const (
	rotationActionCreated      rotationAction = "created"
	rotationActionPaused       rotationAction = "paused"
	rotationActionResumed      rotationAction = "resumed"
	rotationActionDeleted      rotationAction = "deleted"
	rotationActionHoldReleased rotationAction = "hold_released"
)

// rotationDuration renders a bound in the units the CLI flags accept.
func rotationDuration(seconds int64) string {
	// Computed from seconds: converting to time.Duration overflows above ~9.2e9.
	switch {
	case seconds%86400 == 0:
		return fmt.Sprintf("%dd", seconds/86400)
	case seconds%3600 == 0:
		return fmt.Sprintf("%dh", seconds/3600)
	case seconds%60 == 0:
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%ds", seconds)
}

// ValidateRotationBounds enforces the named interval and delay bounds. A run
// supersedes the previous credential only after its overlap, so the delay must
// also be shorter than the interval or two superseded credentials would be live
// at once. Messages speak in the units of the CLI flags and name them, with the
// API field in brackets.
func ValidateRotationBounds(intervalSeconds, delaySeconds int64) error {
	const second = int64(time.Second)
	switch {
	case intervalSeconds < int64(domain.MinRotationInterval)/second:
		return fmt.Errorf("%w: --interval (interval_seconds) %s is below the %s minimum rotation interval", domain.ErrInvalid, rotationDuration(intervalSeconds), rotationDuration(int64(domain.MinRotationInterval)/second))
	case intervalSeconds > int64(domain.MaxRotationInterval)/second:
		return fmt.Errorf("%w: --interval (interval_seconds) %s is above the %s maximum rotation interval", domain.ErrInvalid, rotationDuration(intervalSeconds), rotationDuration(int64(domain.MaxRotationInterval)/second))
	case delaySeconds < int64(domain.MinRevocationDelay)/second:
		return fmt.Errorf("%w: --revocation-delay (revocation_delay_seconds) %s is below the %s minimum revocation delay", domain.ErrInvalid, rotationDuration(delaySeconds), rotationDuration(int64(domain.MinRevocationDelay)/second))
	case delaySeconds > int64(domain.MaxRevocationDelay)/second:
		return fmt.Errorf("%w: --revocation-delay (revocation_delay_seconds) %s is above the %s maximum revocation delay", domain.ErrInvalid, rotationDuration(delaySeconds), rotationDuration(int64(domain.MaxRevocationDelay)/second))
	case delaySeconds >= intervalSeconds:
		return fmt.Errorf("%w: --revocation-delay (revocation_delay_seconds) must be shorter than --interval (interval_seconds)", domain.ErrInvalid)
	}
	return nil
}

func requireRotationEnv(scope domain.Scope, what string, ids ...string) error {
	return requireEnvScope(scope, "rotation "+what+" requires environment scope"+idsHint(ids), ids...)
}

func idsHint(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return " and a rotation id"
}

// requireRotationHuman admits only a signed-in person. A machine, developer
// credential or session-less caller has no ceremony to consume, so a rotation
// would otherwise skip the very step that authorizes it.
func requireRotationHuman(caller authz.Identity) error {
	if caller.Class != domain.ClassHuman || skipsCeremony(caller) {
		return ErrRotationHumanOnly
	}
	return nil
}

// authorizeRotationKeys is the outer authorization of pause, resume, delete,
// run-now and release-hold. The operation is authorized on BOTH stored keys as
// individual keys, so a person who holds its formula through key-narrowed member
// rules passes exactly when they pass for each key, like the create that made it;
// an environment-wide holder passes both keys, so nothing narrows for them.
//
// The rotation is looked up under whichever proof the caller can actually mint,
// decided by the same evaluation Authorize makes (member rules included), with
// no denial recorded for a probe: the operation's own environment-wide proof when
// they hold it (so a publisher without read can still pause), else the inspect
// proof (read is never key-narrowed, and it is how a key-narrowed holder finds the
// keys), else neither, and then the operation itself is authorized so the refusal
// is recorded, once, under it. Every denial, including a missing rotation, answers
// the same uniform not-found, and is recorded the same way: a caller who reaches the
// rotation through inspect only is recorded under the operation whether the rotation
// is absent or its key refuses them, so neither the answer nor the audit trail
// tells an existing rotation from an absent one.
//
// shortcut: a person who holds the operation only through key-narrowed rules AND
// lacks read cannot look the rotation up (their keys are unknown until it is
// read), so they are refused as a denial under the operation; closing that needs
// an authz primitive that resolves a rotation's keys, not a wider system site.
func authorizeRotationKeys(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, actor Actor, op authz.Operation, scope domain.Scope, rotationID string, now time.Time) (caller authz.Identity, proof authz.Proof, cur store.RotationRecord, err error) {
	if caller, err = actor.resolve(ctx, az, now); err != nil {
		return
	}
	az.SetClock(now)
	lookup, held, err := az.AuthorizeFirst(ctx, caller, scope, op, authz.OpRotationInspect)
	if err != nil {
		return
	}
	viaInspect := held != op
	if cur, err = r.Rotations().Get(ctx, lookup, rotationID); err != nil {
		if viaInspect && errors.Is(err, domain.ErrNotFound) {
			// The caller does not hold the operation environment-wide and the
			// rotation is absent: record the refusal under the operation, as the
			// same caller probing an existing rotation is recorded on its key, so
			// the audit trail does not tell the two apart either.
			if _, err = az.Authorize(ctx, caller, op, scope); err == nil {
				err = domain.ErrNotFound
			}
		}
		return
	}
	if proof, err = az.AuthorizeKey(ctx, caller, op, scope, authz.KeyByID(cur.KeyID)); err != nil {
		return
	}
	_, err = az.AuthorizeKey(ctx, caller, op, scope, authz.KeyByID(cur.UsernameKeyID))
	return
}

// requireUnattendedEnvironment refuses an environment whose publishes cannot run
// unattended: a protected one (a per-publish ceremony) or one covered by an
// approval policy (the publish becomes an approval request). Lifting either
// refusal later is additive.
func requireUnattendedEnvironment(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, proof authz.Proof, scope domain.Scope) error {
	settings, err := az.EnvironmentReauthSettings(ctx, string(scope.Env))
	if err != nil {
		return err
	}
	if settings.Protected {
		return ErrRotationProtectedEnvironment
	}
	_, covered, err := r.Approvals().CoveringPolicy(ctx, proof, string(scope.Env))
	if err != nil {
		return err
	}
	if covered {
		return ErrRotationApprovalPolicy
	}
	return nil
}

// requireRotationProvider refuses a provider that cannot mint. A deleted
// provider is a tombstone that reads as absent, which is the same answer here.
func requireRotationProvider(ctx context.Context, r store.Repos, proof authz.Proof, providerID string) error {
	provider, err := r.Dynamic().GetProvider(ctx, proof, providerID)
	if errors.Is(err, domain.ErrNotFound) {
		return ErrRotationProviderInactive
	}
	if err != nil {
		return err
	}
	if !provider.CredentialPresent || provider.State != "active" {
		return ErrRotationProviderInactive
	}
	return nil
}

// requireRotationKeys checks the rotation's keys exist and the password key is
// secret-classified. At create (resuming false) a missing key is the uniform
// not-found and a non-secret password key is an invalid request. At resume a key
// that has vanished or been reclassified since the rotation was created is its
// own named refusal, never the uniform not-found and never "authority lost".
func requireRotationKeys(ctx context.Context, r store.Repos, proof authz.Proof, keyID, usernameKeyID string, resuming bool) error {
	missing := func(err error) error {
		if resuming {
			return ErrRotationKeyMissing
		}
		return err
	}
	key, err := r.Catalogue().Get(ctx, proof, keyID)
	if errors.Is(err, domain.ErrNotFound) {
		return missing(err)
	}
	if err != nil {
		return err
	}
	if key.Classification != string(schema.Secret) {
		return missing(fmt.Errorf("%w: key_id must name a secret-classified key", domain.ErrInvalid))
	}
	usernameKey, err := r.Catalogue().Get(ctx, proof, usernameKeyID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return missing(err)
		}
		return err
	}
	// A grouped key publishes with its whole group, so an unattended publish of
	// either rotated key would carry other keys with it.
	if key.GroupID != "" || usernameKey.GroupID != "" {
		return ErrRotationKeyGrouped
	}
	return nil
}

// requireNoAuthorityDraft refuses when the rotation's authority principal has a
// pending draft (a staged value or a restore) on either rotated key. The
// executor stages its candidate into the authority's own cell, and staging is
// delete-then-insert, so an existing draft would be silently replaced. Another
// principal's draft is theirs alone and never in the way.
func requireNoAuthorityDraft(ctx context.Context, r store.Repos, proof authz.Proof, authority string, keyIDs ...string) error {
	drafts, err := r.Pending().ListForOwnerInEnvironment(ctx, proof, authority)
	if err != nil {
		return err
	}
	for _, draft := range drafts {
		if slices.Contains(keyIDs, draft.KeyID) {
			return ErrRotationDraftPending
		}
	}
	return nil
}

// requireRotationCeremony consumes the publish ceremony over the environment
// and both keys, the unit a values publish of those keys would consume.
func (s *CredentialRotations) requireRotationCeremony(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, scope domain.Scope, keyID, usernameKeyID string) error {
	intent, err := NewPublishReauthIntent(string(scope.Env), []string{keyID, usernameKeyID})
	if err != nil {
		return err
	}
	return requireCeremony(ctx, s.Auth, az, caller, intent)
}

// Create configures a rotation. The caller must be a signed-in person who
// currently holds edit, publish and reveal on both keys; they are recorded as the
// rotation's authority principal, and a publish ceremony over both keys is
// consumed, because no per-publish ceremony will ever gate the unattended
// publishes that follow. A machine, developer or other non-human caller is
// refused: a rotation is a standing delegation of a person's authority.
func (s *CredentialRotations) Create(ctx context.Context, actor Actor, scope domain.Scope, req CreateRotationRequest) (RotationView, error) {
	if err := requireRotationEnv(scope, "create"); err != nil {
		return RotationView{}, err
	}
	if req.ProviderID == "" || req.KeyID == "" || req.UsernameKeyID == "" {
		return RotationView{}, fmt.Errorf("%w: rotation create requires provider_id, key_id and username_key_id", domain.ErrInvalid)
	}
	if req.KeyID == req.UsernameKeyID {
		return RotationView{}, fmt.Errorf("%w: key_id and username_key_id must be different keys", domain.ErrInvalid)
	}
	if err := ValidateRotationBounds(req.IntervalSeconds, req.RevocationDelaySeconds); err != nil {
		return RotationView{}, err
	}
	id, err := newID("rot")
	if err != nil {
		return RotationView{}, err
	}
	now := store.CanonTime(s.now())
	var out RotationView
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		// Both keys are authorized as individual keys so a key-narrowed member
		// access rule applies; an invisible key is the uniform not-found.
		caller, proof, err := authorizeKey(ctx, az, actor, authz.OpRotationCreate, scope, authz.KeyByID(req.KeyID), now)
		if err != nil {
			return err
		}
		if _, err := az.AuthorizeKey(ctx, caller, authz.OpRotationCreate, scope, authz.KeyByID(req.UsernameKeyID)); err != nil {
			return err
		}
		if err := requireRotationHuman(caller); err != nil {
			return err
		}
		if err := requireUnattendedEnvironment(ctx, r, az, proof, scope); err != nil {
			return err
		}
		if err := requireRotationProvider(ctx, r, proof, req.ProviderID); err != nil {
			return err
		}
		if err := requireRotationKeys(ctx, r, proof, req.KeyID, req.UsernameKeyID, false); err != nil {
			return err
		}
		// Refused before the ceremony is consumed, so a refusal costs no reauth.
		if err := requireNoAuthorityDraft(ctx, r, proof, string(caller.Principal), req.KeyID, req.UsernameKeyID); err != nil {
			return err
		}
		if err := s.requireRotationCeremony(ctx, az, caller, scope, req.KeyID, req.UsernameKeyID); err != nil {
			return err
		}
		record, err := r.Rotations().Create(ctx, proof, store.RotationCreate{
			ID: id, ProviderID: req.ProviderID, KeyID: req.KeyID, UsernameKeyID: req.UsernameKeyID,
			IntervalSeconds: req.IntervalSeconds, RevocationDelaySeconds: req.RevocationDelaySeconds,
			AuthorityPrincipalID: string(caller.Principal),
			NextRunAt:            now.Add(time.Duration(req.IntervalSeconds) * time.Second), At: now,
		})
		if errors.Is(err, store.ErrRotationKeyInUse) {
			return ErrRotationKeyInUse
		}
		if err != nil {
			return err
		}
		if out, err = newRotationView(record); err != nil {
			return err
		}
		return insertRotationConfigured(ctx, r, proof, caller.Principal, rotationActionCreated, "", record)
	})
	return out, err
}

func insertRotationConfigured(ctx context.Context, r store.Repos, proof authz.Proof, principal domain.PrincipalID, action rotationAction, reason string, rec store.RotationRecord) error {
	payload := audit.Payload{
		"action": string(action), "rotation_id": rec.ID, "environment_id": rec.EnvironmentID, "provider_id": rec.ProviderID,
		"key_id": rec.KeyID, "username_key_id": rec.UsernameKeyID,
		"interval_seconds": rec.IntervalSeconds, "revocation_delay_seconds": rec.RevocationDelaySeconds,
		"authority_principal_id": rec.AuthorityPrincipalID, "generation": rec.Generation,
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if rec.CredentialExpiresAt != "" {
		payload["credential_expires_at"] = rec.CredentialExpiresAt
	}
	ev, err := domainEvent(ctx, audit.EventRotationConfigured, principal, audit.Object{Type: "rotation", ID: rec.ID}, payload)
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, proof, ev)
}

// pauseRotationsForProvider pauses every active rotation that mints from a
// provider that is being deleted or losing its admin credential, in the same
// transaction, and audits each with the reason. A rotation never stays active
// against a provider that can no longer mint: the run would fail every cycle
// while the previous credential's expiry approached unseen.
func pauseRotationsForProvider(ctx context.Context, r store.Repos, proof authz.Proof, principal domain.PrincipalID, providerID, reason string, at time.Time) error {
	paused, err := r.Rotations().PauseForProvider(ctx, proof, providerID, at)
	if err != nil {
		return err
	}
	return auditRotationPauses(ctx, r, proof, principal, reason, paused)
}

// Reasons recorded when a rotation is paused by a cascade rather than by a person.
const (
	rotationPauseEnvironmentProtected = "environment_protected"
	rotationPauseApprovalPolicy       = "approval_policy_enabled"
)

// pauseRotationsForEnvironment pauses the active rotations of one environment, or
// of every environment of the project when environmentID is empty (a project-wide
// approval policy), in the caller's transaction, and audits each pause with the
// reason and the caller as actor. A rotation publishes unattended, so it must not
// stay active in an environment that now needs a per-publish ceremony or an
// approval request: create and resume refuse such an environment, and this closes
// the same gap for a rotation that already exists.
func pauseRotationsForEnvironment(ctx context.Context, r store.Repos, proof authz.Proof, principal domain.PrincipalID, environmentID, reason string, at time.Time) error {
	paused, err := r.Rotations().PauseForEnvironment(ctx, proof, environmentID, at)
	if err != nil {
		return err
	}
	return auditRotationPauses(ctx, r, proof, principal, reason, paused)
}

func auditRotationPauses(ctx context.Context, r store.Repos, proof authz.Proof, principal domain.PrincipalID, reason string, paused []store.RotationRecord) error {
	for _, rec := range paused {
		if err := insertRotationConfigured(ctx, r, proof, principal, rotationActionPaused, reason, rec); err != nil {
			return err
		}
	}
	return nil
}

// Rotation list paging: a page is RotationListDefaultLimit rows unless the caller
// asks for fewer or up to RotationListMaxLimit, and NextAfter continues it.
const (
	RotationListDefaultLimit = 100
	RotationListMaxLimit     = store.RotationListMaxLimit
)

// RotationListRequest is one page of a rotation list. Deleted (disabled) history
// is shown only when asked for. After is the last id of the previous page.
type RotationListRequest struct {
	IncludeDisabled bool
	After           string
	Limit           int
}

// RotationPage is one page of rotations; NextAfter is empty on the last page.
type RotationPage struct {
	Items     []RotationView
	NextAfter string
}

// List returns one page of the environment's rotations in id order.
func (s *CredentialRotations) List(ctx context.Context, actor Actor, scope domain.Scope, req RotationListRequest) (RotationPage, error) {
	if err := requireRotationEnv(scope, "list"); err != nil {
		return RotationPage{}, err
	}
	limit := req.Limit
	if limit == 0 {
		limit = RotationListDefaultLimit
	}
	if limit < 0 || limit > RotationListMaxLimit {
		return RotationPage{}, fmt.Errorf("%w: limit must be between 1 and %d", domain.ErrInvalid, RotationListMaxLimit)
	}
	var out RotationPage
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpRotationInspect, scope, s.now())
		if err != nil {
			return err
		}
		// One extra row tells whether another page follows.
		rows, err := r.Rotations().List(ctx, proof, store.RotationListQuery{IncludeDisabled: req.IncludeDisabled, KeepOwed: true, After: req.After, Limit: limit + 1})
		if err != nil {
			return err
		}
		if len(rows) > limit {
			rows = rows[:limit]
			out.NextAfter = rows[limit-1].ID
		}
		// The summary is read for the returned page only, never for the extra row.
		if err := r.Rotations().Summarize(ctx, proof, rows); err != nil {
			return err
		}
		out.Items = make([]RotationView, 0, len(rows))
		for _, row := range rows {
			view, err := newRotationView(row)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, view)
		}
		return nil
	})
	return out, err
}

// Get returns one rotation.
func (s *CredentialRotations) Get(ctx context.Context, actor Actor, scope domain.Scope, rotationID string) (RotationView, error) {
	if err := requireRotationEnv(scope, "show", rotationID); err != nil {
		return RotationView{}, err
	}
	var out RotationView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpRotationInspect, scope, s.now())
		if err != nil {
			return err
		}
		rec, err := r.Rotations().Get(ctx, proof, rotationID)
		if err != nil {
			return err
		}
		out, err = summarizedRotationView(ctx, r.Rotations(), proof, rec)
		return err
	})
	return out, err
}

// Pause stops scheduling. S1 has no run, so nothing is in flight to finish; in
// S2 a pause rolls an unpublished run back and lets a published one finish, so
// revocation is never stranded. The credential already delivered stays valid
// until the expiry the view names. Pause needs publish on the environment, checked
// on both stored keys (authorizeRotationKeys), like every other transition.
func (s *CredentialRotations) Pause(ctx context.Context, actor Actor, scope domain.Scope, req RotationStateRequest) (RotationView, error) {
	return s.transition(ctx, actor, scope, req, rotationActionPaused)
}

// Resume re-arms a paused or halted rotation and recomputes its next run from
// now. It is a human act like create: the caller must hold the create formula on
// both keys (key-narrowed member rules apply) and consume the publish ceremony,
// and the recorded authority principal
// must still hold that formula too, so a rotation never resumes into a guaranteed
// halt and never lets a publish-only caller launder another person's authority.
// Every check runs under the resume operation, so a denial is recorded as one.
func (s *CredentialRotations) Resume(ctx context.Context, actor Actor, scope domain.Scope, req RotationStateRequest) (RotationView, error) {
	return s.transition(ctx, actor, scope, req, rotationActionResumed)
}

// Delete disables a rotation. It is soft: the row stays as history, its keys are
// released, and the delivered credential stays valid until the expiry the view
// names. A rotation with an in-flight run is refused (S1 has none; the check is
// the S2 contract).
func (s *CredentialRotations) Delete(ctx context.Context, actor Actor, scope domain.Scope, req RotationStateRequest) (RotationView, error) {
	return s.transition(ctx, actor, scope, req, rotationActionDeleted)
}

// RunNowRequest asks for a run now. ExpectedGeneration 0 means "the current
// generation". SkipOverlap makes the run revoke the superseded credential right
// after the publish (a revocation delay of zero for this run only).
// ConfirmDestructive must be true on every request: a run-now publishes a new
// credential off the schedule, and a skipped overlap revokes the old one at once.
type RunNowRequest struct {
	RotationID         string
	ExpectedGeneration int64
	SkipOverlap        bool
	ConfirmDestructive bool
}

// RunNowResult names the run the executor will start; it does not exist until
// the executor picks the request up, so a run list shows it only then.
type RunNowResult struct {
	RotationID  string
	RunID       string
	SkipOverlap bool
}

var (
	// ErrRotationRunInFlight refuses a run-now while the rotation has an unsettled run.
	ErrRotationRunInFlight = &rotationRefusal{"the rotation has a run in flight; let it finish first", domain.ErrConflict}
	// ErrRotationRunNowUnconfirmed refuses a run-now that did not confirm itself.
	ErrRotationRunNowUnconfirmed = &rotationRefusal{"a run-now publishes a new credential outside the schedule, and with skip-overlap revokes the superseded one at once; confirm it with confirm_destructive", domain.ErrConflict}
	// ErrRotationRestoreHeld refuses to start, re-arm, delete or abandon a rotation a restore holds.
	ErrRotationRestoreHeld = &rotationRefusal{"the rotation is held after a restore; reconcile its grants and provider credential, then release it with release-hold", domain.ErrConflict}
	// ErrRotationNotHeld refuses a release of a rotation no restore holds.
	ErrRotationNotHeld = &rotationRefusal{"the rotation is not held by a restore", domain.ErrConflict}
	// ErrRotationRunPending refuses a second run-now while one waits for the executor.
	ErrRotationRunPending = &rotationRefusal{"a run was already requested and has not started yet", domain.ErrConflict}
)

// RunNow requests a run outside the schedule. It is resume's equivalent: a
// signed-in person who holds edit, publish and reveal on both stored keys, under
// this operation (a denial is recorded as rotation.run_now; key-narrowed member
// rules apply), and who consumes the publish ceremony over both keys, so a
// publisher without reveal cannot start one. It executes nothing: the request makes the
// rotation due now, and the executor starts the run through its normal path with
// the recorded authority, the pre-publish Gate and the overlap, exactly as a
// scheduled run. A manual run bypasses none of them. Because it resets the next
// run time to now it also overrides a retry backoff; the attempt cap and the
// retry deadline still apply.
func (s *CredentialRotations) RunNow(ctx context.Context, actor Actor, scope domain.Scope, req RunNowRequest) (RunNowResult, error) {
	if err := requireRotationEnv(scope, "run-now", req.RotationID); err != nil {
		return RunNowResult{}, err
	}
	now := store.CanonTime(s.now())
	var out RunNowResult
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, cur, err := authorizeRotationKeys(ctx, r, az, actor, authz.OpRotationRunNow, scope, req.RotationID, now)
		if err != nil {
			return err
		}
		if err := requireRotationHuman(caller); err != nil {
			return err
		}
		if !req.ConfirmDestructive {
			return ErrRotationRunNowUnconfirmed
		}
		if req.ExpectedGeneration != 0 && req.ExpectedGeneration != cur.Generation {
			return ErrRotationStale
		}
		if cur.State != store.RotationStateActive {
			return fmt.Errorf("%w: only an active rotation can run now (state is %s)", domain.ErrConflict, cur.State)
		}
		if cur.RestoreHold {
			return ErrRotationRestoreHeld
		}
		open, err := r.Rotations().CountOpenRuns(ctx, proof, cur.ID)
		if err != nil {
			return err
		}
		if open > 0 {
			return ErrRotationRunInFlight
		}
		// The ceremony is consumed inside this transaction, so any refusal below,
		// the pending-request one included, rolls the consumption back with it.
		if err := s.requireRotationCeremony(ctx, az, caller, scope, cur.KeyID, cur.UsernameKeyID); err != nil {
			return err
		}
		runID, err := newID("rrn")
		if err != nil {
			return err
		}
		if err := r.Rotations().RequestRun(ctx, proof, store.RotationRunRequest{RotationID: cur.ID, RunID: runID, SkipOverlap: req.SkipOverlap, ExpectedGeneration: cur.Generation, At: now}); err != nil {
			switch {
			case errors.Is(err, store.ErrRotationRunPending):
				return ErrRotationRunPending
			case errors.Is(err, store.ErrConflict):
				return ErrRotationStale
			}
			return err
		}
		// A run the executor started between the first count and this write is
		// caught here: the write transaction is serializable (retried on a
		// serialization failure on PostgreSQL, BEGIN IMMEDIATE on SQLite), and the
		// UPDATE above holds the row the executor locks to start a run, so the
		// recount sees any run that committed first. Without it that run would leave
		// the request pending until the next scheduled start; the refusal rolls the
		// request back.
		if open, err := r.Rotations().CountOpenRuns(ctx, proof, cur.ID); err != nil {
			return err
		} else if open > 0 {
			return ErrRotationRunInFlight
		}
		ev, err := domainEvent(ctx, audit.EventRotationRunNow, caller.Principal, audit.Object{Type: "rotation", ID: cur.ID}, audit.Payload{"run_id": runID, "skip_overlap": req.SkipOverlap})
		if err != nil {
			return err
		}
		if err := r.Audit().InsertTenant(ctx, proof, ev); err != nil {
			return err
		}
		out = RunNowResult{RotationID: cur.ID, RunID: runID, SkipOverlap: req.SkipOverlap}
		return nil
	})
	return out, err
}

// RotationRunsRequest is one page of a rotation's runs, newest first; After is
// the last run id of the previous page.
type RotationRunsRequest struct {
	RotationID string
	After      string
	Limit      int
}

// RotationRunsPage is one page of runs; NextAfter is empty on the last page.
type RotationRunsPage struct {
	Items     []RotationRunView
	NextAfter string
}

// RotationRunView is one run's metadata with its stamps parsed, so a transport
// maps it without reaching the datastore's record type. CredentialValidUntil is
// the zero time when the run owes no drop or no expiry was recorded; the
// lower-bound flag is only ever true with an expiry.
type RotationRunView struct {
	ID                             string
	Trigger                        string
	Phase                          string
	PublishedRevision              int64
	RevocationDelaySeconds         int64
	StartedAt                      time.Time
	PhaseAt                        time.Time
	ErrorCode                      string
	NotConvergedAdapterTargets     *int64
	CredentialValidUntil           time.Time
	CredentialValidUntilLowerBound bool
}

// rotationRunView parses a stored run's stamps; a malformed one fails the read.
func rotationRunView(row store.RotationRunRecord) (RotationRunView, error) {
	started, err := time.Parse(time.RFC3339Nano, row.StartedAt)
	if err != nil {
		return RotationRunView{}, fmt.Errorf("service: parse run started_at: %w", err)
	}
	phaseAt, err := time.Parse(time.RFC3339Nano, row.PhaseAt)
	if err != nil {
		return RotationRunView{}, fmt.Errorf("service: parse run phase_at: %w", err)
	}
	view := RotationRunView{
		ID: row.ID, Trigger: row.Trigger, Phase: row.Phase, PublishedRevision: row.PublishedRevision,
		RevocationDelaySeconds: row.RevocationDelaySeconds, StartedAt: started, PhaseAt: phaseAt,
		ErrorCode: rotationErrorWord(row.ErrorCode), NotConvergedAdapterTargets: row.NotConvergedAdapterTargets,
	}
	if row.CredentialValidUntil != "" {
		until, err := time.Parse(time.RFC3339Nano, row.CredentialValidUntil)
		if err != nil {
			return RotationRunView{}, fmt.Errorf("service: parse run credential_valid_until: %w", err)
		}
		view.CredentialValidUntil = until
		view.CredentialValidUntilLowerBound = row.CredentialValidUntilLowerBound
	}
	return view, nil
}

// Runs lists a rotation's runs: phase, trigger, revision and the fixed-word code
// of the last failure. It carries no secret, role handle or provider detail.
func (s *CredentialRotations) Runs(ctx context.Context, actor Actor, scope domain.Scope, req RotationRunsRequest) (RotationRunsPage, error) {
	if err := requireRotationEnv(scope, "runs", req.RotationID); err != nil {
		return RotationRunsPage{}, err
	}
	limit := req.Limit
	if limit == 0 {
		limit = RotationListDefaultLimit
	}
	if limit < 0 || limit > RotationListMaxLimit {
		return RotationRunsPage{}, fmt.Errorf("%w: limit must be between 1 and %d", domain.ErrInvalid, RotationListMaxLimit)
	}
	var out RotationRunsPage
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, proof, err := authorize(ctx, az, actor, authz.OpRotationRuns, scope, s.now())
		if err != nil {
			return err
		}
		if _, err := r.Rotations().Get(ctx, proof, req.RotationID); err != nil {
			return err
		}
		rows, err := r.Rotations().ListRuns(ctx, proof, store.RotationRunQuery{RotationID: req.RotationID, After: req.After, Limit: limit + 1})
		if err != nil {
			return err
		}
		if len(rows) > limit {
			rows = rows[:limit]
			out.NextAfter = rows[limit-1].ID
		}
		out.Items = make([]RotationRunView, 0, len(rows))
		for _, row := range rows {
			view, err := rotationRunView(row)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, view)
		}
		return nil
	})
	return out, err
}

func rotationActionOperation(action rotationAction) (authz.Operation, bool) {
	switch action {
	case rotationActionPaused:
		return authz.OpRotationPause, true
	case rotationActionResumed:
		return authz.OpRotationResume, true
	case rotationActionDeleted:
		return authz.OpRotationDelete, true
	}
	return "", false
}

func (s *CredentialRotations) transition(ctx context.Context, actor Actor, scope domain.Scope, req RotationStateRequest, action rotationAction) (RotationView, error) {
	op, ok := rotationActionOperation(action)
	if !ok {
		return RotationView{}, fmt.Errorf("service: unknown rotation action %q", action)
	}
	if err := requireRotationEnv(scope, string(action), req.RotationID); err != nil {
		return RotationView{}, err
	}
	now := store.CanonTime(s.now())
	var out RotationView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, cur, err := authorizeRotationKeys(ctx, r, az, actor, op, scope, req.RotationID, now)
		if err != nil {
			return err
		}
		if req.ExpectedGeneration != 0 && req.ExpectedGeneration != cur.Generation {
			return ErrRotationStale
		}
		next := store.RotationSetState{RotationID: cur.ID, ExpectedGeneration: cur.Generation, At: now}
		switch action {
		case rotationActionPaused:
			if cur.State != store.RotationStateActive {
				return fmt.Errorf("%w: only an active rotation can be paused (state is %s)", domain.ErrConflict, cur.State)
			}
			next.State = store.RotationStatePaused
		case rotationActionResumed:
			// A halted rotation (the executor's named stop after a failed gate) is
			// resumed exactly like a paused one: the full resume authorization below.
			if cur.State != store.RotationStatePaused && cur.State != store.RotationStateHalted {
				return fmt.Errorf("%w: only a paused or halted rotation can be resumed (state is %s)", domain.ErrConflict, cur.State)
			}
			if cur.RestoreHold {
				return ErrRotationRestoreHeld
			}
			if err := s.authorizeResume(ctx, r, az, proof, caller, scope, cur); err != nil {
				return err
			}
			next.State = store.RotationStateActive
			// The delivered credential keeps lapsing while the rotation is
			// stopped, so the first run is not a whole interval away when the
			// credential cannot wait that long.
			var delivered time.Time
			if cur.CredentialExpiresAt != "" {
				if delivered, err = time.Parse(time.RFC3339Nano, cur.CredentialExpiresAt); err != nil {
					return fmt.Errorf("service: rotation %s credential expiry %q: %w", cur.ID, cur.CredentialExpiresAt, err)
				}
			}
			next.NextRunAt = rotationResumeNext(now, time.Duration(cur.IntervalSeconds)*time.Second, time.Duration(cur.RevocationDelaySeconds)*time.Second, delivered)
		case rotationActionDeleted:
			// Abandoning closes runs by hand, a standing-delegation decision like
			// create, resume and run-now, so only a signed-in person may ask for it,
			// whether or not a run is in flight.
			if req.Abandon {
				if err := requireRotationHuman(caller); err != nil {
					return err
				}
			}
			if cur.State == store.RotationStateDisabled {
				return fmt.Errorf("%w: the rotation is already deleted", domain.ErrConflict)
			}
			// A delete disables the row, which clears the hold, and an abandon turns
			// minting and minted runs into quarantine. The reconciler would then
			// find the rotation unheld and drop a candidate the live instance may
			// have published. Only the human release of the hold lifts it.
			if cur.RestoreHold {
				return ErrRotationRestoreHeld
			}
			open, err := r.Rotations().CountOpenRuns(ctx, proof, cur.ID)
			if err != nil {
				return err
			}
			if open > 0 {
				if !req.Abandon {
					return fmt.Errorf("%w: the rotation has a run in flight; let it finish or abandon it", domain.ErrConflict)
				}
				// Abandon closes every unsettled run, loudly. A run before its
				// publish becomes quarantine and keeps its candidate for the
				// reconciler to confirm gone; a run past it keeps its delivered
				// credential live and, when it still owes the superseded role's
				// revoke, becomes quarantine for the revoke-obligation loop to
				// settle; a published run that owes no revoke settles to done.
				// Nothing here revokes a delivered credential. The audit rows are
				// written for exactly the runs the abandon statement changed.
				// The abandon time is read inside the transaction attempt, not taken
				// from the request's start: a retry after a conflict with an in-flight
				// publish must observe a time after that publish committed, because a
				// run still in `published` is given this time plus the delay as its
				// overlap deadline.
				abandonAt := store.CanonTime(s.now())
				abandoned, err := r.Rotations().AbandonOpenRuns(ctx, proof, cur.ID, abandonAt, time.Duration(cur.RevocationDelaySeconds)*time.Second)
				if err != nil {
					return err
				}
				for _, run := range abandoned {
					// Every run the statement closed gets its own row: a run left
					// owing a role is quarantined, and a published run that owes
					// nothing settles to done with an outcome row that says why.
					eventType := audit.EventRotationQuarantined
					payload := audit.Payload{"run_id": run.ID, "candidate_handle": run.CandidateHandle}
					if run.SupersededHandle != "" {
						payload["superseded_handle"] = run.SupersededHandle
					}
					if run.Phase != "quarantine" {
						eventType = audit.EventRotationRunOutcome
						payload = audit.Payload{"run_id": run.ID, "phase": "abandon", "handle": run.CandidateHandle, "error_code": "abandoned"}
					}
					ev, err := newAuditEvent(ctx, eventType, caller.Principal, audit.Object{Type: "rotation", ID: cur.ID}, audit.OutcomeSuccess, run.ID, payload)
					if err != nil {
						return err
					}
					if err := r.Audit().InsertTenant(ctx, proof, ev); err != nil {
						return err
					}
				}
			}
			next.State = store.RotationStateDisabled
		default:
			return fmt.Errorf("service: unknown rotation action %q", action)
		}
		rec, err := r.Rotations().SetState(ctx, proof, next)
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrRotationStale
			}
			return err
		}
		if out, err = summarizedRotationView(ctx, r.Rotations(), proof, rec); err != nil {
			return err
		}
		return insertRotationConfigured(ctx, r, proof, caller.Principal, action, "", rec)
	})
	return out, err
}

// ReleaseHold clears the restore hold a restore put on a rotation (#148, decision
// 9). It is a human act with resume's authorization (both keys, key-narrowed rules
// apply) and consumes the publish ceremony, because the next scheduled run mints
// and publishes unattended again. It changes nothing else: the state, the schedule,
// every run and every provider role stay as they are, so releasing never drops a
// superseded role early and never resumes a paused or halted rotation. A run held
// for its superseded role (published_versions_changed) is not released by this; it
// ends only on its own exits. An active rotation is released only if the executor's
// first Gate would pass it now (the same checks, one implementation); a paused or
// halted one is released without them, since resume re-checks everything.
func (s *CredentialRotations) ReleaseHold(ctx context.Context, actor Actor, scope domain.Scope, req RotationStateRequest) (RotationView, error) {
	if err := requireRotationEnv(scope, "release-hold", req.RotationID); err != nil {
		return RotationView{}, err
	}
	now := store.CanonTime(s.now())
	var out RotationView
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, proof, cur, err := authorizeRotationKeys(ctx, r, az, actor, authz.OpRotationReleaseHold, scope, req.RotationID, now)
		if err != nil {
			return err
		}
		if err := requireRotationHuman(caller); err != nil {
			return err
		}
		if req.ExpectedGeneration != 0 && req.ExpectedGeneration != cur.Generation {
			return ErrRotationStale
		}
		if cur.State == store.RotationStateDisabled {
			return fmt.Errorf("%w: the rotation is deleted", domain.ErrConflict)
		}
		if !cur.RestoreHold {
			return ErrRotationNotHeld
		}
		// Before the ceremony is consumed: a refusal here must not burn it. Only an
		// active rotation is checked, and by the Gate's own checks: it is the one
		// the executor would start. A paused or halted one stays inert until resume,
		// which re-checks everything, so it can always be released (and the restore
		// count can reach zero) even when its provider or keys are gone.
		if cur.State == store.RotationStateActive {
			if err := requireRotationStartable(ctx, r, az, proof, caller, domain.PrincipalID(cur.AuthorityPrincipalID), authz.OpRotationReleaseHold, scope, cur, true); err != nil {
				return err
			}
		}
		if err := s.requireRotationCeremony(ctx, az, caller, scope, cur.KeyID, cur.UsernameKeyID); err != nil {
			return err
		}
		if err := r.Rotations().ReleaseHold(ctx, proof, cur.ID, now); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrRotationNotHeld
			}
			return err
		}
		rec, err := r.Rotations().Get(ctx, proof, cur.ID)
		if err != nil {
			return err
		}
		if out, err = summarizedRotationView(ctx, r.Rotations(), proof, rec); err != nil {
			return err
		}
		return insertRotationConfigured(ctx, r, proof, caller.Principal, rotationActionHoldReleased, "", rec)
	})
	return out, err
}

// requireRotationStartable is the one set of checks that decides whether a run
// could start: the environment still runs unattended, the provider can still mint,
// the keys still exist and fit, the recorded authority principal still holds op's
// formula (resume's: edit, publish and reveal) on both keys, and, when drafts is
// set, that principal holds no draft on either key. The executor's Gate, resume and
// an active rotation's release-hold all call it, so a verb that re-arms a rotation
// can never accept one the Gate would halt. actor is who a denial is recorded
// against; op is the operation the refusal is recorded under.
func requireRotationStartable(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, proof authz.Proof, actor authz.Identity, authority domain.PrincipalID, op authz.Operation, scope domain.Scope, rec store.RotationRecord, drafts bool) error {
	if err := requireUnattendedEnvironment(ctx, r, az, proof, scope); err != nil {
		return err
	}
	if err := requireRotationProvider(ctx, r, proof, rec.ProviderID); err != nil {
		return err
	}
	if err := requireRotationKeys(ctx, r, proof, rec.KeyID, rec.UsernameKeyID, true); err != nil {
		return err
	}
	for _, id := range []string{rec.KeyID, rec.UsernameKeyID} {
		// The actor stays the actor of a denial; AuthorityID names the recorded
		// principal. No identity is synthesized for the authority.
		key := authz.KeyByID(id)
		holds, err := az.RecordedPrincipalHolds(ctx, actor, authority, op, scope, &key)
		if err != nil {
			return err
		}
		if !holds {
			return ErrRotationAuthorityLost
		}
	}
	if drafts {
		return requireNoAuthorityDraft(ctx, r, proof, string(authority), rec.KeyID, rec.UsernameKeyID)
	}
	return nil
}

// authorizeResume is everything a resume needs beyond the environment's publish
// formula, in the order that keeps each refusal honest: only a signed-in person
// resumes; the environment still runs unattended; the provider can still mint;
// the keys still exist and are fit to receive the credential (the key-aware
// check that ran first already answers not-found for a deleted key to a
// key-narrowed caller; an environment-wide caller reaches the named refusal); the recorded authority principal still holds the resume formula
// (edit, publish and reveal at the environment) on both keys; the authority holds no draft on either key; and the publish ceremony is
// consumed.
func (s *CredentialRotations) authorizeResume(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, proof authz.Proof, caller authz.Identity, scope domain.Scope, rec store.RotationRecord) error {
	if err := requireRotationHuman(caller); err != nil {
		return err
	}
	if err := requireRotationStartable(ctx, r, az, proof, caller, domain.PrincipalID(rec.AuthorityPrincipalID), authz.OpRotationResume, scope, rec, true); err != nil {
		return err
	}
	return s.requireRotationCeremony(ctx, az, caller, scope, rec.KeyID, rec.UsernameKeyID)
}
