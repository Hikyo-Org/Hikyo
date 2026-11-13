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
