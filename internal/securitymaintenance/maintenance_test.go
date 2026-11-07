package securitymaintenance

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securityfeed"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

var fixtureTime = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

func fixtureIdentity(version string, sequence uint64) releaseidentity.Identity {
	return releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: version, Sequence: sequence, Commit: strings.Repeat("a", 40),
		CompatibilitySHA256: releaseidentity.Hash([]byte("compatibility-" + version)), ManifestSHA256: releaseidentity.Hash([]byte("manifest-" + version))}
}

func fixtureCatalog() Catalog {
	return Catalog{Format: CatalogFormat, Sequence: 1, IssuedAt: fixtureTime.Add(-time.Hour), ExpiresAt: fixtureTime.Add(time.Hour),
		Advisories: []Advisory{{ID: "HK-SA-EXAMPLE", Summary: "Test-only security remediation", Severity: High, Components: []string{"hikyo"},
			Affected: []Affected{{Profile: releaseidentity.StableV1, Introduced: "1.0.0", Fixed: "1.3.1", Platforms: []string{"linux/amd64"}}}}},
		Targets: []Target{{Identity: fixtureIdentity("1.3.1", 8), Platforms: []string{"linux/amd64"}, Fixes: []string{"HK-SA-EXAMPLE"}, CoverageThrough: "2026-11-01",
			Impact: Impact{Restart: true, Features: true, Schema: true, Maintenance: true}}}}
}

func fixtureObservation() Observation {
	return Observation{Engine: releaseidentity.SQLite, Identity: fixtureIdentity("1.2.0", 7), Instance: "installation-example", Generation: 4, RestoreEpoch: 0,
		Incarnation: strings.Repeat("b", 64), Phase: "healthy", Ready: true}
}

func TestSecurityPolicySeparatesCoverageFromFeatureVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		edit    func(*Policy)
		blocked string
	}{
		{"compatible route", func(*Policy) {}, ""},
		{"patch only", func(p *Policy) { p.UpgradeClass = "patch" }, "feature_upgrade_required"},
		{"pin", func(p *Policy) { p.Pin = "1.2.0" }, "application_pin"},
		{"manual", func(p *Policy) { p.Mode = Manual }, "manual_mode"},
		{"maintenance", func(p *Policy) { p.AllowMaintenance = false }, "maintenance_required"},
		{"window", func(p *Policy) { p.WindowStartMinute = 60; p.WindowDurationMinutes = 30 }, "maintenance_window"},
		{"channel", func(p *Policy) { p.Channel = updatecheck.ChannelNightly }, "channel_change"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := DefaultPolicy()
			tc.edit(&p)
			decision, err := Select(fixtureCatalog(), fixtureObservation().Identity, "linux/amd64", p, fixtureTime)
			if err != nil || decision.Blocked != tc.blocked || len(decision.Applicable) != 1 {
				t.Fatalf("decision=%+v err=%v", decision, err)
			}
			if (decision.Target != nil) != (tc.blocked == "") {
				t.Fatalf("unexpected target: %+v", decision)
			}
		})
	}
}

func TestSecurityPolicyWindowWrapAndCompleteCoverage(t *testing.T) {
	c := fixtureCatalog()
	c.IssuedAt = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	c.ExpiresAt = c.IssuedAt.Add(24 * time.Hour)
	p := DefaultPolicy()
	p.WindowStartMinute = 23 * 60
	p.WindowDurationMinutes = 120
	if d, err := Select(c, fixtureObservation().Identity, "linux/amd64", p, time.Date(2026, 11, 1, 0, 30, 0, 0, time.UTC)); err != nil || d.Target == nil {
		t.Fatalf("midnight window: %+v %v", d, err)
	}
	a := c.Advisories[0]
	a.ID = "HK-SA-SECOND"
	c.Advisories = append(c.Advisories, a)
	p = DefaultPolicy()
	// The only target fixes one of two advisories. Installing it still reduces
	// exposure; the other advisory stays applicable and visible afterwards.
	if d, err := Select(c, fixtureObservation().Identity, "linux/amd64", p, fixtureTime); err != nil || d.Target == nil || len(d.Applicable) != 2 {
		t.Fatalf("partial remediation refused: %+v %v", d, err)
	}
	c.Targets[0].Fixes = []string{"HK-SA-SECOND"}
	c.Advisories[0].Severity, c.Advisories[1].Severity = Low, Low
	newer := c.Targets[0]
	newer.Identity, newer.Fixes = fixtureIdentity("1.3.2", 9), []string{"HK-SA-EXAMPLE", "HK-SA-SECOND"}
	c.Targets = append(c.Targets, newer)
	if d, err := Select(c, fixtureObservation().Identity, "linux/amd64", p, fixtureTime); err != nil || d.Target == nil || d.Target.Identity.Version != "1.3.2" {
		t.Fatalf("complete target not preferred: %+v %v", d, err)
	}
	none := fixtureCatalog()
	none.Targets[0].Fixes = []string{"HK-SA-UNRELATED"}
	none.Advisories = append(none.Advisories, Advisory{ID: "HK-SA-UNRELATED", Summary: "Unrelated", Severity: Low, Components: []string{"hikyo"},
		Affected: []Affected{{Profile: releaseidentity.StableV1, Introduced: "9.0.0", Fixed: "9.0.1", Platforms: []string{"linux/amd64"}}}})
	if d, err := Select(none, fixtureObservation().Identity, "linux/amd64", p, fixtureTime); err != nil || d.Target != nil || d.Blocked != "no_complete_security_target" {
		t.Fatalf("target fixing nothing applicable selected: %+v %v", d, err)
	}
}

func TestSecurityPolicyRefusesMovingOntoAnotherAdvisory(t *testing.T) {
	c := fixtureCatalog()
	// 1.3.1 fixes the applicable advisory but introduced a separate flaw.
	c.Advisories = append(c.Advisories, Advisory{ID: "HK-SA-REGRESSION", Summary: "Introduced later", Severity: Critical, Components: []string{"hikyo"},
		Affected: []Affected{{Profile: releaseidentity.StableV1, Introduced: "1.3.0", Fixed: "1.3.2", Platforms: []string{"linux/amd64"}}}})
	d, err := Select(c, fixtureObservation().Identity, "linux/amd64", DefaultPolicy(), fixtureTime)
	if err != nil || d.Target != nil || d.Blocked != "no_complete_security_target" {
		t.Fatalf("selected a target carrying a new advisory: %+v %v", d, err)
	}
}

func TestSecurityPolicyUrgentFixSkipsOnlyTheWindow(t *testing.T) {
	c := fixtureCatalog()
	c.Advisories[0].Severity = Critical
	closed := DefaultPolicy()
	closed.WindowStartMinute, closed.WindowDurationMinutes = 60, 30
	d, err := Select(c, fixtureObservation().Identity, "linux/amd64", closed, fixtureTime)
	if err != nil || d.Target == nil || !d.Urgent {
		t.Fatalf("critical fix waited for the window: %+v %v", d, err)
	}
	optOut := closed
	optOut.UrgentSeverity = ""
	if d, err := Select(c, fixtureObservation().Identity, "linux/amd64", optOut, fixtureTime); err != nil || d.Blocked != "maintenance_window" {
		t.Fatalf("empty urgent severity did not keep the window: %+v %v", d, err)
	}
	c.Advisories[0].Severity = High
	if d, err := Select(c, fixtureObservation().Identity, "linux/amd64", closed, fixtureTime); err != nil || d.Blocked != "maintenance_window" {
		t.Fatalf("high fix skipped the window: %+v %v", d, err)
	}
	c.Advisories[0].Severity = Critical
	for name, edit := range map[string]func(*Policy){
		"pin":         func(p *Policy) { p.Pin = "1.2.0" },
		"class":       func(p *Policy) { p.UpgradeClass = "patch" },
		"maintenance": func(p *Policy) { p.AllowMaintenance = false },
		"manual":      func(p *Policy) { p.Mode = Manual },
	} {
		p := closed
		edit(&p)
		if d, err := Select(c, fixtureObservation().Identity, "linux/amd64", p, fixtureTime); err != nil || d.Target != nil {
			t.Fatalf("%s: urgency overrode local policy: %+v %v", name, d, err)
		}
	}
	p := DefaultPolicy()
	p.UrgentSeverity = "severe"
	if p.Validate() == nil {
		t.Fatal("unknown urgent severity accepted")
	}
}

func TestSecurityEngineAppliesUrgentFixInsideClosedWindow(t *testing.T) {
	e, f, d := fixtureEngine(t)
	f.catalog.Advisories[0].Severity = Critical
	e.Policy.WindowStartMinute, e.Policy.WindowDurationMinutes = 60, 30
	enrollFixture(t, e)
	var until time.Time
	d.beforeApply = func(target Target) error { until = target.EligibilityUntil; return nil }
	state, err := e.RunOnce(t.Context(), true)
	if err != nil || state.Phase != "healthy" || d.applies() != 1 {
		t.Fatalf("urgent fix not applied: %+v %v", state, err)
	}
	if !until.Equal(f.catalog.ExpiresAt) {
		t.Fatalf("a closed window bounded an urgent application: %s", until)
	}
}

func TestSecurityStagedRolloutIsStableAndBounded(t *testing.T) {
	c := fixtureCatalog()
	c.Targets[0].AvailableAt, c.Targets[0].RolloutMinutes = fixtureTime, 60
	early, late := 0, 0
	for i := range 200 {
		instance := "installation-" + strings.Repeat("x", i)
		start := c.Targets[0].rolloutStart(instance)
		if start.Before(fixtureTime) || !start.Before(fixtureTime.Add(time.Hour)) {
			t.Fatalf("offset outside the published window: %s", start)
		}
		if !start.Equal(c.Targets[0].rolloutStart(instance)) {
			t.Fatal("one installation's start moved")
		}
		if start.Before(fixtureTime.Add(30 * time.Minute)) {
			early++
		} else {
			late++
		}
	}
	if early < 50 || late < 50 {
		t.Fatalf("rollout not spread: %d early, %d late", early, late)
	}
	future := c.Targets[0].rolloutStart("installation-example")
	d, err := selectForDelivery(c, fixtureObservation().Identity, "installation-example", "linux/amd64", DefaultPolicy(), future.Add(-time.Second), false)
	if err != nil || d.Target != nil || d.Blocked != "staged_rollout" {
		t.Fatalf("installation ahead of its rollout slot: %+v %v", d, err)
	}
	d, err = selectForDelivery(c, fixtureObservation().Identity, "installation-example", "linux/amd64", DefaultPolicy(), future, false)
	if err != nil || d.Target == nil {
		t.Fatalf("installation missed its rollout slot: %+v %v", d, err)
	}
}

// reconcilingDriver records owner journal release, as a deployment executor does.
type reconcilingDriver struct {
	*fixtureDriver
	released int
	refuse   error
}

func (d *reconcilingDriver) Reconcile(context.Context, Observation) error {
	d.released++
	return d.refuse
}

type abortRestoringDriver struct {
	*fixtureDriver
	healthy  Observation
	restored int
	failWith error
}

