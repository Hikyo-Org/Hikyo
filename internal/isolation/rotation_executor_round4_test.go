package isolation

import (
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Review fixes on the scheduled rotation executor (#148, S2a, PR review): the
// publication clock, the budget check, the superseded-dead skew, the extend
// intent, the abandon lease and the org lease cap.

// The overlap deadline is the publication time plus the revocation delay, where
// the publication time is read after the project lock is held, not when the
// publish transaction began.
func TestRotationOverlapDeadlineFollowsPublicationTime(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		req := x.request(rotPasswordKey, rotUsernameKey)
		req.RevocationDelaySeconds = 300
		rot, err := x.svc.Create(tctx(t), x.actor, x.scope, req)
		if err != nil {
			t.Fatal(err)
		}
		x.advance(24*time.Hour + time.Minute)
		// The lock wait: 90 seconds pass between the transaction's start and the
		// lock. The run's two-minute lease bounds the wait, so the deadline anchor
		// is what a longer wait would otherwise move.
		x.exec.Revisions.PublishProbe = &rotPublishProbe{onBefore: func() {
			x.exec.Revisions.PublishProbe = nil
			x.advance(90 * time.Second)
		}}
		x.drain()
		run := x.openRunID(rot.ID)
		if got := x.phase(run); got != "overlap" {
			t.Fatalf("run = %s (%s), want overlap", got, x.lastRun(rot.ID, "last_error"))
		}
		published := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "published_at")+" FROM snapshots WHERE environment_id='"+string(x.env)+"' AND revision="+x.lastRun(rot.ID, "published_revision")))
		next := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "next_attempt_at")+" FROM rotation_runs WHERE id='"+run+"'"))
		if want := published.Add(5 * time.Minute); !next.Equal(want) {
			t.Fatalf("overlap deadline = %s, want publication %s + 5m = %s", next, published, want)
		}
		// The clock stood still after the lock was won, so the commit happened at
		// x.now(): the superseded role must stay valid a full delay after it.
		if min := x.now().Add(5 * time.Minute); next.Before(min) {
			t.Fatalf("overlap deadline %s is before the commit time %s + the revocation delay", next, x.now())
		}
		if x.ctl.count("drop:") != 0 {
			t.Fatal("the superseded role was revoked before its overlap")
		}
	})
}

// A publish whose budget runs out while it waits for the project lock rolls back
// instead of publishing a credential the schedule contract no longer allows. The
// run's own lease would normally abort such a wait first (it is two minutes, the
// budget an hour), so the test extends the lease to reach the budget check.
func TestRotationPublishBudgetRechecksAfterTheProjectLock(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		x.ctl.set(func() {
			x.ctl.at = func(p, _ string) {
				if p == "create:after" {
					x.ctl.set(func() { x.ctl.at = nil })
					execRaw(t, db, "UPDATE rotation_runs SET lease_expires_at='2099-01-01T00:00:00.000000Z' WHERE rotation_id='"+rot.ID+"'")
				}
			}
		})
		x.exec.Revisions.PublishProbe = &rotPublishProbe{onBefore: func() {
			x.exec.Revisions.PublishProbe = nil
			x.advance(61 * time.Minute)
		}}
		x.requireSweep(x.worker, true)
		if got := x.runPhases(rot.ID); got != "failed," {
			t.Fatalf("phases = %q, want failed: an over-budget publish must roll back", got)
		}
		if x.snapshots() != 0 {
			t.Fatalf("an over-budget publish left %d revisions", x.snapshots())
		}
		if names := x.ctl.names(); len(names) != 0 {
			t.Fatalf("the rolled-back candidate was not dropped: %v", names)
		}
	})
}

// stampText selects a timestamp column as an RFC 3339 string on either engine
// (PostgreSQL stores these as timestamptz).
func stampText(db *store.DB, column string) string {
	if db.Engine() == store.EnginePostgres {
		return "to_char(" + column + ` AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')`
	}
	return column
}
