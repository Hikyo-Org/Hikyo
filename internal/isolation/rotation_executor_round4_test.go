package isolation

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	sqlite "modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"

	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Review fixes on the scheduled rotation executor (#148, S2a, PR review): the
// publication clock, the budget check, the superseded-dead skew, the extend
// intent, the abandon lease and the org lease cap.

// The overlap deadline gives consumers the whole revocation delay after the new
// credential is committed: it is anchored to the clock at the end of the publish,
// not to the time the project lock was won or the transaction began.
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
		// lock. The run's two-minute lease bounds the wait.
		x.exec.Revisions.PublishProbe = &rotPublishProbe{onBefore: func() {
			x.exec.Revisions.PublishProbe = nil
			x.advance(90 * time.Second)
		}}
		x.drain()
		run := x.openRunID(rot.ID)
		if got := x.phase(run); got != "overlap" {
			t.Fatalf("run = %s (%s), want overlap", got, x.lastRun(rot.ID, "last_error"))
		}
		next := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "next_attempt_at")+" FROM rotation_runs WHERE id='"+run+"'"))
		// The clock stood still after the lock was won, so the commit happened at
		// x.now() and the superseded role must stay valid a full delay after it.
		if want := x.now().Add(5 * time.Minute); !next.Equal(want) {
			t.Fatalf("overlap deadline = %s, want the commit time %s + 5m = %s", next, x.now(), want)
		}
		if x.ctl.count("drop:") != 0 {
			t.Fatal("the superseded role was revoked before its overlap")
		}
	})
}

// Work that takes time after the lock was won (staging, materialization) is not
// taken out of the overlap: the deadline follows the end of the publish.
func TestRotationOverlapDeadlineCountsWorkAfterTheLockRefresh(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		req := x.request(rotPasswordKey, rotUsernameKey)
		req.RevocationDelaySeconds = 300
		rot, err := x.svc.Create(tctx(t), x.actor, x.scope, req)
		if err != nil {
			t.Fatal(err)
		}
		x.advance(24*time.Hour + time.Minute)
		x.exec.Revisions.PublishProbe = &rotPublishProbe{onBaseline: func() {
			x.exec.Revisions.PublishProbe = nil
			x.advance(45 * time.Second) // after the lock was won and the clock refreshed
		}}
		x.drain()
		run := x.openRunID(rot.ID)
		if got := x.phase(run); got != "overlap" {
			t.Fatalf("run = %s (%s), want overlap", got, x.lastRun(rot.ID, "last_error"))
		}
		published := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "published_at")+" FROM snapshots WHERE environment_id='"+string(x.env)+"' AND revision="+x.lastRun(rot.ID, "published_revision")))
		next := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "next_attempt_at")+" FROM rotation_runs WHERE id='"+run+"'"))
		if want := x.now().Add(5 * time.Minute); !next.Equal(want) {
			t.Fatalf("overlap deadline = %s, want the end of the publish %s + 5m = %s (stamped %s)", next, x.now(), want, published)
		}
		if !next.After(published.Add(5 * time.Minute)) {
			t.Fatalf("the deadline %s ignores the 45s of work after the lock refresh (publication %s)", next, published)
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

// The budget is checked again at the end of the publish, on a fresh clock: staging
// and materialization run after the post-lock read and can outlast the budget. The
// probe fires inside the transaction after the stage check and before the commit
// hook; an over-budget run rolls back, an in-budget run's overlap deadline is the
// commit time plus the revocation delay.
func TestRotationPublishBudgetRechecksAtCommit(t *testing.T) {
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
		// The lock was won in budget and the stage check passed; the clock then
		// jumps past the budget before the commit hook runs.
		x.exec.Revisions.PublishProbe = &rotPublishProbe{onBaseline: func() {
			x.exec.Revisions.PublishProbe = nil
			x.advance(61 * time.Minute)
		}}
		x.requireSweep(x.worker, true)
		if got := x.runPhases(rot.ID); got != "failed," {
			t.Fatalf("phases = %q, want failed: a publish over budget at its commit must roll back", got)
		}
		if x.snapshots() != 0 {
			t.Fatalf("an over-budget publish left %d revisions", x.snapshots())
		}
		if got := x.lastRun(rot.ID, "last_error"); got != "publish_budget_exceeded" {
			t.Fatalf("last error = %q, want publish_budget_exceeded", got)
		}
	})
}

