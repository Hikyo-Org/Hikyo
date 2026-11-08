package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitydeployment"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
)

type delayedSecurityDeployment struct {
	observations []securitydeployment.Observation
	calls        int
}

func (d *delayedSecurityDeployment) Prepare(context.Context, securitydeployment.Target) (securitydeployment.Observation, error) {
	return securitydeployment.Observation{}, errors.New("unused staging")
}
func (d *delayedSecurityDeployment) Apply(context.Context, securitydeployment.Target) error {
	return errors.New("unused mutation")
}
func (d *delayedSecurityDeployment) Observe(context.Context) (securitydeployment.Observation, error) {
	index := d.calls
	d.calls++
	if index >= len(d.observations) {
		index = len(d.observations) - 1
	}
	return d.observations[index], nil
}

func TestSecurityDeploymentWaitsForActualNewProcessReadiness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.3.1", Sequence: 2, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compat")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))}
		source := securitymaintenance.Observation{Engine: releaseidentity.SQLite, Identity: identity, Instance: "example", Incarnation: strings.Repeat("b", 64), Generation: 4, Phase: "healthy", Ready: true}
		source.Identity.Version = "1.2.0"
		source.Identity.Sequence = 1
		target := securitymaintenance.Target{Identity: identity, Image: "ghcr.io/example/hikyo@sha256:" + strings.Repeat("c", 64), EligibilityUntil: time.Now().Add(500 * time.Millisecond)}
		old := securitydeployment.Observation{Engine: source.Engine, Identity: source.Identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: source.Generation, Phase: "healthy", Ready: true}
		starting := old
		starting.Identity = identity
		starting.Image = target.Image
		starting.Generation++
		starting.Phase = "schema-applied"
		starting.Maintenance = true
		starting.Ready = false
		healthy := starting
		healthy.Phase = "healthy"
		healthy.Maintenance = false
		healthy.Ready = true
		executor := &delayedSecurityDeployment{observations: []securitydeployment.Observation{old, starting, healthy}}
		driver := &securityDeploymentDriver{executor: executor, timeout: 10 * time.Second}
		if err := driver.waitForSecurityTarget(t.Context(), target, source); err != nil {
			t.Fatal(err)
		}
		if executor.calls != 3 {
			t.Fatalf("did not distinguish old ready/starting/target ready: %d", executor.calls)
		}
		if time.Now().Before(target.EligibilityUntil) {
			t.Fatal("fixture did not cross admission expiry during already-started operation")
		}
		// Metadata expiry after an admitted stop does not cancel the same
		// target's migration/readiness work or authorize an old-image restart.
	})
}

func TestSecurityDeploymentWaitRejectsRestoreAndNeverReadyTarget(t *testing.T) {
	for _, restore := range []bool{false, true} {
		t.Run(map[bool]string{false: "never ready", true: "restore"}[restore], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.3.1", Sequence: 2, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compat")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))}
				source := securitymaintenance.Observation{Engine: releaseidentity.SQLite, Identity: identity, Instance: "example", Incarnation: strings.Repeat("b", 64), Generation: 4, Phase: "healthy", Ready: true}
				observation := securitydeployment.Observation{Engine: source.Engine, Identity: identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: 5, Phase: "schema-applied", Maintenance: true}
				if restore {
					observation.RestoreEpoch = 1
				}
				executor := &delayedSecurityDeployment{observations: []securitydeployment.Observation{observation}}
				driver := &securityDeploymentDriver{executor: executor, timeout: 3 * time.Second}
				if err := driver.waitForSecurityTarget(t.Context(), securitymaintenance.Target{Identity: identity}, source); err == nil {
					t.Fatal("unproven running target accepted")
				}
				if restore && executor.calls != 1 {
					t.Fatal("restore incarnation was retried")
				}
			})
		})
	}
}

type restoringSecurityDeployment struct {
	delayedSecurityDeployment
	restoreErr error
	restored   int
	reconciled int
}

func (d *restoringSecurityDeployment) RestoreSource(context.Context, securitydeployment.Observation) error {
	d.restored++
	return d.restoreErr
}
func (d *restoringSecurityDeployment) Reconcile(context.Context, securitydeployment.Observation) error {
	d.reconciled++
	return nil
}