func (d *abortRestoringDriver) RestoreAborted(_ context.Context, aborted Observation, operation Operation) error {
	source := operation.Source
	d.restored++
	if !aborted.AbortedBeforeMutation || source.Generation != d.healthy.Generation {
		return errors.New("restore called without the target's report or the pinned source")
	}
	if d.failWith != nil {
		return d.failWith
	}
	d.observed = d.healthy
	return errors.Join(ErrNotStarted, ErrSourceRestored)
}

// The process that applied is gone, so only a later run can act on the target's report.
func TestSecurityLaterRunRestoresASourceWhoseTargetReportedAnAbort(t *testing.T) {
	setup := func(t *testing.T) (*Engine, *abortRestoringDriver, *fixtureDriver, *time.Time) {
		e, f, d := fixtureEngine(t)
		f.catalog.ExpiresAt = fixtureTime.Add(1000 * time.Hour)
		r := &abortRestoringDriver{fixtureDriver: d, healthy: d.observed}
		e.Driver = r
		now := fixtureTime
		e.Now = func() time.Time { return now }
		enrollFixture(t, e)
		d.applyFailure = true
		if state, err := e.RunOnce(t.Context(), true); err == nil || state.Operation == nil {
			t.Fatalf("no pin: %+v %v", state, err)
		}
		d.applyFailure = false
		d.observed.Ready, d.observed.Phase, d.observed.Maintenance, d.observed.AbortedBeforeMutation = false, "backup-preparing", true, true
		return e, r, d, &now
	}
	t.Run("transient failure retries and the next run restores", func(t *testing.T) {
		e, r, d, now := setup(t)
		r.failWith = errors.New("API server unavailable")
		state, err := e.RunOnce(t.Context(), true)
		if err == nil || state.Operation == nil || state.Reason != "operation_requires_reconciliation" || !strings.Contains(state.Detail, "API server unavailable") || r.restored != 1 {
			t.Fatalf("transient failure dropped the pin: %+v %v", state, err)
		}
		r.failWith = nil
		*now = now.Add(time.Minute)
		state, err = e.RunOnce(t.Context(), true)
		if err != nil || state.Operation != nil || state.Reason != "retry_backoff" || state.Running == nil || !state.Running.Ready || state.RetryDisruptive != 1 || r.restored != 2 || d.applies() != 1 {
			t.Fatalf("later run did not restore: %+v %v", state, err)
		}
	})
	t.Run("a report that does not describe the pinned source is ignored", func(t *testing.T) {
		for name, change := range map[string]func(*Observation){
			"generation moved": func(o *Observation) { o.Generation++ },
			"other instance":   func(o *Observation) { o.Instance = "another" },
			"other release":    func(o *Observation) { o.Identity = fixtureIdentity("1.9.0", 20) },
			"no report":        func(o *Observation) { o.AbortedBeforeMutation = false },
		} {
			e, r, d, _ := setup(t)
			change(&d.observed)
			state, err := e.RunOnce(t.Context(), true)
			if err == nil || state.Operation == nil || r.restored != 0 {
				t.Fatalf("%s: restored or released: %+v %v", name, state, err)
			}
		}
	})
	t.Run("a feed that cannot be used does not keep the stopped service down", func(t *testing.T) {
		for name, break_ := range map[string]func(*fixtureFeed){
			"unreachable": func(f *fixtureFeed) { f.failAt = f.calls + 1 },
			"expired":     func(f *fixtureFeed) { f.catalog.ExpiresAt = fixtureTime.Add(-time.Hour) },
		} {
			e, r, d, _ := setup(t)
			break_(e.Feed.(*fixtureFeed))
			state, err := e.RunOnce(t.Context(), false)
			if err == nil || state.Operation == nil || r.restored != 0 {
				t.Fatalf("%s: a check restored the source: %+v %v", name, state, err)
			}
			break_(e.Feed.(*fixtureFeed))
			state, err = e.RunOnce(t.Context(), true)
			if err == nil || state.Phase != "stale" || state.Operation != nil || state.Action != "" || state.RetryDisruptive != 1 || state.Running != nil || r.restored != 1 || !d.observed.Healthy() {
				t.Fatalf("%s: the source stayed stopped: %+v %v", name, state, err)
			}
		}
	})
	t.Run("a manual policy does not keep the stopped service down", func(t *testing.T) {
		e, r, d, _ := setup(t)
		e.Policy.Mode = Manual
		state, err := e.RunOnce(t.Context(), true)
		if err != nil || state.Reason != "manual_mode" || state.Operation != nil || state.Running != nil || !strings.Contains(state.Detail, "run check") || r.restored != 1 || !d.observed.Healthy() {
			t.Fatalf("the source stayed stopped: %+v %v", state, err)
		}
	})
	t.Run("a check run never changes the workload", func(t *testing.T) {
		e, r, _, _ := setup(t)
		if state, _ := e.RunOnce(t.Context(), false); state.Operation == nil || r.restored != 0 {
			t.Fatalf("check restored the source: %+v", state)
		}
	})
}

func TestSecurityStatusKeepsActionAndCauseAcrossAFeedOutage(t *testing.T) {
	e, f, d := fixtureEngine(t)
	enrollFixture(t, e)
	d.applyFailure = true
	if _, err := e.RunOnce(t.Context(), true); err == nil {
		t.Fatal("expected a pinned failure")
	}
	state, err := e.RunOnce(t.Context(), true)
	if err == nil || state.Action == "" || state.Detail == "" {
		t.Fatalf("no action or cause: %+v %v", state, err)
	}
	f.failAt = f.calls + 1
	state, err = e.RunOnce(t.Context(), true)
	if err == nil || state.Phase != "stale" || state.Action == "" || state.Operation == nil {
		t.Fatalf("feed outage hid the pinned operation's exit: %+v %v", state, err)
	}
}

func TestSecurityWindowClosingDuringStagingIsAWait(t *testing.T) {
	e, _, d := fixtureEngine(t)
	now := time.Date(2026, 11, 1, 1, 29, 0, 0, time.UTC)
	e.Now = func() time.Time { return now }
	e.Policy.WindowStartMinute, e.Policy.WindowDurationMinutes = 60, 30
	enrollFixture(t, e)
	d.duringPrepare = func() { now = now.Add(5 * time.Minute) }
	f := e.Feed.(*fixtureFeed)
	f.catalog.IssuedAt, f.catalog.ExpiresAt = now.Add(-time.Hour), now.Add(24*time.Hour)
	state, err := e.RunOnce(t.Context(), true)
	if err != nil || state.Reason != "maintenance_window" || state.ProtectionGap() {
		t.Fatalf("window close reported as a withdrawal or gap: %+v %v", state, err)
	}
}

func TestSecurityPinnedOperationReleasesOnlyForUntouchedSourceAfterSettling(t *testing.T) {
	e, f, d := fixtureEngine(t)
	f.catalog.ExpiresAt = fixtureTime.Add(24 * time.Hour)
	r := &reconcilingDriver{fixtureDriver: d}
	e.Driver = r
	enrollFixture(t, e)
	now := fixtureTime
	e.Now = func() time.Time { return now }
	d.applyFailure = true
	if state, err := e.RunOnce(t.Context(), true); err == nil || state.Operation == nil {
		t.Fatalf("apply failure did not pin: %+v %v", state, err)
	}
	// Inside the settle period a slow rollout could still land.
	now = now.Add(operationSettle - time.Minute)
	state, err := e.RunOnce(t.Context(), true)
	if err == nil || state.Operation == nil || state.Reason != "operation_requires_reconciliation" || r.released != 0 || !strings.Contains(state.Action, "reconcile --confirm") {
		t.Fatalf("released too early or without an action: %+v %v", state, err)
	}
	// A moved generation means something was written, so the pin must stay.
	now = now.Add(time.Hour)
	d.observed.Generation++
	state, err = e.RunOnce(t.Context(), true)
	if err == nil || state.Operation == nil || r.released != 0 {
		t.Fatalf("released after possible writes: %+v %v", state, err)
	}
	d.observed.Generation--
	// A refused journal release keeps the pin.
	r.refuse = errors.New("journal refused")
	if state, err = e.RunOnce(t.Context(), true); err == nil || state.Operation == nil {
		t.Fatalf("pin dropped despite journal refusal: %+v %v", state, err)
	}
	r.refuse = nil
	// A check never clears the journal, however long the source has been settled.
	attempts := r.released
	state, err = e.RunOnce(t.Context(), false)
	if err == nil || state.Operation == nil || r.released != attempts {
		t.Fatalf("a check released the pin: %+v %v", state, err)
	}
	// Released, but the failed target backs off instead of stopping writers again.
	state, err = e.RunOnce(t.Context(), true)
	if err != nil || state.Operation != nil || state.Reason != "retry_backoff" || d.applies() != 1 || state.RetryAttempts != 1 || state.RetryDisruptive != 0 {
		t.Fatalf("release did not back off: %+v %v", state, err)
	}
	// After the backoff the same target is retried and can succeed.
	now = now.Add(retryBase + time.Minute)
	d.applyFailure = false
	state, err = e.RunOnce(t.Context(), true)
	if err != nil || state.Phase != "healthy" || d.applies() != 2 || state.RetryTarget != nil {
		t.Fatalf("retry after backoff did not complete: %+v %v", state, err)
	}
}

func TestSecurityRetryBackoffGrowsAndNewTargetResets(t *testing.T) {
	var s Status
	target := fixtureIdentity("1.3.1", 8)
	previous := time.Duration(0)
	for range 12 {
		s.noteFailure(target, fixtureTime, false, nil)
		delay := s.RetryAfter.Sub(fixtureTime)
		if delay < previous || delay > retryCap {
			t.Fatalf("backoff %s after %s", delay, previous)
		}
		previous = delay
	}
	if previous != retryCap {
		t.Fatalf("backoff never reached the cap: %s", previous)
	}
	s.noteFailure(fixtureIdentity("1.3.2", 9), fixtureTime, true, errors.New("x"))
	if s.RetryAttempts != 1 || s.RetryAfter.Sub(fixtureTime) != retryBase {
		t.Fatalf("new target kept old backoff: %+v", s)
	}
}

func TestSecurityEngineNewerTargetSkipsFailedTargetBackoff(t *testing.T) {
	e, f, d := fixtureEngine(t)
	enrollFixture(t, e)
	d.beforeApply = func(Target) error { return ErrNotStarted }
	if state, err := e.RunOnce(t.Context(), true); err == nil || state.RetryAttempts != 1 {
		t.Fatalf("refusal did not record a retry: %+v %v", state, err)
	}
	if state, err := e.RunOnce(t.Context(), true); err != nil || state.Reason != "retry_backoff" || d.applies() != 1 {
		t.Fatalf("failed target retried immediately: %+v %v", state, err)
	}
	d.beforeApply = nil
	f.catalog.Sequence++
	newer := f.catalog.Targets[0]
	newer.Identity = fixtureIdentity("1.4.0", 9)
	f.catalog.Targets = append(f.catalog.Targets, newer)
	state, err := e.RunOnce(t.Context(), true)
	if err != nil || state.Phase != "healthy" || state.Running.Identity != newer.Identity || d.applies() != 2 {
		t.Fatalf("newer fix waited on old backoff: %+v %v", state, err)
	}
}

