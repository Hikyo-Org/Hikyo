package securitymaintenance

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securityfeed"
	"github.com/Hikyo-Org/hikyo/internal/securitystatus"
)

type Observation struct {
	Platform            string                   `json:"platform"`
	Engine              releaseidentity.Engine   `json:"engine"`
	Identity            releaseidentity.Identity `json:"identity"`
	StagedTarget        releaseidentity.Identity `json:"staged_target,omitzero"`
	Image               string                   `json:"image"`
	RuntimeImageSHA256  releaseidentity.Digest   `json:"runtime_image_sha256,omitempty"`
	Instance            string                   `json:"instance"`
	Generation          int64                    `json:"generation"`
	RestoreEpoch        int64                    `json:"restore_epoch"`
	Incarnation         string                   `json:"incarnation"`
	Phase               string                   `json:"phase"`
	Maintenance         bool                     `json:"maintenance"`
	Ready               bool                     `json:"ready"`
	RouteSHA256         releaseidentity.Digest   `json:"route_sha256,omitempty"`
	RequiresMaintenance bool                     `json:"requires_maintenance"`
	SchemaChanges       bool                     `json:"schema_changes"`
	// AbortedBeforeMutation: the target image reports that it stopped before the
	// live database was written, and the other fields describe the untouched source.
	AbortedBeforeMutation bool `json:"aborted_before_mutation,omitzero"`
}

func (o Observation) Validate() error {
	if o.Engine.Validate() != nil || o.Identity.Validate() != nil || o.Instance == "" || len(o.Instance) > 256 || o.Generation < 1 ||
		o.RestoreEpoch < 0 || releaseidentity.Digest(o.Incarnation).Validate() != nil || len(o.Phase) > 64 {
		return errors.New("invalid observed installation identity or recovery incarnation")
	}
	return nil
}

func (o Observation) Healthy() bool {
	return o.Validate() == nil && o.Ready && !o.Maintenance && o.Phase == "healthy"
}

func sameInstallation(a, b Observation) bool {
	return a.Instance == b.Instance && a.Incarnation == b.Incarnation && a.RestoreEpoch == b.RestoreEpoch
}

// Preparation is evidence from a local driver that independently authenticates
// artifacts and stages every route hop. A feed label is never this evidence.
type Preparation struct {
	Target      releaseidentity.Identity
	Source      Observation
	Image       string
	RouteSHA256 releaseidentity.Digest
	Impact      Impact
}

type Driver interface {
	Observe(context.Context) (Observation, error)
	Prepare(context.Context, Target) (Preparation, error)
	Apply(context.Context, Target) error
}

// ErrNotStarted may be returned only by a trusted local driver that refused
// before any workload mutation, or that proved the exact source image is serving
// again after its target stopped before the database was written. Other
// execution failures retain recovery pins. Its text becomes the status detail, so
// it must hold for both cases.
var ErrNotStarted = errors.New("security application did not take effect")

// ErrSourceRestored accompanies ErrNotStarted when a driver stopped the source
// and proved it serving again. Unlike a refusal before any mutation, that cost an
// outage, so it counts toward the retry limit.
var ErrSourceRestored = errors.New("the source was interrupted and is serving again")

// ErrRolledBack accompanies ErrSourceRestored when the target had written and the
// fenced snapshot was restored: nothing was "refused before the database changed".
var ErrRolledBack = errors.New(ReplacementUndone)

// Reconciler is implemented by drivers holding their own recovery journal.
type Reconciler interface {
	Reconcile(context.Context, Observation) error
}

// AbortRestorer is implemented by drivers that can put the source back after its
// target reported that it stopped before writing the database, or, with an
// observation that does not report it, after the operation's deadline passed
// without the target proving healthy. It returns ErrNotStarted once the exact
// source is observed serving untouched again; any other result keeps the
// recovery pins, and a later run asks again.
type AbortRestorer interface {
	RestoreAborted(ctx context.Context, aborted Observation, operation Operation) error
}

// GateProver is implemented by drivers that can show, without the target answering, that
// the target has not crossed the final gate. Without such proof an unobservable target is
// never stopped by the deadline.
type GateProver interface {
	NoFinalGate(ctx context.Context, operation Operation) (bool, error)
}

// ErrPastFinalGate keeps the pin: a target that is healthy at its release may have served.
var ErrPastFinalGate = errors.New("the target passed the final gate; a person decides whether it stays")

// ErrProtectionGap makes a known unremediated advisory fail the scheduled unit,
// so ordinary host monitoring sees it without any project telemetry.
var ErrProtectionGap = errors.New("applicable security advisories remain unremediated")

// A source still healthy at its original generation this long after an attempt
// began was not replaced. Shorter would race a slow provider rollout.
const operationSettle = time.Hour

