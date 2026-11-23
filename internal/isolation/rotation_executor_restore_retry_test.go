package isolation

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// restoreOver restores the first run's published revision over the live values, the
// way an operator rolling back does: the superseded value is served again.
func (x *rotExec) restoreOver(revision int64) {
	x.t.Helper()
	restored, err := x.restorer().Restore(tctx(x.t), x.actor, x.scope, revision, "")
	if err != nil || len(restored.Changes) == 0 {
		x.t.Fatalf("restore revision %d: %v (%d changes)", revision, err, len(restored.Changes))
	}
	var staged []string
	for _, c := range restored.Changes {
		staged = append(staged, c.VersionID)
	}
	if _, err := x.revisions().PublishPlanned(tctx(x.t), service.LocalPrincipal(alice), x.scope, service.PublishRequest{VersionIDs: staged, PreviewToken: restored.Preview.Token}); err != nil {
		x.t.Fatalf("publish the restore: %v", err)
	}
}

// unsetBoth unsets the rotation's two keys and publishes it: the keys serve nothing,
// so the restore assertion holds again.
func (x *rotExec) unsetBoth() {
	x.t.Helper()
	me := service.LocalPrincipal(alice)
	var staged []string
	for _, name := range []string{"ROT_DB_USER", "ROT_DB_PASSWORD"} {
		change, err := x.values().Unset(tctx(x.t), me, x.scope, name)
		if err != nil {
			x.t.Fatalf("unset %s: %v", name, err)
		}
		staged = append(staged, change.VersionID)
	}
	if _, err := x.revisions().PublishPlanned(tctx(x.t), me, x.scope, service.PublishRequest{VersionIDs: staged}); err != nil {
		x.t.Fatalf("publish the unset: %v", err)
	}
}

// failProvider makes every provider open fail until the returned func restores it.
func (x *rotExec) failProvider() (restore func()) {
	original := x.exec.ProviderFactory
	x.exec.ProviderFactory = func(dynamic.Kind, string, string, string) (dynamic.Provider, error) {
		return nil, errors.New("provider unreachable")
	}
	return func() { x.exec.ProviderFactory = original }
}

// A run held as published_versions_changed stays that way when the retry cannot open
// the provider: the restore refusal is a store read and a transient provider code
// must never replace it, because the keys may serve the superseded role again and the
// summary, the CLI and the browser read the code to say "do not drop".
func TestRotationRestoreRefusalSurvivesProviderOutageOnRetry(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		run1 := x.cycle(rot.ID)
		first := queryInt(t, db, "SELECT published_revision FROM rotation_runs WHERE id='"+run1+"'")
		role1 := x.lastRun(rot.ID, "candidate_handle")
		x.advance(24 * time.Hour)
		x.drain()
		run2 := x.openRunID(rot.ID)
		x.restoreOver(first)
		x.advance(time.Hour + time.Minute)
		x.drain()
		if got := x.phase(run2); got != "revoke_refused" || x.lastRun(rot.ID, "last_error") != "published_versions_changed" {
			t.Fatalf("the restore refusal left %s / %q", got, x.lastRun(rot.ID, "last_error"))
		}

		// The retry cannot reach the provider: the code must not become provider_unavailable.
		restore := x.failProvider()
		defer restore()
		x.advance(13 * time.Hour)
		x.drain()
		if got, code := x.phase(run2), x.lastRun(rot.ID, "last_error"); got != "revoke_refused" || code != "published_versions_changed" {
			t.Fatalf("a provider outage on the retry left %s / %q, want revoke_refused / published_versions_changed", got, code)
		}
		view := x.state(rot.ID)
		assertLastRunIsNewestListed(t, x.rotationFixture, rot.ID)
		if view.OwedDropRuns != 1 || view.OwedDropRunsVersionsChanged != 1 {
			t.Fatalf("summary owes %d (%d restore-refused), want 1 (1)", view.OwedDropRuns, view.OwedDropRunsVersionsChanged)
		}
		if !x.ctl.has(role1) || x.ctl.count("drop:"+role1) != 0 {
			t.Fatal("the superseded role was dropped while a restore serves it")
		}
	})
}

