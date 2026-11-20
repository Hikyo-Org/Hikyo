package service

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// RotationExecutor runs scheduled credential rotation (#148, slice S2a): it
// creates a run for a due rotation, mints a candidate role, publishes it as the
// rotation's authority, waits out the overlap and revokes the superseded role.
//
// Every transition is an INTENT then an OUTCOME effect under the run row's own
// lease, exactly like a dynamic lease; a worker that lost its lease affects zero
// rows. The candidate password exists only in this process's memory and in the
// ONE transaction that stages, publishes and records it; a crash after minting
// always rolls the candidate back. The full design, its crash table and the
// schedule proof are in docs/handoff/148-credential-rotation.md.
type RotationExecutor struct {
	DB        *store.DB
	Runtime   *store.RotationRuntime
	Values    *Values
	Revisions *Revisions
	Keyring   *crypto.Keyring
	// ProviderFactory opens the dynamic provider a run mints from.
	ProviderFactory dynamic.Factory
	// ProviderDeadline bounds one provider round-trip; it must be shorter than
	// the claim lease so a slow provider cannot outlive the crash fence.
	ProviderDeadline time.Duration
	Now              func() time.Time
	// AfterExtendSupersessionCheck is a conformance seam: it runs once an extend
	// has recorded its intent and found no later publish, before the extend does
	// anything else. A test publishes a replacement run there to prove that the
	// replacement's revoke read counts the extend as uncertain. nil in production.
	AfterExtendSupersessionCheck func()
	// AfterExtendHoldRead is a conformance seam: it runs after an extend refused by
	// a definite gate error has read that no restore holds the rotation, before the
	// halting settle. A test lands the hold there to prove the settle decides on the
	// hold at write time. nil in production.
	AfterExtendHoldRead func()
	// RestoreAssertionFault is a conformance seam: a non-nil error it returns stands
	// in for a failed read of the restore assertion at the revoke step.
	RestoreAssertionFault func() error
	// BeforeHeldRead is a conformance seam: it runs immediately before the
	// executor's own restore-hold read at a candidate-drop or recovery site
	// (recover, reconcile:entry, reconcile:predrop, drop_orphan, drop_candidate),
	// so a test lands a hold after the claim and proves the read is the guard.
	// Production leaves it nil.
	BeforeHeldRead func(site string)
	// Log receives the executor's operational faults as redacted error classes,
	// never the cause text. nil logs to the default logger.
	Log *slog.Logger
}

func (e *RotationExecutor) now() time.Time { return store.CanonTime(nowOr(e.Now)) }

func (e *RotationExecutor) logger() *slog.Logger {
	if e.Log != nil {
		return e.Log
	}
	return slog.Default()
}

// warn records a fault with a fixed-word class. The cause's text is never logged:
// a driver or provider error can carry a host, a role name or a credential
// fragment.
func (e *RotationExecutor) warn(msg string, run store.RotationRun, op string, err error) {
	e.logger().Warn(msg, "rotation_id", run.RotationID, "run_id", run.ID, "op", op, "class", rotationErrClass(err))
}

// attributed marks ctx so every audit row the executor's transactions write
// (value.staged, revision.published, an authorization denial) carries system
// origin, the recorded authority principal and the run id, while the acting
// principal stays the authority the publish truly rides.
func attributed(ctx context.Context, authority, runID string) context.Context {
	return audit.WithContext(ctx, audit.Context{Origin: audit.OriginSystem, AuthorityID: authority, CorrelationID: runID})
}

// providerOpenError is a failure to open a run's provider, tagged with the stage
// that failed. Its text names the stage only; the cause is reachable through
// errors.Is and never printed.
type providerOpenError struct {
	stage string
	err   error
}

func (e *providerOpenError) Error() string {
	return "service: rotation provider open failed at " + e.stage
}
func (e *providerOpenError) Unwrap() error { return e.err }

// providerInfraFault reports whether opening the provider failed in the
// executor's own datastore or keyring, rather than for anything about the
// provider: such a fault never reached the provider and is no attempt. A
// credential that cannot be decrypted is NOT infrastructure: the stored ciphertext
// is unreadable and retrying it will not heal it, so it is a definite fault that
// counts as an attempt and exhausts the retries loudly.
func providerInfraFault(err error) bool {
	var open *providerOpenError
	if !errors.As(err, &open) || errors.Is(err, store.ErrNoProviderCredential) || errors.Is(err, store.ErrNotFound) {
		return false
	}
	return open.stage == "material" || open.stage == "keyring"
}

// RotationErrorClass is rotationErrClass for the worker loop outside this
// package: a class in fixed words, never the error's own text.
func RotationErrorClass(err error) string {
	class := rotationErrClass(err)
	// A claim or select error is the executor's own datastore failing; it names no
	// run, so an unclassified one is a datastore fault.
	if stage := RotationSweepStage(err); class == "internal" && (stage == SweepStageClaim || stage == SweepStageSelect) {
		return "datastore"
	}
	return class
}