// An in-budget run whose staging and materialization took half an hour after the
// lock stores an overlap deadline of the commit time plus the revocation delay. The
// run's two-minute lease lapsed during that work, so the settle after the commit
// loses its fence and the deadline is the one the commit hook stored: the
// fallback this test pins to the fresh clock read, not the post-lock one.
func TestRotationOverlapDeadlineIsCommitTimePlusDelayAfterLateWork(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		req := x.request(rotPasswordKey, rotUsernameKey)
		req.RevocationDelaySeconds = 300
		rot, err := x.svc.Create(tctx(t), x.actor, x.scope, req)
		if err != nil {
			t.Fatal(err)
		}
		x.advance(24*time.Hour + time.Minute)
		x.exec.Revisions.PublishProbe = &rotPublishProbe{onBaseline: func() {
			x.exec.Revisions.PublishProbe = nil
			x.advance(30 * time.Minute) // still inside the one-hour budget
		}}
		x.drain()
		run := x.openRunID(rot.ID)
		if got := x.phase(run); got != "published" {
			t.Fatalf("run = %s (%s), want published: the lapsed lease keeps the settle from running", got, x.lastRun(rot.ID, "last_error"))
		}
		next := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "next_attempt_at")+" FROM rotation_runs WHERE id='"+run+"'"))
		if want := x.now().Add(5 * time.Minute); !next.Equal(want) {
			t.Fatalf("overlap deadline = %s, want the commit time %s + 5m = %s", next, x.now(), want)
		}
	})
}

// The deadline the commit hook stores with `published` is read before the commit. A
// run whose settle into overlap never happened (here its lease lapsed during a slow
// commit) is recovered from that phase, and recovery must not revoke on the stored
// value: it establishes the overlap from its own, post-commit observation, so the
// superseded role stays valid commit time + RevocationDelaySeconds at least.
func TestRotationPublishedRecoveryWaitsAFullDelayAfterTheCommit(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		req := x.request(rotPasswordKey, rotUsernameKey)
		req.RevocationDelaySeconds = 300
		rot, err := x.svc.Create(tctx(t), x.actor, x.scope, req)
		if err != nil {
			t.Fatal(err)
		}
		x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		x.advance(24*time.Hour + time.Minute)
		// The first executor clock read after the baseline read is the commit hook's
		// deadline read; the clock then moves 20 minutes before the commit lands.
		armed := false
		x.exec.Now = func() time.Time {
			at := x.now()
			if armed {
				armed = false
				x.advance(20 * time.Minute)
			}
			return at
		}
		x.exec.Revisions.PublishProbe = &rotPublishProbe{onBaseline: func() {
			x.exec.Revisions.PublishProbe = nil
			armed = true
		}}
		x.drain()
		run2 := x.openRunID(rot.ID)
		commit := x.now() // the clock stopped when the commit landed
		if x.snapshots() != 2 {
			t.Fatalf("%d revisions, want 2: the second publish did not commit", x.snapshots())
		}
		if got := x.phase(run2); got != "overlap" {
			t.Fatalf("run 2 = %s (%s), want overlap established by the recovery", got, x.lastRun(rot.ID, "last_error"))
		}
		if n := x.ctl.count("drop:" + role1); n != 0 {
			t.Fatalf("the superseded role was dropped %d times before the delay after the commit", n)
		}
		next := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "next_attempt_at")+" FROM rotation_runs WHERE id='"+run2+"'"))
		if want := commit.Add(5 * time.Minute); !next.Equal(want) {
			t.Fatalf("overlap deadline = %s, want the recovery observation %s + 5m = %s", next, commit, want)
		}
		x.advance(5*time.Minute + time.Second)
		x.drain()
		if got := x.phase(run2); got != "done" || x.ctl.count("drop:"+role1) != 1 {
			t.Fatalf("run 2 = %s with %d drops after the delay, want done with 1", got, x.ctl.count("drop:"+role1))
		}
	})
}