// A drop refused because the role owns objects, then a restore that serves the role
// again, then a retry that cannot reach the provider: the next visit must read the
// restore, not keep the plain refusal.
func TestRotationRestoreAfterOwnsObjectsRefusalIsDetectedOnRetry(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		run1 := x.cycle(rot.ID)
		first := queryInt(t, db, "SELECT published_revision FROM rotation_runs WHERE id='"+run1+"'")
		x.advance(24 * time.Hour)
		x.drain()
		run2 := x.openRunID(rot.ID)
		x.ctl.set(func() { x.ctl.dropErr = dynamic.ErrRoleOwnsObjects })
		x.advance(time.Hour + time.Minute)
		x.drain()
		if got, code := x.phase(run2), x.lastRun(rot.ID, "last_error"); got != "revoke_refused" || code != "role_owns_objects" {
			t.Fatalf("an owning role left %s / %q, want revoke_refused / role_owns_objects", got, code)
		}
		x.restoreOver(first)
		restore := x.failProvider()
		defer restore()
		x.advance(13 * time.Hour)
		x.drain()
		if got, code := x.phase(run2), x.lastRun(rot.ID, "last_error"); got != "revoke_refused" || code != "published_versions_changed" {
			t.Fatalf("a restore after the refusal left %s / %q, want revoke_refused / published_versions_changed", got, code)
		}
		if view := x.state(rot.ID); view.OwedDropRunsVersionsChanged != 1 {
			t.Fatalf("summary counts %d restore-refused runs, want 1", view.OwedDropRunsVersionsChanged)
		}
	})
}

// refusedAfterRestore drives a rotation to a second run whose revoke a restore refused:
// revoke_refused / published_versions_changed with the superseded role alive.
func (x *rotExec) refusedAfterRestore() (rotationID, run2, role1 string, firstRevision int64) {
	return x.refusedAfterRestoreEvery(24 * time.Hour)
}

// refusedAfterRestoreEvery is refusedAfterRestore for a rotation with the given
// interval (and a one hour revocation delay).
func (x *rotExec) refusedAfterRestoreEvery(interval time.Duration) (rotationID, run2, role1 string, firstRevision int64) {
	x.t.Helper()
	req := x.request(rotPasswordKey, rotUsernameKey)
	req.IntervalSeconds = int64(interval / time.Second)
	rot, err := x.svc.Create(tctx(x.t), x.actor, x.scope, req)
	if err != nil {
		x.t.Fatalf("create rotation: %v", err)
	}
	run1 := x.cycle(rot.ID)
	firstRevision = queryInt(x.t, x.db, "SELECT published_revision FROM rotation_runs WHERE id='"+run1+"'")
	role1 = x.lastRun(rot.ID, "candidate_handle")
	next := mustParseStamp(x.t, x.state(rot.ID).NextRunAt)
	if d := next.Sub(x.now()); d > 0 {
		x.advance(d + time.Minute)
	}
	x.drain()
	run2 = x.openRunID(rot.ID)
	x.restoreOver(firstRevision)
	x.advance(time.Hour + time.Minute)
	x.drain()
	if got := x.phase(run2); got != "revoke_refused" || x.lastRun(rot.ID, "last_error") != "published_versions_changed" {
		x.t.Fatalf("the restore refusal left %s / %q", got, x.lastRun(rot.ID, "last_error"))
	}
	return rot.ID, run2, role1, firstRevision
}

// The real outage is not an unopenable provider (the provider connects per call): it
// is an ExtendRole that fails on a run whose delivered credential is due. The extend
// step settles the run before the revoke step re-reads the restore assertion, and it
// must not replace published_versions_changed with extend_failed: the summary, the CLI
// and the browser read that code to say "do not drop", and the keys serve the role.
func TestRotationRestoreRefusalSurvivesExtendOutage(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		// A short interval puts the delivered credential's extension inside the
		// superseded role's own validity: it falls due while the role is still alive.
		rotID, run2, role1, _ := x.refusedAfterRestoreEvery(2 * time.Hour)
		x.ctl.set(func() { x.ctl.extendErr = dynamic.ErrUnreachable })
		for i := 0; i < 30 && x.phase(run2) == "revoke_refused"; i++ {
			x.advance(time.Hour)
			x.drain()
			if x.phase(run2) != "revoke_refused" {
				break // the superseded role expired: nothing about it is live
			}
			if code := x.lastRun(rotID, "last_error"); code != "published_versions_changed" {
				t.Fatalf("an extend outage (hour %d, %d ExtendRole calls) left revoke_refused / %q, want published_versions_changed", i, x.ctl.count("extend:"), code)
			}
			if view := x.state(rotID); view.OwedDropRuns != 1 || view.OwedDropRunsVersionsChanged != 1 {
				t.Fatalf("summary owes %d (%d restore-refused), want 1 (1)", view.OwedDropRuns, view.OwedDropRunsVersionsChanged)
			}
			if !x.ctl.has(role1) || x.ctl.count("drop:"+role1) != 0 {
				t.Fatal("the superseded role was dropped while a restore serves it")
			}
		}
		if x.ctl.count("extend:") == 0 {
			t.Fatal("ExtendRole was never called on the held run, so this test proved nothing")
		}
	})
}

