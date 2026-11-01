package securitymaintenance

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securityfeed"
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
// before any workload mutation. Other execution failures retain recovery pins.
var ErrNotStarted = errors.New("security application refused before workload mutation")

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
	Policy            *Policy                `json:"policy,omitempty"`
	Format            string                 `json:"format"`
	Enrollment        *Enrollment            `json:"enrollment,omitempty"`
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
	Selected          *Target                `json:"selected,omitempty"`
	Running           *Observation           `json:"running,omitempty"`
	Operation         *Operation             `json:"operation,omitempty"`
}

const StatusFormat = "hikyo.security-maintenance-status/v1"

type Engine struct {
	RequireRuntimeDigest bool
	Store                *Store
	Feed                 Feed
	Driver               Driver
	Platform             string
	ImageDelivery        bool
	Policy               Policy
	Now                  func() time.Time
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
		observed, err := e.Driver.Observe(ctx)
		if err != nil {
			return err
		}
		if !observed.Healthy() {
			return errors.New("enrollment requires an admitted healthy exact installation")
		}
		if state.Enrollment != nil {
			if state.Enrollment.Instance != observed.Instance || state.Enrollment.Incarnation != observed.Incarnation || state.Enrollment.RestoreEpoch != observed.RestoreEpoch {
				return errors.New("existing enrollment belongs to another recovery incarnation; explicit recovery is required")
			}
			result = state
			return nil
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
		policy := e.Policy
		state.Policy = &policy
		saveFailure := func(phase, reason string, cause error) error {
			state.Phase, state.Reason = phase, reason
			return errors.Join(cause, e.Store.Save(state))
		}
		if e.Policy.Mode == Manual && apply {
			return saveFailure("blocked", "manual_mode", nil)
		}
		document, err := e.Feed.Refresh(ctx)
		if err != nil {
			return saveFailure("stale", "feed_unavailable", err)
		}
		catalog, err := DecodeCatalog(document.Data, state.LastAttempt)
		if err != nil {
			return saveFailure("stale", "catalog_invalid", err)
		}
		digest := releaseidentity.Hash(document.Data)
		if catalog.Sequence < state.CatalogSequence || catalog.Sequence == state.CatalogSequence && state.CatalogSHA256 != "" && state.CatalogSHA256 != digest {
			return saveFailure("stale", "catalog_rollback_or_equivocation", errors.New("security catalog rollback or equivocation refused"))
		}
		state.CatalogSequence, state.CatalogSHA256 = catalog.Sequence, digest
		state.LastVerified, state.KnownUntil = state.LastAttempt, catalog.ExpiresAt
		if !document.Expires.IsZero() && document.Expires.Before(state.KnownUntil) {
			state.KnownUntil = document.Expires
		}
		if !state.KnownUntil.After(state.LastAttempt) {
			return saveFailure("stale", "metadata_expired", errors.New("security metadata has expired"))
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
			return saveFailure("blocked", "installation_unavailable", err)
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
			return saveFailure("blocked", "recovery_incarnation_changed", errors.New("maintenance enrollment invalidated by restore or installation change"))
		}
		if state.Operation != nil {
			if completed(*state.Operation, observed) {
				state.Selected = &state.Operation.Target
				state.Operation, state.Phase, state.Reason = nil, "healthy", ""
				return e.Store.Save(state)
			}
			// The exact target remains pinned after uncertain writes or process
			// death. Only the deployment/upgrade owner can reconcile its journal.
			return saveFailure("blocked", "operation_requires_reconciliation", errors.New("unfinished security operation requires exact-target operator reconciliation"))
		}
		if !observed.Healthy() {
			return saveFailure("blocked", "installation_not_healthy", nil)
		}
		decision, err := selectForDelivery(catalog, observed.Identity, e.Platform, e.Policy, state.LastAttempt, e.ImageDelivery)
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
		state.Phase, state.Reason = "applicable", ""
		if err := e.Store.Save(state); err != nil || !apply {
			return err
		}
		target := *decision.Target
		prepared, err := e.Driver.Prepare(ctx, target)
		if err != nil {
			return saveFailure("blocked", "preparation_failed", err)
		}
		if prepared.Target != target.Identity || prepared.Source.Identity != observed.Identity || !sameInstallation(prepared.Source, observed) ||
			prepared.Source.Generation != observed.Generation || prepared.Image != target.Image || prepared.RouteSHA256.Validate() != nil {
			return saveFailure("blocked", "preparation_mismatch", errors.New("staged release, source or route differs from selected operation"))
		}
		measured := target
		measured.Impact.Schema = measured.Impact.Schema || prepared.Impact.Schema
		measured.Impact.Maintenance = measured.Impact.Maintenance || prepared.Impact.Maintenance
		if reason := e.Policy.permits(observed.Identity, measured, e.now()); reason != "" {
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
		newDecision, err := selectForDelivery(current, observed.Identity, e.Platform, e.Policy, e.now(), e.ImageDelivery)
		if err != nil {
			return saveFailure("stale", "preapply_catalog_invalid", err)
		}
		state.Applicable = slices.Clone(newDecision.Applicable)
		if !state.KnownUntil.After(e.now()) {
			return saveFailure("stale", "metadata_expired", errors.New("preapply metadata has expired"))
		}
		stillAuthorized := false
		for _, candidate := range current.Targets {
			if candidate.Identity == target.Identity && !candidate.Withdrawn && candidate.Image == target.Image &&
				candidate.RuntimeImageSHA256 == target.RuntimeImageSHA256 && slices.Contains(candidate.Platforms, e.Platform) {
				if coversAll(candidate.Fixes, newDecision.Applicable) && len(newDecision.Applicable) > 0 && e.Policy.permits(observed.Identity, candidate, e.now()) == "" {
					stillAuthorized = true
					target = candidate
				}
			}
		}
		if !stillAuthorized {
			state.Selected = nil
			return saveFailure("blocked", "target_withdrawn_or_changed", nil)
		}
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
		if reason := e.Policy.permits(observed.Identity, target, e.now()); reason != "" {
			return saveFailure("blocked", reason, nil)
		}
		target.EligibilityUntil = state.KnownUntil
		if end := e.Policy.windowEnd(e.now()); !end.IsZero() && end.Before(target.EligibilityUntil) {
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
				return saveFailure("blocked", "preapply_refused", errors.Join(applyErr, observeErr))
			}
			return saveFailure("failed", "apply_failed_recovery_required", errors.Join(applyErr, observeErr))
		}
		if observeErr != nil || state.Running == nil || !completed(*state.Operation, running) {
			return saveFailure("failed", "running_target_unproven", errors.Join(observeErr, errors.New("workload replacement did not prove the exact healthy running target")))
		}
		state.Operation = nil
		state.Phase, state.Reason = "healthy", ""
		return e.Store.Save(state)
	})
	return state, err
}