// The target image reports that it stopped before the database was written.
func TestSecurityDeploymentRestoresTheSourceOnlyAfterProvingItUntouched(t *testing.T) {
	identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.3.1", Sequence: 2, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compat")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))}
	source := securitymaintenance.Observation{Engine: releaseidentity.SQLite, Identity: identity, Instance: "example", Incarnation: strings.Repeat("b", 64), Generation: 4, Image: "ghcr.io/example/hikyo@sha256:" + strings.Repeat("d", 64), Phase: "healthy", Ready: true}
	source.Identity.Version, source.Identity.Sequence = "1.2.0", 1
	aborted := securitydeployment.Observation{Engine: source.Engine, Identity: source.Identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: source.Generation, Image: "ghcr.io/example/hikyo@sha256:" + strings.Repeat("c", 64), Phase: "backup-preparing", Maintenance: true, AbortedBeforeMutation: true}
	back := securitydeployment.Observation{Engine: source.Engine, Identity: source.Identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: source.Generation, Image: source.Image, Phase: "healthy", Ready: true}
	moved := back
	moved.Generation++
	target := securitymaintenance.Target{Identity: identity}
	for _, test := range []struct {
		name         string
		observations []securitydeployment.Observation
		restoreErr   error
		plain        bool
		restored     int
		reconciled   int
		released     bool
	}{
		{name: "source returns untouched", observations: []securitydeployment.Observation{aborted, back}, restored: 1, reconciled: 1, released: true},
		{name: "source never returns", observations: []securitydeployment.Observation{aborted}, restored: 1},
		{name: "source returns with a moved generation", observations: []securitydeployment.Observation{aborted, moved}, restored: 1},
		{name: "executor refuses to restore", observations: []securitydeployment.Observation{aborted, back}, restoreErr: securitydeployment.ErrConflict, restored: 1},
		{name: "adapter has no restore", observations: []securitydeployment.Observation{aborted}, restoreErr: securitydeployment.ErrUnsupported, restored: -1},
		{name: "executor without the optional method", observations: []securitydeployment.Observation{aborted}, plain: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				executor := &restoringSecurityDeployment{delayedSecurityDeployment: delayedSecurityDeployment{observations: test.observations}, restoreErr: test.restoreErr}
				var driver *securityDeploymentDriver
				if test.plain {
					driver = &securityDeploymentDriver{executor: &executor.delayedSecurityDeployment, timeout: 5 * time.Second}
				} else {
					driver = &securityDeploymentDriver{executor: executor, timeout: 5 * time.Second}
				}
				err := driver.waitForSecurityTarget(t.Context(), target, source)
				if err == nil || errors.Is(err, securitymaintenance.ErrNotStarted) != test.released {
					t.Fatalf("outcome %v, released=%t", err, test.released)
				}
				// A refusal as unsupported is asked again on every poll.
				if (executor.restored != test.restored && (test.restored >= 0 || executor.restored == 0)) || executor.reconciled != test.reconciled {
					t.Fatalf("restored %d reconciled %d", executor.restored, executor.reconciled)
				}
			})
		})
	}
}