func TestSecurityRepeatedDisruptiveFailureStopsUntilTheTargetChangesOrAnOperatorActs(t *testing.T) {
	e, f, d := fixtureEngine(t)
	now := fixtureTime
	e.Now = func() time.Time { return now }
	f.catalog.ExpiresAt = fixtureTime.Add(1000 * time.Hour)
	// Urgent severity ignores the window; the cap must not.
	e.Policy.WindowStartMinute, e.Policy.WindowDurationMinutes = 0, 1440
	enrollFixture(t, e)
	d.beforeApply = func(Target) error {
		return errors.Join(ErrNotStarted, ErrSourceRestored, errors.New("scratch proof refused"))
	}
	for attempt := 1; attempt <= maxDisruptiveAttempts; attempt++ {
		state, err := e.RunOnce(t.Context(), true)
		if err == nil || state.Reason != "preapply_refused" || state.RetryDisruptive != attempt || d.applies() != attempt {
			t.Fatalf("attempt %d: %+v %v", attempt, state, err)
		}
		now = now.Add(retryCap + time.Minute)
	}
	// A later run, even long after the backoff, does not touch the workload again.
	for range 2 {
		state, err := e.RunOnce(t.Context(), true)
		target := f.catalog.Targets[0].Identity
		if err != nil || d.applies() != maxDisruptiveAttempts || state.Reason != "retry_exhausted" || !state.ProtectionGap() ||
			state.Action != reconcileAction(string(target.ManifestSHA256)) || !strings.Contains(state.Detail, "scratch proof refused") || !strings.Contains(state.Detail, "1.3.1") {
			t.Fatalf("exhausted target retried or hidden: %+v %v", state, err)
		}
		now = now.Add(24 * time.Hour)
	}
	// Reading status never resumes it either.
	if state, err := e.RunOnce(t.Context(), false); err != nil || state.Reason != "retry_exhausted" {
		t.Fatalf("check hid the gap: %+v %v", state, err)
	}
	if _, err := e.Reconcile(t.Context(), "wrong"); err == nil {
		t.Fatal("reconciled the wrong target")
	}
	state, err := e.Reconcile(t.Context(), string(f.catalog.Targets[0].Identity.ManifestSHA256))
	if err != nil || state.RetryTarget != nil || state.Action != "" || state.Detail != "" {
		t.Fatalf("operator action did not clear the cap: %+v %v", state, err)
	}
	d.beforeApply = nil
	if state, err = e.RunOnce(t.Context(), true); err != nil || state.Phase != "healthy" || d.applies() != maxDisruptiveAttempts+1 {
		t.Fatalf("update did not resume after the operator acted: %+v %v", state, err)
	}
}

func TestSecurityRetryCapResetsOnANewTargetButNotOnARepeatedAttempt(t *testing.T) {
	e, f, d := fixtureEngine(t)
	now := fixtureTime
	e.Now = func() time.Time { return now }
	f.catalog.ExpiresAt = fixtureTime.Add(1000 * time.Hour)
	enrollFixture(t, e)
	d.beforeApply = func(Target) error { return errors.Join(ErrNotStarted, ErrSourceRestored) }
	for range maxDisruptiveAttempts {
		if _, err := e.RunOnce(t.Context(), true); err == nil {
			t.Fatal("refusal reported success")
		}
		now = now.Add(retryCap + time.Minute)
	}
	if state, _ := e.RunOnce(t.Context(), true); state.Reason != "retry_exhausted" {
		t.Fatalf("cap not reached: %+v", state)
	}
	// A catalog refresh that still selects the same target keeps the count.
	f.catalog.Sequence++
	if state, _ := e.RunOnce(t.Context(), true); state.Reason != "retry_exhausted" || d.applies() != maxDisruptiveAttempts {
		t.Fatalf("a new catalog sequence reset the count for the same target: %+v", state)
	}
	// A different selected target starts a fresh count.
	f.catalog.Sequence++
	newer := f.catalog.Targets[0]
	newer.Identity = fixtureIdentity("1.4.0", 9)
	f.catalog.Targets = append(f.catalog.Targets, newer)
	state, _ := e.RunOnce(t.Context(), true)
	if state.Reason != "preapply_refused" || state.RetryDisruptive != 1 || state.RetryTarget == nil || *state.RetryTarget != newer.Identity {
		t.Fatalf("new target kept the old count: %+v", state)
	}
}

func TestSecurityPostWriteRollbackIsNotReportedAsARefusalBeforeTheWrite(t *testing.T) {
	e, _, d := fixtureEngine(t)
	enrollFixture(t, e)
	d.beforeApply = func(Target) error { return errors.Join(ErrNotStarted, ErrSourceRestored, ErrRolledBack) }
	state, err := e.RunOnce(t.Context(), true)
	if !errors.Is(err, ErrRolledBack) || state.Reason != "operation_rolled_back" || state.RetryDisruptive != 1 || state.Operation != nil {
		t.Fatalf("rollback misreported: %v %+v", err, state)
	}
}

// A refusal before any mutation costs no outage, such as an eligibility window that closed.
func TestSecurityRefusalBeforeAnyMutationNeverExhaustsTheTarget(t *testing.T) {
	e, f, d := fixtureEngine(t)
	now := fixtureTime
	e.Now = func() time.Time { return now }
	f.catalog.ExpiresAt = fixtureTime.Add(1000 * time.Hour)
	enrollFixture(t, e)
	d.beforeApply = func(Target) error { return ErrNotStarted }
	for range 2 * maxDisruptiveAttempts {
		state, err := e.RunOnce(t.Context(), true)
		if err == nil || state.Reason != "preapply_refused" || state.RetryDisruptive != 0 {
			t.Fatalf("a plain refusal was counted as an outage: %+v %v", state, err)
		}
		now = now.Add(retryCap + time.Minute)
	}
	if d.applies() != 2*maxDisruptiveAttempts {
		t.Fatalf("automation stopped after refusals that cost nothing: %d", d.applies())
	}
}

func TestSecurityFailureBeforeTheWorkloadBacksOffWithoutStoppingAutomation(t *testing.T) {
	var s Status
	target := fixtureIdentity("1.3.1", 8)
	for range 2 * maxDisruptiveAttempts {
		s.noteFailure(target, fixtureTime, false, errors.New("registry unavailable"))
	}
	if s.retriesExhausted() {
		t.Fatal("failures that never reached the service stopped automatic updates")
	}
}

func TestSecurityReconcileLeavesBothWedges(t *testing.T) {
	t.Run("pinned operation", func(t *testing.T) {
		e, _, d := fixtureEngine(t)
		r := &reconcilingDriver{fixtureDriver: d}
		e.Driver = r
		enrollFixture(t, e)
		d.applyFailure = true
		state, err := e.RunOnce(t.Context(), true)
		if err == nil || state.Operation == nil {
			t.Fatalf("no pin: %+v %v", state, err)
		}
		manifest := string(state.Operation.Target.Identity.ManifestSHA256)
		if _, err := e.Reconcile(t.Context(), "wrong"); err == nil || !strings.Contains(err.Error(), manifest) {
			t.Fatalf("wrong confirmation accepted or unhelpful: %v", err)
		}
		d.observed.Ready = false
		if _, err := e.Reconcile(t.Context(), manifest); err == nil {
			t.Fatal("reconciled an unhealthy installation past its recovery fence")
		}
		d.observed.Ready = true
		state, err = e.Reconcile(t.Context(), manifest)
		if err != nil || state.Operation != nil || r.released != 1 {
			t.Fatalf("pin not cleared: %+v %v", state, err)
		}
		d.applyFailure = false
		state, err = e.RunOnce(t.Context(), true)
		if err != nil || state.Phase != "healthy" {
			t.Fatalf("update did not resume: %+v %v", state, err)
		}
		if _, err := e.Reconcile(t.Context(), manifest); err == nil {
			t.Fatal("reconciled with nothing pending")
		}
	})
	t.Run("restored installation", func(t *testing.T) {
		e, _, d := fixtureEngine(t)
		enrollFixture(t, e)
		d.observed.Incarnation = strings.Repeat("c", 64)
		d.observed.RestoreEpoch++
		state, err := e.RunOnce(t.Context(), true)
		if err == nil || state.Reason != "recovery_incarnation_changed" || !strings.Contains(state.Action, d.observed.Incarnation) {
			t.Fatalf("no actionable restore state: %+v %v", state, err)
		}
		if _, err := e.Reconcile(t.Context(), strings.Repeat("b", 64)); err == nil {
			t.Fatal("accepted the old incarnation")
		}
		state, err = e.Reconcile(t.Context(), d.observed.Incarnation)
		if err != nil || state.Enrollment.Incarnation != d.observed.Incarnation || state.Enrollment.RestoreEpoch != d.observed.RestoreEpoch {
			t.Fatalf("restore not accepted: %+v %v", state, err)
		}
		state, err = e.RunOnce(t.Context(), true)
		if err != nil || state.Phase != "healthy" {
			t.Fatalf("update did not resume after restore: %+v %v", state, err)
		}
	})
}

func TestSecurityProtectionGapExcludesExpectedWaits(t *testing.T) {
	gap := Status{Phase: "blocked", Reason: "application_pin", Applicable: []string{"A"}}
	if !gap.ProtectionGap() {
		t.Fatal("pinned advisory is not a gap")
	}
	for _, reason := range []string{"maintenance_window", "staged_rollout", "retry_backoff", "manual_mode"} {
		if (Status{Phase: "blocked", Reason: reason, Applicable: []string{"A"}}).ProtectionGap() {
			t.Fatalf("%s reported as a gap", reason)
		}
	}
	if (Status{Phase: "blocked", Reason: "retry_backoff", Applicable: []string{"A"}, RetryAttempts: gapAfterAttempts}).ProtectionGap() != true ||
		!(Status{Phase: "blocked", Reason: "retry_exhausted", Applicable: []string{"A"}}).ProtectionGap() {
		t.Fatal("a repeating failure hid behind its backoff")
	}
	if (Status{Phase: "blocked", Reason: "installation_not_healthy"}).ProtectionGap() || (Status{Phase: "current", Applicable: []string{"A"}}).ProtectionGap() {
		t.Fatal("no applicable advisory or a settled phase reported as a gap")
	}
}