// rotationErrClass names an error's class in fixed words for logs.
func rotationErrClass(err error) string {
	var open *providerOpenError
	var refusal *rotationRefusal
	switch {
	case errors.As(err, &open):
		return "provider_open_" + open.stage
	case errors.Is(err, store.ErrNoProviderCredential):
		return "no_provider_credential"
	case errors.Is(err, dynamic.ErrUnreachable):
		return "provider_unreachable"
	case errors.Is(err, dynamic.ErrRefused):
		return "provider_refused"
	case errors.Is(err, dynamic.ErrAmbiguous):
		return "provider_ambiguous"
	case errors.Is(err, errRotationBudget):
		return "publish_budget"
	case errors.Is(err, errRotationHeld):
		return "rotation_held"
	case errors.Is(err, errRotationInactive):
		return "rotation_inactive"
	case errors.Is(err, ErrRotationAuthorityLost):
		return "authority_lost"
	case errors.As(err, &refusal):
		return "refused"
	case errors.Is(err, store.ErrRotationFenceLost):
		return "fence_lost"
	case errors.Is(err, store.ErrNotFound), errors.Is(err, domain.ErrNotFound):
		return "not_found"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	return "internal"
}

func (e *RotationExecutor) providerDeadline() time.Duration {
	if e.ProviderDeadline <= 0 {
		return 10 * time.Second
	}
	return e.ProviderDeadline
}

// Run error codes recorded in last_error and the outcome audit's error_code. They
// are fixed words, never provider text, so no credential or host detail leaks.
const (
	rotCodeInterrupted     = "interrupted"
	rotCodeProvider        = "provider_unavailable"
	rotCodeMintFailed      = "mint_failed"
	rotCodeMintAmbiguous   = "mint_ambiguous"
	rotCodePublishFailed   = "publish_failed"
	rotCodeBudget          = "publish_budget_exceeded"
	rotCodePaused          = "rotation_paused"
	rotCodeHeld            = "rotation_held"
	rotCodeDropFailed      = "drop_failed"
	rotCodeOwnsObjects     = "role_owns_objects"
	rotCodeVersionsChanged = "published_versions_changed"
	rotCodeRevokeFailed    = "revoke_failed"
	rotCodeExtendFailed    = "extend_failed"
	rotCodeExtendSkipped   = "extend_skipped"
	rotCodeExpired         = "expired"
	// rotCodeInfra marks a run that failed only because the executor's own
	// datastore or keyring faulted before the provider was touched. It is not
	// counted as an attempt toward the attempt cap, but it backs off exponentially.
	rotCodeInfra = "infrastructure_unavailable"
)

// Halt reasons, shared with the rotation.configured audit's reason enum.
const (
	rotHaltExhausted = "retries_exhausted"
	rotHaltAuthority = "authority_lost"
	rotHaltGate      = "gate_failed"
)

var (
	errRotationInactive = errors.New("service: the rotation is no longer active at the generation this run started under")
	// errRotationHeld is errRotationInactive for a rotation a restore holds, so
	// every inactive check still applies and a record can name the hold itself.
	errRotationHeld   = fmt.Errorf("%w: a restore holds it", errRotationInactive)
	errRotationBudget = errors.New("service: the run exceeded its publish budget")
)

// RunRotationSweep does one unit of executor work and reports whether it did any.
// It claims a due run first (an in-flight run to recover, a settled run owed a
// revoke, a pre-publish run whose candidate is unconfirmed, a delivered
// credential due an extension) and otherwise starts a run for a due rotation.
// It is the body of the worker loop; every node runs it, because every effect is
// claimed under a run's own lease.
func (e *RotationExecutor) RunRotationSweep(ctx context.Context, workerID string) (bool, error) {
	if e.DB == nil || e.Runtime == nil || e.Values == nil || e.Revisions == nil || e.Keyring == nil || e.ProviderFactory == nil {
		return false, &SweepError{Stage: SweepStageMisconfigured, Err: errors.New("service: rotation executor requires its datastore, runtime, value and revision services, keyring and provider factory")}
	}
	now := e.now()
	run, claimed, err := e.Runtime.ClaimDueRun(ctx, workerID, now, now.Add(rotationLeaseTerm), now.Add(-e.providerDeadline()))
	if err != nil {
		return false, &SweepError{Stage: SweepStageClaim, Err: err}
	}
	if claimed {
		if err := e.processClaimed(ctx, run); err != nil {
			return true, &SweepError{Stage: SweepStageRun, Err: err}
		}
		return true, nil
	}
	due, ok, err := e.Runtime.DueRotation(ctx, now)
	if err != nil {
		return false, &SweepError{Stage: SweepStageSelect, Err: err}
	}
	if !ok {
		return false, nil
	}
	started, err := e.startRun(ctx, workerID, due)
	if err != nil {
		return started, &SweepError{Stage: SweepStageRun, Err: err}
	}
	return started, nil
}

// The stages a sweep error can come from, in fixed words for logs.
const (
	SweepStageMisconfigured = "misconfigured"
	SweepStageClaim         = "claim"
	SweepStageSelect        = "select"
	SweepStageRun           = "run"
)

// SweepError names the stage of a sweep that failed. Only the claim and select
// stages hold no run, so only they (and a misconfiguration) are loggable by their
// own text; a run's error can carry tenant, provider or driver detail.
type SweepError struct {
	Stage string
	Err   error
}

func (e *SweepError) Error() string { return "rotation sweep " + e.Stage + ": " + e.Err.Error() }
func (e *SweepError) Unwrap() error { return e.Err }

// RotationSweepStage is the stage of a sweep error, "run" for any other error.
func RotationSweepStage(err error) string {
	var sweep *SweepError
	if errors.As(err, &sweep) {
		return sweep.Stage
	}
	return SweepStageRun
}

func (e *RotationExecutor) processClaimed(ctx context.Context, run store.RotationRun) error {
	ctx = attributed(ctx, run.AuthorityPrincipalID, run.ID)
	switch run.Phase {
	case "minting", "minted":
		return e.recoverInFlight(ctx, run)
	case "published", "overlap", "revoking", "revoke_refused":
		return e.revoke(ctx, run)
	case "failed", "quarantine":
		if run.PublishedRevision == 0 {
			return e.reconcileCandidate(ctx, run)
		}
		return e.revoke(ctx, run)
	case "done":
		_, err := e.extendDelivered(ctx, run, []string{"done"})
		return err
	default:
		return fmt.Errorf("service: claimed rotation run %s in unexpected phase %q", run.ID, run.Phase)
	}
}

// ---------------------------------------------------------------- audit rows

func (e *RotationExecutor) audit(typ audit.EventType, outcome audit.Outcome, chain domain.Scope, rotationID, runID, authority string, now time.Time, payload audit.Payload) (store.RotationAudit, error) {
	id, err := audit.NewEventID()
	if err != nil {
		return store.RotationAudit{}, err
	}
	ev := audit.Event{
		ID: id, Type: typ, SchemaVersion: 1, OccurredAt: now,
		Actor: audit.Actor{Class: audit.ActorSystem}, Origin: audit.OriginSystem,
		Object:  audit.Object{Type: "rotation", ID: rotationID},
		Outcome: outcome, CorrelationID: runID, AuthorityID: authority, Payload: payload,
	}
	if err := audit.Validate(ev, audit.TrailTenant, chain); err != nil {
		return store.RotationAudit{}, err
	}
	body, err := json.Marshal(payload, jsonwire.Stable)
	if err != nil {
		return store.RotationAudit{}, err
	}
	return store.RotationAudit{ID: id, Type: string(typ), Outcome: string(outcome), Payload: body}, nil
}

func (e *RotationExecutor) intent(run store.RotationRun, kind string, now time.Time) (store.RotationEffectIntent, error) {
	a, err := e.audit(audit.EventRotationRunIntent, audit.OutcomeIntent, run.Chain(), run.RotationID, run.ID, run.AuthorityPrincipalID, now,
		audit.Payload{"run_id": run.ID, "phase": kind, "handle": handleFor(run, kind)})
	if err != nil {
		return store.RotationEffectIntent{}, err
	}
	effectID, err := newID("ref")
	if err != nil {
		return store.RotationEffectIntent{}, err
	}
	return store.RotationEffectIntent{EffectID: effectID, Kind: kind, Audit: a}, nil
}

func (e *RotationExecutor) outcome(run store.RotationRun, effectID, kind string, result audit.Outcome, code string, now time.Time) (*store.RotationEffectOutcome, error) {
	return e.outcomeWith(run, effectID, kind, result, code, now, nil)
}

// outcomeWith is outcome with extra payload fields (the publish records its
// revision).
func (e *RotationExecutor) outcomeWith(run store.RotationRun, effectID, kind string, result audit.Outcome, code string, now time.Time, extra audit.Payload) (*store.RotationEffectOutcome, error) {
	payload := audit.Payload{"run_id": run.ID, "phase": kind, "handle": handleFor(run, kind)}
	if code != "" {
		payload["error_code"] = code
	}
	for k, v := range extra {
		payload[k] = v
	}
	a, err := e.audit(audit.EventRotationRunOutcome, result, run.Chain(), run.RotationID, run.ID, run.AuthorityPrincipalID, now, payload)
	if err != nil {
		return nil, err
	}
	return &store.RotationEffectOutcome{EffectID: effectID, Outcome: string(result), Audit: a}, nil
}

// handleFor names the role an effect acts on: the superseded role for a revoke,
// the candidate for everything else. Role names are public metadata.
func handleFor(run store.RotationRun, kind string) string {
	if kind == "revoke" && run.SupersededHandle != "" {
		return run.SupersededHandle
	}
	return run.CandidateHandle
}

func (e *RotationExecutor) quarantined(run store.RotationRun, now time.Time) (store.RotationAudit, error) {
	payload := audit.Payload{"run_id": run.ID, "candidate_handle": run.CandidateHandle}
	if run.SupersededHandle != "" {
		payload["superseded_handle"] = run.SupersededHandle
	}
	return e.audit(audit.EventRotationQuarantined, audit.OutcomeSuccess, run.Chain(), run.RotationID, run.ID, run.AuthorityPrincipalID, now, payload)
}

// haltAudit is the rotation.configured row a halt writes. generation is the
// value the halt moves the rotation to.
func (e *RotationExecutor) haltAudit(chain domain.Scope, rot haltSubject, reason string, now time.Time) (store.RotationAudit, error) {
	return e.audit(audit.EventRotationConfigured, audit.OutcomeSuccess, chain, rot.ID, "", rot.Authority, now, audit.Payload{
		"action": "halted", "rotation_id": rot.ID, "environment_id": string(chain.Env), "provider_id": rot.ProviderID,
		"key_id": rot.KeyID, "username_key_id": rot.UsernameKeyID,
		"interval_seconds": rot.IntervalSeconds, "revocation_delay_seconds": rot.DelaySeconds,
		"authority_principal_id": rot.Authority, "generation": rot.Generation + 1, "reason": reason,
	})
}

type haltSubject struct {
	ID, ProviderID, KeyID, UsernameKeyID, Authority string
	IntervalSeconds, DelaySeconds, Generation       int64
}

func subjectOfRun(run store.RotationRun) haltSubject {
	return haltSubject{ID: run.RotationID, ProviderID: run.ProviderID, KeyID: run.KeyID, UsernameKeyID: run.UsernameKeyID, Authority: run.AuthorityPrincipalID,
		IntervalSeconds: run.IntervalSeconds, DelaySeconds: run.RotationDelaySeconds, Generation: run.RotationGeneration}
}

func subjectOfDue(d store.DueRotation) haltSubject {
	return haltSubject{ID: d.ID, ProviderID: d.ProviderID, KeyID: d.KeyID, UsernameKeyID: d.UsernameKeyID, Authority: d.AuthorityPrincipalID,
		IntervalSeconds: d.IntervalSeconds, DelaySeconds: d.RevocationDelaySeconds, Generation: d.Generation}
}

// ---------------------------------------------------------------- the Gate

// rotationGateReason maps a gate refusal to the halt reason it earns, and false
// for an error that is not a definite refusal (a datastore fault, a maintenance
// window): those retry, never halt.
func rotationGateReason(err error) (string, bool) {
	var refusal *rotationRefusal
	switch {
	case errors.Is(err, ErrRotationAuthorityLost):
		return rotHaltAuthority, true
	case errors.As(err, &refusal), errors.Is(err, errRotationNotPublished):
		return rotHaltGate, true
	case errors.Is(err, domain.ErrNotFound), errors.Is(err, domain.ErrUnauthorized):
		// Uniform: either the authority lost access or a key or the environment is
		// gone. Both stop the rotation, loudly.
		return rotHaltAuthority, true
	}
	if errors.Is(err, ErrProtectedDestination) {
		return rotHaltGate, true
	}
	return "", false
}

// anyGeneration skips the gate's generation check: a delivered credential's own
// run belongs to the generation it was minted under, which a pause, halt or resume
// has since moved past.
const anyGeneration int64 = -1

// gateCheck selects what a gate verifies beyond the always-on checks.
type gateCheck struct {
	// generation is the rotation generation the caller started under, or
	// anyGeneration.
	generation int64
	// drafts also refuses an authority that holds a pending draft on either key
	// (the pre-start gate; the publish checks it again under the project lock).
	drafts bool
}

// gateWithin is the full pre-publish Gate, run inside a transaction: the
// rotation is still active (at the generation the run started under, unless the
// check allows any); the authority still holds edit, publish and reveal on both
// keys; the keys exist, are not grouped, and the password key is still secret;
// the environment is still unattended; the provider can still mint. The caller's
// proof is the rotation-run authorization of the recorded authority principal,
// which is what lets the publish that follows act as that principal.
func (e *RotationExecutor) gateWithin(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, caller authz.Identity, scope domain.Scope, proof authz.Proof, rotationID string, check gateCheck) error {
	rec, err := r.Rotations().Get(ctx, proof, rotationID)
	if err != nil {
		return err
	}
	switch {
	case rec.State == store.RotationStateActive && rec.RestoreHold:
		return errRotationHeld
	case rec.State != store.RotationStateActive || (check.generation != anyGeneration && rec.Generation != check.generation):
		return errRotationInactive
	}
	return requireRotationStartable(ctx, r, az, proof, caller, caller.Principal, authz.OpRotationResume, scope, rec, check.drafts)
}

// authorizeRotationRun is the executor's own publish authorization for the
// recorded authority: OpRotationRun on both of the rotation's keys, so an
// authority that holds publish only through member rules narrowed to those keys
// passes exactly as it does resume's check (RecordedPrincipalHolds on both keys),
// and an environment-wide holder passes for every key. A keyless check could never
// be satisfied by a key-narrowed rule. The caller stays the authority principal;
// no identity is synthesized for it.
func authorizeRotationRun(ctx context.Context, az *authz.TxAuthorizer, caller authz.Identity, scope domain.Scope, keyID, usernameKeyID string) (authz.Proof, error) {
	var proof authz.Proof
	for _, id := range []string{keyID, usernameKeyID} {
		held, err := az.AuthorizeKey(ctx, caller, authz.OpRotationRun, scope, authz.KeyByID(id))
		if err != nil {
			return held, err
		}
		proof = held
	}
	return proof, nil
}

// gate runs gateWithin in its own transaction, as the recorded authority: before
// a run is created, and before a delivered credential is extended.
func (e *RotationExecutor) gate(ctx context.Context, chain domain.Scope, rotationID, authority, keyID, usernameKeyID string, check gateCheck, now time.Time) error {
	actor := LocalPrincipal(domain.PrincipalID(authority))
	return tx.Write(ctx, e.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		az.SetClock(now)
		proof, err := authorizeRotationRun(ctx, az, caller, chain, keyID, usernameKeyID)
		if err != nil {
			return err
		}
		return e.gateWithin(ctx, r, az, caller, chain, proof, rotationID, check)
	})
}