// A later coordinator process has no prepared state, only the persisted operation.
func TestSecurityDeploymentRestoresAnAbortedSourceFromAPersistedOperation(t *testing.T) {
	identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.2.0", Sequence: 1, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compat")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))}
	image := "ghcr.io/example/hikyo@sha256:" + strings.Repeat("d", 64)
	source := securitymaintenance.Observation{Engine: releaseidentity.SQLite, Identity: identity, Instance: "example", Incarnation: strings.Repeat("b", 64), Generation: 4, Image: image, Phase: "healthy", Ready: true}
	aborted := securitydeployment.Observation{Engine: source.Engine, Identity: identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: source.Generation, Image: "ghcr.io/example/hikyo@sha256:" + strings.Repeat("c", 64), Phase: "backup-preparing", Maintenance: true, AbortedBeforeMutation: true}
	back := securitydeployment.Observation{Engine: source.Engine, Identity: identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: source.Generation, Image: image, Phase: "healthy", Ready: true}
	carried := maintenanceObservation(aborted)
	if !carried.AbortedBeforeMutation || carried.Ready {
		t.Fatalf("the target's report was dropped by Observe: %+v", carried)
	}
	for _, test := range []struct {
		name         string
		observations []securitydeployment.Observation
		restoreErr   error
		restored     bool
	}{
		{name: "source returns", observations: []securitydeployment.Observation{back}, restored: true},
		{name: "transient restore error", observations: []securitydeployment.Observation{back}, restoreErr: errors.New("API server unavailable")},
		{name: "source never returns", observations: []securitydeployment.Observation{aborted}},
		{name: "adapter without restore", observations: []securitydeployment.Observation{back}, restoreErr: securitydeployment.ErrUnsupported},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				executor := &restoringSecurityDeployment{delayedSecurityDeployment: delayedSecurityDeployment{observations: test.observations}, restoreErr: test.restoreErr}
				// The policy guard wraps the driver in production.
				var driver securitymaintenance.AbortRestorer = policyBoundSecurityDriver{Driver: &securityDeploymentDriver{executor: executor, timeout: 5 * time.Second}}
				err := driver.RestoreAborted(t.Context(), carried, securitymaintenance.Operation{Source: source})
				if errors.Is(err, securitymaintenance.ErrNotStarted) != test.restored || (err == nil) {
					t.Fatalf("outcome %v, restored=%t", err, test.restored)
				}
				if test.restored && executor.reconciled != 1 {
					t.Fatalf("pin not released: %d", executor.reconciled)
				}
				if !test.restored && executor.reconciled != 0 {
					t.Fatal("pin released without an observed source")
				}
			})
		})
	}
}

// rollingSecurityDeployment is an owner whose rollback puts `back` in front of the observations.
type rollingSecurityDeployment struct {
	delayedSecurityDeployment
	back        securitydeployment.Observation
	rollbackErr error
	rolled      int
	reconciled  int
	liveContext bool
}

func (d *rollingSecurityDeployment) RollBack(ctx context.Context) (bool, error) {
	d.rolled++
	d.liveContext = ctx.Err() == nil
	if d.rollbackErr == nil {
		d.observations, d.calls = []securitydeployment.Observation{d.back}, 0
	}
	return d.rollbackErr == nil, d.rollbackErr
}
func (d *rollingSecurityDeployment) Reconcile(context.Context, securitydeployment.Observation) error {
	d.reconciled++
	return nil
}

// A target that wrote and cannot be proven, or never reports, is rolled back to the source at the generation seen before.
func TestSecurityDeploymentRollsBackAFailedTargetAndProvesTheSource(t *testing.T) {
	identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.3.1", Sequence: 2, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compat")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))}
	image := "ghcr.io/example/hikyo@sha256:" + strings.Repeat("d", 64)
	source := securitymaintenance.Observation{Engine: releaseidentity.SQLite, Identity: identity, Instance: "example", Incarnation: strings.Repeat("b", 64), Generation: 4, Image: image, Phase: "healthy", Ready: true}
	source.Identity.Version, source.Identity.Sequence = "1.2.0", 1
	target := securitymaintenance.Target{Identity: identity, Image: "ghcr.io/example/hikyo@sha256:" + strings.Repeat("c", 64)}
	back := securitydeployment.Observation{Engine: source.Engine, Identity: source.Identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: source.Generation, Image: image, Phase: "healthy", Ready: true}
	moved := back
	moved.Generation++
	running := securitydeployment.Observation{Engine: source.Engine, Identity: identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: 5, Image: target.Image, Phase: "schema-applied", Maintenance: true}
	required := running
	required.Phase = "restore-required"
	pastGate := running
	pastGate.Phase, pastGate.Maintenance, pastGate.Ready = "healthy", false, false
	for _, test := range []struct {
		name     string
		observed securitydeployment.Observation
		back     securitydeployment.Observation
		failWith error
		plain    bool
		released bool
		rolled   int
	}{
		{name: "target reports restore-required", observed: required, back: back, released: true, rolled: 1},
		{name: "deadline passes with the target never ready", observed: running, back: back, released: true, rolled: 1},
		{name: "source returns at another generation", observed: required, back: moved, rolled: 1},
		{name: "rollback fails", observed: required, back: back, failWith: errors.New("one-shot failed"), rolled: 1},
		{name: "owner without a rollback path keeps its pins", observed: required, back: back, plain: true},
		{name: "target past the final gate is never stopped", observed: pastGate, back: back},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				executor := &rollingSecurityDeployment{delayedSecurityDeployment: delayedSecurityDeployment{observations: []securitydeployment.Observation{test.observed}}, back: test.back, rollbackErr: test.failWith}
				driver := &securityDeploymentDriver{executor: executor, timeout: 5 * time.Second}
				if test.plain {
					driver = &securityDeploymentDriver{executor: &executor.delayedSecurityDeployment, timeout: 5 * time.Second}
				}
				err := driver.waitForSecurityTarget(t.Context(), target, source)
				if err == nil || errors.Is(err, securitymaintenance.ErrNotStarted) != test.released || errors.Is(err, securitymaintenance.ErrSourceRestored) != test.released || errors.Is(err, securitymaintenance.ErrRolledBack) != test.released {
					t.Fatalf("outcome %v, released=%t", err, test.released)
				}
				if executor.rolled != test.rolled || (test.rolled > 0 && !executor.liveContext) || (executor.reconciled == 1) != test.released {
					t.Fatalf("rolled=%d live=%t reconciled=%d", executor.rolled, executor.liveContext, executor.reconciled)
				}
			})
		})
	}
}