func TestSecurityCompletionAcceptsSignedIndexDigest(t *testing.T) {
	index := strings.Repeat("d", 64)
	child := releaseidentity.Digest(strings.Repeat("e", 64))
	source := fixtureObservation()
	op := Operation{RequireRuntimeDigest: true, Source: source, Target: Target{Identity: fixtureIdentity("1.3.1", 8), Image: "ghcr.io/hikyo-org/hikyo@sha256:" + index, RuntimeImageSHA256: child}}
	running := source
	running.Identity, running.Image, running.Generation = op.Target.Identity, op.Target.Image, source.Generation+1
	for digest, want := range map[releaseidentity.Digest]bool{releaseidentity.Digest(index): true, child: true, releaseidentity.Digest(strings.Repeat("f", 64)): false} {
		running.RuntimeImageSHA256 = digest
		if completed(op, running) != want {
			t.Fatalf("digest %s: want %v", digest, want)
		}
	}
	// Without a catalog digest the runtime must still report a well-formed one.
	op.Target.RuntimeImageSHA256 = ""
	for digest, want := range map[releaseidentity.Digest]bool{"": false, "sha256:" + child: false, child: true, releaseidentity.Digest(index): true} {
		running.RuntimeImageSHA256 = digest
		if completed(op, running) != want {
			t.Fatalf("digest %q without catalog digest: want %v", digest, want)
		}
	}
	op.RequireRuntimeDigest, running.RuntimeImageSHA256 = false, ""
	if !completed(op, running) {
		t.Fatal("an image proof without runtime digest refused where none is required")
	}
}

func TestSecurityCatalogRejectsUnsafeCoverageAndAmbiguousInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Catalog)
	}{
		{"expired", func(c *Catalog) { c.ExpiresAt = fixtureTime }},
		{"future", func(c *Catalog) { c.IssuedAt = fixtureTime.Add(time.Hour); c.ExpiresAt = c.IssuedAt.Add(time.Hour) }},
		{"unbounded lifetime", func(c *Catalog) { c.ExpiresAt = c.IssuedAt.Add(MaxCatalogLifetime + time.Second) }},
		{"still affected", func(c *Catalog) { c.Targets[0].Identity = fixtureIdentity("1.2.1", 8) }},
		{"unknown fix", func(c *Catalog) { c.Targets[0].Fixes = []string{"unknown"} }},
		{"duplicate advisory", func(c *Catalog) { c.Advisories = append(c.Advisories, c.Advisories[0]) }},
		{"duplicate target", func(c *Catalog) { c.Targets = append(c.Targets, c.Targets[0]) }},
		{"mutable image", func(c *Catalog) { c.Targets[0].Image = "ghcr.io/example/hikyo:latest" }},
		{"unknown delivery", func(c *Catalog) { c.Advisories[0].Affected[0].Delivery = "remote-host" }},
		{"false date", func(c *Catalog) { c.Targets[0].CoverageThrough = "2099-01-01" }},
		{"rollout without availability", func(c *Catalog) { c.Targets[0].RolloutMinutes = 60 }},
		{"rollout too long", func(c *Catalog) {
			c.Targets[0].AvailableAt, c.Targets[0].RolloutMinutes = fixtureTime, MaxRolloutMinutes+1
		}},
		{"negative rollout", func(c *Catalog) { c.Targets[0].AvailableAt, c.Targets[0].RolloutMinutes = fixtureTime, -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureCatalog()
			tc.edit(&c)
			raw, _ := json.Marshal(c)
			if _, err := DecodeCatalog(raw, fixtureTime); err == nil {
				t.Fatal("unsafe catalog accepted")
			}
		})
	}
	valid, _ := json.Marshal(fixtureCatalog())
	for _, raw := range [][]byte{append(valid, []byte(" {}")...), []byte(strings.Replace(string(valid), `"sequence":1`, `"sequence":1,"sequence":2`, 1)), []byte(strings.Replace(string(valid), `"sequence":1`, `"sequence":1,"unreviewed":true`, 1)), []byte("null")} {
		if _, err := DecodeCatalog(raw, fixtureTime); err == nil {
			t.Fatal("ambiguous closed artifact accepted")
		}
	}
}

// Stable and nightly release sequences are allocated independently.
func TestSecurityCatalogScopesSequenceEquivocationByProfile(t *testing.T) {
	c := fixtureCatalog()
	nightly := c.Targets[0]
	nightly.Identity = fixtureIdentity("1.4.0-nightly.8", 8)
	nightly.Identity.Profile = releaseidentity.NightlyV1
	c.Targets = append(c.Targets, nightly)
	if err := c.Validate(fixtureTime); err != nil {
		t.Fatalf("stable and nightly targets at one sequence refused: %v", err)
	}
	stable := c.Targets[0]
	stable.Identity = fixtureIdentity("1.3.2", 8)
	c.Targets = append(c.Targets, stable)
	if err := c.Validate(fixtureTime); err == nil || !strings.Contains(err.Error(), "equivocate") {
		t.Fatalf("stable equivocation accepted: %v", err)
	}
}

func TestSecurityCatalogAcceptsMaximumLifetimeRolloutAndWithdrawnBadFix(t *testing.T) {
	c := fixtureCatalog()
	c.ExpiresAt = c.IssuedAt.Add(MaxCatalogLifetime)
	c.Targets[0].AvailableAt, c.Targets[0].RolloutMinutes = fixtureTime, MaxRolloutMinutes
	// The fixed version moved past a release that once claimed this fix.
	c.Targets[0].Identity = fixtureIdentity("1.2.1", 8)
	c.Targets[0].Withdrawn = true
	raw, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeCatalog(raw, fixtureTime)
	if err != nil {
		t.Fatalf("withdrawn target or maximum lifetime refused: %v", err)
	}
	if !decoded.Targets[0].AvailableAt.Equal(fixtureTime) || decoded.Targets[0].RolloutMinutes != MaxRolloutMinutes {
		t.Fatalf("rollout fields lost: %+v", decoded.Targets[0])
	}
	if fixes := decoded.FixesFor(Target{Identity: fixtureIdentity("1.2.1", 8), Platforms: []string{"linux/amd64"}}); len(fixes) != 0 {
		t.Fatalf("affected target credited with a fix: %v", fixes)
	}
	if fixes := decoded.FixesFor(decoded.Targets[0]); len(fixes) != 0 {
		t.Fatalf("withdrawn advisory state or range ignored: %v", fixes)
	}
}

// A target is credited only with advisories naming its profile, delivery and a platform of it.
func TestSecurityFixesForStaysWithinTheTargetScope(t *testing.T) {
	c := fixtureCatalog()
	scoped := func(id, delivery string, profile releaseidentity.Profile, platform string) Advisory {
		advisory := c.Advisories[0]
		advisory.ID, advisory.Affected = id, []Affected{{Profile: profile, Fixed: "1.3.1", Platforms: []string{platform}, Delivery: delivery}}
		return advisory
	}
	c.Advisories = append(c.Advisories, scoped("binary", "binary", releaseidentity.StableV1, "linux/amd64"), scoped("image", "image", releaseidentity.StableV1, "linux/amd64"),
		scoped("darwin", "", releaseidentity.StableV1, "darwin/arm64"), scoped("nightly", "", releaseidentity.NightlyV1, "linux/amd64"))
	binary := c.Targets[0]
	image := binary
	image.Image = "ghcr.io/example/hikyo@sha256:" + strings.Repeat("a", 64)
	if fixes := c.FixesFor(binary); !slices.Equal(fixes, []string{"HK-SA-EXAMPLE", "binary"}) {
		t.Fatalf("native fixes = %v", fixes)
	}
	if fixes := c.FixesFor(image); !slices.Equal(fixes, []string{"HK-SA-EXAMPLE", "image"}) {
		t.Fatalf("image fixes = %v", fixes)
	}
}

func TestSecurityCoverageAndApplicabilityStayWithinArtifactDelivery(t *testing.T) {
	for _, image := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "image"}[image], func(t *testing.T) {
			e, f, d := fixtureEngine(t)
			binary := f.catalog.Targets[0]
			binary.CoverageThrough = "2026-10-31"
			f.catalog.Advisories[0].Affected[0].Delivery = "binary"
			imageAdvisory := f.catalog.Advisories[0]
			imageAdvisory.ID, imageAdvisory.Components = "HK-SA-IMAGE", []string{"image-base"}
			imageAdvisory.Affected = slices.Clone(imageAdvisory.Affected)
			imageAdvisory.Affected[0].Delivery = "image"
			f.catalog.Advisories = append(f.catalog.Advisories, imageAdvisory)
			oci := binary
			oci.Image, oci.Fixes, oci.CoverageThrough = "ghcr.io/example/hikyo@sha256:"+strings.Repeat("a", 64), []string{imageAdvisory.ID}, "2026-11-01"
			// Put OCI first so catalog order cannot give native its coverage.
			f.catalog.Targets = []Target{oci, binary}
			e.ImageDelivery = image
			decision, err := selectForDelivery(f.catalog, d.observed.Identity, d.observed.Instance, e.Platform, e.Policy, fixtureTime, image)
			want := binary
			if image {
				want = oci
			}
			if err != nil || decision.Target == nil || decision.Target.Image != want.Image || !slices.Equal(decision.Applicable, want.Fixes) {
				t.Fatalf("artifact applicability escaped its scope: %+v %v", decision, err)
			}
			d.observed.Identity, d.observed.Image = want.Identity, want.Image
			enrollFixture(t, e)
			state, err := e.RunOnce(t.Context(), false)
			if err != nil || state.Phase != "current" || !slices.Equal(state.Coverage, want.Fixes) || state.CoverageThrough != want.CoverageThrough {
				t.Fatalf("current coverage came from another artifact: %+v %v", state, err)
			}
			if image {
				d.observed.Image = "ghcr.io/example/hikyo@sha256:" + strings.Repeat("d", 64)
				state, err = e.RunOnce(t.Context(), false)
				if err != nil || len(state.Coverage) != 0 || state.CoverageThrough != "" {
					t.Fatal("unlisted image borrowed another artifact's coverage", err)
				}
			}
		})
	}
}

func TestSecurityCoverageRefreshesWhenBlockedAfterCorrectionOrManualUpgrade(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "corrected bulletin", true: "manual artifact change"}[manual], func(t *testing.T) {
			e, f, d := fixtureEngine(t)
			target := f.catalog.Targets[0]
			other := f.catalog.Advisories[0]
			other.ID, other.Components = "HK-SA-STILL-COVERED", []string{"other-component"}
			other.Affected = slices.Clone(other.Affected)
			f.catalog.Advisories = append(f.catalog.Advisories, other)
			f.catalog.Targets[0].Fixes = append(slices.Clone(target.Fixes), other.ID)
			d.observed.Identity = target.Identity
			enrollFixture(t, e)
			state, err := e.RunOnce(t.Context(), false)
			if err != nil || state.Phase != "current" || len(state.Coverage) != 2 {
				t.Fatal("initial verified coverage missing", err)
			}
			f.catalog.Sequence++
			if manual {
				d.observed.Identity = fixtureIdentity("1.3.2", 9)
				d.observed.Generation++
			}
			// Fresh authenticated metadata retracts the previous fix claim.
			// The still-running artifact is affected with no eligible target.
			f.catalog.Advisories[0].Affected[0].Fixed = "1.4.0"
			f.catalog.Targets[0].Fixes = []string{other.ID}
			f.catalog.Targets[0].CoverageThrough = "2026-10-31"
			state, err = e.RunOnce(t.Context(), false)
			wantIDs, wantComponents, wantDate := []string{other.ID}, other.Components, "2026-10-31"
			if manual {
				wantIDs, wantComponents, wantDate = nil, nil, ""
			}
			if err != nil || state.Phase != "blocked" || len(state.Applicable) != 1 || !slices.Equal(state.Coverage, wantIDs) || !slices.Equal(state.CoveredComponents, wantComponents) || state.CoverageThrough != wantDate || state.Running.Identity != d.observed.Identity {
				t.Fatalf("blocked status retained another catalog/artifact's coverage: %+v %v", state, err)
			}
		})
	}
}

