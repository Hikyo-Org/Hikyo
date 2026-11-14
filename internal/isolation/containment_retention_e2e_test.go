package isolation

import (
	"context"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// O8 (incident-containment ADR): a contained owner still holds the personal
// value retention predicate. The clock is paused (cleared, not started), no
// value is deleted while contained, and a release restarts the clock fresh
// instead of expiring values retroactively.
func TestPersonalValueRetentionPausedWhileContained(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-contained")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		owner := e.admin.boot.PrincipalID
		clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
		sweep := &service.PersonalValues{DB: db, Keyring: e.values.Keyring, Auth: e.admin.auth, Now: func() time.Time { return clock }}
		run := func() {
			t.Helper()
			if err := sweep.SweepRetention(ctx); err != nil {
				t.Fatal(err)
			}
		}
		suspended := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM personal_values WHERE suspended_at IS NOT NULL`)
		}
		present := func() int64 { return queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) }
		contain := func() {
			t.Helper()
			if err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				return az.ContainPrincipal(ctx, owner, clock, "inc_retention")
			}); err != nil {
				t.Fatal(err)
			}
		}
		release := func() {
			t.Helper()
			if err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				return az.ReleasePrincipalContainment(ctx, owner)
			}); err != nil {
				t.Fatal(err)
			}
		}
		// Containment hides the grants from the chokepoint but removes none, so
		// the predicate is made to fail by deleting the reveal grant itself.
		grant := "g_cer_personal-contained_reveal"
		execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id='`+grant+`'`)
		execRaw(t, db, `DELETE FROM grants WHERE id='`+grant+`'`)
		window := service.PersonalRetentionWindow

		// A contained owner that would otherwise lose the predicate starts no clock.
		contain()
		run()
		if suspended() != 0 {
			t.Fatal("a contained owner's value was suspended")
		}
		clock = clock.Add(2 * window)
		run()
		if present() != 1 {
			t.Fatal("a value was deleted while its owner was contained")
		}

		// Release: the clock starts now, not retroactively.
		release()
		run()
		if suspended() != 1 || present() != 1 {
			t.Fatalf("release did not restart the clock: suspended=%d present=%d", suspended(), present())
		}
		clock = clock.Add(window - time.Second)

		// Containment again clears the running clock and deletes nothing.
		contain()
		run()
		if suspended() != 0 {
			t.Fatal("containment left a running clock")
		}
		clock = clock.Add(2 * window)
		run()
		if present() != 1 {
			t.Fatal("a value was deleted while its owner was contained a second time")
		}

		// A privacy-restricted owner keeps privacy's behavior even when also contained.
		execRaw(t, db, `UPDATE principals SET privacy_state = 'restricted' WHERE id = '`+string(owner)+`'`)
		run()
		if suspended() != 1 {
			t.Fatal("containment paused the clock of a privacy-restricted owner")
		}
		execRaw(t, db, `UPDATE principals SET privacy_state = 'active' WHERE id = '`+string(owner)+`'`)
		run()
		if suspended() != 0 {
			t.Fatal("a contained, privacy-active owner kept a running clock")
		}

		// Final release: a fresh full window must elapse before deletion.
		release()
		run()
		if suspended() != 1 || present() != 1 {
			t.Fatalf("second release did not restart the clock: suspended=%d present=%d", suspended(), present())
		}
		clock = clock.Add(window - time.Second)
		run()
		if present() != 1 {
			t.Fatal("deleted before a fresh window elapsed after release")
		}
		clock = clock.Add(time.Second)
		run()
		if present() != 0 {
			t.Fatal("not deleted once the fresh window elapsed")
		}
	})
}