// ---------------------------------------------------------------- schedule

func (e *RotationExecutor) window(ctx context.Context, d store.DueRotation, now time.Time) (rotationWindow, error) {
	cycle, err := e.Runtime.Cycle(ctx, d)
	if err != nil {
		return rotationWindow{}, err
	}
	interval := time.Duration(d.IntervalSeconds) * time.Second
	delay := time.Duration(d.RevocationDelaySeconds) * time.Second
	switch {
	case cycle.OwnPublishedRevision != 0:
		published, err := e.Runtime.PublishedAt(ctx, store.RotationRun{OrgID: d.OrgID, ProjectID: d.ProjectID, EnvironmentID: d.EnvironmentID, PublishedRevision: cycle.OwnPublishedRevision})
		if err != nil {
			return rotationWindow{}, err
		}
		return rotationCycleWindow(published, interval), nil
	case !cycle.KeyEverPublished || cycle.DeliveredValidUntil.IsZero():
		return rotationWindow{}, nil
	default:
		// The cycle began over a credential it did not publish (a resumed or
		// recreated rotation): D comes from that credential's own VALID UNTIL.
		return rotationInheritedWindow(now, cycle.DeliveredValidUntil, delay, cycle.FirstRunAt.IsZero()), nil
	}
}

func dueOfRun(run store.RotationRun) store.DueRotation {
	return store.DueRotation{
		ID: run.RotationID, OrgID: run.OrgID, ProjectID: run.ProjectID, EnvironmentID: run.EnvironmentID, ProviderID: run.ProviderID,
		KeyID: run.KeyID, UsernameKeyID: run.UsernameKeyID, IntervalSeconds: run.IntervalSeconds,
		RevocationDelaySeconds: run.RotationDelaySeconds, AuthorityPrincipalID: run.AuthorityPrincipalID, Generation: run.RunGeneration,
	}
}

// infraNext is the next start after an infrastructure fault of the run being
// settled: the infrastructure faults this cycle has already had (the run being
// settled is still in flight and not counted) size the backoff.
func (e *RotationExecutor) infraNext(ctx context.Context, run store.RotationRun, now time.Time) (time.Time, error) {
	w, err := e.window(ctx, dueOfRun(run), now)
	if err != nil {
		return time.Time{}, err
	}
	prior, err := e.Runtime.InfraFaults(ctx, run.Chain(), run.RotationID, run.RunGeneration, w.Since)
	if err != nil {
		return time.Time{}, err
	}
	return rotationInfraNext(now, prior, w), nil
}

// retryPlan decides what follows a failed pre-publish run: the rotation's next
// attempt time, or the halt that replaces it. The run being failed counts as an
// attempt.
func (e *RotationExecutor) retryPlan(ctx context.Context, run store.RotationRun, now time.Time) (next time.Time, halt string, err error) {
	d := dueOfRun(run)
	w, err := e.window(ctx, d, now)
	if err != nil {
		return time.Time{}, "", err
	}
	spent, err := e.Runtime.FailedAttempts(ctx, run.Chain(), run.RotationID, run.RunGeneration, w.Since)
	if err != nil {
		return time.Time{}, "", err
	}
	next, exhausted := rotationRetry(now, spent+1, w)
	if exhausted {
		return time.Time{}, rotHaltExhausted, nil
	}
	return next, "", nil
}

// ---------------------------------------------------------------- start

// startRun creates and drives a run for a due rotation. The pre-publish Gate
// runs first: a definite refusal halts the rotation with its reason and creates
// no run, so no candidate is ever minted for a rotation that could not publish it.
func (e *RotationExecutor) startRun(ctx context.Context, worker string, d store.DueRotation) (bool, error) {
	now := e.now()
	// The Gate's own transaction and its denials carry the authority; no run id
	// exists yet.
	if err := e.gate(attributed(ctx, d.AuthorityPrincipalID, ""), d.Chain(), d.ID, d.AuthorityPrincipalID, d.KeyID, d.UsernameKeyID, gateCheck{generation: d.Generation, drafts: true}, now); err != nil {
		reason, definite := rotationGateReason(err)
		if errors.Is(err, errRotationInactive) {
			// Paused or changed between selection and the gate: nothing to do.
			return true, nil
		}
		if !definite {
			return false, e.deferDue(ctx, d, now, "gate", err)
		}
		return true, e.haltDue(ctx, d, reason, now)
	}
	w, err := e.window(ctx, d, now)
	if err != nil {
		return false, e.deferDue(ctx, d, now, "window", err)
	}
	spent, err := e.Runtime.FailedAttempts(ctx, d.Chain(), d.ID, d.Generation, w.Since)
	if err != nil {
		return false, e.deferDue(ctx, d, now, "attempts", err)
	}
	if !rotationMayStart(now, spent, w) {
		return true, e.haltDue(ctx, d, rotHaltExhausted, now)
	}
	// A run-now request names the run it asked for and may skip the overlap; every
	// check above and below is the same one a scheduled start passes.
	runID, trigger := d.ManualRunID, "manual"
	if runID == "" {
		var err error
		if runID, err = newID("rrn"); err != nil {
			return false, e.deferDue(ctx, d, now, "run_id", err)
		}
		trigger = "schedule"
	}
	run := store.RotationRun{
		ID: runID, OrgID: d.OrgID, ProjectID: d.ProjectID, EnvironmentID: d.EnvironmentID, RotationID: d.ID,
		CandidateHandle: dynamic.RoleName(runID), AuthorityPrincipalID: d.AuthorityPrincipalID,
	}
	intent, err := e.intent(run, "mint", now)
	if err != nil {
		return false, e.deferDue(ctx, d, now, "intent", err)
	}
	interval := time.Duration(d.IntervalSeconds) * time.Second
	// The candidate lives as long as a scheduled run's even when this run skips its
	// own overlap (a revocation delay of zero on the run row): it becomes the
	// superseded role of the next run, whose overlap it must outlast.
	delay := time.Duration(d.RevocationDelaySeconds) * time.Second
	started, ok, err := e.Runtime.StartRun(ctx, d, store.StartRun{
		RunID: runID, Worker: worker, Now: now, LeaseUntil: now.Add(rotationLeaseTerm), Trigger: trigger,
		CandidateHandle:     run.CandidateHandle,
		CandidateValidUntil: rotationCandidateValidUntil(now, interval, delay),
		ParkedUntil:         now.Add(rotationPublishBudget), Intent: intent,
	})
	if err != nil {
		// Deferred like every other pre-start failure, so a row whose start keeps
		// failing yields the head of the queue instead of starving the tenants
		// behind it. If the start did commit before the error, the run's own publish
		// or settle replaces the schedule.
		return false, e.deferDue(ctx, d, now, "start", err)
	}
	if !ok {
		// Another worker took the rotation, or it was paused or already running, or
		// a run-now request arrived since the selection and the next sweep picks it up.
		return true, nil
	}
	return true, e.mint(attributed(ctx, d.AuthorityPrincipalID, runID), started, intent.EffectID)
}

// deferDue moves a due rotation's next run out after a pre-start check failed for
// a reason that is not the rotation's own refusal (a datastore fault), then
// reports the original error. Without it the oldest due row would be re-selected
// first on every sweep and starve every other tenant behind it.
func (e *RotationExecutor) deferDue(ctx context.Context, d store.DueRotation, now time.Time, op string, cause error) error {
	e.logger().Warn("rotation pre-start check failed; next run deferred", "rotation_id", d.ID, "op", op, "class", rotationErrClass(cause))
	if err := e.Runtime.DeferRotation(ctx, d, now.Add(rotationBackoffFloor), now); err != nil {
		e.logger().Warn("rotation deferral failed", "rotation_id", d.ID, "class", rotationErrClass(err))
	}
	return cause
}

func (e *RotationExecutor) haltDue(ctx context.Context, d store.DueRotation, reason string, now time.Time) error {
	a, err := e.haltAudit(d.Chain(), subjectOfDue(d), reason, now)
	if err != nil {
		return err
	}
	return e.Runtime.HaltRotation(ctx, d, reason, now, a)
}

// ---------------------------------------------------------------- provider

// openProvider opens the provider the run's own rotation mints from.
func (e *RotationExecutor) openProvider(ctx context.Context, run store.RotationRun) (dynamic.Provider, store.LeaseProviderMaterial, error) {
	material, err := e.Runtime.LoadProviderMaterial(ctx, run, e.now())
	if err != nil {
		return nil, store.LeaseProviderMaterial{}, &providerOpenError{stage: "material", err: err}
	}
	provider, err := e.openWith(ctx, run, material)
	return provider, material, err
}

// openWith opens a provider from loaded connection material.
func (e *RotationExecutor) openWith(ctx context.Context, run store.RotationRun, material store.LeaseProviderMaterial) (dynamic.Provider, error) {
	if len(material.CredentialCiphertext) == 0 {
		return nil, &providerOpenError{stage: "material", err: store.ErrNoProviderCredential}
	}
	sealer, err := e.Keyring.ForProject(ctx, run.OrgID, run.ProjectID)
	if err != nil {
		return nil, &providerOpenError{stage: "keyring", err: err}
	}
	credential, err := sealer.OpenField(providerCredentialAAD(run.OrgID, run.ProjectID, material.CredentialOwnerID), material.CredentialCiphertext)
	if err != nil {
		return nil, &providerOpenError{stage: "decrypt", err: err}
	}
	defer crypto.Zero(credential)
	kind, err := dynamic.ParseKind(material.Kind)
	if err != nil {
		return nil, &providerOpenError{stage: "kind", err: err}
	}
	provider, err := e.ProviderFactory(kind, material.Origin, material.TLSMode, string(credential))
	if err != nil {
		return nil, &providerOpenError{stage: "factory", err: err}
	}
	return provider, nil
}

