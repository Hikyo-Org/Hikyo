package service

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
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
}

func (e *RotationExecutor) now() time.Time { return store.CanonTime(nowOr(e.Now)) }

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
	rotCodeDropFailed      = "drop_failed"
	rotCodeOwnsObjects     = "role_owns_objects"
	rotCodeVersionsChanged = "published_versions_changed"
	rotCodeRevokeFailed    = "revoke_failed"
	rotCodeExtendFailed    = "extend_failed"
	rotCodeExpired         = "expired"
)

// Halt reasons, shared with the rotation.configured audit's reason enum.
const (
	rotHaltExhausted = "retries_exhausted"
	rotHaltAuthority = "authority_lost"
	rotHaltGate      = "gate_failed"
)

var (
	errRotationInactive = errors.New("service: the rotation is no longer active at the generation this run started under")
	errRotationBudget   = errors.New("service: the run exceeded its publish budget")
)

// RunRotationSweep does one unit of executor work and reports whether it did any.
// It claims a due run first (an in-flight run to recover, a settled run owed a
// revoke, a pre-publish run whose candidate is unconfirmed, a delivered
// credential due an extension) and otherwise starts a run for a due rotation.
// It is the body of the worker loop; every node runs it, because every effect is
// claimed under a run's own lease.
func (e *RotationExecutor) RunRotationSweep(ctx context.Context, workerID string) (bool, error) {
	if e.DB == nil || e.Runtime == nil || e.Values == nil || e.Revisions == nil || e.Keyring == nil || e.ProviderFactory == nil {
		return false, errors.New("service: rotation executor requires its datastore, runtime, value and revision services, keyring and provider factory")
	}
	now := e.now()
	run, claimed, err := e.Runtime.ClaimDueRun(ctx, workerID, now, now.Add(rotationLeaseTerm))
	if err != nil {
		return false, err
	}
	if claimed {
		return true, e.processClaimed(ctx, run)
	}
	due, ok, err := e.Runtime.DueRotation(ctx, now)
	if err != nil || !ok {
		return false, err
	}
	started, err := e.startRun(ctx, workerID, due)
	return started, err
}

func (e *RotationExecutor) processClaimed(ctx context.Context, run store.RotationRun) error {
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
		return e.extend(ctx, run)
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
	payload := audit.Payload{"run_id": run.ID, "phase": kind, "handle": handleFor(run, kind)}
	if code != "" {
		payload["error_code"] = code
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
		IntervalSeconds: run.IntervalSeconds, DelaySeconds: run.RevocationDelaySeconds, Generation: run.RotationGeneration}
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

// gateWithin is the full pre-publish Gate, run inside a transaction: the
// rotation is still active at the generation the run started under; the
// authority still holds edit, publish and reveal on both keys; the keys exist and
// the password key is still secret; the environment is still unattended; the
// provider can still mint. It authorizes as the recorded authority principal,
// which is what lets the publish that follows act as that principal.
func (e *RotationExecutor) gateWithin(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, caller authz.Identity, scope domain.Scope, rotationID string, generation int64) (authz.Proof, error) {
	proof, err := az.Authorize(ctx, caller, authz.OpRotationRun, scope)
	if err != nil {
		return nil, err
	}
	rec, err := r.Rotations().Get(ctx, proof, rotationID)
	if err != nil {
		return nil, err
	}
	if rec.State != store.RotationStateActive || rec.Generation != generation {
		return nil, errRotationInactive
	}
	if err := requireUnattendedEnvironment(ctx, r, az, proof, scope); err != nil {
		return nil, err
	}
	if err := requireRotationProvider(ctx, r, proof, rec.ProviderID); err != nil {
		return nil, err
	}
	if err := requireRotationKeys(ctx, r, proof, rec.KeyID, rec.UsernameKeyID, true); err != nil {
		return nil, err
	}
	for _, id := range []string{rec.KeyID, rec.UsernameKeyID} {
		key := authz.KeyByID(id)
		holds, err := az.RecordedPrincipalHolds(ctx, caller, caller.Principal, authz.OpRotationResume, scope, &key)
		if err != nil {
			return nil, err
		}
		if !holds {
			return nil, ErrRotationAuthorityLost
		}
	}
	return proof, nil
}

// gate runs gateWithin in its own transaction, before a run is created.
func (e *RotationExecutor) gate(ctx context.Context, d store.DueRotation, now time.Time) error {
	actor := LocalPrincipal(domain.PrincipalID(d.AuthorityPrincipalID))
	return tx.Write(ctx, e.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, err := actor.resolve(ctx, az, now)
		if err != nil {
			return err
		}
		az.SetClock(now)
		_, err = e.gateWithin(ctx, r, az, caller, d.Chain(), d.ID, d.Generation)
		return err
	})
}

