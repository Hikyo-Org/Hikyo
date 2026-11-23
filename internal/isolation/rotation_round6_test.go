package isolation

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// S2b-2 review round 6: a delete, with or without abandon, clears the restore
// hold when the state becomes disabled, and the abandon turns minting and minted
// runs into quarantine. The reconciler would then read "not held" and drop a
// candidate the live instance may have published. A held rotation therefore
// refuses delete and delete --abandon until a person releases the hold.

func auditRows(t *testing.T, db *store.DB) int64 {
	t.Helper()
	return queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events`)
}

func TestRotationDeleteRefusedWhileRestoreHeld(t *testing.T) {
	for _, abandon := range []bool{false, true} {
		name := "delete"
		if abandon {
			name = "abandon"
		}
		t.Run(name, func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				x := newRotExec(t, db)
				rot := x.create(rotPasswordKey, rotUsernameKey)
				x.advance(24*time.Hour + time.Minute)
				x.crashOnce("create:after")
				if !x.crashSweep("wrk_a") {
					t.Fatal("the worker did not reach the crash point")
				}
				run := x.openRunID(rot.ID)
				if got := x.phase(run); got != "minting" {
					t.Fatalf("setup: run is %s, want minting", got)
				}
				holdRestored(t, db)
				before := x.state(rot.ID)
				rows := auditRows(t, db)

				_, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID, Abandon: abandon})
				if !errors.Is(err, service.ErrRotationRestoreHeld) {
					t.Fatalf("delete (abandon=%v) while held = %v, want ErrRotationRestoreHeld", abandon, err)
				}
				after := x.state(rot.ID)
				if after.State != before.State || after.Generation != before.Generation || !after.RestoreHold {
					t.Fatalf("a refused delete changed the rotation: %+v -> %+v", before, after)
				}
				if got := x.phase(run); got != "minting" {
					t.Fatalf("a refused delete moved the run to %s", got)
				}
				if n := auditRows(t, db); n != rows {
					t.Fatalf("a refused delete wrote %d audit rows", n-rows)
				}
			})
		})
	}
}

// After the release the delete goes through as before, abandon included.
func TestRotationDeleteSucceedsAfterRelease(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		holdRestored(t, db)
		if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID}); !errors.Is(err, service.ErrRotationRestoreHeld) {
			t.Fatalf("delete while held = %v", err)
		}
		releaseRestoreHold(t, db, rot.ID)
		if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID}); err != nil {
			t.Fatalf("delete after the release: %v", err)
		}
		if got := x.state(rot.ID).State; got != store.RotationStateDisabled {
			t.Fatalf("state after delete = %s", got)
		}
	})
}

// Pause stays allowed while held: it neither clears the hold nor starts anything.
func TestRotationPauseAllowedWhileRestoreHeld(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		holdRestored(t, db)
		if _, err := x.svc.Pause(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID}); err != nil {
			t.Fatalf("pause while held: %v", err)
		}
		if st := x.state(rot.ID); st.State != store.RotationStatePaused || !st.RestoreHold {
			t.Fatalf("after pause: %+v, want paused and still held", st)
		}
	})
}

// A disabled row never keeps a hold: the delete clears it, and a later restore
// does not set one on a deleted rotation.
func TestRotationDisabledRowNeverKeepsAHold(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		holdRestored(t, db)
		releaseRestoreHold(t, db, rot.ID)
		if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID}); err != nil {
			t.Fatal(err)
		}
		holdRestored(t, db)
		if n := queryInt(t, db, `SELECT COUNT(*) FROM rotations WHERE state='disabled' AND restore_hold`); n != 0 {
			t.Fatalf("%d disabled rotations kept a hold", n)
		}
	})
}

// A hold that lands after the claim, between the executor's own reads, must
// still stop every candidate drop: each in-executor RestoreHeld read is the
// guard for its site, so a test lands the hold at the site and deleting the read
// turns it red. A run handed back this way keeps no lease: it is due again at
// once after the release, not after the lease term.

func landHoldAt(t *testing.T, x *rotExec, db *store.DB, site string) *bool {
	t.Helper()
	landed := false
	x.exec.BeforeHeldRead = func(s string) {
		if s == site && !landed {
			landed = true
			holdRestored(t, db)
		}
	}
	return &landed
}

func leaseHeld(t *testing.T, db *store.DB, run string) bool {
	t.Helper()
	return queryInt(t, db, `SELECT COUNT(*) FROM rotation_runs WHERE id='`+run+`' AND lease_owner IS NOT NULL`) == 1
}

func TestRotationHoldAfterTheClaimStopsRecovery(t *testing.T) {
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
				landed := landHoldAt(t, x, db, "recover")
				x.advance(rotationLeaseTermForTest + time.Second)
				x.requireSweep("wrk_b", true)
				if !*landed {
					t.Fatal("the hold never landed at the recovery read")
				}
				if n := x.ctl.count("drop:"); n != 0 {
					t.Fatalf("%d DROP ROLE calls after a hold landed post-claim", n)
				}
				if got := x.phase(run); got != tc.wantPhase || len(x.ctl.names()) != 1 {
					t.Fatalf("run is %s with roles %v, want it untouched", got, x.ctl.names())
				}
				if leaseHeld(t, db, run) {
					t.Fatal("the run kept its lease after being handed back")
				}

				releaseRestoreHold(t, db, rot.ID)
				x.advance(time.Second)
				x.requireSweep("wrk_c", true)
				if got := x.phase(run); got != "failed" || len(x.ctl.names()) != 0 {
					t.Fatalf("after the release: run %s, roles %v, want the normal rollback at once", got, x.ctl.names())
				}
				if n := x.ctl.count("drop:"); n != 1 {
					t.Fatalf("%d DROP ROLE calls in all, want the one after the release", n)
				}
			})
		})
	}
}

func TestRotationHoldAfterTheClaimStopsTheReconciler(t *testing.T) {
	for _, site := range []string{"reconcile:entry", "reconcile:predrop"} {
		t.Run(site, func(t *testing.T) {
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

				x.ctl.set(func() { x.ctl.dropErr = nil })
				landed := landHoldAt(t, x, db, site)
				x.advance(2 * time.Hour)
				x.requireSweep("wrk_b", true)
				if !*landed {
					t.Fatalf("the hold never landed at the %s read", site)
				}
				if n := x.ctl.count("drop:"); n != dropped {
					t.Fatalf("%d more DROP ROLE calls after a hold landed post-claim", n-dropped)
				}
				if len(x.ctl.names()) != 1 {
					t.Fatal("the candidate vanished while the rotation was held")
				}
				if n := x.effects("drop_candidate", "open"); n != 0 {
					t.Fatalf("%d drop_candidate effects left open", n)
				}
				if leaseHeld(t, db, run) {
					t.Fatal("the run kept its lease after being handed back")
				}

				releaseRestoreHold(t, db, rot.ID)
				x.advance(time.Second)
				x.requireSweep("wrk_b", true)
				if names := x.ctl.names(); len(names) != 0 {
					t.Fatalf("candidate roles left behind after the release: %v", names)
				}
			})
		})
	}
}

// A worker whose lease lapsed mid-CreateRole drops its orphan only after seeing
// the run rolled back; a hold landing before that drop stops it too.
func TestRotationHoldAfterTheClaimStopsTheOrphanDrop(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		landed := landHoldAt(t, x, db, "drop_orphan")
		dropped := -1
		x.ctl.set(func() {
			x.ctl.at = func(p, _ string) {
				if p != "create:after" {
					return
				}
				x.ctl.set(func() {
					x.ctl.at = nil
					x.ctl.dropErr = dynamic.ErrUnreachable
				})
				x.advance(rotationLeaseTermForTest + time.Second)
				if _, err := x.sweep("wrk_b"); err != nil {
					t.Errorf("takeover sweep: %v", err)
				}
				dropped = x.ctl.count("drop:")
			}
		})
		if _, err := x.sweep("wrk_a"); err != nil {
			t.Fatalf("the fence-losing worker: %v", err)
		}
		if !*landed || dropped < 1 {
			t.Fatalf("setup: hold landed %v, takeover drops %d", *landed, dropped)
		}
		if n := x.ctl.count("drop:"); n != dropped {
			t.Fatalf("the fence-losing worker issued %d DROP ROLE calls under a hold", n-dropped)
		}
	})
}