// ---------------------------------------------------------------- mint + publish

// mint creates the candidate role, then publishes it. The run is `minting` with
// its INTENT recorded and its handle durable before the provider is touched.
func (e *RotationExecutor) mint(ctx context.Context, run store.RotationRun, mintEffect string) error {
	provider, material, err := e.openProvider(ctx, run)
	if err != nil {
		// Nothing was sent to the provider: a definite failure, no drop owed. A fault
		// of the executor's own datastore, keyring or decryption is not an attempt.
		e.warn("rotation mint could not open its provider", run, "mint", err)
		code := rotCodeProvider
		if providerInfraFault(err) {
			code = rotCodeInfra
		}
		return e.failUnminted(ctx, run, mintEffect, code)
	}
	defer provider.Close()
	password, err := dynamic.GeneratePassword()
	if err != nil {
		return e.failUnminted(ctx, run, mintEffect, rotCodeMintFailed)
	}
	opCtx, cancel := context.WithTimeout(ctx, e.providerDeadline())
	err = provider.CreateRole(opCtx, dynamic.CreateRoleRequest{
		Name: run.CandidateHandle, Password: password, GrantRole: material.GrantRole, ValidUntil: run.CandidateValidUntil,
	})
	cancel()
	switch {
	case err == nil:
	case errors.Is(err, dynamic.ErrUnreachable), errors.Is(err, dynamic.ErrRefused):
		// A definite answer: no role exists.
		return e.failUnminted(ctx, run, mintEffect, rotCodeMintFailed)
	default:
		// Ambiguous: a role may exist. Roll the run back under the fenced claim
		// write, close the mint effect as unknown, then drop.
		closed, cerr := e.outcome(run, mintEffect, "mint", audit.OutcomeUnknown, rotCodeMintAmbiguous, e.now())
		if cerr != nil {
			return cerr
		}
		return e.rollback(ctx, provider, run, rotCodeMintAmbiguous, "", closed)
	}
	now := e.now()
	closed, err := e.outcome(run, mintEffect, "mint", audit.OutcomeSuccess, "", now)
	if err != nil {
		return err
	}
	err = e.Runtime.Settle(ctx, run, store.RotationSettle{
		Now: now, FromPhases: []string{"minting"}, Phase: "minted", NextAttempt: now, KeepLease: true, Close: closed,
	})
	if errors.Is(err, store.ErrRotationFenceLost) {
		// Another worker took the run while the role was created. A definite
		// fence loss: drop our candidate best-effort, but only once the run is
		// seen rolled back (never when it might have published).
		return e.dropOrphan(ctx, provider, run)
	}
	if err != nil {
		return err
	}
	run.Phase = "minted"
	return e.publish(ctx, provider, run, password)
}

// failUnminted closes a run whose mint never reached the provider, or was
// definitely refused: the mint effect closes as a failure, the run fails, and the
// rotation schedules its next attempt (or halts). No role exists, so no drop is owed.
func (e *RotationExecutor) failUnminted(ctx context.Context, run store.RotationRun, mintEffect, code string) error {
	now := e.now()
	closed, err := e.outcome(run, mintEffect, "mint", audit.OutcomeFailure, code, now)
	if err != nil {
		return err
	}
	settle, err := e.failureSettle(ctx, run, code, now, "")
	if err != nil {
		return err
	}
	settle.FromPhases = []string{"minting", "minted"}
	settle.Close = closed
	// No candidate exists, so the run is confirmed clean the moment it fails: it
	// is recorded as a dropped candidate so the reconciler never re-probes it.
	return e.finishFailed(ctx, run, settle)
}

// finishFailed writes a failure that owes no drop, then records the candidate as
// confirmed absent so it is purgeable and invisible to the reconciler.
func (e *RotationExecutor) finishFailed(ctx context.Context, run store.RotationRun, settle store.RotationSettle) error {
	settle.KeepLease = true
	if err := e.Runtime.Settle(ctx, run, settle); err != nil {
		if errors.Is(err, store.ErrRotationFenceLost) {
			return nil
		}
		return err
	}
	return e.confirmAbsent(ctx, run, "no_candidate", settle.LastError)
}

// confirmAbsent records the drop_candidate effect as a success for a run whose
// candidate is known not to exist (or to be gone), and releases the lease. how is
// the outcome's error_code (why nothing needed dropping); lastError is the run's
// failure code, kept so that closing the effect does not erase why the run failed.
func (e *RotationExecutor) confirmAbsent(ctx context.Context, run store.RotationRun, how, lastError string) error {
	now := e.now()
	intent, err := e.intent(run, "drop_candidate", now)
	if err != nil {
		return err
	}
	if err := e.Runtime.RecordIntent(ctx, run, now, intent); err != nil {
		if errors.Is(err, store.ErrRotationFenceLost) {
			return nil
		}
		return err
	}
	closed, err := e.outcome(run, intent.EffectID, "drop_candidate", audit.OutcomeSuccess, how, now)
	if err != nil {
		return err
	}
	err = e.Runtime.Settle(ctx, run, store.RotationSettle{
		Now: now, FromPhases: []string{"failed", "quarantine"}, Phase: "failed", LastError: lastError, NextAttempt: rotationNever, Close: closed,
	})
	if errors.Is(err, store.ErrRotationFenceLost) {
		return nil
	}
	return err
}

// failureSettle builds the write that fails a pre-publish run: the run goes to
// `failed` (due to the reconciler now), and the rotation either schedules its next
// attempt or halts. Written WITH the fenced claim, in one transaction, so a crash
// can never leave a failed run on an unscheduled rotation.
func (e *RotationExecutor) failureSettle(ctx context.Context, run store.RotationRun, code string, now time.Time, forcedHalt string) (store.RotationSettle, error) {
	settle := store.RotationSettle{Now: now, FromPhases: []string{"minting", "minted"}, Phase: "failed", LastError: code, NextAttempt: now, KeepLease: true}
	if run.RotationState != "active" && run.RotationState != "" {
		// Paused, deleted, held by a restore or already halted: the schedule is not ours to move.
		return settle, nil
	}
	if code == rotCodeInfra {
		// Not an attempt: the executor's own infrastructure faulted before the
		// provider was touched, so it never counts toward the attempt cap. It backs
		// off like any retry (5 minutes doubling to the 6 hour cap) on the infrastructure
		// faults of this cycle. A window that closes meanwhile is
		// still caught by the start check, which halts retries_exhausted.
		next, err := e.infraNext(ctx, run, now)
		if err != nil {
			return settle, err
		}
		settle.NextRunAt = next
		return settle, nil
	}
	halt := forcedHalt
	if halt == "" {
		next, h, err := e.retryPlan(ctx, run, now)
		if err != nil {
			return settle, err
		}
		halt = h
		settle.NextRunAt = next
	}
	if halt != "" {
		settle.NextRunAt = time.Time{}
		settle.HaltReason = halt
		// Written only if the halt changes a row: a rotation paused or deleted
		// while the provider call was in flight is not halted and records no halt.
		a, err := e.haltAudit(run.Chain(), subjectOfRun(run), halt, now)
		if err != nil {
			return settle, err
		}
		settle.HaltAudit = &a
	}
	return settle, nil
}

// rollback is crash-safety rules 3 and 8 for a run that may own a live candidate:
// FIRST the fenced claim write (run to `failed` WITH the drop_candidate INTENT, in
// one transaction, under the claim token; it waits on a publish transaction's row
// lock), THEN the drop. Zero rows matched means the run is no longer
// minting/minted: it published, or another worker owns it; follow that, never
// drop. `closing`, when set, is the open effect this write closes.
func (e *RotationExecutor) rollback(ctx context.Context, provider dynamic.Provider, run store.RotationRun, code, forcedHalt string, closing *store.RotationEffectOutcome) error {
	now := e.now()
	settle, err := e.failureSettle(ctx, run, code, now, forcedHalt)
	if err != nil {
		return err
	}
	intent, err := e.intent(run, "drop_candidate", now)
	if err != nil {
		return err
	}
	settle.Close = closing
	settle.Open = &intent
	err = e.Runtime.Settle(ctx, run, settle)
	if errors.Is(err, store.ErrRotationFenceLost) {
		return e.followState(ctx, run)
	}
	if err != nil {
		return err
	}
	run.LastError = code
	return e.dropCandidate(ctx, provider, run, intent.EffectID)
}

// overlapSettle settles a published run into its overlap wait. The revoke deadline
// is from plus the delay, stored as RevokeNotBefore; the wake is that deadline, or
// the extension margin when it comes first (rotationOverlapWake), so a long delay
// never parks a run past the point its credential can still be extended.
func overlapSettle(r store.RotationRun, now, from time.Time) store.RotationSettle {
	deadline := from.Add(time.Duration(r.RevocationDelaySeconds) * time.Second)
	return store.RotationSettle{
		Now: now, FromPhases: []string{"published"}, Phase: "overlap",
		NextAttempt: overlapWakeFor(r, now, deadline), RevokeNotBefore: deadline,
	}
}

// overlapWakeFor is rotationOverlapWake for a claimed run. A paused or halted
// rotation does not extend, so its wake is the deadline alone. A rotation a
// restore holds extends nothing either, but only until release-hold, which changes
// no run schedule: the wake keeps the credential's extension margin so the run is
// visited before the credential expires, and a margin already passed is polled at
// the backoff floor, not claimed back to back for the length of the hold. The
// deadline can lie past VALID UNTIL when the overlap is established late (a
// recovered run takes it from the recovery's own observation).
func overlapWakeFor(r store.RotationRun, now, deadline time.Time) time.Time {
	held := r.RotationState == store.RotationStateRestoreHeld
	validUntil := r.CandidateValidUntil
	if r.RotationState != "active" && !held {
		validUntil = time.Time{}
	}
	wake := rotationOverlapWake(now, deadline, validUntil)
	if held && !wake.After(now) && now.Before(deadline) {
		wake = now.Add(rotationBackoffFloor)
		if wake.After(deadline) {
			wake = deadline
		}
	}
	return wake
}