func TestSecurityCompletionCannotRestoreRetractedCoverage(t *testing.T) {
	e, f, d := fixtureEngine(t)
	enrollFixture(t, e)
	d.falseSuccess = true
	state, err := e.RunOnce(t.Context(), true)
	if err == nil || state.Operation == nil {
		t.Fatal("fixture did not retain ambiguous replacement", err)
	}
	d.observed.Identity = state.Operation.Target.Identity
	d.observed.Generation++
	f.catalog.Sequence++
	f.catalog.Targets[0].Withdrawn = true
	state, err = e.RunOnce(t.Context(), true)
	if err != nil || state.Operation != nil || state.Phase != "healthy" || len(state.Coverage) != 0 || state.CoverageThrough != "" {
		t.Fatalf("operation reconciliation restored withdrawn coverage claims: %+v %v", state, err)
	}
}

func TestSecurityFinalRefreshRetractsSourceCoverageAndStagedSelection(t *testing.T) {
	e, f, d := fixtureEngine(t)
	source := f.catalog.Targets[0]
	d.observed.Identity = source.Identity
	remaining := f.catalog.Advisories[0]
	remaining.ID, remaining.Components = "HK-SA-RETAINED", []string{"other-component"}
	remaining.Affected = slices.Clone(remaining.Affected)
	pending := remaining
	pending.ID = "HK-SA-PENDING"
	pending.Affected = slices.Clone(pending.Affected)
	pending.Affected[0].Fixed = "1.4.0"
	f.catalog.Advisories = append(f.catalog.Advisories, remaining, pending)
	source.Fixes = []string{f.catalog.Advisories[0].ID, remaining.ID}
	target := source
	target.Identity = fixtureIdentity("1.4.0", 9)
	target.Fixes = append(slices.Clone(source.Fixes), pending.ID)
	f.catalog.Targets = []Target{source, target}
	enrollFixture(t, e)
	state, err := e.RunOnce(t.Context(), false)
	if err != nil || len(state.Coverage) != 2 || state.Selected == nil {
		t.Fatal("fixture did not establish covered source and applicable target", err)
	}
	f.calls = 0
	f.change = func(call int, c *Catalog) {
		if call == 2 {
			c.Sequence++
			c.Advisories[0].Affected[0].Fixed = "1.4.0"
			c.Targets[0].Fixes, c.Targets[0].CoverageThrough = []string{remaining.ID}, "2026-10-31"
			c.Targets[1].Withdrawn = true
		}
	}
	state, err = e.RunOnce(t.Context(), true)
	if err != nil || state.CatalogSequence != 2 || state.Phase != "blocked" || state.Reason != "target_withdrawn_or_changed" || state.Operation != nil || state.Selected != nil || d.applies() != 0 || !slices.Equal(state.Coverage, []string{remaining.ID}) || !slices.Equal(state.CoveredComponents, remaining.Components) || state.CoverageThrough != "2026-10-31" || len(state.Applicable) != 2 {
		t.Fatalf("final metadata inherited retracted coverage/selection/applicability: %+v %v", state, err)
	}
}

func TestSecurityFreshFeedCannotRenewUnobservedCoverage(t *testing.T) {
	e, f, d := fixtureEngine(t)
	d.observed.Identity = f.catalog.Targets[0].Identity
	enrollFixture(t, e)
	if state, err := e.RunOnce(t.Context(), false); err != nil || len(state.Coverage) != 1 {
		t.Fatal("initial running coverage missing", err)
	}
	f.catalog.Sequence++
	d.observeFailure = errors.New("source cannot be observed")
	state, err := e.RunOnce(t.Context(), false)
	if err == nil || state.CatalogSequence != 2 || state.Reason != "installation_unavailable" || len(state.Coverage) != 0 || state.CoverageThrough != "" {
		t.Fatalf("fresh feed renewed old unobserved coverage: %+v %v", state, err)
	}
}

func TestSecurityOutcomeStatusUsesActualObservationAfterPossibleMutation(t *testing.T) {
	for _, scenario := range []string{"ambiguous apply unavailable", "ambiguous apply unhealthy", "success observation unavailable", "different running artifact", "definite refusal"} {
		t.Run(scenario, func(t *testing.T) {
			e, f, d := fixtureEngine(t)
			source := f.catalog.Targets[0]
			d.observed.Identity = source.Identity
			pending := f.catalog.Advisories[0]
			pending.ID, pending.Affected = "HK-SA-PENDING", slices.Clone(pending.Affected)
			pending.Affected[0].Fixed = "1.4.0"
			f.catalog.Advisories = append(f.catalog.Advisories, pending)
			target := source
			target.Identity, target.Fixes = fixtureIdentity("1.4.0", 10), append(slices.Clone(source.Fixes), pending.ID)
			f.catalog.Targets = []Target{source, target}
			enrollFixture(t, e)
			state, err := e.RunOnce(t.Context(), false)
			if err != nil || len(state.Coverage) != 1 || state.Selected == nil {
				t.Fatal("fixture did not establish covered source and pending remediation", err)
			}
			switch scenario {
			case "ambiguous apply unavailable":
				d.applyFailure = true
				d.duringApply = func() { d.observeFailure = errors.New("workload outcome unavailable") }
			case "ambiguous apply unhealthy":
				d.applyFailure = true
				d.duringApply = func() {
					d.observed.Identity, d.observed.Ready, d.observed.Maintenance, d.observed.Phase = target.Identity, false, true, "schema-applied"
					d.observed.Generation++
				}
			case "success observation unavailable":
				d.duringApply = func() { d.observeFailure = errors.New("replacement cannot be observed") }
			case "different running artifact":
				d.falseSuccess = true
				d.duringApply = func() {
					d.observed.Identity = fixtureIdentity("1.3.2", 9)
					d.observed.Generation++
				}
			case "definite refusal":
				d.beforeApply = func(Target) error { return ErrNotStarted }
			}
			state, err = e.RunOnce(t.Context(), true)
			if err == nil {
				t.Fatal("unproven/refused update reported success")
			}
			if scenario == "definite refusal" {
				if state.Phase != "blocked" || state.Operation != nil || state.Running == nil || state.Running.Identity != source.Identity || !slices.Equal(state.Coverage, source.Fixes) {
					t.Fatalf("definite refusal lost newly observed unchanged source: %+v", state)
				}
				return
			}
			if state.Phase != "failed" || state.Operation == nil || state.Operation.Source.Identity != source.Identity || len(state.Coverage) != 0 || state.CoverageThrough != "" {
				t.Fatalf("possible mutation reused stopped source claims or lost recovery context: %+v", state)
			}
			if scenario == "different running artifact" {
				if state.Running == nil || state.Running.Identity != d.observed.Identity || !slices.Equal(state.Applicable, []string{pending.ID}) {
					t.Fatal("failure retained the predecessor rather than actual artifact")
				}
			} else if scenario == "ambiguous apply unhealthy" {
				if state.Running == nil || state.Running.Ready || state.Running.Identity != target.Identity {
					t.Fatal("failed target observation was replaced with the old healthy source")
				}
			} else if state.Running != nil || len(state.Applicable) != 0 {
				t.Fatal("unavailable observation retained current artifact claims")
			}
		})
	}
}

type fixtureFeed struct {
	catalog Catalog
	calls   int
	failAt  int
	change  func(int, *Catalog)
}

func (f *fixtureFeed) Refresh(context.Context) (securityfeed.Document, error) {
	f.calls++
	if f.failAt == f.calls {
		return securityfeed.Document{}, errors.New("test publisher unavailable")
	}
	if f.change != nil {
		f.change(f.calls, &f.catalog)
	}
	raw, err := json.Marshal(f.catalog)
	return securityfeed.Document{Data: raw, Version: f.catalog.Sequence, Expires: f.catalog.ExpiresAt}, err
}

type fixtureDriver struct {
	observed       Observation
	observeFailure error
	calls          []string
	prepareImpact  Impact
	duringPrepare  func()
	duringObserve  func()
	duringApply    func()
	beforeApply    func(Target) error
	applyFailure   bool
	falseSuccess   bool
}

func (d *fixtureDriver) Observe(context.Context) (Observation, error) {
	d.calls = append(d.calls, "observe")
	if d.duringObserve != nil {
		d.duringObserve()
	}
	return d.observed, d.observeFailure
}
func (d *fixtureDriver) Prepare(_ context.Context, target Target) (Preparation, error) {
	d.calls = append(d.calls, "prepare")
	source := d.observed
	if d.duringPrepare != nil {
		d.duringPrepare()
	}
	return Preparation{Target: target.Identity, Source: source, Image: target.Image, RouteSHA256: releaseidentity.Hash([]byte("verified route")), Impact: d.prepareImpact}, nil
}
func (d *fixtureDriver) Apply(_ context.Context, target Target) error {
	d.calls = append(d.calls, "apply")
	if d.beforeApply != nil {
		if err := d.beforeApply(target); err != nil {
			return err
		}
	}
	if d.duringApply != nil {
		d.duringApply()
	}
	if d.applyFailure {
		return errors.New("test failure after possible schema writes")
	}
	if !d.falseSuccess {
		d.observed.Identity = target.Identity
		d.observed.Generation++
		d.observed.Image = target.Image
	}
	return nil
}
func (d *fixtureDriver) applies() int {
	n := 0
	for _, c := range d.calls {
		if c == "apply" {
			n++
		}
	}
	return n
}

func fixtureEngine(t *testing.T) (*Engine, *fixtureFeed, *fixtureDriver) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("local state ownership requires the Unix installation contract")
	}
	store, err := NewStore(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	f := &fixtureFeed{catalog: fixtureCatalog()}
	d := &fixtureDriver{observed: fixtureObservation()}
	e := &Engine{Store: store, Feed: f, Driver: d, Platform: "linux/amd64", Policy: DefaultPolicy(), Now: func() time.Time { return fixtureTime }}
	return e, f, d
}

func TestSecurityMaintenanceWindowDeadlineSurvivesLongFinalValidation(t *testing.T) {
	for _, elapsed := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline delivered", true: "window closed during observation"}[elapsed], func(t *testing.T) {
			e, _, d := fixtureEngine(t)
			enrollFixture(t, e)
			now := fixtureTime.Add(30 * time.Second)
			e.Now = func() time.Time { return now }
			e.Policy.WindowStartMinute, e.Policy.WindowDurationMinutes = 12*60, 1
			observations := 0
			d.duringObserve = func() {
				observations++
				if elapsed && observations == 2 {
					now = fixtureTime.Add(time.Minute)
				}
			}
			d.beforeApply = func(target Target) error {
				if !target.EligibilityUntil.Equal(fixtureTime.Add(time.Minute)) {
					t.Fatalf("window end not bound to deployment admission: %s", target.EligibilityUntil)
				}
				return nil
			}
			state, err := e.RunOnce(t.Context(), true)
			if err != nil {
				t.Fatal(err)
			}
			if elapsed && (d.applies() != 0 || state.Reason != "maintenance_window" || state.Operation != nil) {
				t.Fatalf("closed window admitted mutation: %+v", state)
			}
			if !elapsed && (state.Phase != "healthy" || d.applies() != 1) {
				t.Fatalf("eligible operation refused: %+v", state)
			}
		})
	}
}