// `rotation delete --abandon` after the publish committed but before the overlap
// settle leaves the run quarantined with the deadline stored before the commit
// unless the abandon replaces it: the revoke-obligation loop must wait the full
// delay after the commit, measured at the abandon (an observation after it).
func TestRotationAbandonOfAPublishedRunWaitsAFullDelayAfterTheCommit(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		req := x.request(rotPasswordKey, rotUsernameKey)
		req.RevocationDelaySeconds = 300
		req.IntervalSeconds = 3600 // keeps the fake clock inside the fixture session's 48 hours
		rot, err := x.svc.Create(tctx(t), x.actor, x.scope, req)
		if err != nil {
			t.Fatal(err)
		}
		x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		x.advance(time.Hour + time.Minute)
		// The clock moves three minutes between the hook's deadline read and the
		// commit: the lease (two minutes) lapses, so the settle into overlap is
		// skipped, and the stored deadline is two minutes short of commit + delay.
		armed := false
		x.exec.Now = func() time.Time {
			at := x.now()
			if armed {
				armed = false
				x.advance(3 * time.Minute)
			}
			return at
		}
		x.exec.Revisions.PublishProbe = &rotPublishProbe{onBaseline: func() {
			x.exec.Revisions.PublishProbe = nil
			armed = true
		}}
		x.drain()
		run2 := x.openRunID(rot.ID)
		if got := x.phase(run2); got != "published" {
			t.Fatalf("run 2 = %s (%s), want published: the settle must be skipped", got, x.lastRun(rot.ID, "last_error"))
		}
		commit := x.now()
		x.svc.Now = x.now // the abandon time must follow the executor's clock
		if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID, Abandon: true}); err != nil {
			t.Fatal(err)
		}
		if got := x.phase(run2); got != "quarantine" {
			t.Fatalf("run 2 = %s, want quarantine", got)
		}
		next := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "next_attempt_at")+" FROM rotation_runs WHERE id='"+run2+"'"))
		if want := commit.Add(5 * time.Minute); !next.Equal(want) {
			t.Fatalf("quarantine deadline = %s, want the abandon observation %s + 5m = %s", next, commit, want)
		}
		// Past the deadline stored before the commit, before commit + delay.
		x.advance(4 * time.Minute)
		x.drain()
		if n := x.ctl.count("drop:" + role1); n != 0 {
			t.Fatalf("the superseded role was dropped %d times before the delay after the commit", n)
		}
		x.advance(2 * time.Minute)
		x.drain()
		if n := x.ctl.count("drop:" + role1); n != 1 {
			t.Fatalf("%d drops of the superseded role after the delay, want 1", n)
		}
	})
}

// A recovery that comes late, once the delivered credential is due for an extension
// (inside its retry window), still runs the extend step first: the credential is
// extended before the overlap wait starts, instead of the wait quietly pushing the
// run toward the credential's expiry.
func TestRotationPublishedRecoveryStillRunsTheExtendStepFirst(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		req := x.request(rotPasswordKey, rotUsernameKey)
		req.RevocationDelaySeconds = 300
		rot, err := x.svc.Create(tctx(t), x.actor, x.scope, req)
		if err != nil {
			t.Fatal(err)
		}
		x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		x.advance(24*time.Hour + time.Minute)
		armed := false
		x.exec.Now = func() time.Time {
			at := x.now()
			if armed {
				armed = false
				x.advance(40 * time.Hour) // the executor is away until run 2 is due for an extension
			}
			return at
		}
		x.exec.Revisions.PublishProbe = &rotPublishProbe{onBaseline: func() {
			x.exec.Revisions.PublishProbe = nil
			armed = true
		}}
		x.drain()
		run2 := x.openRunID(rot.ID)
		if x.snapshots() != 2 {
			t.Fatalf("%d revisions, want 2: the second publish did not commit", x.snapshots())
		}
		role2 := x.lastRun(rot.ID, "candidate_handle")
		if x.ctl.count("extend:"+role2) != 1 {
			t.Fatalf("the late recovery did not extend the delivered credential first (%d extends, run %s)", x.ctl.count("extend:"+role2), x.phase(run2))
		}
		if got := x.phase(run2); got != "overlap" {
			t.Fatalf("run 2 = %s (%s), want overlap after the extend", got, x.lastRun(rot.ID, "last_error"))
		}
		if n := x.ctl.count("drop:" + role1); n != 0 {
			t.Fatalf("the superseded role was dropped %d times before the overlap wait", n)
		}
	})
}