// followState re-reads a run after a fence loss and acts on what it now is. A run
// that published carries on as published (the publish transaction committed
// first); anything else belongs to another worker or to the reconciler, and the
// candidate is never dropped from here.
func (e *RotationExecutor) followState(ctx context.Context, run store.RotationRun) error {
	cur, err := e.Runtime.GetRun(ctx, run.Chain(), run.ID)
	if err != nil {
		return err
	}
	if cur.Phase != "published" || cur.PublishedRevision == 0 {
		return nil
	}
	// An ambiguous commit that did commit: the publish kept this claim, so settle
	// into the overlap wait and release it. The deadline comes from this
	// observation, which is after the commit, not from the one the commit hook
	// stored before it.
	now := e.now()
	return e.settleDrop(ctx, run, overlapSettle(cur, now, now))
}

// dropOrphan drops the candidate of a worker that lost its claim while the role
// was created, but only after seeing the run rolled back with no publish. It is
// best effort and records no effect: this worker no longer holds the run's fence,
// so it cannot write to the run, and the worker that took the run owns its drop
// under its own INTENT and OUTCOME. The claim query keeps a provider-deadline
// margin after a lease expires, so that worker's rollback runs after the create
// call this worker started has finished or timed out. What a failed drop leaves is
// an inert role (its password existed only in this process) that expires at its
// VALID UNTIL; the fault is logged by class, never swallowed.
func (e *RotationExecutor) dropOrphan(ctx context.Context, provider dynamic.Provider, run store.RotationRun) error {
	cur, err := e.Runtime.GetRun(ctx, run.Chain(), run.ID)
	if err != nil {
		return err
	}
	if cur.PublishedRevision != 0 || (cur.Phase != "failed" && cur.Phase != "quarantine") {
		return nil
	}
	if held, err := e.heldRead(ctx, run, "drop_orphan"); err != nil || held {
		// A held rotation drops no candidate (see leaveCandidateHeld); the role expires.
		return err
	}
	opCtx, cancel := context.WithTimeout(ctx, e.providerDeadline())
	defer cancel()
	if err := provider.DropRole(opCtx, run.CandidateHandle); err != nil {
		e.warn("rotation orphan candidate drop failed; the role is inert and expires", run, "drop_orphan", err)
	}
	return nil
}

// dropCandidate drops the candidate of a run already moved to `failed` with its
// INTENT recorded, and records the outcome: a confirmed drop settles the run; a
// definite failure leaves it to the reconciler; an unknown outcome quarantines it.
func (e *RotationExecutor) dropCandidate(ctx context.Context, provider dynamic.Provider, run store.RotationRun, effectID string) error {
	if held, err := e.heldRead(ctx, run, "drop_candidate"); err != nil {
		return err
	} else if held {
		return e.leaveCandidateHeld(ctx, run, []string{"failed"}, "failed", effectID)
	}
	opCtx, cancel := context.WithTimeout(ctx, e.providerDeadline())
	err := provider.DropRole(opCtx, run.CandidateHandle)
	cancel()
	now := e.now()
	switch {
	case err == nil:
		closed, err := e.outcome(run, effectID, "drop_candidate", audit.OutcomeSuccess, "", now)
		if err != nil {
			return err
		}
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{"failed"}, Phase: "failed", LastError: run.LastError, NextAttempt: rotationNever, Close: closed})
	case errors.Is(err, dynamic.ErrUnreachable), errors.Is(err, dynamic.ErrRefused):
		closed, err := e.outcome(run, effectID, "drop_candidate", audit.OutcomeFailure, rotCodeDropFailed, now)
		if err != nil {
			return err
		}
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{"failed"}, Phase: "failed", LastError: rotCodeDropFailed, NextAttempt: now.Add(rotationBackoff(run.AttemptCount)), Close: closed})
	default:
		closed, err := e.outcome(run, effectID, "drop_candidate", audit.OutcomeUnknown, rotCodeDropFailed, now)
		if err != nil {
			return err
		}
		q, err := e.quarantined(run, now)
		if err != nil {
			return err
		}
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{"failed"}, Phase: "quarantine", LastError: rotCodeDropFailed, NextAttempt: now.Add(rotationBackoff(run.AttemptCount)), Close: closed, Audits: []store.RotationAudit{q}})
	}
}

// leaveCandidateHeld ends a candidate drop that a restore hold forbids. While a
// restore holds the rotation no DROP ROLE is issued for a candidate that never
// published: the archive can say a run never published that the live instance did
// publish and consumers fetched, and a drop would kill that credential with no
// restore assertion to guard it. The INTENT is closed as a failure (the provider
// was never called) and the run is left due; the claim skips its held rotation, so
// the reconciler revisits it after release-hold. The role lingers until then,
// bounded by its VALID UNTIL, and the release lets the executor drop it.
func (e *RotationExecutor) leaveCandidateHeld(ctx context.Context, run store.RotationRun, from []string, phase, effectID string) error {
	now := e.now()
	closed, err := e.outcome(run, effectID, "drop_candidate", audit.OutcomeFailure, rotCodeHeld, now)
	if err != nil {
		return err
	}
	return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: from, Phase: phase, LastError: run.LastError, NextAttempt: now, Close: closed})
}

// heldRead is the executor's own restore-hold read at a recovery or drop site.
func (e *RotationExecutor) heldRead(ctx context.Context, run store.RotationRun, site string) (bool, error) {
	if e.BeforeHeldRead != nil {
		e.BeforeHeldRead(site)
	}
	return e.Runtime.RestoreHeld(ctx, run)
}

// yieldHeldRun hands back a claimed run whose rotation a restore hold reached
// after the claim, before the step touched anything: the lease is released and the
// run stays due in its own phase, so the claim (which skips held rotations) takes
// it again after release-hold instead of after the lease term. The claim already
// counted this visit in attempt_count; that is one bump per hold landing in the
// claim window, only sizes the backoff, and stays bounded.
func (e *RotationExecutor) yieldHeldRun(ctx context.Context, run store.RotationRun) error {
	return e.settleDrop(ctx, run, store.RotationSettle{Now: e.now(), FromPhases: []string{run.Phase}, Phase: run.Phase, LastError: run.LastError, NextAttempt: e.now()})
}

func (e *RotationExecutor) settleDrop(ctx context.Context, run store.RotationRun, s store.RotationSettle) error {
	// A done run parked at "never" is never claimed again, and a restore release does
	// not re-arm it; the write decides on the hold itself so one landing after the
	// caller's own reads cannot strand a delivered credential.
	s.DueIfHeld = s.DueIfHeld || (s.Phase == "done" && s.NextAttempt.Equal(rotationNever))
	err := e.Runtime.Settle(ctx, run, s)
	if errors.Is(err, store.ErrRotationFenceLost) {
		return nil
	}
	return err
}

