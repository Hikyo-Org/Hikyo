package isolation

import (
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// S2b-2 review round 5: owner decision B on restored in-flight runs. While a
// restore holds a rotation, no DROP ROLE is issued for a candidate that never
// published: the archive may say a run never published that the live instance
// did publish and consumers fetched. The role and the run are left alone and
// revisited after release-hold; the role is bounded by its VALID UNTIL.

func releaseRestoreHold(t *testing.T, db *store.DB, rotationID string) {
	t.Helper()
	execRaw(t, db, `UPDATE rotations SET restore_hold=FALSE WHERE id='`+rotationID+`'`)
}

// A run found minting or minted after a restore is not rolled back while the
// rotation is held; once the hold is released the normal rollback and drop happen.
func TestRotationHeldRotationDoesNotRecoverInFlightRuns(t *testing.T) {
	cases := []struct {
		name      string
		arm       func(x *rotExec)
		wantPhase string
	}{
		{"minting", func(x *rotExec) { x.crashOnce("create:after") }, "minting"},
		{"minted", func(x *rotExec) {
			x.exec.Revisions.PublishProbe = &rotPublishProbe{onBefore: func() {
				x.exec.Revisions.PublishProbe = nil
				panic(rotCrash{at: "publish"})
			}}
		}, "minted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				x := newRotExec(t, db)
				rot := x.create(rotPasswordKey, rotUsernameKey)
				x.advance(24*time.Hour + time.Minute)
				tc.arm(x)
				if !x.crashSweep("wrk_a") {
					t.Fatal("the worker did not reach the crash point")
				}
				run := x.openRunID(rot.ID)
				if got := x.phase(run); got != tc.wantPhase {
					t.Fatalf("phase after the crash = %s, want %s", got, tc.wantPhase)
				}
				holdRestored(t, db)

				x.advance(rotationLeaseTermForTest + time.Second)
				x.requireSweep("wrk_b", false)
				x.advance(time.Hour)
				x.requireSweep("wrk_b", false)
				if n := x.ctl.count("drop:"); n != 0 {
					t.Fatalf("%d DROP ROLE calls were issued for a held rotation's candidate", n)
				}
				if names := x.ctl.names(); len(names) != 1 {
					t.Fatalf("candidate roles = %v, want the one lingering role", names)
				}
				if got := x.phase(run); got != tc.wantPhase {
					t.Fatalf("a held rotation's run moved %s -> %s", tc.wantPhase, got)
				}

				releaseRestoreHold(t, db, rot.ID)
				x.requireSweep("wrk_b", true)
				if got := x.phase(run); got != "failed" {
					t.Fatalf("phase after the release = %s, want the normal rollback to failed", got)
				}
				if names := x.ctl.names(); len(names) != 0 {
					t.Fatalf("candidate roles left behind after the release: %v", names)
				}
				if x.effects("drop_candidate", "success") != 1 {
					t.Fatal("the candidate drop was not recorded after the release")
				}
			})
		})
	}
}

// A failed run whose candidate drop is still unconfirmed is not reconciled while
// the rotation is held; the reconciler drops it after the release.
func TestRotationHeldRotationDoesNotReconcileCandidates(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		x.ctl.set(func() { x.ctl.dropErr = dynamic.ErrUnreachable })
		x.crashOnce("create:after")
		if !x.crashSweep("wrk_a") {
			t.Fatal("the worker did not reach the crash point")
		}
		run := x.openRunID(rot.ID)
		x.advance(rotationLeaseTermForTest + time.Second)
		x.requireSweep("wrk_b", true)
		if got := x.phase(run); got != "failed" || len(x.ctl.names()) != 1 {
			t.Fatalf("setup: run is %s with roles %v, want failed with the unconfirmed candidate", got, x.ctl.names())
		}
		dropped := x.ctl.count("drop:")

		holdRestored(t, db)
		x.ctl.set(func() { x.ctl.dropErr = nil })
		x.advance(2 * time.Hour)
		x.requireSweep("wrk_b", false)
		if n := x.ctl.count("drop:"); n != dropped {
			t.Fatalf("%d more DROP ROLE calls were issued for a held rotation's candidate", n-dropped)
		}
		if len(x.ctl.names()) != 1 {
			t.Fatal("the candidate vanished while the rotation was held")
		}

		releaseRestoreHold(t, db, rot.ID)
		x.requireSweep("wrk_b", true)
		if names := x.ctl.names(); len(names) != 0 {
			t.Fatalf("candidate roles left behind after the release: %v", names)
		}
		if x.effects("drop_candidate", "success") != 1 {
			t.Fatal("the reconciled drop was not recorded after the release")
		}
	})
}

// A hold that lands after the run was claimed, while its publish is refused and
// rolls the run back, still issues no DROP ROLE: the rollback leaves the role to
// the reconciler, which waits for the release.
func TestRotationHoldDuringARunSkipsTheRollbackDrop(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		held := false
		x.ctl.set(func() {
			x.ctl.at = func(point, _ string) {
				if point == "create:after" && !held {
					held = true
					holdRestored(t, db)
				}
			}
		})
		x.advance(24*time.Hour + time.Minute)
		x.drain()
		if !held {
			t.Fatal("the hold never landed between the claim and the publish")
		}
		if n := x.ctl.count("drop:"); n != 0 {
			t.Fatalf("%d DROP ROLE calls were issued under a restore hold", n)
		}
		if len(x.ctl.names()) != 1 {
			t.Fatalf("candidate roles = %v, want the one lingering role", x.ctl.names())
		}
		if n := x.effects("drop_candidate", "open"); n != 0 {
			t.Fatalf("%d drop_candidate effects left open", n)
		}

		releaseRestoreHold(t, db, rot.ID)
		x.advance(time.Hour)
		x.drain()
		if x.effects("drop_candidate", "success") != 1 {
			t.Fatalf("the lingering candidate was not dropped after the release (phases %q)", x.runPhases(rot.ID))
		}
		if n := x.ctl.count("drop:"); n != 1 {
			t.Fatalf("%d DROP ROLE calls in all, want exactly the one after the release", n)
		}
	})
}