// ---------------------------------------------------------------- schedule

func (e *RotationExecutor) window(ctx context.Context, d store.DueRotation, now time.Time) (rotationWindow, error) {
	cycle, err := e.Runtime.Cycle(ctx, d)
	if err != nil {
		return rotationWindow{}, err
	}
	interval := time.Duration(d.IntervalSeconds) * time.Second
	switch {
	case cycle.OwnPublishedRevision != 0:
		published, err := e.Runtime.PublishedAt(ctx, store.RotationRun{OrgID: d.OrgID, ProjectID: d.ProjectID, EnvironmentID: d.EnvironmentID, PublishedRevision: cycle.OwnPublishedRevision})
		if err != nil {
			return rotationWindow{}, err
		}
		return rotationCycleWindow(published, interval), nil
	case !cycle.KeyEverPublished:
		return rotationWindow{}, nil
	case cycle.FirstRunAt.IsZero():
		return rotationInheritedWindow(now), nil
	default:
		return rotationInheritedWindow(cycle.FirstRunAt), nil
	}
}

func dueOfRun(run store.RotationRun) store.DueRotation {
	return store.DueRotation{
		ID: run.RotationID, OrgID: run.OrgID, ProjectID: run.ProjectID, EnvironmentID: run.EnvironmentID, ProviderID: run.ProviderID,
		KeyID: run.KeyID, UsernameKeyID: run.UsernameKeyID, IntervalSeconds: run.IntervalSeconds,
		RevocationDelaySeconds: run.RevocationDelaySeconds, AuthorityPrincipalID: run.AuthorityPrincipalID, Generation: run.RunGeneration,
	}
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
	if err := e.gate(ctx, d, now); err != nil {
		reason, definite := rotationGateReason(err)
		if errors.Is(err, errRotationInactive) {
			// Paused or changed between selection and the gate: nothing to do.
			return true, nil
		}
		if !definite {
			return false, err
		}
		return true, e.haltDue(ctx, d, reason, now)
	}
	w, err := e.window(ctx, d, now)
	if err != nil {
		return false, err
	}
	spent, err := e.Runtime.FailedAttempts(ctx, d.Chain(), d.ID, d.Generation, w.Since)
	if err != nil {
		return false, err
	}
	if !rotationMayStart(now, spent, w) {
		return true, e.haltDue(ctx, d, rotHaltExhausted, now)
	}
	runID, err := newID("rrn")
	if err != nil {
		return false, err
	}
	run := store.RotationRun{
		ID: runID, OrgID: d.OrgID, ProjectID: d.ProjectID, EnvironmentID: d.EnvironmentID, RotationID: d.ID,
		CandidateHandle: dynamic.RoleName(runID), AuthorityPrincipalID: d.AuthorityPrincipalID,
	}
	intent, err := e.intent(run, "mint", now)
	if err != nil {
		return false, err
	}
	interval := time.Duration(d.IntervalSeconds) * time.Second
	delay := time.Duration(d.RevocationDelaySeconds) * time.Second
	started, ok, err := e.Runtime.StartRun(ctx, d, store.StartRun{
		RunID: runID, Worker: worker, Now: now, LeaseUntil: now.Add(rotationLeaseTerm), Trigger: "schedule",
		CandidateHandle: run.CandidateHandle,
		// VALID UNTIL = mint + publish_budget + interval + delay + slack, so a
		// publish anywhere inside the budget leaves at least interval + delay +
		// slack of validity.
		CandidateValidUntil: now.Add(rotationPublishBudget + interval + delay + rotationSlack),
		ParkedUntil:         now.Add(rotationPublishBudget), Intent: intent,
	})
	if err != nil {
		return false, err
	}
	if !ok {
		// Another worker took the rotation, or it was paused or already running.
		return true, nil
	}
	return true, e.mint(ctx, started, intent.EffectID)
}

func (e *RotationExecutor) haltDue(ctx context.Context, d store.DueRotation, reason string, now time.Time) error {
	a, err := e.haltAudit(d.Chain(), subjectOfDue(d), reason, now)
	if err != nil {
		return err
	}
	_, err = e.Runtime.HaltRotation(ctx, d, reason, now, a)
	return err
}