// recoverInFlight is a claimed run found minting or minted: its worker died and
// its password with it. It always rolls back, under the fenced claim write.
func (e *RotationExecutor) recoverInFlight(ctx context.Context, run store.RotationRun) error {
	// A rotation a restore holds recovers nothing (see leaveCandidateHeld): the run and
	// its role are left as they are until release-hold. The claim already skips such
	// a rotation; this read covers a hold that landed after the claim.
	if held, err := e.heldRead(ctx, run, "recover"); err != nil {
		return err
	} else if held {
		return e.yieldHeldRun(ctx, run)
	}
	code := rotCodeInterrupted
	if run.RotationState != "active" {
		code = rotCodePaused
	}
	var closing *store.RotationEffectOutcome
	if id, kind, err := e.Runtime.OpenEffect(ctx, run); err == nil {
		// A mint left without an outcome may have created the role; a publish
		// that never recorded its outcome never committed (the commit records it).
		result := audit.OutcomeUnknown
		if kind == "publish" {
			result = audit.OutcomeFailure
		}
		closing, err = e.outcome(run, id, kind, result, rotCodeInterrupted, e.now())
		if err != nil {
			return err
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	provider, _, err := e.openProvider(ctx, run)
	if err != nil {
		// No provider to drop with: still take the fenced claim write so the run
		// is `failed` and the reconciler owns the candidate once a provider is back.
		e.warn("rotation recovery could not open its provider", run, "recover", err)
		return e.rollbackBlind(ctx, run, code, closing)
	}
	defer provider.Close()
	return e.rollback(ctx, provider, run, code, "", closing)
}

// rollbackBlind is rollback when no provider can be opened: the fenced claim
// write moves the run to `failed` and the candidate reconciler drops later.
func (e *RotationExecutor) rollbackBlind(ctx context.Context, run store.RotationRun, code string, closing *store.RotationEffectOutcome) error {
	now := e.now()
	settle, err := e.failureSettle(ctx, run, code, now, "")
	if err != nil {
		return err
	}
	settle.Close = closing
	settle.KeepLease = false
	settle.NextAttempt = now.Add(rotationBackoff(run.AttemptCount))
	err = e.Runtime.Settle(ctx, run, settle)
	if errors.Is(err, store.ErrRotationFenceLost) {
		return nil
	}
	return err
}

// ---------------------------------------------------------------- publish

// publish stages the candidate credential, publishes it and records the run as
// published in ONE transaction, as the rotation's recorded authority. The
// transaction takes the run row first, so a takeover's fenced claim write waits on
// it (rule 8). Any error rolls the whole transaction back, staged drafts included.
func (e *RotationExecutor) publish(ctx context.Context, provider dynamic.Provider, run store.RotationRun, password string) error {
	scope := run.Chain()
	authority := domain.PrincipalID(run.AuthorityPrincipalID)
	actor := LocalPrincipal(authority)
	publishIntent, err := e.intent(run, "publish", e.now())
	if err != nil {
		return err
	}
	if err := e.Runtime.RecordIntent(ctx, run, e.now(), publishIntent); err != nil {
		if errors.Is(err, store.ErrRotationFenceLost) {
			return e.dropOrphan(ctx, provider, run)
		}
		return err
	}
	sealer, err := e.Keyring.ForProject(ctx, run.OrgID, run.ProjectID)
	if err != nil {
		return e.rollbackAfterPublishError(ctx, provider, run, publishIntent.EffectID, err)
	}
	var committedAt time.Time
	hooks := &rotationPublish{
		key: authz.KeyByID(run.KeyID),
		begin: func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, caller authz.Identity, now time.Time) error {
			now = store.CanonTime(now)
			if now.After(run.StartedAt.Add(rotationPublishBudget)) {
				return errRotationBudget
			}
			proof, err := authorizeRotationRun(ctx, az, caller, scope, run.KeyID, run.UsernameKeyID)
			if err != nil {
				return err
			}
			if err := e.gateWithin(ctx, r, az, caller, scope, proof, run.RotationID, gateCheck{generation: run.RunGeneration}); err != nil {
				return err
			}
			return r.Rotations().BeginRunPublish(ctx, proof, run, now)
		},
		stage: func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, caller authz.Identity, now time.Time) ([]string, error) {
			// `now` is read after the project lock was won: the begin check above ran
			// before a lock wait that can outlast the budget, so an over-budget run
			// rolls back here, before anything is staged.
			if store.CanonTime(now).After(run.StartedAt.Add(rotationPublishBudget)) {
				return nil, errRotationBudget
			}
			proof, err := authorizeRotationRun(ctx, az, caller, scope, run.KeyID, run.UsernameKeyID)
			if err != nil {
				return nil, err
			}
			// Under the project lock: the authority's own draft on either key would be
			// replaced by the candidate (staging is delete-then-insert), so it
			// refuses, by name, and nothing is overwritten.
			if err := requireNoAuthorityDraft(ctx, r, proof, run.AuthorityPrincipalID, run.KeyID, run.UsernameKeyID); err != nil {
				return nil, err
			}
			passwordKey, err := r.Catalogue().Get(ctx, proof, run.KeyID)
			if err != nil {
				return nil, err
			}
			usernameKey, err := r.Catalogue().Get(ctx, proof, run.UsernameKeyID)
			if err != nil {
				return nil, err
			}
			ids := make([]string, 0, 2)
			for _, cell := range []struct{ name, value string }{{usernameKey.Name, run.CandidateHandle}, {passwordKey.Name, password}} {
				staged, err := e.Values.stageWithin(ctx, r, az, caller, scope, cell.name, store.PendingSet, cell.value, nil, authz.OpValueStage, sealer)
				if err != nil {
					return nil, err
				}
				ids = append(ids, staged.change.VersionID)
			}
			return ids, nil
		},
		commit: func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, caller authz.Identity, now time.Time, out PublishResult) error {
			// The publication time: read after the project lock, it stamps the
			// snapshot and the publish record. The budget is checked against it here
			// and again against a fresh read below, and the overlap deadline is
			// anchored to that fresh read and to the clock after the commit, because
			// the staging and materialization that follow the lock also take time
			// that consumers must not lose.
			now = store.CanonTime(now)
			if now.After(run.StartedAt.Add(rotationPublishBudget)) {
				return errRotationBudget
			}
			proof, err := authorizeRotationRun(ctx, az, caller, scope, run.KeyID, run.UsernameKeyID)
			if err != nil {
				return err
			}
			var revision int64
			for _, env := range out.Environments {
				if env.EnvironmentID == string(scope.Env) {
					revision = env.Revision
				}
			}
			if revision == 0 {
				return errors.New("service: rotation publish produced no revision for its environment")
			}
			closed, err := e.outcomeWith(run, publishIntent.EffectID, "publish", audit.OutcomeSuccess, "", now, audit.Payload{"revision": revision})
			if err != nil {
				return err
			}
			interval := time.Duration(run.IntervalSeconds) * time.Second
			delay := time.Duration(run.RevocationDelaySeconds) * time.Second
			// Everything but the commit itself is done: this is the latest time the
			// callback can stamp. The deadline stored here is only the claim time of
			// the `published` phase: it can precede the commit by the commit latency,
			// so a run recovered in `published` re-establishes its overlap from its
			// own later observation (see revoke) instead of revoking on it.
			committedAt = store.CanonTime(e.now())
			if committedAt.Before(now) {
				committedAt = now
			}
			// The budget is checked against this fresh read too: staging and
			// materialization ran after the post-lock read above and can outlast it,
			// and a publish that would commit past the budget rolls back instead.
			if committedAt.After(run.StartedAt.Add(rotationPublishBudget)) {
				return errRotationBudget
			}
			if err := r.Rotations().RecordRunPublish(ctx, proof, store.RotationPublishRecord{
				Run: run, Revision: revision, Now: now, NextRunAt: now.Add(interval), NextAttempt: committedAt.Add(delay), Outcome: *closed,
			}); err != nil {
				return err
			}
			return nil
		},
	}
	_, err = e.Revisions.PublishPlanned(ctx, actor, scope, PublishRequest{rotation: hooks})
	if err != nil {
		return e.rollbackAfterPublishError(ctx, provider, run, publishIntent.EffectID, err)
	}
	// Committed. The phase is `published`; settle into the overlap wait and release
	// the claim. A failed fence here is harmless: the next claim finds `published`.
	// The deadline is the clock read now, which is at or after the commit, plus the
	// delay: consumers get the whole delay after the new credential is durable, not
	// after the moment the callback stamped it.
	now := e.now()
	deadlineFrom := now
	if committedAt.After(deadlineFrom) {
		deadlineFrom = committedAt
	}
	err = e.Runtime.Settle(ctx, run, overlapSettle(run, now, deadlineFrom))
	if errors.Is(err, store.ErrRotationFenceLost) {
		return nil
	}
	return err
}

// rollbackAfterPublishError resolves a publish that returned an error. The
// transaction rolled back, or its commit is ambiguous; the fenced claim write
// inside rollback decides which, waiting on the row lock a still-open publish
// holds. A run that did publish comes back as a fence loss and carries on.
func (e *RotationExecutor) rollbackAfterPublishError(ctx context.Context, provider dynamic.Provider, run store.RotationRun, publishEffect string, cause error) error {
	code, halt := rotCodePublishFailed, ""
	switch {
	case errors.Is(cause, errRotationBudget):
		code = rotCodeBudget
	case errors.Is(cause, errRotationHeld):
		code = rotCodeHeld
	case errors.Is(cause, errRotationInactive):
		code = rotCodePaused
	default:
		if reason, definite := rotationGateReason(cause); definite {
			halt = reason
		}
	}
	// The publish rolled back: log its class so an operator can tell a budget, a
	// refusal and a datastore fault apart, never the cause's text.
	e.warn("rotation publish rolled back", run, "publish", cause)
	closed, err := e.outcome(run, publishEffect, "publish", audit.OutcomeFailure, code, e.now())
	if err != nil {
		return err
	}
	return e.rollback(ctx, provider, run, code, halt, closed)
}

// staleEffect closes, as unknown, an effect a crashed worker left open on the
// run. The recovering step re-issues its idempotent provider call under a fresh
// INTENT, so exactly one effect is ever open and every INTENT gets an OUTCOME.
func (e *RotationExecutor) staleEffect(ctx context.Context, run store.RotationRun, now time.Time) (*store.RotationEffectOutcome, error) {
	id, kind, err := e.Runtime.OpenEffect(ctx, run)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return e.outcome(run, id, kind, audit.OutcomeUnknown, rotCodeInterrupted, now)
}

// ---------------------------------------------------------------- reconcile

// errNoProvider stands for "no provider could be opened" where a role whose VALID
// UNTIL has passed is still settled: the expiry exit needs no provider.
var errNoProvider = errors.New("service: rotation provider unavailable")

// rotationExpirySkew is the margin past a role's recorded VALID UNTIL before
// Hikyo settles it as expired without asking the provider. VALID UNTIL is judged
// by the database server's clock when the role logs in, and Hikyo reads its own:
// the margin keeps a clock that runs ahead here from declaring a still-valid role
// dead and skipping its drop or its restore assertion.
const rotationExpirySkew = 5 * time.Minute

// roleExpired reports whether a role can no longer authenticate: its recorded
// VALID UNTIL has passed by more than the clock-skew margin and no extend of it is
// unaccounted for (an extend that ended unknown may have moved the real VALID
// UNTIL later).
func roleExpired(validUntil time.Time, extendUncertain bool, now time.Time) bool {
	return !extendUncertain && !validUntil.IsZero() && now.After(validUntil.Add(rotationExpirySkew))
}