// scriptedSecurityDeployment answers each read from a script and is rolled back like a real owner: Observe fails
// while its context is spent, so a write-back window that was not renewed shows.
type scriptedSecurityDeployment struct {
	observe     func(call int) (securitydeployment.Observation, error)
	calls       int
	proven      bool
	proofErr    error
	proofs      int
	rolled      int
	restored    bool
	rollbackUse time.Duration
	back        securitydeployment.Observation
	reconciled  int
	hangObserve bool
	proofWindow error
}

func (d *scriptedSecurityDeployment) Prepare(context.Context, securitydeployment.Target) (securitydeployment.Observation, error) {
	return securitydeployment.Observation{}, errors.New("unused staging")
}
func (d *scriptedSecurityDeployment) Apply(context.Context, securitydeployment.Target) error {
	return errors.New("unused mutation")
}
func (d *scriptedSecurityDeployment) Observe(ctx context.Context) (securitydeployment.Observation, error) {
	if err := ctx.Err(); err != nil {
		return securitydeployment.Observation{}, err
	}
	d.calls++
	if d.rolled > 0 {
		return d.back, nil
	}
	if d.hangObserve {
		<-ctx.Done()
		return securitydeployment.Observation{}, ctx.Err()
	}
	return d.observe(d.calls)
}
func (d *scriptedSecurityDeployment) NoFinalGate(ctx context.Context, _ securitydeployment.Observation) (bool, error) {
	d.proofs++
	d.proofWindow = ctx.Err()
	return d.proven, d.proofErr
}
func (d *scriptedSecurityDeployment) RollBack(ctx context.Context) (bool, error) {
	d.rolled++
	// The one-shot may use the whole window the caller gave it.
	select {
	case <-ctx.Done():
	case <-time.After(d.rollbackUse):
	}
	return d.restored, nil
}
func (d *scriptedSecurityDeployment) Reconcile(context.Context, securitydeployment.Observation) error {
	d.reconciled++
	return nil
}