// O8: a suspension recorded before containment must not survive a contain and
// release that no sweep observed. Release starts a fresh window, so a value
// whose owner lost access before containment is not deleted from the old clock.
func TestReleaseRestartsRetentionWindowWithoutAnIntermediateSweep(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-release-clock")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		owner := e.admin.boot.PrincipalID
		window := service.PersonalRetentionWindow
		clock := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
		sweep := &service.PersonalValues{DB: db, Keyring: e.values.Keyring, Auth: e.admin.auth, Now: func() time.Time { return clock }}
		run := func() {
			t.Helper()
			if err := sweep.SweepRetention(ctx); err != nil {
				t.Fatal(err)
			}
		}
		write := func(f func(context.Context, *authz.TxAuthorizer) error) {
			t.Helper()
			if err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error { return f(ctx, az) }); err != nil {
				t.Fatal(err)
			}
		}
		present := func() int64 { return queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) }
		suspended := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM personal_values WHERE suspended_at IS NOT NULL`)
		}

		// The owner loses the reveal grant, and the sweep starts the clock at t0.
		grant := "g_cer_personal-release-clock_reveal"
		execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id='`+grant+`'`)
		execRaw(t, db, `DELETE FROM grants WHERE id='`+grant+`'`)
		run()
		if suspended() != 1 {
			t.Fatalf("the sweep did not start the clock: suspended=%d", suspended())
		}

		// Contain and release with no sweep in between: release clears the old clock.
		clock = clock.Add(time.Hour)
		write(func(ctx context.Context, az *authz.TxAuthorizer) error {
			return az.ContainPrincipal(ctx, owner, clock, "inc_release_clock")
		})
		clock = clock.Add(time.Hour)
		write(func(ctx context.Context, az *authz.TxAuthorizer) error {
			return az.ReleasePrincipalContainment(ctx, owner)
		})
		if suspended() != 0 {
			t.Fatal("release left the pre-containment suspension in place")
		}

		// The old clock would have expired by now; the fresh one has not.
		clock = clock.Add(window - time.Hour)
		run()
		if present() != 1 || suspended() != 1 {
			t.Fatalf("the value was judged from the old clock: present=%d suspended=%d", present(), suspended())
		}
		clock = clock.Add(window)
		run()
		if present() != 0 {
			t.Fatal("not deleted once the fresh window elapsed")
		}
	})
}

// A privacy-restricted owner keeps privacy's running clock across contain and
// release: release restarts the window only for a principal privacy leaves
// active, so the stored suspension is unchanged and the value is deleted at the
// ORIGINAL deadline.
func TestReleaseKeepsTheClockOfAPrivacyRestrictedOwner(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-release-restricted")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		owner := e.admin.boot.PrincipalID
		window := service.PersonalRetentionWindow
		start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
		clock := start
		sweep := &service.PersonalValues{DB: db, Keyring: e.values.Keyring, Auth: e.admin.auth, Now: func() time.Time { return clock }}
		run := func() {
			t.Helper()
			if err := sweep.SweepRetention(ctx); err != nil {
				t.Fatal(err)
			}
		}
		suspendedAt := func() string {
			return queryString(t, db, `SELECT CAST(suspended_at AS TEXT) FROM personal_values`)
		}
		execRaw(t, db, `UPDATE principals SET privacy_state = 'restricted' WHERE id = '`+string(owner)+`'`)
		run()
		started := suspendedAt()

		// Contain and release well after the suspension began: a release that
		// replaced the timestamp would move the deadline.
		clock = start.Add(3 * 24 * time.Hour)
		for _, f := range []func(context.Context, *authz.TxAuthorizer) error{
			func(ctx context.Context, az *authz.TxAuthorizer) error {
				return az.ContainPrincipal(ctx, owner, clock, "inc_restricted")
			},
			func(ctx context.Context, az *authz.TxAuthorizer) error {
				return az.ReleasePrincipalContainment(ctx, owner)
			},
		} {
			if err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error { return f(ctx, az) }); err != nil {
				t.Fatal(err)
			}
		}
		if got := suspendedAt(); got != started {
			t.Fatalf("release changed a privacy-restricted owner's suspension: %q -> %q", started, got)
		}
		run()
		if got := suspendedAt(); got != started {
			t.Fatalf("the sweep after release changed the suspension: %q -> %q", started, got)
		}

		// The original deadline, not one counted from the release, deletes the value.
		clock = start.Add(window - time.Second)
		run()
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 1 {
			t.Fatal("deleted before the original deadline")
		}
		clock = start.Add(window)
		run()
		if queryInt(t, db, `SELECT COUNT(*) FROM personal_values`) != 0 {
			t.Fatal("not deleted at the original deadline")
		}
	})
}