// A replacement run that publishes AFTER the extend's supersession check is not
// seen by that check, so the extend proceeds; the replacement's revoke read must
// then count the extend as uncertain (its VALID UNTIL is only a lower bound) and
// must not settle the superseded role as expired. This holds only because the
// extend intent is recorded before the check: the hook fires right after it.
func TestRotationExtendIntentCoversAPublishAfterTheSupersessionCheck(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		run1 := x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		validUntil := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "candidate_valid_until")+" FROM rotation_runs WHERE id='"+run1+"'"))
		// Failing mints keep run 1 the newest published run until its credential is
		// due for an extension.
		x.ctl.set(func() { x.ctl.createErr = dynamic.ErrUnreachable })
		fired := false
		x.exec.AfterExtendSupersessionCheck = func() {
			x.exec.AfterExtendSupersessionCheck = nil
			fired = true
			openExtends := func() int64 {
				return queryInt(t, db, "SELECT COUNT(*) FROM rotation_effects WHERE run_id='"+run1+"' AND kind='extend' AND (outcome IS NULL OR outcome='unknown')")
			}
			if got := openExtends(); got != 1 {
				t.Fatalf("%d extend intents of run 1 are open after the supersession check, want 1", got)
			}
			// Run 1's lease stays with the extending worker for the whole scenario;
			// another worker takes over the rotation.
			execRaw(t, db, "UPDATE rotation_runs SET lease_expires_at='2099-01-01T00:00:00.000000Z' WHERE id='"+run1+"'")
			x.ctl.set(func() { x.ctl.createErr = nil })
			sweep := func() {
				if _, err := x.sweep("wrk_b"); err != nil {
					t.Fatalf("replacement sweep: %v", err)
				}
			}
			for i := 0; i < 60 && x.phase(x.openRunID(rot.ID)) != "overlap"; i++ {
				x.advance(5 * time.Minute)
				sweep()
			}
			run2 := x.openRunID(rot.ID)
			if run2 == run1 || x.phase(run2) != "overlap" {
				t.Fatalf("the replacement run = %s (%s), want a published run in overlap", x.phase(run2), x.lastRun(rot.ID, "last_error"))
			}
			// Past the superseded role's recorded VALID UNTIL plus the skew, and past
			// the overlap: if the extend were not counted, the role would read expired.
			deadline := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "next_attempt_at")+" FROM rotation_runs WHERE id='"+run2+"'"))
			target := validUntil.Add(10 * time.Minute)
			if deadline.Add(time.Minute).After(target) {
				target = deadline.Add(time.Minute)
			}
			x.advance(target.Sub(x.now()))
			x.ctl.set(func() { x.ctl.dropErr = dynamic.ErrRefused })
			for i := 0; i < 5; i++ {
				sweep()
			}
			if got := openExtends(); got != 1 {
				t.Fatalf("%d extend intents of run 1 are open at the replacement's revoke, want 1", got)
			}
			if x.ctl.count("drop:"+role1) == 0 {
				t.Fatal("the replacement's revoke never tried to drop the superseded role")
			}
			if got := x.phase(run2); got == "done" {
				t.Fatalf("the replacement settled the superseded role as %s (%s): its extend was open", got, x.lastRun(rot.ID, "last_error"))
			}
			if got := x.lastRun(rot.ID, "last_error"); got != "revoke_failed" {
				t.Fatalf("last error = %q, want revoke_failed: the role may still authenticate", got)
			}
			if got := queryInt(t, db, "SELECT COUNT(*) FROM rotation_effects WHERE run_id='"+run2+"' AND kind='revoke' AND outcome='success'"); got != 0 {
				t.Fatalf("%d revoke effects settled as success for a role with an uncertain extend", got)
			}
		}
		for i := 0; i < 90 && !fired; i++ {
			x.advance(30 * time.Minute)
			x.drain()
		}
		if !fired {
			t.Fatal("the extend never reached its supersession check")
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

// The restore hold lets go when the provider says the superseded role is dead, but
// only past the same skew margin the expiry exits use: with the application clock
// ahead of PostgreSQL, a role whose VALID UNTIL passed a minute ago may still log
// in, and dropping it would cut it off early.
func TestRotationSupersededDeadNeedsTheSkewMargin(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		x.advance(24 * time.Hour)
		x.drain()
		run2 := x.openRunID(rot.ID)
		me := service.LocalPrincipal(alice)
		values := &service.Values{DB: db, Keyring: probeKeyring(t, db)}
		var staged []string
		for name, value := range map[string]string{"ROT_DB_USER": "manual_user", "ROT_DB_PASSWORD": "manual-pw-value"} {
			change, err := values.Set(tctx(t), me, x.scope, name, value, nil)
			if err != nil {
				t.Fatalf("stage %s: %v", name, err)
			}
			staged = append(staged, change.VersionID)
		}
		if _, err := x.revisions().PublishPlanned(tctx(t), me, x.scope, service.PublishRequest{VersionIDs: staged}); err != nil {
			t.Fatalf("manual publish: %v", err)
		}
		x.advance(time.Hour + time.Minute)
		x.drain()
		if got := x.phase(run2); got != "revoke_refused" {
			t.Fatalf("phase with another value serving = %s, want revoke_refused", got)
		}
		validUntil := func(ago time.Duration) {
			x.ctl.set(func() {
				role := x.ctl.roles[role1]
				role.validUntil = x.now().Add(-ago)
				x.ctl.roles[role1] = role
			})
		}
		// The provider reports VALID UNTIL a minute in the past: inside the margin.
		x.advance(time.Hour)
		validUntil(time.Minute)
		x.drain()
		if got := x.phase(run2); got != "revoke_refused" || x.ctl.count("drop:"+role1) != 0 {
			t.Fatalf("a role one minute past VALID UNTIL was treated as dead (phase %s)", got)
		}
		// Past the margin, the role is dead and the hold lets go.
		x.advance(time.Hour)
		validUntil(rotationSkewForTest + time.Minute)
		x.drain()
		if got := x.phase(run2); got != "done" {
			t.Fatalf("a role past the skew margin kept the hold: phase %s", got)
		}
	})
}

