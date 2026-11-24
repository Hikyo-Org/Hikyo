package isolation

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// S2b-2 review round 7: a published run recovered late, while a restore holds its
// rotation, takes its overlap deadline from the recovery observation (now plus the
// delay), which can lie past the delivered credential's VALID UNTIL. The hold
// zeroed the credential's expiry in the wake, so the run waited for that deadline
// and, after release-hold, found the credential already expired. The wake keeps
// the expiry while held, and a held run whose extension is due is polled at the
// backoff floor instead of being re-claimed at once.
func TestRotationHeldPublishedRunIsWokenBeforeItsCredentialExpires(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		req := x.request(rotPasswordKey, rotUsernameKey)
		req.IntervalSeconds = 3600
		req.RevocationDelaySeconds = 3300
		rot, err := x.svc.Create(tctx(t), x.actor, x.scope, req)
		if err != nil {
			t.Fatal(err)
		}
		x.cycle(rot.ID) // a first run, so the second has a superseded role to wait out
		x.advance(time.Hour + time.Minute)
		// The lease lapses during slow work, so the settle after the commit loses its
		// fence and the run stays `published` for a later recovery.
		x.exec.Revisions.PublishProbe = &rotPublishProbe{onBaseline: func() {
			x.exec.Revisions.PublishProbe = nil
			x.advance(30 * time.Minute)
		}}
		x.drain()
		run := x.openRunID(rot.ID)
		if got := x.phase(run); got != "published" {
			t.Fatalf("run = %s (%s), want published", got, x.lastRun(rot.ID, "last_error"))
		}
		validUntil := func() time.Time {
			return mustParseStamp(t, queryString(t, db, "SELECT "+stampText(db, "candidate_valid_until")+" FROM rotation_runs WHERE id='"+run+"'"))
		}
		v0 := validUntil()

		// The executor was away: the recovery happens so late that the deadline it
		// derives (now + 55m) lies past the credential's VALID UNTIL.
		x.advance(v0.Sub(x.now()) - 25*time.Minute)
		holdRestored(t, db)
		x.requireSweep("wrk_a", true)
		if got := x.phase(run); got != "overlap" {
			t.Fatalf("run after the held recovery = %s, want overlap", got)
		}
		if !validUntil().Equal(v0) || x.ctl.count("extend:") != 0 {
			t.Fatal("a held rotation extended its credential")
		}
		// Held visits are polled, never re-claimed back to back.
		x.requireSweep("wrk_a", false)
		x.advance(6 * time.Minute)
		x.requireSweep("wrk_a", true)
		x.requireSweep("wrk_a", false)
		if x.ctl.count("extend:") != 0 {
			t.Fatal("a held rotation extended its credential on a poll")
		}

		releaseRestoreHold(t, db, rot.ID)
		x.advance(6 * time.Minute)
		x.drain()
		if x.now().After(v0) {
			t.Fatalf("setup: the clock %s is past the credential's expiry %s", x.now(), v0)
		}
		// A deadline past VALID UNTIL means the cycle window is long spent, so the
		// documented rule applies: the rotation halts by name (retries_exhausted)
		// before the credential lapses, instead of sleeping through the expiry.
		st := x.state(rot.ID)
		reason := queryString(t, db, "SELECT COALESCE(halt_reason,'') FROM rotations WHERE id='"+rot.ID+"'")
		if st.State != store.RotationStateHalted || reason != "retries_exhausted" {
			t.Fatalf("after the release at %s (VALID UNTIL %s): state %s, halt %q; the run slept through the credential's expiry", x.now(), v0, st.State, reason)
		}
	})
}

// A person whose publish comes only from rules narrowed to the rotation's two keys
// needs environment read to drive the rotation: the lookup of the stored keys runs
// under read, which no publish rule supplies. Without it the transition is refused
// as a denial under the operation. Read is environment-level, so a read rule
// narrowed to those keys still supplies it, exactly like one over all keys.
func TestRotationKeyNarrowedTransitionsNeedEnvironmentRead(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := tctx(t)
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		rules := &service.Rules{DB: db}
		envMode, envs := onlyEnvs(x.env)
		grant := func(p domain.PrincipalID, c domain.Capability, keyMode domain.AxisMode, keys ...string) {
			t.Helper()
			items := make([]domain.RuleKeyItem, len(keys))
			for i, k := range keys {
				items[i] = domain.RuleKeyItem{KeyID: k}
			}
			if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{
				Target: p, Capability: c, Org: orgA, Where: whereIn(envMode, envs, keyMode, items...),
			}); err != nil {
				t.Fatalf("rule %s for %s: %v", c, p, err)
			}
		}
		const noRead, readNarrowed, readAll = domain.PrincipalID("usr_zr7_noread"), domain.PrincipalID("usr_zr7_readnarrow"), domain.PrincipalID("usr_zr7_readall")
		for _, p := range []domain.PrincipalID{noRead, readNarrowed, readAll} {
			rotationHuman(t, db, p)
			grant(p, domain.CapPublish, domain.AxisOnly, rotPasswordKey, rotUsernameKey)
		}
		grant(readNarrowed, domain.CapRead, domain.AxisOnly, rotPasswordKey, rotUsernameKey)
		grant(readAll, domain.CapRead, domain.AxisAll)

		pause := func(p domain.PrincipalID) error {
			_, err := x.svc.Pause(ctx, x.session(p, true), x.scope, service.RotationStateRequest{RotationID: rot.ID})
			return err
		}
		if err := pause(noRead); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("pause by publish narrowed to both keys with no read rule = %v, want the uniform not-found", err)
		}
		if got := deniedUnder(t, db, noRead, "rotation.pause"); got != 1 {
			t.Fatalf("denial rows under rotation.pause = %d, want 1", got)
		}
		if x.state(rot.ID).State != store.RotationStateActive {
			t.Fatal("a refused pause changed the rotation")
		}
		for name, p := range map[string]domain.PrincipalID{"a read rule narrowed to the keys": readNarrowed, "a read rule over all keys": readAll} {
			if _, err := x.svc.Pause(ctx, x.session(p, true), x.scope, service.RotationStateRequest{RotationID: rot.ID}); err != nil {
				t.Fatalf("pause with publish narrowed to both keys and %s: %v", name, err)
			}
			if _, err := x.svc.Resume(ctx, x.session(alice, true), x.scope, service.RotationStateRequest{RotationID: rot.ID}); err != nil {
				t.Fatalf("resume: %v", err)
			}
		}
	})
}