func TestSecurityMaintenanceRuntimeDigestProofMatchesDeploymentContract(t *testing.T) {
	for _, required := range []bool{false, true} {
		t.Run(map[bool]string{false: "Compose equivalent image proof", true: "runtime manifest required"}[required], func(t *testing.T) {
			e, f, d := fixtureEngine(t)
			e.ImageDelivery, e.RequireRuntimeDigest = true, required
			f.catalog.Targets[0].Image = "ghcr.io/example/hikyo@sha256:" + strings.Repeat("a", 64)
			f.catalog.Targets[0].RuntimeImageSHA256 = releaseidentity.Digest(strings.Repeat("c", 64))
			enrollFixture(t, e)
			state, err := e.RunOnce(t.Context(), true)
			if !required {
				if err != nil || state.Phase != "healthy" || state.Operation != nil {
					t.Fatalf("equivalent Docker image/payload proof rejected: %+v %v", state, err)
				}
				return
			}
			if err == nil || state.Operation == nil || !state.Operation.RequireRuntimeDigest {
				t.Fatalf("missing runtime digest counted as completion: %+v %v", state, err)
			}
			// A restart or local setting change cannot weaken the pending proof.
			e.RequireRuntimeDigest = false
			if state, err = e.RunOnce(t.Context(), true); err == nil || state.Operation == nil || d.applies() != 1 {
				t.Fatal("reopened checker weakened the durable completion requirement", err)
			}
			d.observed.RuntimeImageSHA256 = f.catalog.Targets[0].RuntimeImageSHA256
			if state, err = e.RunOnce(t.Context(), true); err != nil || state.Operation != nil || state.Phase != "healthy" {
				t.Fatal("exact runtime proof did not reconcile original operation", err)
			}
		})
	}
}