const rotationSkewForTest = 5 * time.Minute

// An extend that ends because a later run published since closes its INTENT as a
// definite failure: no provider call was made, so the role is not left permanently
// "uncertain" for the later run's revoke read.
func TestRotationExtendSkippedBySupersessionClosesItsIntent(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		run1 := x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		x.cycle(rot.ID)
		// Run 1's credential falls due for an extension after run 2 replaced it.
		x.advance(40 * time.Hour)
		x.drain()
		if x.ctl.count("extend:"+role1) != 0 {
			t.Fatal("a superseded credential was extended")
		}
		if got := x.effects("extend", "open"); got != 0 {
			t.Fatalf("%d extend intents left open", got)
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM rotation_effects WHERE run_id='`+run1+`' AND kind='extend' AND outcome='failure'`); got != 1 {
			t.Fatal("the skipped extend did not record a closed intent")
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM rotation_effects WHERE run_id='`+run1+`' AND kind='extend' AND (outcome IS NULL OR outcome='unknown')`); got != 0 {
			t.Fatalf("run 1 is left with %d uncertain extends", got)
		}
	})
}

// An abandon cannot stop a provider call a worker already started, so the run of a
// worker that held its lease stays claimed until one provider deadline has passed:
// the candidate reconciler then probes after that call has landed, not before it.
func TestRotationAbandonDelaysTheCandidateReconcileByAProviderDeadline(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		x.crashOnce("create:after")
		if !x.crashSweep("wrk_a") {
			t.Fatal("no crash")
		}
		run := x.openRunID(rot.ID)
		x.svc.Now = x.now // the abandon time must follow the executor's clock
		if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID, Abandon: true}); err != nil {
			t.Fatal(err)
		}
		if got := x.phase(run); got != "quarantine" {
			t.Fatalf("run = %s, want quarantine", got)
		}
		// The run was held by wrk_a: no other worker may probe its candidate yet.
		x.requireSweep(x.worker, false)
		x.advance(6 * time.Second)
		x.drain()
		if got := x.phase(run); got != "failed" {
			t.Fatalf("run = %s after the provider deadline, want failed", got)
		}
	})
}

// The org lease cap is re-read inside the creation transaction: a start whose
// selection saw room is refused once the org's leases filled in the meantime, and
// succeeds again once they lapse.
func TestRotationStartRunRechecksTheOrgLeaseCap(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		rt := store.NewRotationRuntime(db)
		due, ok, err := rt.DueRotation(tctx(t), x.now())
		if err != nil || !ok || due.ID != rot.ID {
			t.Fatalf("due rotation = %+v, %v, %v", due, ok, err)
		}
		// Four other runs of the org take their leases after the selection.
		stamp := func(at time.Time) string { return at.UTC().Format("2006-01-02T15:04:05.000000Z") }
		for i := 0; i < store.RotationOrgLeaseCap; i++ {
			rotID, runID := fmt.Sprintf("rot_cap_%d", i), fmt.Sprintf("run_cap_%d", i)
			rawRotation(t, db, x.rotationFixture, rotID, fmt.Sprintf("rk_cap_%d", i))
			rawRun(t, db, x.rotationFixture, runID, rotID, "overlap", "2", "", "2026-01-02T00:00:00.000000Z")
			execRaw(t, db, fmt.Sprintf(`UPDATE rotation_runs SET lease_owner='wrk_other', lease_expires_at='%s' WHERE id='%s'`, stamp(x.now().Add(time.Minute)), runID))
		}
		start := func(runID string) bool {
			t.Helper()
			now := x.now()
			_, started, err := rt.StartRun(tctx(t), due, store.StartRun{
				RunID: runID, Worker: x.worker, Now: now, LeaseUntil: now.Add(2 * time.Minute), Trigger: "schedule",
				CandidateHandle: "role_" + runID, CandidateValidUntil: now.Add(48 * time.Hour), ParkedUntil: now.Add(time.Hour),
				Intent: store.RotationEffectIntent{EffectID: "ref_" + runID, Kind: "mint", Audit: store.RotationAudit{ID: "evt_" + runID, Type: "rotation.run_intent", Outcome: "intent", Payload: []byte("{}")}},
			})
			if err != nil {
				t.Fatalf("StartRun: %v", err)
			}
			return started
		}
		if start("run_capped") {
			t.Fatal("a run started past the org lease cap")
		}
		if got := queryInt(t, db, "SELECT COUNT(*) FROM rotation_runs WHERE id='run_capped'"); got != 0 {
			t.Fatal("the refused start left a run row")
		}
		x.advance(2 * time.Minute) // the other leases lapse
		if !start("run_allowed") {
			t.Fatal("a start with room under the cap was refused")
		}
	})
}