// reconcileCandidate settles a pre-publish run (failed or quarantine) whose
// candidate drop is unconfirmed: it probes the role, drops it when present and
// records the confirmed drop, after which the run is `failed` and purgeable. It
// owns published_revision IS NULL only; a run with a recorded publish is never
// touched here, because its candidate IS the delivered credential. A candidate
// whose VALID UNTIL has passed is settled by that expiry without the provider, so
// a revoked admin credential or an unreachable provider cannot hold the run (and
// with it the provider and environment) forever.
func (e *RotationExecutor) reconcileCandidate(ctx context.Context, run store.RotationRun) error {
	if held, err := e.heldRead(ctx, run, "reconcile:entry"); err != nil {
		return err
	} else if held {
		return e.yieldHeldRun(ctx, run)
	}
	now := e.now()
	expired := roleExpired(run.CandidateValidUntil, false, now)
	retry := func(code string) error {
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{run.Phase}, Phase: run.Phase, LastError: code, NextAttempt: now.Add(rotationBackoff(run.AttemptCount))})
	}
	provider, _, openErr := e.openProvider(ctx, run)
	if openErr != nil {
		e.warn("rotation candidate reconcile could not open its provider", run, "reconcile", openErr)
		if !expired {
			return retry(rotCodeProvider)
		}
	} else {
		defer provider.Close()
	}
	intent, err := e.intent(run, "drop_candidate", now)
	if err != nil {
		return err
	}
	stale, err := e.staleEffect(ctx, run, now)
	if err != nil {
		return err
	}
	if err := e.Runtime.Settle(ctx, run, store.RotationSettle{
		Now: now, FromPhases: []string{run.Phase}, Phase: run.Phase, LastError: run.LastError, NextAttempt: now, KeepLease: true, Open: &intent, Close: stale,
	}); err != nil {
		if errors.Is(err, store.ErrRotationFenceLost) {
			return nil
		}
		return err
	}
	if held, err := e.heldRead(ctx, run, "reconcile:predrop"); err != nil {
		return err
	} else if held {
		return e.leaveCandidateHeld(ctx, run, []string{run.Phase}, run.Phase, intent.EffectID)
	}
	err = errNoProvider
	if provider != nil {
		opCtx, cancel := context.WithTimeout(ctx, e.providerDeadline())
		var status dynamic.RoleStatus
		status, err = provider.RoleStatus(opCtx, run.CandidateHandle)
		if err == nil && status.Exists {
			err = provider.DropRole(opCtx, run.CandidateHandle)
		}
		cancel()
	}
	switch {
	case err == nil:
		closed, err := e.outcome(run, intent.EffectID, "drop_candidate", audit.OutcomeSuccess, "", now)
		if err != nil {
			return err
		}
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{run.Phase}, Phase: "failed", LastError: run.LastError, NextAttempt: rotationNever, Close: closed})
	case expired:
		// An expired role cannot authenticate: the candidate is no longer a live
		// credential even though the drop could not be confirmed.
		closed, err := e.outcome(run, intent.EffectID, "drop_candidate", audit.OutcomeSuccess, rotCodeExpired, now)
		if err != nil {
			return err
		}
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{run.Phase}, Phase: "failed", LastError: rotCodeExpired, NextAttempt: rotationNever, Close: closed})
	default:
		result := audit.OutcomeFailure
		if errors.Is(err, dynamic.ErrAmbiguous) {
			result = audit.OutcomeUnknown
		}
		closed, err := e.outcome(run, intent.EffectID, "drop_candidate", result, rotCodeDropFailed, now)
		if err != nil {
			return err
		}
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{run.Phase}, Phase: run.Phase, LastError: rotCodeDropFailed, NextAttempt: now.Add(rotationBackoff(run.AttemptCount)), Close: closed})
	}
}

// ---------------------------------------------------------------- revoke

// revoke is the post-publish step for a published, overlapping, revoking or
// revoke_refused run, and the revoke-obligation loop for a quarantined run that
// published. Only fail-safe checks apply here: the row fence, the recorded
// published_revision, the provider's existence, and the restore assertion, which
// can only refuse a destructive step. The authority gate never runs after a
// publish, so losing authority can never strand a live superseded credential.
//
// The superseded role is dropped on the provider that MINTED it, which a
// recreated rotation may not share with this run. A role whose VALID UNTIL has
// passed is settled by that expiry without the provider (and without the restore
// assertion: it can no longer authenticate).
//
// While the run stays open, the next cycle cannot start, so a run still held when
// its delivered credential comes due for extension keeps that credential alive
// first (extendDelivered), or halts the rotation by name when it may not.
func (e *RotationExecutor) revoke(ctx context.Context, run store.RotationRun) error {
	now := e.now()
	executorPhase := slices.Contains([]string{"published", "overlap", "revoking", "revoke_refused"}, run.Phase)
	from := []string{run.Phase}
	if executorPhase {
		from = []string{"published", "overlap", "revoking", "revoke_refused"}
	}
	if run.SupersededHandle == "" {
		// The previous value was the operator's own: nothing to revoke.
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: from, Phase: "done", NextAttempt: rotationExtendDue(run.CandidateValidUntil)})
	}
	hold := func(phase, code string) error {
		// Only called after a fresh restore assertion read this visit, or for a role that
		// has expired (the revoke settle and finish() then also set it after a failed read:
		// finish always ends in done there), so it may replace a restore refusal's code.
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: from, Phase: phase, LastError: code, NextAttempt: now.Add(rotationBackoff(run.AttemptCount)), RestoreChecked: true})
	}
	refusedPhase := run.Phase
	if executorPhase {
		refusedPhase = "revoke_refused"
	}
	if executorPhase {
		if handled, err := e.extendDelivered(ctx, run, from); handled || err != nil {
			return err
		}
	}
	if run.Phase == "published" {
		// The settle into overlap did not happen (a crash, or a lease that lapsed
		// during the publish). The deadline stored with `published` was read before
		// the commit, so it can be earlier than the commit plus the delay by the
		// commit latency. Nothing is revoked on it: this observation is after the
		// commit, so the overlap is established from it and revoke waits it out. It
		// follows the extend step: a delivered credential due for extension is
		// extended, or halts by name, before the wait is started.
		return e.settleDrop(ctx, run, overlapSettle(run, now, now))
	}
	if until := run.RevokeNotBefore; now.Before(until) {
		// A visit before the overlap deadline: the wake was the extension margin (the
		// extend step above has run) or a lease retry. Nothing is revoked before the
		// deadline, so park again for the next margin or the deadline itself. The
		// states that never extend (a quarantined run) wait for the deadline.
		wake := until
		if executorPhase {
			wake = overlapWakeFor(run, now, until)
			if !wake.After(now) {
				// The extension was due and did not happen (a failed or skipped try):
				// retry on the backoff, not in a tight loop.
				wake = now.Add(rotationBackoff(run.AttemptCount))
				if wake.After(until) {
					wake = until
				}
			}
		}
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: from, Phase: run.Phase, LastError: run.LastError, NextAttempt: wake})
	}
	// The restore assertion is a store read and needs no provider, so it is read
	// first: a provider that cannot be opened (or a role that cannot be loaded) must
	// never replace the restore refusal with a transient code. While the assertion
	// fails, the keys may serve the superseded role again and nothing but the
	// provider can prove it dead, so every path that cannot reach the provider
	// keeps the run held as published_versions_changed, which tells the operator
	// not to drop the role. It is read before the role is loaded and the provider
	// opened, so a restore that lands between that read and the drop is the same
	// accepted race as before, a little wider by the local steps in between (a remote
	// keyring would need a re-read after the open). A read that fails blocks the
	// revoke, but not the settle of a role that has already expired: nothing about
	// an expired role is live, so there is no restore to refuse.
	holds, holdsErr := e.Runtime.RestoreAssertion(ctx, run)
	if e.RestoreAssertionFault != nil {
		if fault := e.RestoreAssertionFault(); fault != nil {
			holds, holdsErr = false, fault
		}
	}
	unreachable := func() error {
		if !holds {
			return hold(refusedPhase, rotCodeVersionsChanged)
		}
		return hold(run.Phase, rotCodeProvider)
	}
	role, err := e.Runtime.LoadSupersededRole(ctx, run, now)
	if errors.Is(err, store.ErrNotFound) && holdsErr != nil {
		return holdsErr
	}
	if errors.Is(err, store.ErrNotFound) {
		// The lease was lost, or no run minted this role: nothing to act on safely.
		e.warn("rotation revoke found no minting run for its superseded role", run, "revoke", err)
		return unreachable()
	}
	if err != nil {
		return err
	}
	expired := roleExpired(role.ValidUntil, role.ExtendUncertain, now)
	if holdsErr != nil && !expired {
		return holdsErr
	}
	provider, openErr := e.openWith(ctx, run, role.Material)
	if openErr != nil {
		e.warn("rotation revoke could not open its provider", run, "revoke", openErr)
		if !expired {
			return unreachable()
		}
	} else {
		defer provider.Close()
	}
	if !expired && !holds && !e.supersededDead(ctx, provider, run.SupersededHandle, now) {
		return hold(refusedPhase, rotCodeVersionsChanged)
	}
	intent, err := e.intent(run, "revoke", now)
	if err != nil {
		return err
	}
	revokingPhase := run.Phase
	if executorPhase {
		revokingPhase = "revoking"
	}
	stale, err := e.staleEffect(ctx, run, now)
	if err != nil {
		return err
	}
	if err := e.Runtime.Settle(ctx, run, store.RotationSettle{
		Now: now, FromPhases: from, Phase: revokingPhase, LastError: "", NextAttempt: now, KeepLease: true, Open: &intent, Close: stale, RestoreChecked: true,
	}); err != nil {
		if errors.Is(err, store.ErrRotationFenceLost) {
			return nil
		}
		return err
	}
	// The adapter targets still behind the published revision, read before the drop: the
	// report a run that skips its overlap owes. A failed read leaves it out; the
	// revoke does not wait on a report.
	notConverged := e.notConverged(ctx, run)
	err = errNoProvider
	if provider != nil {
		opCtx, cancel := context.WithTimeout(ctx, e.providerDeadline())
		err = provider.DropRole(opCtx, run.SupersededHandle)
		cancel()
	}
	now = e.now()
	backoff := now.Add(rotationBackoff(run.AttemptCount))
	finish := func(result audit.Outcome, code, phase string, next time.Time) error {
		var extra audit.Payload
		if notConverged != nil {
			extra = audit.Payload{"not_converged_adapter_targets": *notConverged}
		}
		closed, err := e.outcomeWith(run, intent.EffectID, "revoke", result, code, now, extra)
		if err != nil {
			return err
		}
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{revokingPhase}, Phase: phase, LastError: code, NextAttempt: next, Close: closed, NotConverged: notConverged, RestoreChecked: true})
	}
	switch {
	case err == nil:
		return finish(audit.OutcomeSuccess, "", "done", rotationExtendDue(run.CandidateValidUntil))
	case expired:
		// The role could not be dropped (or no provider could be opened) but it can
		// no longer authenticate, so nothing about it is live.
		return finish(audit.OutcomeSuccess, rotCodeExpired, "done", rotationExtendDue(run.CandidateValidUntil))
	case errors.Is(err, dynamic.ErrRoleOwnsObjects):
		phase := revokingPhase
		if executorPhase {
			phase = "revoke_refused"
		}
		return finish(audit.OutcomeFailure, rotCodeOwnsObjects, phase, backoff)
	case errors.Is(err, dynamic.ErrAmbiguous):
		return finish(audit.OutcomeUnknown, rotCodeRevokeFailed, revokingPhase, backoff)
	default:
		return finish(audit.OutcomeFailure, rotCodeRevokeFailed, revokingPhase, backoff)
	}
}