const (
	retryBase = 5 * time.Minute
	retryCap  = 6 * time.Hour

	gapAfterAttempts = 3
	maxDetailBytes   = 1024

	// Each failure that reached the workload can be an outage, so a deterministic
	// one must not repeat forever. Only an operator or a different target resumes.
	maxDisruptiveAttempts = 3
)

type Feed interface {
	Refresh(context.Context) (securityfeed.Document, error)
}

type Enrollment struct {
	Instance     string    `json:"instance"`
	Incarnation  string    `json:"incarnation"`
	RestoreEpoch int64     `json:"restore_epoch"`
	ConsentedAt  time.Time `json:"consented_at"`
}

type Operation struct {
	RequireRuntimeDigest bool                   `json:"require_runtime_digest"`
	Target               Target                 `json:"target"`
	Source               Observation            `json:"source"`
	RouteSHA256          releaseidentity.Digest `json:"route_sha256"`
	StartedAt            time.Time              `json:"started_at"`
}

type Status struct {
	Policy            *Policy                `json:"policy,omitzero"`
	Format            string                 `json:"format"`
	Enrollment        *Enrollment            `json:"enrollment,omitzero"`
	Phase             string                 `json:"phase"`
	Reason            string                 `json:"reason,omitempty"`
	CatalogSequence   uint64                 `json:"catalog_sequence"`
	CatalogSHA256     releaseidentity.Digest `json:"catalog_sha256,omitempty"`
	LastAttempt       time.Time              `json:"last_attempt,omitzero"`
	LastVerified      time.Time              `json:"last_verified,omitzero"`
	KnownUntil        time.Time              `json:"known_until,omitzero"`
	Applicable        []string               `json:"applicable"`
	CoverageThrough   string                 `json:"coverage_through,omitempty"`
	Coverage          []string               `json:"coverage"`
	CoveredComponents []string               `json:"covered_components"`
	Selected          *Target                `json:"selected,omitzero"`
	Running           *Observation           `json:"running,omitzero"`
	// RunningWithdrawn: the authenticated catalog of the latest run withdraws the
	// running release. The coordinator then refuses to adopt it as its own.
	RunningWithdrawn bool       `json:"running_withdrawn,omitzero"`
	Operation        *Operation `json:"operation,omitzero"`
	// Action names the exact local command that leaves a state needing an operator.
	Action string `json:"action,omitempty"`
	// Detail keeps the cause of the latest failure, so status explains it alone.
	Detail string `json:"detail,omitempty"`
	// Retry bounds repeated failure on one target; a different target resets it.
	RetryTarget   *releaseidentity.Identity `json:"retry_target,omitzero"`
	RetryAttempts int                       `json:"retry_attempts,omitzero"`
	RetryAfter    time.Time                 `json:"retry_after,omitzero"`
	// RetryDisruptive counts the failed attempts at RetryTarget that reached the
	// workload. A failure before that point costs no availability and only backs off.
	RetryDisruptive int    `json:"retry_disruptive,omitzero"`
	RetryCause      string `json:"retry_cause,omitempty"`
}

func (s Status) retriesExhausted() bool {
	return s.RetryTarget != nil && s.RetryDisruptive >= maxDisruptiveAttempts
}

// ProtectionGap distinguishes a stuck installation from one waiting for an
// expected local or published schedule.
func (s Status) ProtectionGap() bool {
	// A pinned operation with a command for a person is a gap, whatever else is known.
	if s.Operation != nil && s.Action != "" {
		return true
	}
	if s.Phase != "blocked" || len(s.Applicable) == 0 {
		return false
	}
	// A failure that keeps repeating is not an expected wait, even between attempts.
	if s.Reason == "retry_backoff" {
		return s.RetryAttempts >= gapAfterAttempts
	}
	return !slices.Contains([]string{"maintenance_window", "staged_rollout", "manual_mode"}, s.Reason)
}

// noteUndone records a restore that put the source back. Only a restore that stopped
// the source counts as a disruption; one that found it untouched costs no availability.
func (s *Status) noteUndone(restoreErr error) {
	stopped := errors.Is(restoreErr, ErrSourceRestored)
	cause := errors.New("the replacement never took effect and the source is serving untouched")
	if stopped {
		cause = errors.New(ReplacementUndone)
		s.Detail = ReplacementUndone + "; run check to see which advisories still apply"
	}
	s.noteFailure(s.Operation.Target.Identity, s.LastAttempt, stopped, cause)
}