// ---------------------------------------------------------------- provider

func (e *RotationExecutor) openProvider(ctx context.Context, run store.RotationRun) (dynamic.Provider, store.LeaseProviderMaterial, error) {
	material, err := e.Runtime.LoadProviderMaterial(ctx, run, e.now())
	if err != nil {
		return nil, store.LeaseProviderMaterial{}, err
	}
	sealer, err := e.Keyring.ForProject(ctx, run.OrgID, run.ProjectID)
	if err != nil {
		return nil, store.LeaseProviderMaterial{}, err
	}
	credential, err := sealer.OpenField(providerCredentialAAD(run.OrgID, run.ProjectID, material.CredentialOwnerID), material.CredentialCiphertext)
	if err != nil {
		return nil, store.LeaseProviderMaterial{}, err
	}
	defer crypto.Zero(credential)
	kind, err := dynamic.ParseKind(material.Kind)
	if err != nil {
		return nil, store.LeaseProviderMaterial{}, err
	}
	provider, err := e.ProviderFactory(kind, material.Origin, material.TLSMode, string(credential))
	if err != nil {
		return nil, store.LeaseProviderMaterial{}, err
	}
	return provider, material, nil
}

// ---------------------------------------------------------------- mint + publish

// mint creates the candidate role, then publishes it. The run is `minting` with
// its INTENT recorded and its handle durable before the provider is touched.
func (e *RotationExecutor) mint(ctx context.Context, run store.RotationRun, mintEffect string) error {
	provider, material, err := e.openProvider(ctx, run)
	if err != nil {
		// Nothing was sent to the provider: a definite failure, no drop owed.
		return e.failUnminted(ctx, run, mintEffect, rotCodeProvider)
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
	return e.confirmAbsent(ctx, run, "no_candidate")
}

// confirmAbsent records the drop_candidate effect as a success for a run whose
// candidate is known not to exist (or to be gone), and releases the lease.
func (e *RotationExecutor) confirmAbsent(ctx context.Context, run store.RotationRun, how string) error {
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
	closed, err := e.outcome(run, intent.EffectID, "drop_candidate", audit.OutcomeSuccess, "", now)
	if err != nil {
		return err
	}
	_ = how
	err = e.Runtime.Settle(ctx, run, store.RotationSettle{
		Now: now, FromPhases: []string{"failed", "quarantine"}, Phase: "failed", NextAttempt: rotationNever, Close: closed,
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
		// Paused, deleted or already halted: the schedule is not ours to move.
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
		a, err := e.haltAudit(run.Chain(), subjectOfRun(run), halt, now)
		if err != nil {
			return settle, err
		}
		settle.Audits = append(settle.Audits, a)
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
	return e.dropCandidate(ctx, provider, run, intent.EffectID)
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
	// into the overlap wait and release it.
	return e.settleDrop(ctx, run, store.RotationSettle{
		Now: e.now(), FromPhases: []string{"published"}, Phase: "overlap", NextAttempt: cur.NextAttemptAt,
	})
}

// dropOrphan drops the candidate of a worker that lost its claim while the role
// was created, but only after seeing the run rolled back with no publish. Errors
// are ignored: the candidate reconciler owns the run and will re-probe.
func (e *RotationExecutor) dropOrphan(ctx context.Context, provider dynamic.Provider, run store.RotationRun) error {
	cur, err := e.Runtime.GetRun(ctx, run.Chain(), run.ID)
	if err != nil {
		return err
	}
	if cur.PublishedRevision != 0 || (cur.Phase != "failed" && cur.Phase != "quarantine") {
		return nil
	}
	opCtx, cancel := context.WithTimeout(ctx, e.providerDeadline())
	defer cancel()
	_ = provider.DropRole(opCtx, run.CandidateHandle)
	return nil
}

// dropCandidate drops the candidate of a run already moved to `failed` with its
// INTENT recorded, and records the outcome: a confirmed drop settles the run; a
// definite failure leaves it to the reconciler; an unknown outcome quarantines it.
func (e *RotationExecutor) dropCandidate(ctx context.Context, provider dynamic.Provider, run store.RotationRun, effectID string) error {
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
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{"failed"}, Phase: "failed", NextAttempt: rotationNever, Close: closed})
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

func (e *RotationExecutor) settleDrop(ctx context.Context, run store.RotationRun, s store.RotationSettle) error {
	err := e.Runtime.Settle(ctx, run, s)
	if errors.Is(err, store.ErrRotationFenceLost) {
		return nil
	}
	return err
}

// recoverInFlight is a claimed run found minting or minted: its worker died and
// its password with it. It always rolls back, under the fenced claim write.
func (e *RotationExecutor) recoverInFlight(ctx context.Context, run store.RotationRun) error {
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
	_ = sealer
	var committedAt time.Time
	hooks := &rotationPublish{
		key: authz.KeyByID(run.KeyID),
		begin: func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, caller authz.Identity, now time.Time) error {
			now = store.CanonTime(now)
			if now.After(run.StartedAt.Add(rotationPublishBudget)) {
				return errRotationBudget
			}
			proof, err := e.gateWithin(ctx, r, az, caller, scope, run.RotationID, run.RunGeneration)
			if err != nil {
				return err
			}
			return r.Rotations().BeginRunPublish(ctx, proof, run, now)
		},
		stage: func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer, caller authz.Identity, now time.Time) ([]string, error) {
			proof, err := az.Authorize(ctx, caller, authz.OpRotationRun, scope)
			if err != nil {
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
			now = store.CanonTime(now)
			proof, err := az.Authorize(ctx, caller, authz.OpRotationRun, scope)
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
			closed, err := e.outcome(run, publishIntent.EffectID, "publish", audit.OutcomeSuccess, "", now)
			if err != nil {
				return err
			}
			interval := time.Duration(run.IntervalSeconds) * time.Second
			delay := time.Duration(run.RevocationDelaySeconds) * time.Second
			if err := r.Rotations().RecordRunPublish(ctx, proof, store.RotationPublishRecord{
				Run: run, Revision: revision, Now: now, NextRunAt: now.Add(interval), NextAttempt: now.Add(delay), Outcome: *closed,
			}); err != nil {
				return err
			}
			committedAt = now
			return nil
		},
	}
	_, err = e.Revisions.PublishPlanned(ctx, actor, scope, PublishRequest{rotation: hooks})
	if err != nil {
		return e.rollbackAfterPublishError(ctx, provider, run, publishIntent.EffectID, err)
	}
	// Committed. The phase is `published`; settle into the overlap wait and release
	// the claim. A failed fence here is harmless: the next claim finds `published`.
	now := e.now()
	err = e.Runtime.Settle(ctx, run, store.RotationSettle{
		Now: now, FromPhases: []string{"published"}, Phase: "overlap",
		NextAttempt: committedAt.Add(time.Duration(run.RevocationDelaySeconds) * time.Second),
	})
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
	case errors.Is(cause, errRotationInactive):
		code = rotCodePaused
	default:
		if reason, definite := rotationGateReason(cause); definite {
			halt = reason
		}
	}
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

// reconcileCandidate settles a pre-publish run (failed or quarantine) whose
// candidate drop is unconfirmed: it probes the role, drops it when present and
// records the confirmed drop, after which the run is `failed` and purgeable. It
// owns published_revision IS NULL only; a run with a recorded publish is never
// touched here, because its candidate IS the delivered credential.
func (e *RotationExecutor) reconcileCandidate(ctx context.Context, run store.RotationRun) error {
	now := e.now()
	retry := func(code string) error {
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{run.Phase}, Phase: run.Phase, LastError: code, NextAttempt: now.Add(rotationBackoff(run.AttemptCount))})
	}
	provider, _, err := e.openProvider(ctx, run)
	if err != nil {
		return retry(rotCodeProvider)
	}
	defer provider.Close()
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
	opCtx, cancel := context.WithTimeout(ctx, e.providerDeadline())
	status, err := provider.RoleStatus(opCtx, run.CandidateHandle)
	if err == nil && status.Exists {
		err = provider.DropRole(opCtx, run.CandidateHandle)
	}
	cancel()
	switch {
	case err == nil:
		closed, err := e.outcome(run, intent.EffectID, "drop_candidate", audit.OutcomeSuccess, "", now)
		if err != nil {
			return err
		}
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{run.Phase}, Phase: "failed", NextAttempt: rotationNever, Close: closed})
	case !run.CandidateValidUntil.IsZero() && now.After(run.CandidateValidUntil):
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
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: from, Phase: phase, LastError: code, NextAttempt: now.Add(rotationBackoff(run.AttemptCount))})
	}
	refusedPhase := run.Phase
	if executorPhase {
		refusedPhase = "revoke_refused"
	}
	provider, _, err := e.openProvider(ctx, run)
	if err != nil {
		return hold(run.Phase, rotCodeProvider)
	}
	defer provider.Close()
	restore, err := e.Runtime.RestoreAssertion(ctx, run)
	if err != nil {
		return err
	}
	if !restore.Holds {
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
		Now: now, FromPhases: from, Phase: revokingPhase, LastError: "", NextAttempt: now, KeepLease: true, Open: &intent, Close: stale,
	}); err != nil {
		if errors.Is(err, store.ErrRotationFenceLost) {
			return nil
		}
		return err
	}
	opCtx, cancel := context.WithTimeout(ctx, e.providerDeadline())
	err = provider.DropRole(opCtx, run.SupersededHandle)
	cancel()
	now = e.now()
	backoff := now.Add(rotationBackoff(run.AttemptCount))
	finish := func(result audit.Outcome, code, phase string, next time.Time) error {
		closed, err := e.outcome(run, intent.EffectID, "revoke", result, code, now)
		if err != nil {
			return err
		}
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{revokingPhase}, Phase: phase, LastError: code, NextAttempt: next, Close: closed})
	}
	switch {
	case err == nil:
		return finish(audit.OutcomeSuccess, "", "done", rotationExtendDue(run.CandidateValidUntil))
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

// ---------------------------------------------------------------- extend

// extend pushes a delivered credential's VALID UNTIL out before it lapses, while
// the rotation is active and its retry window is open. It moves VALID UNTIL only:
// not P, S or D, and not the attempt count, so repeated extends cannot keep a
// failing rotation retrying forever.
func (e *RotationExecutor) extend(ctx context.Context, run store.RotationRun) error {
	now := e.now()
	park := func(next time.Time) error {
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{"done"}, Phase: "done", NextAttempt: next})
	}
	if run.RotationState != "active" || run.CandidateValidUntil.IsZero() {
		return park(rotationNever)
	}
	if due := rotationExtendDue(run.CandidateValidUntil); now.Before(due) {
		return park(due)
	}
	superseded, err := e.Runtime.LaterPublished(ctx, run)
	if err != nil {
		return err
	}
	if superseded {
		return park(rotationNever)
	}
	published, err := e.Runtime.PublishedAt(ctx, run)
	if err != nil {
		return err
	}
	if w := rotationCycleWindow(published, time.Duration(run.IntervalSeconds)*time.Second); now.After(w.D) {
		return park(rotationNever)
	}
	provider, _, err := e.openProvider(ctx, run)
	if err != nil {
		return park(now.Add(rotationBackoff(run.AttemptCount)))
	}
	defer provider.Close()
	intent, err := e.intent(run, "extend", now)
	if err != nil {
		return err
	}
	stale, err := e.staleEffect(ctx, run, now)
	if err != nil {
		return err
	}
	if err := e.Runtime.Settle(ctx, run, store.RotationSettle{
		Now: now, FromPhases: []string{"done"}, Phase: "done", NextAttempt: now, KeepLease: true, Open: &intent, Close: stale,
	}); err != nil {
		if errors.Is(err, store.ErrRotationFenceLost) {
			return nil
		}
		return err
	}
	newValidUntil := run.CandidateValidUntil.Add(rotationSlack)
	opCtx, cancel := context.WithTimeout(ctx, e.providerDeadline())
	err = provider.ExtendRole(opCtx, run.CandidateHandle, newValidUntil)
	cancel()
	now = e.now()
	if err == nil {
		closed, err := e.outcome(run, intent.EffectID, "extend", audit.OutcomeSuccess, "", now)
		if err != nil {
			return err
		}
		return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{"done"}, Phase: "done", ValidUntil: newValidUntil, NextAttempt: rotationExtendDue(newValidUntil), Close: closed})
	}
	result := audit.OutcomeFailure
	if errors.Is(err, dynamic.ErrAmbiguous) {
		result = audit.OutcomeUnknown
	}
	closed, cerr := e.outcome(run, intent.EffectID, "extend", result, rotCodeExtendFailed, now)
	if cerr != nil {
		return cerr
	}
	return e.settleDrop(ctx, run, store.RotationSettle{Now: now, FromPhases: []string{"done"}, Phase: "done", LastError: rotCodeExtendFailed, NextAttempt: now.Add(rotationBackoff(run.AttemptCount)), Close: closed})
}