// Two starts racing for the last slot under the org lease cap never both commit:
// whichever loses is refused or fails its transaction, and the org ends at the cap.
func TestRotationConcurrentStartsRespectTheOrgLeaseCap(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		x.advance(time.Hour)
		rt := store.NewRotationRuntime(db)
		stamp := func(at time.Time) string { return at.UTC().Format("2006-01-02T15:04:05.000000Z") }
		for i := 0; i < store.RotationOrgLeaseCap-1; i++ {
			rotID, runID := fmt.Sprintf("rot_held_%d", i), fmt.Sprintf("run_held_%d", i)
			rawRotation(t, db, x.rotationFixture, rotID, fmt.Sprintf("rk_held_%d", i))
			rawRun(t, db, x.rotationFixture, runID, rotID, "overlap", "2", "", "2026-01-02T00:00:00.000000Z")
			execRaw(t, db, fmt.Sprintf(`UPDATE rotation_runs SET lease_owner='wrk_other', lease_expires_at='%s' WHERE id='%s'`, stamp(x.now().Add(time.Minute)), runID))
		}
		var dues []store.DueRotation
		for _, id := range []string{"rot_race_a", "rot_race_b"} {
			rawRotation(t, db, x.rotationFixture, id, "rk_"+id)
			execRaw(t, db, fmt.Sprintf(`UPDATE rotations SET state='active', next_run_at='%s' WHERE id='%s'`, stamp(x.now().Add(-time.Minute)), id))
			dues = append(dues, store.DueRotation{ID: id, OrgID: "org_a", ProjectID: "prj_a1", EnvironmentID: string(x.env), ProviderID: x.provider,
				KeyID: "rk_" + id, UsernameKeyID: "rk_" + id + "_user", IntervalSeconds: 86400, RevocationDelaySeconds: 3600, AuthorityPrincipalID: "usr_alice", Generation: 1})
		}
		var wg sync.WaitGroup
		gate := make(chan struct{})
		started := make([]bool, len(dues))
		errs := make([]error, len(dues))
		for i, d := range dues {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-gate
				now := x.now()
				runID := fmt.Sprintf("run_race_%d", i)
				_, ok, err := rt.StartRun(tctx(t), d, store.StartRun{
					RunID: runID, Worker: fmt.Sprintf("wrk_%d", i), Now: now, LeaseUntil: now.Add(2 * time.Minute), Trigger: "schedule",
					CandidateHandle: "role_" + runID, CandidateValidUntil: now.Add(48 * time.Hour), ParkedUntil: now.Add(time.Hour),
					Intent: store.RotationEffectIntent{EffectID: "ref_" + runID, Kind: "mint", Audit: store.RotationAudit{ID: "evt_" + runID, Type: "rotation.run_intent", Outcome: "intent", Payload: []byte("{}")}},
				})
				started[i], errs[i] = ok && err == nil, err
			}()
		}
		close(gate)
		wg.Wait()
		winners := 0
		for i, ok := range started {
			if ok {
				winners++
			}
			// The losing start may be refused or lose a transaction conflict; any
			// other error is a real failure.
			if errs[i] != nil && !rotationTxConflict(errs[i]) {
				t.Fatalf("start %d failed with a non-conflict error: %v", i, errs[i])
			}
		}
		if winners == 0 {
			t.Fatalf("no concurrent start succeeded while one lease slot was free (errors: %v)", errs)
		}
		leased := queryInt(t, db, fmt.Sprintf(`SELECT COUNT(*) FROM rotation_runs WHERE org_id='org_a' AND lease_owner IS NOT NULL AND lease_expires_at>'%s'`, stamp(x.now())))
		if leased > store.RotationOrgLeaseCap {
			t.Fatalf("%d leases live in the org, cap %d (started: %v)", leased, store.RotationOrgLeaseCap, started)
		}
	})
}