// `delete --abandon` turns the refused run into a quarantine; the restore refusal is
// the one thing it must not erase, in the delete's own response and in the default list.
func TestRotationAbandonKeepsTheRestoreRefusal(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rotID, run2, _, _ := x.refusedAfterRestore()
		deleted, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rotID, Abandon: true})
		if err != nil {
			t.Fatalf("delete --abandon: %v", err)
		}
		if got, code := x.phase(run2), x.lastRun(rotID, "last_error"); got != "quarantine" || code != "published_versions_changed" {
			t.Fatalf("the abandon left %s / %q, want quarantine / published_versions_changed", got, code)
		}
		if deleted.OwedDropRuns != 1 || deleted.OwedDropRunsVersionsChanged != 1 {
			t.Fatalf("the delete response owes %d (%d restore-refused), want 1 (1)", deleted.OwedDropRuns, deleted.OwedDropRunsVersionsChanged)
		}
		listed, err := x.list(x.actor, x.scope, false)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, v := range listed {
			if v.ID == rotID {
				found = true
				if v.OwedDropRunsVersionsChanged != 1 {
					t.Fatalf("the default list shows %d restore-refused runs, want 1", v.OwedDropRunsVersionsChanged)
				}
			}
		}
		if !found {
			t.Fatal("the deleted rotation that owes a drop is missing from the default list")
		}
	})
}

// The sticky signal is not permanent: once the restore assertion holds again the
// revoke step re-reads it and replaces the code, on a refused run and on the
// quarantine an abandon leaves.
func TestRotationRestoreRefusalClearsOnceTheAssertionHoldsAgain(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) { restoreRefusalClears(t, db, false) })
}

func TestRotationRestoreRefusalClearsOnQuarantineOnceTheAssertionHoldsAgain(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) { restoreRefusalClears(t, db, true) })
}

func restoreRefusalClears(t *testing.T, db *store.DB, abandon bool) {
	name := "revoke_refused"
	if abandon {
		name = "quarantine"
	}
	x := newRotExec(t, db)
	rotID, run2, role1, _ := x.refusedAfterRestore()
	if abandon {
		if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rotID, Abandon: true}); err != nil {
			t.Fatal(err)
		}
	}
	// The restored values are unset again, so no key serves the superseded role, and
	// the provider is out.
	x.unsetBoth()
	restore := x.failProvider()
	x.advance(13 * time.Hour)
	x.drain()
	if got, code := x.phase(run2), x.lastRun(rotID, "last_error"); got != name || code != "provider_unavailable" {
		t.Fatalf("a held-again assertion on an unreachable provider left %s / %q, want %s / provider_unavailable", got, code, name)
	}
	if view := x.state(rotID); view.OwedDropRunsVersionsChanged != 0 {
		t.Fatalf("summary still counts %d restore-refused runs", view.OwedDropRunsVersionsChanged)
	}
	// The provider is back: the role is dropped and the run ends.
	restore()
	x.advance(13 * time.Hour)
	x.drain()
	if x.ctl.count("drop:"+role1) == 0 {
		t.Fatalf("the superseded role was never dropped once the restore was undone (phase %s)", x.phase(run2))
	}
}

// A read of the restore assertion that fails must not stop an expired superseded role
// from settling: nothing about it is live, so there is nothing a restore could refuse.
func TestRotationAssertionReadErrorDoesNotBlockAnExpiredRole(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rotID, run2, role1, _ := x.refusedAfterRestore()
		x.exec.RestoreAssertionFault = func() error { return errors.New("assertion read failed") }
		// While the role is live, a failed read blocks the revoke and keeps the signal.
		x.advance(13 * time.Hour)
		if _, err := x.sweep(x.worker); err == nil {
			t.Fatal("a failed assertion read on a live role did not stop the revoke")
		}
		if got, code := x.phase(run2), x.lastRun(rotID, "last_error"); got != "revoke_refused" || code != "published_versions_changed" {
			t.Fatalf("a failed read left %s / %q while the role is live", got, code)
		}
		// Past the superseded role's expiry the settle proceeds.
		x.advance(5 * 24 * time.Hour)
		x.ctl.set(func() { x.ctl.extendErr = nil; x.ctl.dropErr = dynamic.ErrUnreachable })
		for i := 0; i < 6; i++ {
			if _, err := x.sweep(x.worker); err != nil {
				t.Logf("sweep: %v", err)
			}
			x.advance(7 * time.Hour)
		}
		if got, code := x.phase(run2), x.lastRun(rotID, "last_error"); got != "done" || code != "expired" {
			t.Fatalf("an expired role did not settle behind a failed assertion read: %s / %q (role %s)", got, code, role1)
		}
	})
}