func enrollFixture(t *testing.T, e *Engine) {
	t.Helper()
	if _, err := e.Enroll(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSecurityMaintenanceEndToEndStateAndExactRunningProof(t *testing.T) {
	e, f, d := fixtureEngine(t)
	state, err := e.RunOnce(t.Context(), true)
	if err != nil || state.Reason != "explicit_enrollment_required" || d.applies() != 0 {
		t.Fatalf("implicit adoption: %+v %v", state, err)
	}
	enrollFixture(t, e)
	state, err = e.RunOnce(t.Context(), false)
	if err != nil || state.Phase != "applicable" || d.applies() != 0 {
		t.Fatalf("read-only check applied: %+v %v", state, err)
	}
	state, err = e.RunOnce(t.Context(), true)
	if err != nil || state.Phase != "healthy" || state.Operation != nil || state.Running.Identity != f.catalog.Targets[0].Identity || len(state.Applicable) != 0 || d.applies() != 1 {
		t.Fatalf("automatic replacement failed: %+v %v", state, err)
	}
	persisted, err := e.Store.Load()
	if err != nil || persisted.Phase != "healthy" || persisted.Running.Generation != 5 {
		t.Fatalf("outcome not durable: %+v %v", persisted, err)
	}
	state, err = e.RunOnce(t.Context(), true)
	if err != nil || state.Phase != "current" || d.applies() != 1 {
		t.Fatalf("already installed repeated: %+v %v", state, err)
	}
}

func TestSecurityMaintenanceRefusesWithdrawalOutageAndSourceDriftBeforeStop(t *testing.T) {
	for _, name := range []string{"withdrawal", "feed outage", "source drift", "measured maintenance"} {
		t.Run(name, func(t *testing.T) {
			e, f, d := fixtureEngine(t)
			enrollFixture(t, e)
			switch name {
			case "withdrawal":
				f.change = func(call int, c *Catalog) {
					if call == 2 {
						c.Sequence++
						c.Targets[0].Withdrawn = true
					}
				}
			case "feed outage":
				f.failAt = 2
			case "source drift":
				d.duringPrepare = func() { d.observed.Generation++ }
			case "measured maintenance":
				e.Policy.AllowMaintenance = false
				f.catalog.Targets[0].Impact.Schema = false
				f.catalog.Targets[0].Impact.Maintenance = false
				d.prepareImpact.Maintenance = true
			}
			state, _ := e.RunOnce(t.Context(), true)
			if d.applies() != 0 || state.Operation != nil || state.Phase == "healthy" {
				t.Fatalf("unsafe apply: %+v calls=%v", state, d.calls)
			}
			if !d.observed.Ready {
				t.Fatal("healthy source was stopped by a preapply failure")
			}
		})
	}
}

func TestSecurityMaintenancePinsUncertainOperationAcrossRestart(t *testing.T) {
	e, f, d := fixtureEngine(t)
	enrollFixture(t, e)
	d.falseSuccess = true
	state, err := e.RunOnce(t.Context(), true)
	if err == nil || state.Operation == nil || state.Reason != "running_target_unproven" {
		t.Fatalf("false success: %+v %v", state, err)
	}
	if state.Action != reconcileAction(string(state.Operation.Target.Identity.ManifestSHA256)) || !state.ProtectionGap() || state.Public().Action == "" {
		t.Fatalf("pinned unproven target hides its recovery command: %+v", state)
	}
	selected := state.Operation.Target.Identity
	f.catalog.Sequence++
	newer := f.catalog.Targets[0]
	newer.Identity = fixtureIdentity("1.4.0", 9)
	f.catalog.Targets = append(f.catalog.Targets, newer)
	state, err = e.RunOnce(t.Context(), true)
	if err == nil || d.applies() != 1 || state.Operation.Target.Identity != selected || state.Reason != "operation_requires_reconciliation" {
		t.Fatalf("pending operation stolen: %+v %v", state, err)
	}
	// Independent deployment-owner reconciliation can finish the same exact
	// operation. The checker observes that outcome; it never restarts old code.
	d.observed.Identity = selected
	d.observed.Generation++
	state, err = e.RunOnce(t.Context(), true)
	if err != nil || state.Phase != "healthy" || state.Operation != nil || d.applies() != 1 || state.Action != "" {
		t.Fatalf("exact recovery not observed or kept a stale action: %+v %v", state, err)
	}
}

func TestSecurityMaintenanceOutageAndRestoreDoNotResetTrustOrEnrollment(t *testing.T) {
	e, f, d := fixtureEngine(t)
	enrollFixture(t, e)
	if _, err := e.RunOnce(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	f.failAt = f.calls + 1
	state, err := e.RunOnce(t.Context(), true)
	if err == nil || state.Phase != "stale" || state.CatalogSequence != 1 || d.applies() != 0 || !d.observed.Healthy() {
		t.Fatalf("outage changed service or trust: %+v %v", state, err)
	}
	f.failAt = 0
	f.catalog.Sequence = 2
	d.observed.Incarnation = strings.Repeat("c", 64)
	d.observed.RestoreEpoch++
	state, err = e.RunOnce(t.Context(), true)
	if err == nil || state.Reason != "recovery_incarnation_changed" || d.applies() != 0 {
		t.Fatalf("restored authority reused: %+v %v", state, err)
	}
	before := state.Enrollment
	if after, err := e.Enroll(t.Context()); err != nil || after.Enrollment == nil || *after.Enrollment != *before {
		t.Fatalf("re-enroll must succeed without adopting the restored incarnation: %+v %v", after.Enrollment, err)
	}
	if saved, err := e.Store.Load(); err != nil || *saved.Enrollment != *before || saved.Reason != "recovery_incarnation_changed" {
		t.Fatalf("re-enroll reset recovery authority: %+v %v", saved, err)
	}
	// The authenticated sequence survives the independent observation refusal.
	f.catalog.Sequence = 1
	state, err = e.RunOnce(t.Context(), true)
	if err == nil || state.Reason != "catalog_rollback_or_equivocation" {
		t.Fatalf("floor lost after refusal: %+v %v", state, err)
	}
}

func TestSecurityMaintenanceStateRefusesCorruptionAndSymlink(t *testing.T) {
	e, _, d := fixtureEngine(t)
	enrollFixture(t, e)
	path := filepath.Join(e.Store.directory, "status.json")
	if err := os.WriteFile(path, []byte(`{"format":"hikyo.security-maintenance-status/v1","phase":"healthy","catalog_sequence":99}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := e.RunOnce(t.Context(), true); err == nil {
		t.Fatal("corrupt floor accepted")
	}
	if d.applies() != 0 {
		t.Fatal("corrupt state applied")
	}
	// Defaults must not stand in for missing members: that would read as unenrolled.
	for _, incomplete := range []string{`{}`, `{"applicable":[]}`, `{"phase":"healthy"}`, `{"format":"hikyo.security-maintenance-status/v1"}`} {
		if err := os.WriteFile(path, []byte(incomplete), 0600); err != nil {
			t.Fatal(err)
		}
		if state, err := e.Store.Load(); err == nil {
			t.Fatalf("incomplete state %s loaded as %+v", incomplete, state)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(out, []byte("protected"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, path); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Store.Load(); err == nil {
		t.Fatal("state symlink followed")
	}
	raw, err := os.ReadFile(out)
	if err != nil || string(raw) != "protected" {
		t.Fatal("outside file changed")
	}
}

// The trailing newline counts: a state Save accepts must be one Load reads back.
func TestSecurityMaintenanceStateSizeBoundIncludesTheNewline(t *testing.T) {
	e, _, _ := fixtureEngine(t)
	padded := func(size int) Status {
		state := Status{Format: StatusFormat, Phase: "enrolled", Applicable: []string{}, Coverage: []string{""}}
		base, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		state.Coverage[0] = strings.Repeat("a", size-len(base))
		return state
	}
	if err := e.Store.Save(padded(MaxCatalogBytes - 1)); err != nil {
		t.Fatalf("largest state refused: %v", err)
	}
	if _, err := e.Store.Load(); err != nil {
		t.Fatalf("largest saved state unreadable: %v", err)
	}
	if err := e.Store.Save(padded(MaxCatalogBytes)); err == nil {
		t.Fatal("state one byte over the file bound saved")
	}
	if _, err := e.Store.Load(); err != nil {
		t.Fatalf("refused save damaged the stored state: %v", err)
	}
}

// The chart's enrollment hook runs on every upgrade, so an enrolled installation
// must never fail it, whether unhealthy mid-recovery or unobservable.
func TestSecurityEnrollWhenAlreadyEnrolledSucceedsWithoutActing(t *testing.T) {
	e, _, d := fixtureEngine(t)
	enrollFixture(t, e)
	enrolled, err := e.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	d.observed.Ready, d.observed.Phase, d.observed.Maintenance = false, "backup-preparing", true
	d.observeFailure = errors.New("workload unavailable")
	d.calls = nil
	state, err := e.Enroll(t.Context())
	if err != nil || state.Enrollment == nil || *state.Enrollment != *enrolled.Enrollment || len(d.calls) != 0 {
		t.Fatalf("enrolled installation failed or acted: %+v %v calls=%v", state.Enrollment, err, d.calls)
	}
	if saved, err := e.Store.Load(); err != nil || saved.Phase != enrolled.Phase {
		t.Fatalf("repeat enroll changed the stored status: %+v %v", saved, err)
	}
	fresh, _, d2 := fixtureEngine(t)
	d2.observed.Ready = false
	if _, err := fresh.Enroll(t.Context()); err == nil {
		t.Fatal("first enrollment of an unhealthy installation was accepted")
	}
}

func TestSecurityRestoredSourceDetailSaysWhatHappened(t *testing.T) {
	e, _, d := fixtureEngine(t)
	enrollFixture(t, e)
	d.beforeApply = func(Target) error {
		return errors.Join(ErrNotStarted, ErrSourceRestored, errors.New("the target stopped before the database was written; the source image is serving again"))
	}
	state, err := e.RunOnce(t.Context(), true)
	if !errors.Is(err, ErrNotStarted) || !errors.Is(err, ErrSourceRestored) || state.Reason != "preapply_refused" || state.RetryDisruptive != 1 {
		t.Fatalf("classification changed: %+v %v", state, err)
	}
	if strings.Contains(state.Detail, "before workload mutation") || !strings.Contains(state.Detail, "interrupted") || !strings.Contains(state.Detail, "serving again") {
		t.Fatalf("detail does not state what happened: %q", state.Detail)
	}
}

func TestSecurityStatusMarksAWithdrawnRunningRelease(t *testing.T) {
	e, f, d := fixtureEngine(t)
	d.observed.Identity = f.catalog.Targets[0].Identity
	enrollFixture(t, e)
	if state, err := e.RunOnce(t.Context(), false); err != nil || state.RunningWithdrawn {
		t.Fatalf("a listed release was marked withdrawn: %+v %v", state, err)
	}
	f.catalog.Sequence++
	f.catalog.Targets[0].Withdrawn = true
	state, err := e.RunOnce(t.Context(), false)
	if err != nil || !state.RunningWithdrawn {
		t.Fatalf("withdrawn running release not marked: %+v %v", state, err)
	}
	d.observed.Identity = fixtureIdentity("1.2.0", 7)
	if state, err = e.RunOnce(t.Context(), false); err != nil || state.RunningWithdrawn {
		t.Fatalf("an unrelated running release was marked withdrawn: %+v %v", state, err)
	}
}

// deadlineDriver restores the source whenever asked, recording what it was given.
type deadlineDriver struct {
	*fixtureDriver
	healthy  Observation
	restored int
	gotSrc   []Observation
}

func (d *deadlineDriver) RestoreAborted(_ context.Context, _ Observation, operation Operation) error {
	d.restored++
	d.gotSrc = append(d.gotSrc, operation.Source)
	d.observed = d.healthy
	return errors.Join(ErrNotStarted, ErrSourceRestored)
}

// NoFinalGate proves the fixture's unreadable target short of the gate. A driver that proves nothing is the
// case TestSecurityDeadlineNeverStopsATargetPastTheFinalGate pins.
func (d *deadlineDriver) NoFinalGate(context.Context, Operation) (bool, error) { return true, nil }

const deadlineFixture = time.Hour

func deadlineEngine(t *testing.T) (*Engine, *deadlineDriver, *fixtureDriver, *time.Time) {
	t.Helper()
	e, f, d := fixtureEngine(t)
	f.catalog.ExpiresAt = fixtureTime.Add(1000 * time.Hour)
	r := &deadlineDriver{fixtureDriver: d, healthy: d.observed}
	e.Driver = r
	e.OperationTimeout = deadlineFixture
	now := fixtureTime
	e.Now = func() time.Time { return now }
	enrollFixture(t, e)
	d.applyFailure = true
	if state, err := e.RunOnce(t.Context(), true); err == nil || state.Operation == nil {
		t.Fatalf("no pin: %+v %v", state, err)
	}
	d.applyFailure = false
	// The target stopped serving and wrote a generation above the source's.
	d.observed.Ready, d.observed.Phase, d.observed.Maintenance = false, "backup-preparing", true
	d.observed.Generation++
	return e, r, d, &now
}

func operationSource(t *testing.T, e *Engine) Observation {
	t.Helper()
	state, err := e.Store.Load()
	if err != nil || state.Operation == nil {
		t.Fatalf("no operation: %+v %v", state, err)
	}
	return state.Operation.Source
}

func TestSecurityDeadlineRollsBackAnUnfinishedReplacement(t *testing.T) {
	t.Run("before the deadline the operation waits", func(t *testing.T) {
		e, r, _, now := deadlineEngine(t)
		*now = now.Add(deadlineFixture - time.Second)
		state, err := e.RunOnce(t.Context(), true)
		if err == nil || state.Operation == nil || r.restored != 0 {
			t.Fatalf("rolled back early: %+v %v", state, err)
		}
	})
	t.Run("a check never rolls back", func(t *testing.T) {
		e, r, _, now := deadlineEngine(t)
		*now = now.Add(deadlineFixture)
		if state, _ := e.RunOnce(t.Context(), false); state.Operation == nil || r.restored != 0 {
			t.Fatalf("check restored the source: %+v", state)
		}
	})
	for name, prepare := range map[string]func(*Engine, *deadlineDriver){
		"deadline passed": func(*Engine, *deadlineDriver) {},
		"manual policy":   func(e *Engine, _ *deadlineDriver) { e.Policy.Mode = Manual },
		"stale feed": func(e *Engine, _ *deadlineDriver) {
			e.Feed.(*fixtureFeed).catalog.ExpiresAt = fixtureTime.Add(-time.Hour)
		},
		"unreachable feed":    func(e *Engine, _ *deadlineDriver) { f := e.Feed.(*fixtureFeed); f.failAt = f.calls + 1 },
		"unobservable target": func(_ *Engine, r *deadlineDriver) { r.observeFailure = errors.New("workload unavailable") },
	} {
		t.Run(name, func(t *testing.T) {
			e, r, d, now := deadlineEngine(t)
			source := operationSource(t, e)
			failedGeneration := d.observed.Generation
			*now = now.Add(deadlineFixture)
			prepare(e, r)
			_, _ = e.RunOnce(t.Context(), true)
			state, _ := e.Store.Load()
			if r.restored != 1 || state.Operation != nil || state.RetryDisruptive != 1 || !strings.Contains(state.RetryCause, "earlier release is serving again") || !strings.Contains(state.RetryCause, "not applied") {
				t.Fatalf("not rolled back: %+v restored=%d", state, r.restored)
			}
			// Whatever else stopped the run, status must still say the earlier release is serving.
			if !strings.Contains(state.Detail, "earlier release is serving again") {
				t.Fatalf("status detail lost the rollback: %q", state.Detail)
			}
			// The generation compared is the operation's source, not what the failed target wrote.
			if got := r.gotSrc[0].Generation; got != source.Generation || got == failedGeneration {
				t.Fatalf("restored against generation %d, want the operation source's %d", got, source.Generation)
			}
		})
	}
	t.Run("a healthy target completes even after the deadline", func(t *testing.T) {
		e, r, d, now := deadlineEngine(t)
		state, _ := e.Store.Load()
		d.observed = r.healthy
		d.observed.Identity, d.observed.Image = state.Operation.Target.Identity, state.Operation.Target.Image
		d.observed.Generation = state.Operation.Source.Generation + 1
		*now = now.Add(10 * deadlineFixture)
		state, err := e.RunOnce(t.Context(), true)
		if err != nil || r.restored != 0 || state.Operation != nil || state.Phase != "healthy" {
			t.Fatalf("a completed target was rolled back: %+v %v restored=%d", state, err, r.restored)
		}
	})
	t.Run("an owner without a deadline stays pinned", func(t *testing.T) {
		e, r, _, now := deadlineEngine(t)
		e.OperationTimeout = 0
		*now = now.Add(100 * deadlineFixture)
		if state, err := e.RunOnce(t.Context(), true); err == nil || state.Operation == nil || r.restored != 0 {
			t.Fatalf("rolled back without a deadline: %+v %v", state, err)
		}
	})
	t.Run("a third rollback ends in retry_exhausted", func(t *testing.T) {
		e, r, d, now := deadlineEngine(t)
		for attempt := 1; ; attempt++ {
			*now = now.Add(deadlineFixture)
			state, err := e.RunOnce(t.Context(), true)
			if state.RetryDisruptive != attempt || r.restored != attempt || state.Operation != nil {
				t.Fatalf("rollback %d: %+v %v", attempt, state, err)
			}
			if attempt == maxDisruptiveAttempts {
				break
			}
			*now = now.Add(retryCap + time.Minute)
			d.applyFailure = true
			if _, err := e.RunOnce(t.Context(), true); err == nil {
				t.Fatalf("attempt %d did not pin", attempt+1)
			}
			d.applyFailure = false
			d.observed.Ready, d.observed.Phase, d.observed.Maintenance = false, "backup-preparing", true
			d.observed.Generation++
		}
		*now = now.Add(retryCap + time.Minute)
		state, err := e.RunOnce(t.Context(), true)
		if err != nil || state.Reason != "retry_exhausted" || !state.ProtectionGap() {
			t.Fatalf("not exhausted: %+v %v", state, err)
		}
	})
}

// An apply that fails after the target may have written leaves the operation pinned in this very
// run, so status must already carry the command a person needs and report a protection gap.
func TestSecurityFailedApplyNamesTheRecoveryCommand(t *testing.T) {
	e, _, d := fixtureEngine(t)
	enrollFixture(t, e)
	d.applyFailure = true
	state, err := e.RunOnce(t.Context(), true)
	if err == nil || state.Operation == nil || state.Reason != "apply_failed_recovery_required" {
		t.Fatalf("no pinned failure: %+v %v", state, err)
	}
	if want := reconcileAction(string(state.Operation.Target.Identity.ManifestSHA256)); state.Action != want || !state.ProtectionGap() {
		t.Fatalf("a pinned failure does not name the command or report a gap: action=%q gap=%t", state.Action, state.ProtectionGap())
	}
}

type provingDriver struct {
	*deadlineDriver
	proven bool
}

func (d *provingDriver) NoFinalGate(context.Context, Operation) (bool, error) { return d.proven, nil }

func TestSecurityDeadlineNeverStopsATargetPastTheFinalGate(t *testing.T) {
	t.Run("healthy at another release but not ready", func(t *testing.T) {
		e, r, d, now := deadlineEngine(t)
		state, _ := e.Store.Load()
		d.observed.Identity, d.observed.Phase, d.observed.Maintenance, d.observed.Ready = state.Operation.Target.Identity, "healthy", false, false
		*now = now.Add(deadlineFixture)
		state, err := e.RunOnce(t.Context(), true)
		if err == nil || r.restored != 0 || state.Operation == nil || state.Action == "" || !state.ProtectionGap() || !errors.Is(err, ErrPastFinalGate) {
			t.Fatalf("a target past the final gate was stopped or the case is not reported: %+v %v restored=%d", state, err, r.restored)
		}
	})
	t.Run("unobservable and unproven", func(t *testing.T) {
		e, r, _, now := deadlineEngine(t)
		e.Driver = &provingDriver{deadlineDriver: r}
		r.observeFailure = errors.New("workload unavailable")
		*now = now.Add(deadlineFixture)
		for name, apply := range map[string]func(){"policy": func() {}, "manual": func() { e.Policy.Mode = Manual }} {
			apply()
			state, err := e.RunOnce(t.Context(), true)
			if err == nil || r.restored != 0 || state.Operation == nil || state.Action == "" || !state.ProtectionGap() {
				t.Fatalf("%s: an unproven target was stopped or the case is not reported: %+v %v", name, state, err)
			}
		}
	})
	t.Run("unobservable but proven short of the gate", func(t *testing.T) {
		e, r, _, now := deadlineEngine(t)
		e.Driver = &provingDriver{deadlineDriver: r, proven: true}
		r.observeFailure = errors.New("workload unavailable")
		*now = now.Add(deadlineFixture)
		if state, _ := e.RunOnce(t.Context(), true); r.restored != 1 || state.Operation != nil {
			t.Fatalf("a proven target was not rolled back: %+v", state)
		}
	})
	t.Run("a source still serving is not stopped", func(t *testing.T) {
		e, r, d, now := deadlineEngine(t)
		d.observed = r.healthy
		*now = now.Add(deadlineFixture)
		if _, _ = e.RunOnce(t.Context(), true); r.restored != 0 {
			t.Fatal("a healthy untouched source was stopped by the deadline")
		}
	})
}

func TestSecurityRestoreThatStoppedNothingIsNotADisruption(t *testing.T) {
	e, r, _, now := deadlineEngine(t)
	e.Driver = &untouchedRestorer{deadlineDriver: r}
	*now = now.Add(deadlineFixture)
	state, _ := e.RunOnce(t.Context(), true)
	if state.Operation != nil || state.RetryDisruptive != 0 || state.RetryAttempts != 1 {
		t.Fatalf("a restore that stopped nothing counted as an outage: %+v", state)
	}
}

type untouchedRestorer struct{ *deadlineDriver }

func (d *untouchedRestorer) RestoreAborted(context.Context, Observation, Operation) error {
	d.observed = d.healthy
	return ErrNotStarted
}

// rollingBackDriver restores the source the way a post-write rollback does, and can be read again afterwards.
type rollingBackDriver struct{ *deadlineDriver }

func (d *rollingBackDriver) RestoreAborted(_ context.Context, aborted Observation, operation Operation) error {
	d.observeFailure = nil
	return errors.Join(d.deadlineDriver.RestoreAborted(context.Background(), aborted, operation), ErrRolledBack)
}

// A rollback that put the snapshot back is a failed run with its own reason, whatever stopped the update
// from continuing, and exits nonzero so a scheduled unit reports it.
func TestSecurityRollbackIsReportedAsOneWhateverElseStoppedTheRun(t *testing.T) {
	for name, prepare := range map[string]func(*Engine, *deadlineDriver){
		"deadline passed": func(*Engine, *deadlineDriver) {},
		"manual policy":   func(e *Engine, _ *deadlineDriver) { e.Policy.Mode = Manual },
		"stale feed": func(e *Engine, _ *deadlineDriver) {
			e.Feed.(*fixtureFeed).catalog.ExpiresAt = fixtureTime.Add(-time.Hour)
		},
		"unreachable feed":    func(e *Engine, _ *deadlineDriver) { f := e.Feed.(*fixtureFeed); f.failAt = f.calls + 1 },
		"unobservable target": func(_ *Engine, r *deadlineDriver) { r.observeFailure = errors.New("workload unavailable") },
	} {
		t.Run(name, func(t *testing.T) {
			e, r, _, now := deadlineEngine(t)
			e.Driver = &rollingBackDriver{r}
			*now = now.Add(deadlineFixture)
			prepare(e, r)
			state, err := e.RunOnce(t.Context(), true)
			if !errors.Is(err, ErrRolledBack) || state.Reason != "operation_rolled_back" || state.Operation != nil || state.RetryDisruptive != 1 {
				t.Fatalf("not reported as a rollback: %v %+v", err, state)
			}
			if !strings.HasPrefix(state.Detail, ReplacementUndone) || len(state.Detail) > maxDetailBytes {
				t.Fatalf("the status does not lead with the rollback: %q", state.Detail)
			}
		})
	}
	t.Run("the source is described by the verified catalog", func(t *testing.T) {
		e, r, _, now := deadlineEngine(t)
		e.Driver = &rollingBackDriver{r}
		r.observeFailure = errors.New("workload unavailable")
		*now = now.Add(deadlineFixture)
		state, err := e.RunOnce(t.Context(), true)
		if err == nil || state.Running == nil || len(state.Applicable) == 0 || !state.ProtectionGap() {
			t.Fatalf("status does not describe the restored source: %v %+v", err, state)
		}
	})
}

// A restore that is not a rollback keeps the reason of the run, and the target's refusal remains a refusal.
func TestSecurityRestoreThatWroteNothingIsNotReportedAsARollback(t *testing.T) {
	e, r, _, now := deadlineEngine(t)
	e.Policy.Mode = Manual
	*now = now.Add(deadlineFixture)
	state, err := e.RunOnce(t.Context(), true)
	if errors.Is(err, ErrRolledBack) || state.Reason != "manual_mode" || state.Operation != nil || r.restored != 1 {
		t.Fatalf("an abort restore was reported as a rollback: %v %+v", err, state)
	}
}

// The account of a rollback is what a truncated status must keep, not the cause that followed it.
func TestSecurityStatusDetailKeepsTheRollbackAccountWhenTheCauseIsLong(t *testing.T) {
	e, r, _, now := deadlineEngine(t)
	e.Driver = &rollingBackDriver{r}
	e.Feed.(*fixtureFeed).failAt = e.Feed.(*fixtureFeed).calls + 1
	e.Feed = longFailureFeed{e.Feed.(*fixtureFeed)}
	*now = now.Add(deadlineFixture)
	state, _ := e.RunOnce(t.Context(), true)
	if !strings.HasPrefix(state.Detail, ReplacementUndone) || len(state.Detail) != maxDetailBytes {
		t.Fatalf("the cause pushed out the account: %d %q", len(state.Detail), state.Detail[:min(len(state.Detail), 120)])
	}
}

type longFailureFeed struct{ *fixtureFeed }

func (longFailureFeed) Refresh(context.Context) (securityfeed.Document, error) {
	return securityfeed.Document{}, errors.New(strings.Repeat("publisher unavailable ", 200))
}

// A driver that cannot prove the gate never stops an unreadable target, on any path.
func TestSecurityUnreadableTargetIsNotStoppedByADriverThatProvesNothing(t *testing.T) {
	e, r, d, now := deadlineEngine(t)
	bare := &bareRestorer{fixtureDriver: d}
	e.Driver = bare
	d.observeFailure = errors.New("workload unavailable")
	*now = now.Add(deadlineFixture)
	state, err := e.RunOnce(t.Context(), true)
	if !errors.Is(err, ErrPastFinalGate) || bare.restored != 0 || r.restored != 0 || state.Operation == nil || state.Action == "" {
		t.Fatalf("an unprovable target was stopped: %v %+v restored=%d", err, state, bare.restored)
	}
}

// bareRestorer restores but implements no GateProver.
type bareRestorer struct {
	*fixtureDriver
	restored int
}

func (d *bareRestorer) RestoreAborted(context.Context, Observation, Operation) error {
	d.restored++
	return errors.Join(ErrNotStarted, ErrSourceRestored)
}

// A run that rolls back with the source observed again ends there: nothing is selected and nothing is
// applied in the same run, so a later-run rollback cannot be buried under a fresh apply that exits 0.
func TestSecurityRunThatRolledBackEndsBeforeSelectingAnything(t *testing.T) {
	e, r, d, now := deadlineEngine(t)
	e.Driver = &rollingBackDriver{r}
	applies := d.applies()
	// The next catalog fixes the source's own release, so a run that went on would report "current" and exit 0.
	e.Feed.(*fixtureFeed).change = func(_ int, c *Catalog) { c.Sequence++; c.Advisories[0].Affected[0].Fixed = "1.2.0" }
	*now = now.Add(deadlineFixture)
	state, err := e.RunOnce(t.Context(), true)
	if !errors.Is(err, ErrRolledBack) || state.Phase != "blocked" || state.Reason != "operation_rolled_back" || state.Operation != nil || state.Action != "" || d.applies() != applies || state.RetryDisruptive != 1 || state.RetryAttempts != 1 {
		t.Fatalf("the run went on after the rollback: %v %+v", err, state)
	}
}

// The source serves because an earlier rollback put it back, and only the release came late: the run
// counts that rollback, reports it, and does not call the source untouched.
func TestSecurityReleaseThatFollowsAnEarlierRollbackIsCountedAndReported(t *testing.T) {
	e, f, d := fixtureEngine(t)
	f.catalog.ExpiresAt = fixtureTime.Add(24 * time.Hour)
	r := &reconcilingDriver{fixtureDriver: d}
	e.Driver = r
	enrollFixture(t, e)
	now := fixtureTime
	e.Now = func() time.Time { return now }
	d.applyFailure = true
	if state, err := e.RunOnce(t.Context(), true); err == nil || state.Operation == nil {
		t.Fatalf("apply failure did not pin: %+v %v", state, err)
	}
	applies := d.applies()
	now = now.Add(operationSettle + time.Minute)
	r.refuse = ErrRolledBack
	state, err := e.RunOnce(t.Context(), true)
	if !errors.Is(err, ErrRolledBack) || state.Reason != "operation_rolled_back" || state.Operation != nil || state.RetryDisruptive != 1 || state.Action != "" || d.applies() != applies {
		t.Fatalf("a release after a rollback was not reported as one: %v %+v", err, state)
	}
	if !strings.HasPrefix(state.Detail, ReplacementUndone) {
		t.Fatalf("detail = %q", state.Detail)
	}
}

// The operator's own reconcile accepts the same outcome as a release.
func TestSecurityOperatorReconcileAcceptsAReleaseThatFollowsARollback(t *testing.T) {
	e, f, d := fixtureEngine(t)
	f.catalog.ExpiresAt = fixtureTime.Add(24 * time.Hour)
	r := &reconcilingDriver{fixtureDriver: d, refuse: ErrRolledBack}
	e.Driver = r
	enrollFixture(t, e)
	d.applyFailure = true
	state, err := e.RunOnce(t.Context(), true)
	if err == nil || state.Operation == nil {
		t.Fatalf("apply failure did not pin: %+v %v", state, err)
	}
	state, err = e.Reconcile(t.Context(), string(state.Operation.Target.Identity.ManifestSHA256))
	if err != nil || state.Operation != nil {
		t.Fatalf("reconcile refused a release that follows a rollback: %v %+v", err, state)
	}
}