// The extend INTENT is durable before the executor looks at supersession, the
// gate or the provider: a later publish's revoke read must see the extend as
// uncertain from the first of those checks on. The provider is opened after the
// supersession check and the gate, so an open extend effect at that moment proves
// the intent was recorded ahead of them.
func TestRotationExtendIntentIsDurableBeforeTheProviderOpens(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.cycle(rot.ID)
		openAtFactory := int64(-1)
		inner := x.exec.ProviderFactory
		x.exec.ProviderFactory = func(kind dynamic.Kind, origin, mode, secret string) (dynamic.Provider, error) {
			open := queryInt(t, db, "SELECT COUNT(*) FROM rotation_effects WHERE kind='extend' AND outcome IS NULL")
			if open > openAtFactory {
				openAtFactory = open
			}
			return inner(kind, origin, mode, secret)
		}
		// Failing mints keep run 1 the newest published run until its credential is
		// due for an extension.
		x.ctl.set(func() { x.ctl.createErr = dynamic.ErrUnreachable })
		for i := 0; i < 90 && x.ctl.count("extend:") == 0; i++ {
			x.advance(30 * time.Minute)
			x.drain()
		}
		if x.ctl.count("extend:") == 0 {
			t.Fatal("the delivered role was never extended")
		}
		if openAtFactory < 1 {
			t.Fatalf("no extend intent was open when the provider opened (max %d)", openAtFactory)
		}
	})
}

// rotationTxConflict reports a transient serialization failure, classified as the
// store's transaction retry does (internal/store/tx): PostgreSQL SQLSTATE 40001 or
// 40P01, SQLite BUSY or LOCKED including the extended codes.
func rotationTxConflict(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001" || pgErr.Code == "40P01"
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		primary := se.Code() & 0xff
		return primary == sqlitelib.SQLITE_BUSY || primary == sqlitelib.SQLITE_LOCKED
	}
	return false
}

// longOverlapRecovery drives run 2 of a rotation (90 minute delay, 2 hour interval)
// into a lapsed `published` run that is recovered 15.5 hours after the commit. The
// delivered credential's extension margin (12 hours before its VALID UNTIL) falls
// half an hour before the recovered overlap deadline (the delay must stay below the interval), so the wake is clamped to it.
// It returns the old role, the new role, the run, the recovery observation (the
// start of the delay) and the extension margin.
func longOverlapRecovery(t *testing.T, db *store.DB) (x *rotExec, rotationID, role1, role2, run2 string, observed, margin time.Time) {
	t.Helper()
	x = newRotExec(t, db)
	req := x.request(rotPasswordKey, rotUsernameKey)
	req.RevocationDelaySeconds = 5400
	req.IntervalSeconds = 2 * 3600 // keeps the fake clock inside the fixture session's 48 hours
	rot, err := x.svc.Create(tctx(t), x.actor, x.scope, req)
	if err != nil {
		t.Fatal(err)
	}
	x.advance(mustParseStamp(t, x.state(rot.ID).NextRunAt).Sub(x.now()) + time.Minute)
	x.drain()
	role1 = x.lastRun(rot.ID, "candidate_handle")
	x.advance(91 * time.Minute) // past the delay: run 1 revokes
	x.drain()
	if got := x.phase(x.openRunID(rot.ID)); got != "done" {
		t.Fatalf("run 1 = %s, want done", got)
	}
	x.advance(mustParseStamp(t, x.state(rot.ID).NextRunAt).Sub(x.now()) + time.Minute) // run 2 is due
	armed := false
	x.exec.Now = func() time.Time {
		at := x.now()
		if armed {
			armed = false
			x.advance(15*time.Hour + 30*time.Minute) // the executor is away: the lease lapses before the settle
		}
		return at
	}
	x.exec.Revisions.PublishProbe = &rotPublishProbe{onBaseline: func() {
		x.exec.Revisions.PublishProbe = nil
		armed = true
	}}
	x.drain()
	run2 = x.openRunID(rot.ID)
	if x.snapshots() != 2 {
		t.Fatalf("%d revisions, want 2: the second publish did not commit", x.snapshots())
	}
	role2 = x.lastRun(rot.ID, "candidate_handle")
	if got := x.phase(run2); got != "overlap" {
		t.Fatalf("run 2 = %s (%s), want overlap after the recovery", got, x.lastRun(rot.ID, "last_error"))
	}
	observed = x.now()
	validUntil := mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "candidate_valid_until")+" FROM rotation_runs WHERE id='"+run2+"'"))
	margin = validUntil.Add(-12 * time.Hour)
	return x, rot.ID, role1, role2, run2, observed, margin
}