// notConverged counts the adapter targets still behind the run's published
// revision, nil when the count could not be read. Machine principals reading the
// keys directly are not counted: no delivery to them is recorded.
func (e *RotationExecutor) notConverged(ctx context.Context, run store.RotationRun) *int64 {
	if run.PublishedRevision == 0 {
		return nil
	}
	n, err := e.Runtime.NotConverged(ctx, run)
	if err != nil {
		e.warn("rotation revoke could not count unconverged adapter targets", run, "revoke", err)
		return nil
	}
	return &n
}

// supersededDead reports whether the superseded role can no longer authenticate:
// it is gone, or its VALID UNTIL passed more than rotationExpirySkew ago (the same
// margin every expiry exit uses, so a clock ahead of the database never drops a role
// that can still log in). Only then can a value that serves it
// again (the restore hold) no longer be an outage, so dropping it is harmless and
// the hold stops blocking the rotation. A probe that fails leaves the hold. The
// provider is the one that minted the role.
func (e *RotationExecutor) supersededDead(ctx context.Context, provider dynamic.Provider, handle string, now time.Time) bool {
	opCtx, cancel := context.WithTimeout(ctx, e.providerDeadline())
	defer cancel()
	status, err := provider.RoleStatus(opCtx, handle)
	if err != nil {
		return false
	}
	return !status.Exists || (!status.ValidUntil.IsZero() && now.After(status.ValidUntil.Add(rotationExpirySkew)))
}

// ---------------------------------------------------------------- extend

// haltClosing halts the rotation of a run that stays where it is, and closes the
// extend INTENT it holds (a definite failure, no provider call was made) in the same
// fenced write. The halt audit is written only if the halt changed a row. It reports
// the run as handled.
func (e *RotationExecutor) haltClosing(ctx context.Context, run store.RotationRun, from []string, reason string, now, next time.Time, intent store.RotationEffectIntent) (bool, error) {
	closed, err := e.outcome(run, intent.EffectID, "extend", audit.OutcomeFailure, rotCodeExtendSkipped, now)
	if err != nil {
		return true, err
	}
	a, err := e.haltAudit(run.Chain(), subjectOfRun(run), reason, now)
	if err != nil {
		return true, err
	}
	return true, e.settleDrop(ctx, run, store.RotationSettle{
		Now: now, FromPhases: from, Phase: run.Phase, LastError: run.LastError, NextAttempt: next, HaltReason: reason, HaltAudit: &a, Close: closed,
	})
}

// extendDelivered pushes a delivered credential's VALID UNTIL out before it
// lapses, while the rotation is active and its retry window is open. It serves
// two callers: a settled (done) run whose credential falls due, and a run still
// held open after its publish (revoke refused or failing), whose open state
// blocks the next cycle so that nothing else would keep its credential alive. It
// moves VALID UNTIL only: not P, S or D, and not the attempt count, so repeated
// extends cannot keep a failing rotation retrying forever.
//
// It is gated by the pre-publish Gate minus the generation check (the run belongs
// to the generation it was minted under): a rotation whose authority or
// environment no longer permits the credential halts, by name, instead of being
// extended. A held run that reaches the end of its window halts the rotation too,
// loudly, before the credential lapses.
//
// handled reports that the run was settled here; false means nothing was due or
// applicable and the caller carries on with its own step.
func (e *RotationExecutor) extendDelivered(ctx context.Context, run store.RotationRun, from []string) (bool, error) {
	now := e.now()
	held := run.Phase != "done"
	backoff := now.Add(rotationBackoff(run.AttemptCount))
	park := func(next time.Time) (bool, error) {
		if held {
			return false, nil
		}
		return true, e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: from, Phase: "done", NextAttempt: next})
	}
	if run.RotationState != "active" || run.CandidateValidUntil.IsZero() {
		return park(rotationNever)
	}
	if due := rotationExtendDue(run.CandidateValidUntil); now.Before(due) {
		return park(due)
	}
	// The extend INTENT is durable before the supersession check, so a later run that
	// publishes after that check reads this extend as uncertain when it decides
	// whether the role can be dropped; a publish that landed before the check is seen
	// by it and ends the extend. Every exit below closes the intent as a definite
	// failure (the provider was never called), which no reader counts as uncertain.
	intent, err := e.intent(run, "extend", now)
	if err != nil {
		return true, err
	}
	stale, err := e.staleEffect(ctx, run, now)
	if err != nil {
		return true, err
	}
	if err := e.Runtime.Settle(ctx, run, store.RotationSettle{
		Now: now, FromPhases: from, Phase: run.Phase, LastError: run.LastError, NextAttempt: now, KeepLease: true, Open: &intent, Close: stale,
	}); err != nil {
		if errors.Is(err, store.ErrRotationFenceLost) {
			return true, nil
		}
		return true, err
	}
	// skip ends the extend without a provider call: it closes the intent and then
	// parks (or, for a held run, leaves the run to its own revoke step).
	skip := func(next time.Time) (bool, error) {
		closed, err := e.outcome(run, intent.EffectID, "extend", audit.OutcomeFailure, rotCodeExtendSkipped, now)
		if err != nil {
			return true, err
		}
		if held {
			err := e.Runtime.Settle(ctx, run, store.RotationSettle{Now: now, FromPhases: from, Phase: run.Phase, LastError: run.LastError, NextAttempt: now, KeepLease: true, Close: closed})
			if errors.Is(err, store.ErrRotationFenceLost) {
				return true, nil
			}
			return err != nil, err
		}
		return true, e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: from, Phase: "done", NextAttempt: next, Close: closed})
	}
	// failed is skip for a read that errored: the intent is closed best effort, and
	// the read's error is what the caller sees.
	failed := func(readErr error) (bool, error) {
		_, cerr := skip(backoff)
		return true, errors.Join(readErr, cerr)
	}
	superseded, err := e.Runtime.LaterPublished(ctx, run)
	if err != nil {
		return failed(err)
	}
	if superseded {
		return skip(rotationNever)
	}
	if e.AfterExtendSupersessionCheck != nil {
		e.AfterExtendSupersessionCheck()
	}
	published, err := e.Runtime.PublishedAt(ctx, run)
	if err != nil {
		return failed(err)
	}
	if w := rotationCycleWindow(published, time.Duration(run.IntervalSeconds)*time.Second); now.After(w.D) {
		if !held {
			return skip(rotationNever)
		}
		// The window is spent and the run is still open: the credential is about to
		// lapse with nothing left to extend it. Stop loudly rather than silently.
		return e.haltClosing(ctx, run, from, rotHaltExhausted, now, now, intent)
	}
	if err := e.gate(ctx, run.Chain(), run.RotationID, run.AuthorityPrincipalID, run.KeyID, run.UsernameKeyID, gateCheck{generation: anyGeneration}, now); err != nil {
		if errors.Is(err, errRotationHeld) {
			// A restore hold landed after the claim. The claim skips held rotations, so
			// the run stays due without being picked up until the hold is released;
			// parking it at "never" would leave the delivered credential with nothing
			// to extend it.
			return skip(now)
		}
		if errors.Is(err, errRotationInactive) {
			return skip(rotationNever)
		}
		reason, definite := rotationGateReason(err)
		if !definite {
			e.warn("rotation extend gate failed", run, "extend", err)
			return skip(backoff)
		}
		// The gate authorizes before it reads the rotation, so a held rotation whose
		// authority is also refused arrives here as a definite halt. The hold blocks the
		// halt itself, so settling the run at "never" would strand the delivered
		// credential: classify the hold first and keep the run due like the held branch.
		restoreHeld, herr := e.Runtime.RestoreHeld(ctx, run)
		if herr != nil {
			return failed(herr)
		}
		if restoreHeld {
			return skip(now)
		}
		if e.AfterExtendHoldRead != nil {
			e.AfterExtendHoldRead()
		}
		next := rotationNever
		if held {
			// The halt is on record: the held run's own revoke follows at once.
			next = now
		}
		return e.haltClosing(ctx, run, from, reason, now, next, intent)
	}
	provider, _, err := e.openProvider(ctx, run)
	if err != nil {
		e.warn("rotation extend could not open its provider", run, "extend", err)
		return skip(backoff)
	}
	defer provider.Close()
	newValidUntil := run.CandidateValidUntil.Add(rotationSlack)
	opCtx, cancel := context.WithTimeout(ctx, e.providerDeadline())
	err = provider.ExtendRole(opCtx, run.CandidateHandle, newValidUntil)
	cancel()
	now = e.now()
	if err == nil {
		closed, err := e.outcome(run, intent.EffectID, "extend", audit.OutcomeSuccess, "", now)
		if err != nil {
			return true, err
		}
		// A held run is revisited at once: the revoke it was held on gets its
		// next try. A settled run waits for the next extension margin.
		next := rotationExtendDue(newValidUntil)
		if held {
			next = now
		}
		return true, e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: from, Phase: run.Phase, LastError: run.LastError, ValidUntil: newValidUntil, NextAttempt: next, Close: closed})
	}
	result := audit.OutcomeFailure
	if errors.Is(err, dynamic.ErrAmbiguous) {
		result = audit.OutcomeUnknown
	}
	closed, cerr := e.outcome(run, intent.EffectID, "extend", result, rotCodeExtendFailed, now)
	if cerr != nil {
		return true, cerr
	}
	return true, e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: from, Phase: run.Phase, LastError: rotCodeExtendFailed, NextAttempt: now.Add(rotationBackoff(run.AttemptCount)), Close: closed})
}
