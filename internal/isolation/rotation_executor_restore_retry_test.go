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