func deadlineFixtureIdentities() (securitymaintenance.Observation, securitymaintenance.Target, securitydeployment.Observation, securitydeployment.Observation) {
	identity := releaseidentity.Identity{Profile: releaseidentity.StableV1, Version: "1.3.1", Sequence: 2, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compat")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))}
	image := "ghcr.io/example/hikyo@sha256:" + strings.Repeat("d", 64)
	source := securitymaintenance.Observation{Engine: releaseidentity.SQLite, Identity: identity, Instance: "example", Incarnation: strings.Repeat("b", 64), Generation: 4, Image: image, Phase: "healthy", Ready: true}
	source.Identity.Version, source.Identity.Sequence = "1.2.0", 1
	target := securitymaintenance.Target{Identity: identity, Image: "ghcr.io/example/hikyo@sha256:" + strings.Repeat("c", 64)}
	back := securitydeployment.Observation{Engine: source.Engine, Identity: source.Identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: source.Generation, Image: image, Phase: "healthy", Ready: true}
	running := securitydeployment.Observation{Engine: source.Engine, Identity: identity, Instance: source.Instance, Incarnation: source.Incarnation, Generation: 5, Image: target.Image, Phase: "schema-applied", Maintenance: true}
	return source, target, back, running
}

// The deadline never stops a target it has not read, whatever the last read of the wait said.
func TestSecurityDeploymentDeadlineReadsTheTargetAgainBeforeStoppingIt(t *testing.T) {
	source, target, back, running := deadlineFixtureIdentities()
	pastGate := running
	pastGate.Phase, pastGate.Maintenance = "healthy", false
	for _, test := range []struct {
		name     string
		observe  func(int) (securitydeployment.Observation, error)
		proven   bool
		proofErr error
		pastGate bool
		rolled   int
		proofs   int
	}{
		{name: "still short of the gate", observe: func(int) (securitydeployment.Observation, error) { return running, nil }, rolled: 1},
		{name: "crossed the gate after the last read of the wait", observe: func(call int) (securitydeployment.Observation, error) {
			if call < 6 {
				return running, nil
			}
			return pastGate, nil
		}, pastGate: true},
		{name: "unreadable and proven short of the gate", observe: func(int) (securitydeployment.Observation, error) {
			return securitydeployment.Observation{}, errors.New("unavailable")
		}, proven: true, rolled: 1, proofs: 1},
		{name: "unreadable and unproven", observe: func(int) (securitydeployment.Observation, error) {
			return securitydeployment.Observation{}, errors.New("unavailable")
		}, pastGate: true, proofs: 1},
		{name: "unreadable and the proof fails", observe: func(int) (securitydeployment.Observation, error) {
			return securitydeployment.Observation{}, errors.New("unavailable")
		}, proofErr: errors.New("row unreadable"), pastGate: true, proofs: 1},
		{name: "unreadable on an owner with no rollback path", observe: func(int) (securitydeployment.Observation, error) {
			return securitydeployment.Observation{}, errors.New("unavailable")
		}, proofErr: securitydeployment.ErrUnsupported, proofs: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				executor := &scriptedSecurityDeployment{observe: test.observe, proven: test.proven, proofErr: test.proofErr, restored: true, back: back}
				driver := &securityDeploymentDriver{executor: executor, timeout: 5 * time.Second}
				err := driver.waitForSecurityTarget(t.Context(), target, source)
				if err == nil || errors.Is(err, securitymaintenance.ErrPastFinalGate) != test.pastGate || executor.rolled != test.rolled || executor.proofs != test.proofs {
					t.Fatalf("outcome %v rolled=%d proofs=%d", err, executor.rolled, executor.proofs)
				}
				if test.rolled == 1 && (!errors.Is(err, securitymaintenance.ErrRolledBack) || !errors.Is(err, securitymaintenance.ErrNotStarted)) {
					t.Fatalf("a rollback was not reported as one: %v", err)
				}
			})
		})
	}
}

func TestSecurityDeploymentDeadlineDoesNotStopAnotherInstallation(t *testing.T) {
	source, _, back, running := deadlineFixtureIdentities()
	running.Instance = "ins_another"
	executor := &scriptedSecurityDeployment{observe: func(int) (securitydeployment.Observation, error) { return running, nil }, restored: true, back: back}
	driver := &securityDeploymentDriver{executor: executor, timeout: 5 * time.Second}
	err := driver.rollBackAtDeadline(t.Context(), source, errors.New("timed out"))
	if !errors.Is(err, securitymaintenance.ErrPastFinalGate) || executor.rolled != 0 {
		t.Fatalf("outcome %v rolled=%d", err, executor.rolled)
	}
}

// The one-shot may use the whole window and the source must still be proven serving afterwards.
func TestSecurityDeploymentProvesTheSourceOnAFreshWindowAfterTheOneShot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source, target, back, running := deadlineFixtureIdentities()
		executor := &scriptedSecurityDeployment{observe: func(int) (securitydeployment.Observation, error) { return running, nil }, restored: true, rollbackUse: time.Hour, back: back}
		driver := &securityDeploymentDriver{executor: executor, timeout: 5 * time.Second}
		err := driver.waitForSecurityTarget(t.Context(), target, source)
		if !errors.Is(err, securitymaintenance.ErrNotStarted) || executor.reconciled != 1 {
			t.Fatalf("the source was not proven after a one-shot that used its window: %v reconciled=%d", err, executor.reconciled)
		}
	})
}