func (s *Status) noteFailure(target releaseidentity.Identity, now time.Time, disruptive bool, cause error) {
	if s.RetryTarget == nil || *s.RetryTarget != target {
		s.clearRetry()
		s.RetryTarget = &target
	}
	if disruptive {
		s.RetryDisruptive++
	}
	if cause != nil {
		s.RetryCause = strings.ToValidUTF8(cause.Error()[:min(len(cause.Error()), maxDetailBytes/2)], "")
	}
	delay := retryCap
	if s.RetryAttempts < 8 && retryBase<<s.RetryAttempts < retryCap {
		delay = retryBase << s.RetryAttempts
	}
	s.RetryAttempts++
	s.RetryAfter = now.Add(delay)
}

func (s *Status) clearRetry() {
	s.RetryTarget, s.RetryAttempts, s.RetryAfter, s.RetryDisruptive, s.RetryCause = nil, 0, time.Time{}, 0, ""
}

const StatusFormat = securitystatus.Format

type Engine struct {
	RequireRuntimeDigest bool
	Store                *Store
	Feed                 Feed
	Driver               Driver
	Platform             string
	ImageDelivery        bool
	Policy               Policy
	Now                  func() time.Time
	// OperationTimeout is how long a replacement may stay unfinished before a later
	// run has the driver roll it back to the source. Zero never rolls back by time.
	OperationTimeout time.Duration
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func (e *Engine) validate() error {
	if e.Store == nil || e.Feed == nil || e.Driver == nil || !platformName.MatchString(e.Platform) {
		return errors.New("security maintenance requires configured state, feed, driver and platform")
	}
	return e.Policy.Validate()
}

// Enroll is an explicit local operator action. Merely running a checker or
// changing a configuration flag can never adopt a populated installation.
func (e *Engine) Enroll(ctx context.Context) (Status, error) {
	if err := e.validate(); err != nil {
		return Status{}, err
	}
	var result Status
	err := e.Store.locked(ctx, func() error {
		state, err := e.Store.Load()
		if err != nil {
			return err
		}
		// The chart's enrollment hook runs on every upgrade, so an existing enrollment
		// must succeed unobserved or it would block the upgrade a recovery needs.
		if state.Enrollment != nil {
			result = state
			return nil
		}
		observed, err := e.Driver.Observe(ctx)
		if err != nil {
			return err
		}
		if !observed.Healthy() {
			return errors.New("enrollment requires an admitted healthy exact installation")
		}
		state.Enrollment = &Enrollment{Instance: observed.Instance, Incarnation: observed.Incarnation, RestoreEpoch: observed.RestoreEpoch, ConsentedAt: e.now()}
		policy := e.Policy
		state.Policy = &policy
		state.Running, state.Phase, state.Reason = &observed, "enrolled", ""
		result = state
		return e.Store.Save(state)
	})
	return result, err
}

func reconcileAction(confirm string) string {
	return "hikyo security-maintenance reconcile --confirm " + confirm
}

// Reconcile is the explicit local exit from a state that blocks every later
// update: abandoning a pinned operation, or accepting a restored installation.
// confirm must name what is being given up, so it cannot be run by rote, and the
// installation must be healthy, so it cannot bypass an owner's recovery fence.
func (e *Engine) Reconcile(ctx context.Context, confirm string) (Status, error) {
	if err := e.validate(); err != nil {
		return Status{}, err
	}
	var result Status
	err := e.Store.locked(ctx, func() error {
		state, err := e.Store.Load()
		if err != nil {
			return err
		}
		result = state
		if state.Enrollment == nil {
			return errors.New("installation is not enrolled; use enroll")
		}
		observed, err := e.Driver.Observe(ctx)
		if err != nil {
			return err
		}
		if !observed.Healthy() {
			return errors.New("reconciliation requires a healthy installation; complete the owning upgrade recovery first")
		}
		restored := state.Enrollment.Instance != observed.Instance || state.Enrollment.Incarnation != observed.Incarnation || state.Enrollment.RestoreEpoch != observed.RestoreEpoch
		var token string
		switch {
		case restored:
			token = observed.Incarnation
		case state.Operation != nil:
			token = string(state.Operation.Target.Identity.ManifestSHA256)
		case state.retriesExhausted():
			token = string(state.RetryTarget.ManifestSHA256)
		default:
			return errors.New("nothing to reconcile")
		}
		if confirm != token {
			return errors.New("confirmation does not match; run: " + reconcileAction(token))
		}
		if reconciler, ok := e.Driver.(Reconciler); ok {
			// An error matching ErrRolledBack is a release that follows an earlier rollback: released all the same.
			if err := reconciler.Reconcile(ctx, observed); err != nil && !errors.Is(err, ErrRolledBack) {
				return err
			}
		}
		state.Operation, state.Selected, state.Action, state.Detail = nil, nil, "", ""
		state.clearRetry()
		if restored {
			state.Enrollment = &Enrollment{Instance: observed.Instance, Incarnation: observed.Incarnation, RestoreEpoch: observed.RestoreEpoch, ConsentedAt: e.now()}
		}
		state.Running, state.Phase, state.Reason = &observed, "enrolled", ""
		result = state
		return e.Store.Save(state)
	})
	return result, err
}

// RunOnce performs a bounded check. apply=false never stages artifacts or
// changes a workload. apply=true still requires prior explicit enrollment and
// eligible local policy. Failures never stop an otherwise admitted service.
func (e *Engine) RunOnce(ctx context.Context, apply bool) (Status, error) {
	if err := e.validate(); err != nil {
		return Status{}, err
	}
	var state Status
	err := e.Store.locked(ctx, func() error {
		var err error
		state, err = e.Store.Load()
		if err != nil {
			return err
		}
		state.LastAttempt = e.now()
		state.Detail = ""
		policy := e.Policy
		state.Policy = &policy
		// rolledBack is set once this run put the source back after the target had written: whatever
		// else stops the run afterwards, the rollback is the outcome the status reports, and the run fails.
		rolledBack := false
		saveFailure := func(phase, reason string, cause error) error {
			if rolledBack {
				reason = "operation_rolled_back"
				if !errors.Is(cause, ErrRolledBack) {
					cause = rolledBackRun{cause}
				}
			}
			state.Phase, state.Reason = phase, reason
			if cause != nil {
				// A rollback earlier in this run left its own account in Detail (RunOnce clears it first); the
				// failure that stopped the run must not hide that the earlier release is serving, and the
				// truncation must cut the cause, not that account.
				detail := cause.Error()
				if state.Detail != "" && !strings.HasPrefix(state.Detail, detail) {
					detail = state.Detail + "\n" + detail
				} else if state.Detail != "" {
					detail = state.Detail
				}
				state.Detail = strings.ToValidUTF8(detail[:min(len(detail), maxDetailBytes)], "")
			}
			return errors.Join(cause, e.Store.Save(state))
		}
		// Putting the source back is not an update, so neither a manual policy nor a feed that cannot be trusted may keep a stopped service down.
		restoreDue := func(cause error) error {
			if !apply || state.Operation == nil {
				return cause
			}
			observed, err := e.Driver.Observe(ctx)
			var seen *Observation
			if err == nil {
				if observed.Validate() != nil {
					return cause
				}
				seen = &observed
			}
			restored, restoreErr := e.restoreOrRollBack(ctx, *state.Operation, seen)
			if !restored {
				if restoreErr != nil {
					// Due, and not undone: the installation may be down, so name the exact command.
					state.Action = reconcileAction(string(state.Operation.Target.Identity.ManifestSHA256))
				}
				return errors.Join(cause, err, restoreErr)
			}
			state.noteUndone(restoreErr)
			state.Operation, state.Action = nil, ""
			// What was observed described the stopped target, not the restored source.
			e.recordObservation(&state, Catalog{}, nil)
			rolledBack = rolledBack || errors.Is(restoreErr, ErrRolledBack)
			return cause
		}
		if e.Policy.Mode == Manual && apply {
			return saveFailure("blocked", "manual_mode", restoreDue(nil))
		}
		stale := func(reason string, cause error) error {
			return saveFailure("stale", reason, restoreDue(cause))
		}
		document, err := e.Feed.Refresh(ctx)
		if err != nil {
			return stale("feed_unavailable", err)
		}
		catalog, err := DecodeCatalog(document.Data, state.LastAttempt)
		if err != nil {
			return stale("catalog_invalid", err)
		}
		digest := releaseidentity.Hash(document.Data)
		if catalog.Sequence < state.CatalogSequence || catalog.Sequence == state.CatalogSequence && state.CatalogSHA256 != "" && state.CatalogSHA256 != digest {
			return stale("catalog_rollback_or_equivocation", errors.New("security catalog rollback or equivocation refused"))
		}
		state.CatalogSequence, state.CatalogSHA256 = catalog.Sequence, digest
		state.LastVerified, state.KnownUntil = state.LastAttempt, catalog.ExpiresAt
		if !document.Expires.IsZero() && document.Expires.Before(state.KnownUntil) {
			state.KnownUntil = document.Expires
		}
		if !state.KnownUntil.After(state.LastAttempt) {
			return stale("metadata_expired", errors.New("security metadata has expired"))
		}
		// Fresh feed knowledge cannot carry forward an unobserved artifact's
		// old applicability or coverage claims under its new verification time.
		e.recordObservation(&state, catalog, nil)
		if state.Operation == nil {
			state.Selected = nil
		}
		// Persist authenticated knowledge even if platform inspection fails. A
		// later retry cannot erase a higher catalog sequence after a local error.
		if err := e.Store.Save(state); err != nil {
			return err
		}
		observed, err := e.Driver.Observe(ctx)
		if err != nil {
			// A target that never started cannot be observed, and its deadline still passes.
			pinned := state.Operation != nil
			rollbackErr := restoreDue(nil)
			if pinned && state.Operation == nil {
				// The source is serving again: describe it with the catalog this run verified, so applicability
				// and the protection gap are the source's, not those of a target that is gone.
				seen, seenErr := e.Driver.Observe(ctx)
				if seenErr != nil {
					e.recordObservation(&state, catalog, nil)
				} else {
					e.recordObservation(&state, catalog, &seen)
				}
				if rollbackErr == nil && !rolledBack {
					rollbackErr = errors.New(ReplacementUndone)
				}
				return saveFailure("blocked", "preapply_refused", errors.Join(rollbackErr, seenErr))
			}
			return saveFailure("blocked", "installation_unavailable", errors.Join(err, rollbackErr))
		}
		if err := observed.Validate(); err != nil {
			return saveFailure("blocked", "installation_invalid", err)
		}
		if observed.Platform != "" && observed.Platform != e.Platform {
			return saveFailure("blocked", "installation_platform_mismatch", errors.New("maintenance executor and installed workload platforms differ"))
		}
		e.recordObservation(&state, catalog, &observed)
		if state.Enrollment == nil {
			return saveFailure("blocked", "explicit_enrollment_required", nil)
		}
		if state.Enrollment.Instance != observed.Instance || state.Enrollment.Incarnation != observed.Incarnation || state.Enrollment.RestoreEpoch != observed.RestoreEpoch {
			state.Action = reconcileAction(observed.Incarnation)
			return saveFailure("blocked", "recovery_incarnation_changed", errors.New("maintenance enrollment invalidated by restore or installation change"))
		}
		if state.Operation != nil {
			if completed(*state.Operation, observed) {
				state.Selected = &state.Operation.Target
				state.Operation, state.Phase, state.Reason, state.Action = nil, "healthy", "", ""
				state.clearRetry()
				return e.Store.Save(state)
			}
			// The exact target remains pinned after uncertain writes or process
			// death, unless the original source is provably still serving at its
			// original generation, which shows no replacement took effect.
			pinned := errors.New("unfinished security operation requires exact-target operator reconciliation")
			released, restored := false, false
			var restoreFault error
			// The target's own report is a positive signal, so it needs no settling time.
			// It is acted on here as well as in Apply, because the process that applied
			// may be gone, or its restore may have failed transiently.
			if apply {
				var restoreErr error
				restored, restoreErr = e.restoreOrRollBack(ctx, *state.Operation, &observed)
				released, restoreFault = restored, restoreErr
				if !released {
					pinned = errors.Join(pinned, restoreErr)
				}
			}
			// The release clears a journal, so a check leaves it to run.
			if reconciler, ok := e.Driver.(Reconciler); apply && ok && !released && untouched(*state.Operation, observed) && state.LastAttempt.Sub(state.Operation.StartedAt) >= operationSettle {
				releaseErr := reconciler.Reconcile(ctx, observed)
				if errors.Is(releaseErr, ErrRolledBack) {
					// The release follows a rollback an earlier run did not finish recording: it stopped the
					// source and restored the snapshot, so it counts as that rollback and not as an untouched source.
					released, restored, restoreFault = true, true, errors.Join(ErrSourceRestored, releaseErr)
				} else {
					released, pinned = releaseErr == nil, errors.Join(pinned, releaseErr)
				}
			}
			if !released {
				state.Action = reconcileAction(string(state.Operation.Target.Identity.ManifestSHA256))
				return saveFailure("blocked", "operation_requires_reconciliation", pinned)
			}
			if restored {
				state.noteUndone(restoreFault)
				rolledBack = rolledBack || errors.Is(restoreFault, ErrRolledBack)
			} else {
				// Nothing was stopped, so this is a wait and not an outage: backoff still paces the retry.
				state.noteFailure(state.Operation.Target.Identity, state.LastAttempt, false, errors.New("the replacement never took effect and the source is serving untouched"))
			}
			state.Operation = nil
			if restored {
				// The observation above describes the target that has just been replaced.
				if observed, err = e.Driver.Observe(ctx); err != nil {
					e.recordObservation(&state, catalog, nil)
					return saveFailure("blocked", "installation_unavailable", err)
				}
				e.recordObservation(&state, catalog, &observed)
				if rolledBack {
					// The source is serving again after the target had written. This run ends here, with no
					// selection and no new apply: the next timer run continues under the backoff just recorded.
					state.Action = ""
					return saveFailure("blocked", "operation_rolled_back", ErrRolledBack)
				}
			}
		}
		state.Action = ""
		if !observed.Healthy() {
			return saveFailure("blocked", "installation_not_healthy", nil)
		}
		decision, err := selectForDelivery(catalog, observed.Identity, observed.Instance, e.Platform, e.Policy, state.LastAttempt, e.ImageDelivery)
		if err != nil {
			return saveFailure("blocked", "selection_failed", err)
		}
		state.Selected = decision.Target
		if len(decision.Applicable) == 0 {
			state.Phase, state.Reason = "current", ""
			return e.Store.Save(state)
		}
		if decision.Target == nil {
			return saveFailure("blocked", decision.Blocked, nil)
		}
		target := *decision.Target
		if state.RetryTarget != nil && *state.RetryTarget != target.Identity {
			state.clearRetry()
		}
		if state.retriesExhausted() {
			// No window, urgency or backoff reopens this: the same target has already
			// interrupted the service repeatedly, so only a person or a new target may.
			state.Action = reconcileAction(string(target.Identity.ManifestSHA256))
			detail := fmt.Sprintf("automatic attempts at %s stopped after %d failures that reached the running service; last failure: %s", target.Identity.Version, state.RetryDisruptive, state.RetryCause)
			state.Detail = strings.ToValidUTF8(detail[:min(len(detail), maxDetailBytes)], "")
			return saveFailure("blocked", "retry_exhausted", nil)
		}
		state.Phase, state.Reason = "applicable", ""
		if err := e.Store.Save(state); err != nil || !apply {
			return err
		}
		if state.RetryTarget != nil && state.LastAttempt.Before(state.RetryAfter) {
			return saveFailure("blocked", "retry_backoff", nil)
		}
		prepared, err := e.Driver.Prepare(ctx, target)
		if err != nil {
			state.noteFailure(target.Identity, state.LastAttempt, false, err)
			return saveFailure("blocked", "preparation_failed", err)
		}
		if prepared.Target != target.Identity || prepared.Source.Identity != observed.Identity || !sameInstallation(prepared.Source, observed) ||
			prepared.Source.Generation != observed.Generation || prepared.Image != target.Image || prepared.RouteSHA256.Validate() != nil {
			return saveFailure("blocked", "preparation_mismatch", errors.New("staged release, source or route differs from selected operation"))
		}
		measured := target
		measured.Impact.Schema = measured.Impact.Schema || prepared.Impact.Schema
		measured.Impact.Maintenance = measured.Impact.Maintenance || prepared.Impact.Maintenance
		if reason := e.Policy.permits(observed.Identity, measured, e.now(), decision.Urgent); reason != "" {
			return saveFailure("blocked", reason, nil)
		}
		state.Phase = "staged"
		if err := e.Store.Save(state); err != nil {
			return err
		}
		// Recheck authorization after a potentially long download. A withdrawal
		// or expiry must take effect before the first stop/deployment mutation.
		fresh, err := e.Feed.Refresh(ctx)
		if err != nil {
			return saveFailure("stale", "preapply_feed_unavailable", err)
		}
		current, err := DecodeCatalog(fresh.Data, e.now())
		if err != nil {
			return saveFailure("stale", "preapply_catalog_invalid", err)
		}
		freshDigest := releaseidentity.Hash(fresh.Data)
		if current.Sequence < state.CatalogSequence || current.Sequence == state.CatalogSequence && freshDigest != state.CatalogSHA256 {
			return saveFailure("stale", "catalog_rollback_or_equivocation", errors.New("preapply catalog rollback or equivocation refused"))
		}
		state.CatalogSequence, state.CatalogSHA256, state.LastVerified = current.Sequence, freshDigest, e.now()
		state.KnownUntil = current.ExpiresAt
		if !fresh.Expires.IsZero() && fresh.Expires.Before(state.KnownUntil) {
			state.KnownUntil = fresh.Expires
		}
		e.recordObservation(&state, current, &observed)
		newDecision, err := selectForDelivery(current, observed.Identity, observed.Instance, e.Platform, e.Policy, e.now(), e.ImageDelivery)
		if err != nil {
			return saveFailure("stale", "preapply_catalog_invalid", err)
		}
		state.Applicable = slices.Clone(newDecision.Applicable)
		if !state.KnownUntil.After(e.now()) {
			return saveFailure("stale", "metadata_expired", errors.New("preapply metadata has expired"))
		}
		// Reselection covers withdrawal, a changed record and a newer better target;
		// the staged artifact is applied only if it is still the one to install.
		if newDecision.Target == nil || newDecision.Target.Identity != target.Identity || newDecision.Target.Image != target.Image || newDecision.Target.RuntimeImageSHA256 != target.RuntimeImageSHA256 {
			state.Selected = nil
			// A window closing or a rollout slot is a wait, not a withdrawal.
			if newDecision.Target == nil && slices.Contains([]string{"maintenance_window", "staged_rollout"}, newDecision.Blocked) {
				return saveFailure("blocked", newDecision.Blocked, nil)
			}
			return saveFailure("blocked", "target_withdrawn_or_changed", nil)
		}
		target = *newDecision.Target
		target.Impact.Schema = target.Impact.Schema || prepared.Impact.Schema
		target.Impact.Maintenance = target.Impact.Maintenance || prepared.Impact.Maintenance
		state.Selected = &target
		before, err := e.Driver.Observe(ctx)
		if err != nil || !before.Healthy() || !sameInstallation(before, observed) || before.Identity != observed.Identity || before.Generation != observed.Generation {
			state.Selected = nil
			if err == nil {
				e.recordObservation(&state, current, &before)
			} else {
				e.recordObservation(&state, current, nil)
			}
			return saveFailure("blocked", "source_changed_during_staging", errors.Join(err, errors.New("source changed before security application")))
		}
		if reason := e.Policy.permits(observed.Identity, target, e.now(), newDecision.Urgent); reason != "" {
			return saveFailure("blocked", reason, nil)
		}
		target.EligibilityUntil = state.KnownUntil
		if end := e.Policy.windowEnd(e.now()); !newDecision.Urgent && !end.IsZero() && end.Before(target.EligibilityUntil) {
			target.EligibilityUntil = end
		}
		state.Operation = &Operation{Target: target, Source: observed, RouteSHA256: prepared.RouteSHA256, StartedAt: e.now(), RequireRuntimeDigest: e.RequireRuntimeDigest}
		state.Phase, state.Reason = "applying", ""
		// Source evidence remains in the operation, but it cannot describe
		// currently running code once workload mutation may have begun.
		e.recordObservation(&state, current, nil)
		if err := e.Store.Save(state); err != nil {
			return err
		}
		applyErr := e.Driver.Apply(ctx, target)
		running, observeErr := e.Driver.Observe(ctx)
		if observeErr == nil {
			e.recordObservation(&state, current, &running)
		} else {
			e.recordObservation(&state, current, nil)
		}
		if applyErr != nil {
			if errors.Is(applyErr, ErrNotStarted) {
				state.Operation = nil
				state.noteFailure(target.Identity, state.LastAttempt, errors.Is(applyErr, ErrSourceRestored), applyErr)
				reason := "preapply_refused"
				if errors.Is(applyErr, ErrRolledBack) {
					reason = "operation_rolled_back"
				}
				return saveFailure("blocked", reason, errors.Join(applyErr, observeErr))
			}
			// The target may have written and its rollback may have failed: the operation stays pinned, so name the exact command now.
			state.Action = reconcileAction(string(target.Identity.ManifestSHA256))
			return saveFailure("failed", "apply_failed_recovery_required", errors.Join(applyErr, observeErr))
		}
		if observeErr != nil || state.Running == nil || !completed(*state.Operation, running) {
			// The operation stays pinned exactly as after a failed apply.
			state.Action = reconcileAction(string(target.Identity.ManifestSHA256))
			return saveFailure("failed", "running_target_unproven", errors.Join(observeErr, errors.New("workload replacement did not prove the exact healthy running target")))
		}
		state.Operation = nil
		state.clearRetry()
		state.Phase, state.Reason = "healthy", ""
		return e.Store.Save(state)
	})
	return state, err
}

// recordObservation updates the current artifact and all of its catalog claims
// together. Nil, invalid or incompatible evidence clears current claims while
// the operation retains its immutable source and recovery context separately.
func (e *Engine) recordObservation(state *Status, catalog Catalog, observed *Observation) {
	state.Running, state.RunningWithdrawn = nil, false
	state.Applicable = []string{}
	state.CoverageThrough, state.Coverage, state.CoveredComponents = "", []string{}, []string{}
	if observed == nil || observed.Validate() != nil || observed.Platform != "" && observed.Platform != e.Platform {
		return
	}
	actual := *observed
	state.Running = &actual
	state.RunningWithdrawn = slices.ContainsFunc(catalog.Targets, func(t Target) bool { return t.Withdrawn && t.Identity == actual.Identity })
	state.Applicable = applicableAdvisories(catalog, actual.Identity, e.Platform, e.Policy.MinimumSeverity, e.ImageDelivery)
	state.CoverageThrough, state.Coverage, state.CoveredComponents = runningCoverage(catalog, actual, e.Platform)
}

func completed(operation Operation, observed Observation) bool {
	return observed.Healthy() && sameInstallation(operation.Source, observed) && observed.Identity == operation.Target.Identity &&
		observed.Generation > operation.Source.Generation && (operation.Target.Image == "" || observed.Image == operation.Target.Image) &&
		(!operation.RequireRuntimeDigest || observed.RuntimeImageSHA256.Validate() == nil && (operation.Target.RuntimeImageSHA256 == "" ||
			observed.RuntimeImageSHA256 == operation.Target.RuntimeImageSHA256 ||
			// A runtime can report the signed index digest instead of the platform child.
			operation.Target.Image != "" && strings.HasSuffix(operation.Target.Image, "@sha256:"+string(observed.RuntimeImageSHA256))))
}

// restoreAbortedSource acts on the target's own report that it stopped before the
// database was written, and only when that report describes the pinned source.
func (e *Engine) restoreAbortedSource(ctx context.Context, operation Operation, observed Observation) (bool, error) {
	restorer, ok := e.Driver.(AbortRestorer)
	if !ok || !observed.AbortedBeforeMutation || !untouchedByAbort(operation, observed) {
		return false, nil
	}
	err := restorer.RestoreAborted(ctx, observed, operation)
	return errors.Is(err, ErrNotStarted), err
}

// rolledBackRun is the error of a run that put the source back after the target had written,
// whatever else stopped it: it reads as its cause and is an ErrRolledBack.
type rolledBackRun struct{ cause error }

func (r rolledBackRun) Error() string {
	if r.cause == nil {
		return ReplacementUndone
	}
	return r.cause.Error()
}

func (r rolledBackRun) Unwrap() []error {
	if r.cause == nil {
		return []error{ErrRolledBack}
	}
	return []error{ErrRolledBack, r.cause}
}

// ReplacementUndone is the status text after the source was put back, whether the
// target reported an abort or ran out of time.
const ReplacementUndone = "the replacement did not complete; the earlier release is serving again and the security fix is not applied"

// restoreOrRollBack answers an unfinished operation: the target's own abort report
// first, then the deadline. observed is nil when the installation cannot be observed.
func (e *Engine) restoreOrRollBack(ctx context.Context, operation Operation, observed *Observation) (bool, error) {
	if observed != nil {
		if restored, err := e.restoreAbortedSource(ctx, operation, *observed); restored || err != nil {
			return restored, err
		}
	}
	restorer, ok := e.Driver.(AbortRestorer)
	if !ok || e.OperationTimeout <= 0 || e.now().Sub(operation.StartedAt) < e.OperationTimeout {
		return false, nil
	}
	var seen Observation
	if observed != nil {
		// A target proven healthy completes, however late. Another installation is not ours to touch.
		if completed(operation, *observed) || !sameInstallation(operation.Source, *observed) {
			return false, nil
		}
		// A source that still serves untouched is not stopped; reconciliation releases it.
		if untouched(operation, *observed) {
			return false, nil
		}
		// Healthy at another release, ready or not: the target may have served, so nothing is stopped.
		if observed.Validate() == nil && observed.Phase == "healthy" && !observed.Maintenance && observed.Identity != operation.Source.Identity {
			return false, ErrPastFinalGate
		}
		seen = *observed
	} else {
		prover, ok := e.Driver.(GateProver)
		if !ok {
			return false, errors.Join(ErrPastFinalGate, errors.New("the target cannot be read and this driver cannot prove it did not pass the final gate"))
		}
		// shortcut: a target can cross the gate between this read and the stop, and rule 2 then refuses the restore with it stopped, upgrade if a lock that excludes the gate becomes available.
		proven, err := prover.NoFinalGate(ctx, operation)
		if err != nil || !proven {
			return false, errors.Join(ErrPastFinalGate, err, errors.New("the target cannot be read and nothing proves it did not pass the final gate"))
		}
	}
	err := restorer.RestoreAborted(ctx, seen, operation)
	return errors.Is(err, ErrNotStarted), err
}

func untouchedByAbort(operation Operation, observed Observation) bool {
	return sameInstallation(operation.Source, observed) && observed.Identity == operation.Source.Identity && observed.Generation == operation.Source.Generation
}

func untouched(operation Operation, observed Observation) bool {
	return observed.Healthy() && sameInstallation(operation.Source, observed) && observed.Identity == operation.Source.Identity &&
		observed.Generation == operation.Source.Generation && observed.Image == operation.Source.Image
}

func runningCoverage(c Catalog, observed Observation, platform string) (string, []string, []string) {
	if !observed.Healthy() {
		return "", []string{}, []string{}
	}
	for _, target := range c.Targets {
		if target.Identity == observed.Identity && target.Image == observed.Image && !target.Withdrawn && slices.Contains(target.Platforms, platform) {
			covered, components := coverageDetails(c, target.Fixes)
			return target.CoverageThrough, covered, components
		}
	}
	return "", []string{}, []string{}
}

func coverageDetails(c Catalog, ids []string) ([]string, []string) {
	covered := slices.Clone(ids)
	slices.Sort(covered)
	components := []string{}
	for _, advisory := range c.Advisories {
		if slices.Contains(ids, advisory.ID) {
			for _, component := range advisory.Components {
				if !slices.Contains(components, component) {
					components = append(components, component)
				}
			}
		}
	}
	slices.Sort(components)
	return covered, components
}