// recordObservation updates the current artifact and all of its catalog claims
// together. Nil, invalid or incompatible evidence clears current claims while
// the operation retains its immutable source and recovery context separately.
func (e *Engine) recordObservation(state *Status, catalog Catalog, observed *Observation) {
	state.Running = nil
	state.Applicable = []string{}
	state.CoverageThrough, state.Coverage, state.CoveredComponents = "", []string{}, []string{}
	if observed == nil || observed.Validate() != nil || observed.Platform != "" && observed.Platform != e.Platform {
		return
	}
	actual := *observed
	state.Running = &actual
	state.Applicable = applicableAdvisories(catalog, actual.Identity, e.Platform, e.Policy.MinimumSeverity, e.ImageDelivery)
	state.CoverageThrough, state.Coverage, state.CoveredComponents = runningCoverage(catalog, actual, e.Platform)
}

func coversAll(fixes, needed []string) bool {
	for _, id := range needed {
		if !slices.Contains(fixes, id) {
			return false
		}
	}
	return true
}

func completed(operation Operation, observed Observation) bool {
	return observed.Healthy() && sameInstallation(operation.Source, observed) && observed.Identity == operation.Target.Identity &&
		observed.Generation > operation.Source.Generation && (operation.Target.Image == "" || observed.Image == operation.Target.Image) &&
		(!operation.RequireRuntimeDigest || operation.Target.RuntimeImageSHA256 == "" || observed.RuntimeImageSHA256 == operation.Target.RuntimeImageSHA256)
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