func runStamp(t *testing.T, x *rotExec, run, column string) time.Time {
	t.Helper()
	return mustParseStamp(t, queryString(t, x.db, "SELECT "+stampText(x.db, column)+" FROM rotation_runs WHERE id='"+run+"'"))
}

// A recovered overlap whose delay runs past the delivered credential's extension
// margin is visited at the margin, not at the deadline: the credential is extended
// while it still can be, and the run parks again for the real deadline. Nothing is
// revoked before the commit plus the delay.
func TestRotationRecoveredOverlapIsVisitedAtTheExtendMargin(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x, _, role1, role2, run2, observed, margin := longOverlapRecovery(t, db)
		deadline := observed.Add(90 * time.Minute)
		if !margin.Before(deadline) {
			t.Fatalf("fixture: margin %s is not before the deadline %s", margin, deadline)
		}
		if got := runStamp(t, x, run2, "next_attempt_at"); !got.Equal(margin) {
			t.Fatalf("wake = %s, want the extension margin %s", got, margin)
		}
		if got := runStamp(t, x, run2, "revoke_not_before_at"); !got.Equal(deadline) {
			t.Fatalf("stored deadline = %s, want %s", got, deadline)
		}
		validBefore := runStamp(t, x, run2, "candidate_valid_until")
		x.advance(margin.Sub(x.now()) + time.Minute)
		x.drain()
		if n := x.ctl.count("extend:" + role2); n != 1 {
			t.Fatalf("%d extends of the delivered credential at the margin, want 1 (run %s)", n, x.phase(run2))
		}
		if got := runStamp(t, x, run2, "candidate_valid_until"); !got.After(validBefore) {
			t.Fatalf("VALID UNTIL %s did not move past %s", got, validBefore)
		}
		if n := x.ctl.count("drop:" + role1); n != 0 {
			t.Fatalf("the superseded role was dropped %d times at the margin, before the deadline", n)
		}
		if got := x.phase(run2); got != "overlap" {
			t.Fatalf("run 2 = %s after the margin visit, want overlap", got)
		}
		if got := runStamp(t, x, run2, "next_attempt_at"); !got.Equal(deadline) {
			t.Fatalf("parked for %s after the extend, want the deadline %s", got, deadline)
		}
		x.advance(deadline.Sub(x.now()) + time.Minute)
		x.drain()
		if n := x.ctl.count("drop:" + role1); n != 1 {
			t.Fatalf("%d drops of the superseded role after the deadline, want 1", n)
		}
	})
}

// The clamp only moves the wake: the overlap keeps its whole delay after the
// recovery observation, however often the run is visited before that, and an
// abandon during the wait neither shortens it nor drops early.
func TestRotationOverlapClampDoesNotShortenTheDelay(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x, rotationID, role1, _, run2, observed, margin := longOverlapRecovery(t, db)
		deadline := observed.Add(90 * time.Minute)
		x.svc.Now = x.now // the abandon time must follow the executor's clock
		// The abandon keeps the clamped wake (the margin), which is before the deadline.
		if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rotationID, Abandon: true}); err != nil {
			t.Fatal(err)
		}
		if got := x.phase(run2); got != "quarantine" {
			t.Fatalf("run 2 = %s, want quarantine", got)
		}
		// Visits from the margin to the deadline revoke nothing.
		x.advance(margin.Sub(x.now()) + time.Minute)
		for _, at := range []time.Duration{0, 10 * time.Minute, 10 * time.Minute} {
			x.advance(at)
			x.drain()
			if n := x.ctl.count("drop:" + role1); n != 0 {
				t.Fatalf("dropped %d times at %s, before the deadline %s", n, x.now(), deadline)
			}
		}
		x.advance(deadline.Sub(x.now()) + time.Minute)
		x.drain()
		if n := x.ctl.count("drop:" + role1); n != 1 {
			t.Fatalf("%d drops of the superseded role after the deadline, want 1", n)
		}
	})
}