// A rollback that restored nothing, because the target had written nothing, is not reported as one.
func TestSecurityDeploymentRollbackThatRestoredNothingIsNotReportedAsARestore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source, target, back, running := deadlineFixtureIdentities()
		executor := &scriptedSecurityDeployment{observe: func(int) (securitydeployment.Observation, error) { return running, nil }, restored: false, back: back}
		driver := &securityDeploymentDriver{executor: executor, timeout: 5 * time.Second}
		err := driver.waitForSecurityTarget(t.Context(), target, source)
		if !errors.Is(err, securitymaintenance.ErrNotStarted) || !errors.Is(err, securitymaintenance.ErrSourceRestored) || errors.Is(err, securitymaintenance.ErrRolledBack) {
			t.Fatalf("outcome %v", err)
		}
	})
}

type provingExecutor struct {
	scriptedSecurityDeployment
	got securitydeployment.Observation
}

func (e *provingExecutor) NoFinalGate(_ context.Context, source securitydeployment.Observation) (bool, error) {
	e.got = source
	return e.proven, e.proofErr
}

type provingTestDriver struct {
	policyTestSecurityDriver
	proven bool
}

func (d *provingTestDriver) NoFinalGate(context.Context, securitymaintenance.Operation) (bool, error) {
	return d.proven, nil
}

// An owner that cannot read the row proves nothing: the gate fails closed at both layers.
func TestSecurityGateProofFailsClosedWithoutAProver(t *testing.T) {
	source, _, _, _ := deadlineFixtureIdentities()
	operation := securitymaintenance.Operation{Source: source}
	t.Run("deployment driver", func(t *testing.T) {
		if proven, err := (&securityDeploymentDriver{executor: &delayedSecurityDeployment{}}).NoFinalGate(t.Context(), operation); proven || !errors.Is(err, securitydeployment.ErrUnsupported) {
			t.Fatalf("an executor that cannot read the row proved the gate: %t %v", proven, err)
		}
		executor := &provingExecutor{}
		executor.proven = true
		if proven, err := (&securityDeploymentDriver{executor: executor, timeout: time.Second}).NoFinalGate(t.Context(), operation); err != nil || !proven || executor.got != deploymentObservation(source) {
			t.Fatalf("the executor's proof was not forwarded with the source: %t %v %+v", proven, err, executor.got)
		}
		executor.proven, executor.proofErr = false, errors.New("unreadable")
		if proven, err := (&securityDeploymentDriver{executor: executor, timeout: time.Second}).NoFinalGate(t.Context(), operation); err == nil || proven {
			t.Fatalf("a failed proof was accepted: %t %v", proven, err)
		}
	})
	t.Run("policy guard", func(t *testing.T) {
		if proven, err := (policyBoundSecurityDriver{Driver: &policyTestSecurityDriver{}}).NoFinalGate(t.Context(), operation); proven || err == nil {
			t.Fatalf("a driver with no prover proved the gate: %t %v", proven, err)
		}
		for _, want := range []bool{true, false} {
			if proven, err := (policyBoundSecurityDriver{Driver: &provingTestDriver{proven: want}}).NoFinalGate(t.Context(), operation); err != nil || proven != want {
				t.Fatalf("the prover's answer %t was not forwarded: %t %v", want, proven, err)
			}
		}
	})
}

// A read that hangs spends its own window, and the proof that follows must not inherit it.
func TestSecurityDeploymentDeadlineGivesTheProofItsOwnWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source, _, back, _ := deadlineFixtureIdentities()
		executor := &scriptedSecurityDeployment{hangObserve: true, proven: true, restored: true, back: back}
		driver := &securityDeploymentDriver{executor: executor, timeout: 5 * time.Second}
		err := driver.rollBackAtDeadline(t.Context(), source, errors.New("timed out"))
		if executor.proofs != 1 || executor.proofWindow != nil || executor.rolled != 1 || !errors.Is(err, securitymaintenance.ErrRolledBack) {
			t.Fatalf("proofs=%d window=%v rolled=%d outcome=%v", executor.proofs, executor.proofWindow, executor.rolled, err)
		}
	})
}
