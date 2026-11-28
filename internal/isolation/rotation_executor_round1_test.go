package isolation

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Round 1 review fixes for the scheduled rotation executor (#148, S2a): grouped
// keys and authority drafts, the restore assertion, resume and held-run keep-alive,
// expiry exits, superseded-role provider binding, audit attribution, and the race
// and head-of-line behaviors. Same harness as rotation_executor_e2e_test.go.

const (
	rotBob = domain.PrincipalID("usr_rot_bob")
)

// groupKey puts a key into a key group, the shape the executor refuses.
func (x *rotExec) groupKey(keyID string) {
	x.t.Helper()
	execRaw(x.t, x.db, `INSERT INTO key_groups (id, org_id, project_id, name, created_at) VALUES ('grp_rot_`+keyID+`', 'org_a', 'prj_a1', 'rot-group-`+keyID+`', `+ts+`)`)
	execRaw(x.t, x.db, `UPDATE keys SET group_id='grp_rot_`+keyID+`' WHERE id='`+keyID+`'`)
}

// restorer is a Revisions service that can consume the reveal ceremony a restore
// of historical secret values requires.
func (x *rotExec) restorer() *service.Revisions {
	// A second restore in one test finds the grant already there.
	_ = execRawErr(x.t, x.db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_rot_reveal_history','usr_alice','reveal-history','org_a',NULL,NULL,`+ts+`)`)
	auth := authService(x.t, x.db)
	auth.ReauthWindow = 15 * time.Minute
	return &service.Revisions{DB: x.db, Keyring: probeKeyring(x.t, x.db), Auth: auth}
}

func (x *rotExec) values() *service.Values {
	return &service.Values{DB: x.db, Keyring: probeKeyring(x.t, x.db)}
}

// stageDraft saves a pending draft of a key as principal, without publishing it.
func (x *rotExec) stageDraft(principal domain.PrincipalID, name, value string) string {
	x.t.Helper()
	change, err := x.values().Set(tctx(x.t), service.LocalPrincipal(principal), x.scope, name, value, nil)
	if err != nil {
		x.t.Fatalf("stage %s as %s: %v", name, principal, err)
	}
	return change.VersionID
}

func (x *rotExec) drafts(principal domain.PrincipalID, keyID string) int64 {
	x.t.Helper()
	return queryInt(x.t, x.db, "SELECT COUNT(*) FROM pending_changes WHERE owner_id='"+string(principal)+"' AND key_id='"+keyID+"'")
}

func (x *rotExec) halts(reason string) int64 {
	x.t.Helper()
	return queryInt(x.t, x.db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.configured' AND payload LIKE '%"action":"halted"%' AND payload LIKE '%"reason":"`+reason+`"%'`)
}

func (x *rotExec) publishedRuns(rotationID string) int64 {
	x.t.Helper()
	return queryInt(x.t, x.db, "SELECT COUNT(*) FROM rotation_runs WHERE rotation_id='"+rotationID+"' AND published_revision IS NOT NULL")
}

func (x *rotExec) entryIDs() string {
	x.t.Helper()
	return queryStrings(x.t, x.db, "SELECT key_id || '=' || id || ';' FROM value_entries WHERE environment_id='"+string(x.env)+"' ORDER BY key_id")
}

// driveUntilPublished steps the clock in half hours, draining each step, until the
// rotation has published runs published runs. It fails if the delivered role
// lapses before that: the credential must stay valid through the next publish.
func (x *rotExec) driveUntilPublished(rotationID, delivered string, published int64, limit time.Duration) {
	x.t.Helper()
	deadline := x.now().Add(limit)
	for x.publishedRuns(rotationID) < published {
		if x.now().After(deadline) {
			x.t.Fatalf("the rotation did not publish within %s", limit)
		}
		x.advance(30 * time.Minute)
		x.drain()
		if x.publishedRuns(rotationID) >= published {
			return
		}
		if role, ok := x.ctl.roles[delivered]; !ok || !role.validUntil.After(x.now()) {
			x.t.Fatalf("the delivered credential %s lapsed at %s before the next publish (role present %v, valid until %s)", delivered, x.now(), ok, role.validUntil)
		}
	}
}

// ------------------------------------------------------------ grouped keys

func TestRotationGroupedKeyRefusedAtCreate(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		x.groupKey(rotSecondUser)
		_, err := x.svc.Create(tctx(t), x.actor, x.scope, x.request(rotSecondKey, rotSecondUser))
		if !errors.Is(err, service.ErrRotationKeyGrouped) || !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("create over a grouped key = %v, want ErrRotationKeyGrouped", err)
		}
	})
}

func TestRotationGroupedKeyRefusedAtResume(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		if _, err := x.transition(x.actor, "pause", rot.ID); err != nil {
			t.Fatal(err)
		}
		x.groupKey(rotPasswordKey)
		if _, err := x.transition(x.actor, "resume", rot.ID); !errors.Is(err, service.ErrRotationKeyGrouped) {
			t.Fatalf("resume over a grouped key = %v, want ErrRotationKeyGrouped", err)
		}
	})
}

// The Gate runs before every mint: a key grouped after the rotation was created
// halts it by name before any candidate is created.
func TestRotationGroupedKeyHaltsAtTheGate(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.groupKey(rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		x.drain()
		if x.ctl.count("create:") != 0 {
			t.Fatal("a rotation over a grouped key minted a candidate")
		}
		if got := x.state(rot.ID).State; got != store.RotationStateHalted {
			t.Fatalf("state = %s, want halted", got)
		}
		if x.halts("gate_failed") != 1 {
			t.Fatalf("%d halted audits naming gate_failed", x.halts("gate_failed"))
		}
	})
}

// ------------------------------------------------------------ authority drafts

// The scheduled publish acts as its authority principal, so it would consume that
// principal's own pending draft on a rotated key. It refuses instead, loudly, and
// leaves the draft alone.
func TestRotationAuthorityDraftRefusedBeforeMint(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.stageDraft(alice, "ROT_DB_PASSWORD", "alice-working-draft")
		x.advance(24*time.Hour + time.Minute)
		x.drain()
		if x.ctl.count("create:") != 0 || x.snapshots() != 0 {
			t.Fatal("a rotation minted or published over the authority's draft")
		}
		if got := x.state(rot.ID).State; got != store.RotationStateHalted || x.halts("gate_failed") != 1 {
			t.Fatalf("state = %s, gate_failed halts = %d", got, x.halts("gate_failed"))
		}
		if x.drafts(alice, rotPasswordKey) != 1 {
			t.Fatal("the authority's draft was consumed")
		}
	})
}

// A draft saved between the Gate and the publish (during the provider call) is
// caught under the project lock: the run rolls back, nothing is published and
// the draft survives.
func TestRotationAuthorityDraftRefusedAtStage(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		x.ctl.set(func() {
			x.ctl.at = func(p, _ string) {
				if p == "create:after" {
					x.ctl.set(func() { x.ctl.at = nil })
					x.stageDraft(alice, "ROT_DB_USER", "alice-late-draft")
				}
			}
		})
		x.requireSweep(x.worker, true)
		if got := x.runPhases(rot.ID); got != "failed," {
			t.Fatalf("phases = %q, want failed", got)
		}
		if x.snapshots() != 0 || len(x.ctl.names()) != 0 {
			t.Fatalf("a refused stage left %d revisions and roles %v", x.snapshots(), x.ctl.names())
		}
		if x.drafts(alice, rotUsernameKey) != 1 {
			t.Fatal("the authority's draft was overwritten")
		}
	})
}

// Another principal's draft is not the authority's to consume and does not stop
// the rotation or get touched by it.
func TestRotationOtherPrincipalDraftDoesNotBreakRotation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rotationHuman(t, db, rotBob, "read", "edit")
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.stageDraft(rotBob, "ROT_DB_PASSWORD", "bob-working-draft")
		x.advance(24*time.Hour + time.Minute)
		x.drain()
		if got := x.runPhases(rot.ID); got != "overlap," {
			t.Fatalf("phases = %q, want overlap: another principal's draft stopped the rotation", got)
		}
		if x.drafts(rotBob, rotPasswordKey) != 1 {
			t.Fatal("the rotation consumed another principal's draft")
		}
	})
}

// A pending restore draft by the authority is a draft like any other.
func TestRotationAuthorityRestoreDraftRefused(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		me := service.LocalPrincipal(alice)
		var first int64
		for i, value := range []string{"first", "second"} {
			var staged []string
			for _, name := range []string{"ROT_DB_USER", "ROT_DB_PASSWORD"} {
				change, err := x.values().Set(tctx(t), me, x.scope, name, name+"-"+value, nil)
				if err != nil {
					t.Fatalf("stage %s: %v", name, err)
				}
				staged = append(staged, change.VersionID)
			}
			if _, err := x.revisions().PublishPlanned(tctx(t), me, x.scope, service.PublishRequest{VersionIDs: staged}); err != nil {
				t.Fatalf("publish %d: %v", i, err)
			}
			if i == 0 {
				first = queryInt(t, db, "SELECT MAX(revision) FROM snapshots WHERE environment_id='"+string(x.env)+"'")
			}
		}
		rot := x.create(rotPasswordKey, rotUsernameKey)
		restored, err := x.restorer().Restore(tctx(t), x.actor, x.scope, first, "ROT_DB_PASSWORD")
		if err != nil || len(restored.Changes) == 0 {
			t.Fatalf("stage a restore draft: %v (%d changes)", err, len(restored.Changes))
		}
		x.advance(24*time.Hour + time.Minute)
		x.drain()
		if x.ctl.count("create:") != 0 || x.halts("gate_failed") != 1 {
			t.Fatalf("a pending restore draft did not halt the rotation before the mint (creates %d)", x.ctl.count("create:"))
		}
		if x.drafts(alice, rotPasswordKey) != 1 {
			t.Fatal("the restore draft was consumed")
		}
		_ = rot
	})
}

// ------------------------------------------------------------ restore assertion

// A value that was unset after the publish serves nothing: the superseded role
// cannot be serving, so the revoke proceeds.
func TestRotationRestoreAssertionPassesAbsentValue(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		x.advance(24 * time.Hour)
		x.drain()
		run2 := x.openRunID(rot.ID)
		me := service.LocalPrincipal(alice)
		var staged []string
		for _, name := range []string{"ROT_DB_USER", "ROT_DB_PASSWORD"} {
			change, err := x.values().Unset(tctx(t), me, x.scope, name)
			if err != nil {
				t.Fatalf("unset %s: %v", name, err)
			}
			staged = append(staged, change.VersionID)
		}
		if _, err := x.revisions().PublishPlanned(tctx(t), me, x.scope, service.PublishRequest{VersionIDs: staged}); err != nil {
			t.Fatalf("publish the unset: %v", err)
		}
		if queryInt(t, db, "SELECT COUNT(*) FROM value_entries WHERE environment_id='"+string(x.env)+"' AND key_id IN ('"+rotPasswordKey+"','"+rotUsernameKey+"')") != 0 {
			t.Fatal("the unset left a value entry")
		}
		x.advance(time.Hour + time.Minute)
		x.drain()
		if got := x.phase(run2); got != "done" || x.ctl.has(role1) {
			t.Fatalf("an absent value held the revoke: phase %s, role1 live %v", got, x.ctl.has(role1))
		}
	})
}

// A revision restore puts the superseded value back in service: the revoke holds
// with the named reason and drops nothing.
func TestRotationRestoreAssertionHoldsOnRevisionRestore(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		run1 := x.cycle(rot.ID)
		first := queryInt(t, db, "SELECT published_revision FROM rotation_runs WHERE id='"+run1+"'")
		role1 := x.lastRun(rot.ID, "candidate_handle")
		x.advance(24 * time.Hour)
		x.drain()
		run2 := x.openRunID(rot.ID)
		me := service.LocalPrincipal(alice)
		restored, err := x.restorer().Restore(tctx(t), x.actor, x.scope, first, "")
		if err != nil || len(restored.Changes) == 0 {
			t.Fatalf("restore revision %d: %v (%d changes)", first, err, len(restored.Changes))
		}
		var staged []string
		for _, c := range restored.Changes {
			staged = append(staged, c.VersionID)
		}
		if _, err := x.revisions().PublishPlanned(tctx(t), me, x.scope, service.PublishRequest{VersionIDs: staged, PreviewToken: restored.Preview.Token}); err != nil {
			t.Fatalf("publish the restore: %v", err)
		}
		x.advance(time.Hour + time.Minute)
		x.drain()
		if got := x.phase(run2); got != "revoke_refused" || x.lastRun(rot.ID, "last_error") != "published_versions_changed" {
			t.Fatalf("a restore that re-serves the superseded value left %s / %q", got, x.lastRun(rot.ID, "last_error"))
		}
		if !x.ctl.has(role1) || x.ctl.count("drop:"+role1) != 0 {
			t.Fatal("the superseded role was dropped while a restore serves it")
		}
	})
}

// The values a later run published are legitimate for an earlier run's revoke: a
// run abandoned past its publish still drops its superseded role once a recreated
// rotation has published newer versions.
func TestRotationRestoreAssertionPassesLaterRunsVersions(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		req := x.request(rotPasswordKey, rotUsernameKey)
		req.RevocationDelaySeconds = 12 * 3600
		first, err := x.svc.Create(tctx(t), x.actor, x.scope, req)
		if err != nil {
			t.Fatal(err)
		}
		x.advance(24*time.Hour + time.Minute)
		x.drain() // run 1 published (nothing to supersede)
		x.advance(12*time.Hour + time.Minute)
		x.drain() // run 1 done
		role1 := x.lastRun(first.ID, "candidate_handle")
		x.advance(12 * time.Hour)
		x.drain() // run 2 published, role1 superseded and owed a revoke after 12h
		run2 := x.openRunID(first.ID)
		if got := x.phase(run2); got != "overlap" {
			t.Fatalf("run 2 is %s, want overlap", got)
		}
		if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: first.ID, Abandon: true}); err != nil {
			t.Fatal(err)
		}
		again := x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		x.drain() // the recreated rotation publishes newer versions
		if x.publishedRuns(again.ID) != 1 {
			t.Fatalf("the recreated rotation published %d runs", x.publishedRuns(again.ID))
		}
		x.advance(13 * time.Hour)
		x.drain()
		if got := x.phase(run2); got != "done" || x.ctl.has(role1) {
			t.Fatalf("the abandoned run's revoke was held by a later run's values: %s, role1 live %v", got, x.ctl.has(role1))
		}
	})
}

// ------------------------------------------------------------ resume keeps the credential valid

func TestRotationResumeAfterExhaustionKeepsCredentialValid(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		x.ctl.set(func() { x.ctl.createErr = dynamic.ErrUnreachable })
		for i := 0; i < 400 && x.state(rot.ID).State == store.RotationStateActive; i++ {
			x.advance(30 * time.Minute)
			x.drain()
		}
		if x.state(rot.ID).State != store.RotationStateHalted || x.halts("retries_exhausted") != 1 {
			t.Fatalf("the rotation did not halt by retries_exhausted (state %s)", x.state(rot.ID).State)
		}
		x.ctl.set(func() { x.ctl.createErr = nil })
		if _, err := x.transition(x.actor, "resume", rot.ID); err != nil {
			t.Fatal(err)
		}
		x.driveUntilPublished(rot.ID, role1, 2, 96*time.Hour)
		if !x.ctl.has(role1) {
			t.Fatal("the delivered role vanished")
		}
	})
}

func TestRotationResumeAfterAuthorityLossKeepsCredentialValid(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		restore := x.parkAuthority()
		x.advance(24*time.Hour + time.Minute)
		x.drain()
		if x.state(rot.ID).State != store.RotationStateHalted || x.halts("authority_lost") != 1 {
			t.Fatalf("the rotation did not halt by authority_lost (state %s)", x.state(rot.ID).State)
		}
		restore()
		if _, err := x.transition(x.actor, "resume", rot.ID); err != nil {
			t.Fatal(err)
		}
		x.driveUntilPublished(rot.ID, role1, 2, 96*time.Hour)
	})
}

// ------------------------------------------------------------ held runs and extend

// A revoke the provider keeps refusing holds the run open, which blocks the next
// cycle; the delivered credential is extended while the hold lasts and the rotation
// halts by name before it lapses.
func TestRotationHeldRevokeExtendsDeliveredCredentialThenHalts(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		run1 := x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		x.advance(24 * time.Hour)
		x.drain()
		run2 := x.openRunID(rot.ID)
		role2 := x.lastRun(rot.ID, "candidate_handle")
		// The superseded role would normally lapse about a day after the publish and
		// release the hold on its own; keep it valid so the hold outlasts the window.
		far := x.now().Add(300 * time.Hour).UTC()
		execRaw(t, db, "UPDATE rotation_runs SET candidate_valid_until='"+far.Format("2006-01-02T15:04:05.000000Z")+"' WHERE id='"+run1+"'")
		x.ctl.set(func() { x.ctl.roles[role1] = rotRole{password: x.ctl.roles[role1].password, validUntil: far} })
		x.ctl.set(func() { x.ctl.dropErr = dynamic.ErrRoleOwnsObjects })
		x.advance(time.Hour + time.Minute)
		x.drain()
		if got := x.phase(run2); got != "revoke_refused" || x.lastRun(rot.ID, "last_error") != "role_owns_objects" {
			t.Fatalf("run 2 = %s / %q, want revoke_refused / role_owns_objects", got, x.lastRun(rot.ID, "last_error"))
		}
		validUntil := x.ctl.roles[role2].validUntil
		for i := 0; i < 120 && x.state(rot.ID).State == store.RotationStateActive; i++ {
			x.advance(time.Hour)
			x.drain()
			if role, ok := x.ctl.roles[role2]; !ok || !role.validUntil.After(x.now()) {
				t.Fatalf("the delivered credential lapsed while a revoke was held (present %v, now %s, valid until %s, created %s, extends %d)", ok, x.now(), role.validUntil, validUntil, x.ctl.count("extend:"))
			}
		}
		if x.ctl.count("extend:"+role2) != 1 || !x.ctl.roles[role2].validUntil.Equal(validUntil.Add(24*time.Hour)) {
			t.Fatalf("delivered credential extended %d times, valid until %s (was %s)", x.ctl.count("extend:"+role2), x.ctl.roles[role2].validUntil, validUntil)
		}
		if x.state(rot.ID).State != store.RotationStateHalted || x.halts("retries_exhausted") != 1 {
			t.Fatalf("the held rotation did not halt by name (state %s)", x.state(rot.ID).State)
		}
		if got := x.phase(run2); got != "revoke_refused" {
			t.Fatalf("the halt moved the held run to %s", got)
		}
		// The halt does not abandon the obligation: once the provider lets go the
		// revoke completes.
		x.ctl.set(func() { x.ctl.dropErr = nil })
		x.advance(7 * time.Hour)
		x.drain()
		if got := x.phase(run2); got != "done" || x.ctl.has(role1) {
			t.Fatalf("the held revoke did not complete after the halt: %s, role1 live %v", got, x.ctl.has(role1))
		}
	})
}

// A delivered credential past its window is not extended after the fact: the
// settled run parks and nothing reaches the provider.
func TestRotationExtendAfterWindowParks(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		run1 := x.cycle(rot.ID)
		// No sweeps while the nodes were down: the clock moves past D and the role's
		// own VALID UNTIL.
		x.advance(60 * time.Hour)
		x.requireSweep(x.worker, true)
		if x.ctl.count("extend:") != 0 {
			t.Fatal("a credential past its window was extended")
		}
		if got := x.phase(run1); got != "done" {
			t.Fatalf("run 1 = %s", got)
		}
		if next := queryString(t, db, "SELECT CAST(next_attempt_at AS TEXT) FROM rotation_runs WHERE id='"+run1+"'"); !strings.HasPrefix(next, "9000") {
			t.Fatalf("the settled run is not parked: next_attempt_at = %s", next)
		}
	})
}

// A rotation that no longer passes the Gate halts by name instead of extending
// the delivered credential.
func TestRotationExtendIsGated(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.cycle(rot.ID)
		x.parkAuthority()
		x.ctl.set(func() { x.ctl.createErr = dynamic.ErrUnreachable })
		for i := 0; i < 100 && x.state(rot.ID).State == store.RotationStateActive; i++ {
			x.advance(30 * time.Minute)
			x.drain()
		}
		if x.ctl.count("extend:") != 0 {
			t.Fatal("a credential whose authority was lost was extended")
		}
		if x.state(rot.ID).State != store.RotationStateHalted {
			t.Fatal("the rotation did not halt")
		}
	})
}

// ------------------------------------------------------------ expiry exits without the provider

// A provider credential revoked mid-run (which only pauses rotations) cannot hold
// a superseded role forever: the role's own expiry settles the run.
func TestRotationRevokedCredentialSettlesByExpiry(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		x.advance(24 * time.Hour)
		x.drain()
		run2 := x.openRunID(rot.ID)
		if err := x.dyn.RevokeCredential(tctx(t), service.LocalPrincipal(alice), prjScope(), x.provider); err != nil {
			t.Fatal(err)
		}
		x.advance(time.Hour + time.Minute)
		x.drain()
		if got := x.phase(run2); got != "overlap" && got != "published" {
			t.Fatalf("a run without a usable provider credential moved to %s while role1 was still valid", got)
		}
		if !x.ctl.has(role1) {
			t.Fatal("the role was dropped without a provider")
		}
		x.advance(72 * time.Hour)
		x.drain()
		if got := x.phase(run2); got != "done" {
			t.Fatalf("an expired superseded role still holds the run: %s", got)
		}
		if x.effects("revoke", "success") != 1 || x.effects("revoke", "open") != 0 {
			t.Fatal("the expiry exit did not record the revoke as settled")
		}
	})
}

// The same for a pre-publish candidate whose drop could not be confirmed.
func TestRotationRevokedCredentialSettlesCandidateByExpiry(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		x.ctl.set(func() { x.ctl.dropErr = dynamic.ErrUnreachable })
		x.crashOnce("create:after")
		if !x.crashSweep("wrk_a") {
			t.Fatal("no crash")
		}
		if _, err := x.transition(x.actor, "pause", rot.ID); err != nil {
			t.Fatal(err)
		}
		x.advance(rotationLeaseTermForTest + time.Second)
		x.drain()
		run := x.openRunID(rot.ID)
		if got := x.phase(run); got != "failed" {
			t.Fatalf("run = %s, want failed with an unconfirmed drop", got)
		}
		if err := x.dyn.RevokeCredential(tctx(t), service.LocalPrincipal(alice), prjScope(), x.provider); err != nil {
			t.Fatal(err)
		}
		x.advance(7 * time.Hour)
		x.drain()
		if x.effects("drop_candidate", "success") != 0 {
			t.Fatal("the drop was recorded as confirmed without a provider")
		}
		x.advance(60 * time.Hour)
		x.drain()
		if x.effects("drop_candidate", "success") != 1 || x.lastRun(rot.ID, "last_error") != "expired" {
			t.Fatalf("the expired candidate was not settled: success effects %d, last_error %q", x.effects("drop_candidate", "success"), x.lastRun(rot.ID, "last_error"))
		}
	})
}

// ------------------------------------------------------------ provider binding

// A rotation deleted on one provider and recreated on another still revokes the
// superseded role on the provider that minted it.
func TestRotationSupersededRoleDroppedOnMintingProvider(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		second := &rotProviderCtl{roles: map[string]rotRole{}}
		const secondOrigin = "postgres://admin@db2.rotation.example:5432/app"
		x.exec.ProviderFactory = func(_ dynamic.Kind, origin, _, _ string) (dynamic.Provider, error) {
			if origin == secondOrigin {
				return rotProvider{ctl: second}, nil
			}
			return rotProvider{ctl: x.ctl}, nil
		}
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.cycle(rot.ID)
		role1 := x.lastRun(rot.ID, "candidate_handle")
		if _, err := x.transition(x.actor, "delete", rot.ID); err != nil {
			t.Fatal(err)
		}
		p2, err := x.dyn.Configure(tctx(t), service.LocalPrincipal(alice), prjScope(), service.CreateDynamicProviderRequest{
			Kind: string(dynamic.KindPostgres), Origin: secondOrigin, GrantRole: "app_rw", Credential: []byte("second-admin-credential"),
		})
		if err != nil {
			t.Fatalf("configure the second provider: %v", err)
		}
		req := x.request(rotPasswordKey, rotUsernameKey)
		req.ProviderID = p2.ID
		again, err := x.svc.Create(tctx(t), x.actor, x.scope, req)
		if err != nil {
			t.Fatalf("recreate on the second provider: %v", err)
		}
		x.advance(25 * time.Hour)
		x.drain()
		run := x.openRunID(again.ID)
		if got := queryString(t, db, "SELECT COALESCE(superseded_handle,'') FROM rotation_runs WHERE id='"+run+"'"); got != role1 {
			t.Fatalf("superseded_handle = %q, want %q", got, role1)
		}
		x.advance(time.Hour + time.Minute)
		x.drain()
		if got := x.phase(run); got != "done" {
			t.Fatalf("run = %s, want done", got)
		}
		if x.ctl.has(role1) || x.ctl.count("drop:"+role1) != 1 {
			t.Fatalf("role1 was not dropped on the provider that minted it (live %v, drops %d)", x.ctl.has(role1), x.ctl.count("drop:"+role1))
		}
		if second.count("drop:"+role1) != 0 || second.count("status:"+role1) != 0 {
			t.Fatal("the recreated rotation's provider was asked about a role it never minted")
		}
	})
}

// ------------------------------------------------------------ audit attribution

// Everything the executor writes is the system acting for the recorded authority,
// correlated to its run; the publish outcome names the revision it produced.
func TestRotationExecutorAuditAttribution(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		x.drain()
		run := x.openRunID(rot.ID)
		published := x.lastRun(rot.ID, "published_revision")
		for _, typ := range []string{"value.staged", "revision.published", "rotation.run_intent", "rotation.run_outcome"} {
			n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='"+typ+"' AND correlation_id='"+run+"'")
			if n == 0 {
				t.Fatalf("no %s audit row correlated to the run", typ)
			}
			bad := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='"+typ+"' AND correlation_id='"+run+"' AND (origin<>'system' OR COALESCE(authority_id,'')<>'usr_alice')")
			if bad != 0 {
				t.Fatalf("%d %s rows are not attributed to the system acting for the authority", bad, typ)
			}
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.run_outcome' AND correlation_id='`+run+`' AND payload LIKE '%"phase":"publish"%' AND payload LIKE '%"revision":`+published+`%'`); n != 1 {
			t.Fatalf("%d publish outcomes name revision %s", n, published)
		}
	})
}

// ------------------------------------------------------------ abandon

// A published run with no superseded role owes nothing: abandoning it settles it
// straight to done so the provider and environment can be deleted.
func TestRotationAbandonPublishedRunWithoutSupersededSettlesDone(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		x.drain()
		run := x.openRunID(rot.ID)
		if got := x.phase(run); got != "overlap" {
			t.Fatalf("run = %s, want overlap", got)
		}
		if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID, Abandon: true}); err != nil {
			t.Fatal(err)
		}
		if got := x.phase(run); got != "done" {
			t.Fatalf("an abandoned run with nothing to revoke = %s, want done", got)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.quarantined' AND correlation_id='"+run+"'"); n != 0 {
			t.Fatalf("%d quarantine audits for a run that owes nothing", n)
		}
		provider := service.LocalPrincipal(alice)
		if _, err := x.dyn.Delete(tctx(t), provider, domain.Scope{Org: orgA, Project: prjA1}, x.provider, false); err != nil {
			t.Fatalf("provider delete after the abandon: %v", err)
		}
		if err := envs.DeleteConfirming(tctx(t), provider, x.scope, nil); err != nil {
			t.Fatalf("environment delete after the abandon: %v", err)
		}
	})
}

// A run that already settled is not touched by an abandon and earns no audit.
func TestRotationAbandonAuditsOnlyChangedRows(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		x.crashOnce("create:after")
		if !x.crashSweep("wrk_a") {
			t.Fatal("no crash")
		}
		x.advance(rotationLeaseTermForTest + time.Second)
		if _, err := x.transition(x.actor, "pause", rot.ID); err != nil {
			t.Fatal(err)
		}
		x.drain() // the takeover rolls the run back to failed
		run := x.openRunID(rot.ID)
		if got := x.phase(run); got != "failed" {
			t.Fatalf("run = %s, want failed", got)
		}
		if _, err := x.svc.Delete(tctx(t), x.actor, x.scope, service.RotationStateRequest{RotationID: rot.ID, Abandon: true}); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.quarantined'"); n != 0 {
			t.Fatalf("%d quarantine audits for runs the abandon did not change", n)
		}
	})
}

// ------------------------------------------------------------ races

// A pause that lands during the provider call of the attempt that would exhaust
// the retries leaves the rotation paused; the halt matched no row, so it writes no
// halt audit.
func TestRotationHaltAuditSkippedWhenPauseWon(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.ctl.set(func() { x.ctl.createErr = dynamic.ErrUnreachable })
		for i := 0; i < 11; i++ {
			x.advance(mustParseStamp(t, x.state(rot.ID).NextRunAt).Sub(x.now()) + time.Second)
			x.requireSweep(x.worker, true)
		}
		if x.state(rot.ID).State != store.RotationStateActive {
			t.Fatalf("state = %s before the last attempt", x.state(rot.ID).State)
		}
		x.advance(mustParseStamp(t, x.state(rot.ID).NextRunAt).Sub(x.now()) + time.Second)
		x.ctl.set(func() {
			x.ctl.at = func(p, _ string) {
				if p == "create:before" {
					x.ctl.set(func() { x.ctl.at = nil })
					if _, err := x.transition(x.actor, "pause", rot.ID); err != nil {
						t.Errorf("pause: %v", err)
					}
				}
			}
		})
		x.requireSweep(x.worker, true)
		if got := x.state(rot.ID).State; got != store.RotationStatePaused {
			t.Fatalf("state = %s, want paused", got)
		}
		if n := x.halts("retries_exhausted"); n != 0 {
			t.Fatalf("%d halt audits for a halt that matched no row", n)
		}
	})
}

// The orphan margin: a worker never takes a run over inside one provider deadline
// of the lease expiring.
func TestRotationTakeoverWaitsAProviderDeadlinePastTheLease(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		x.create(rotPasswordKey, rotUsernameKey)
		x.advance(24*time.Hour + time.Minute)
		x.crashOnce("create:before")
		if !x.crashSweep("wrk_a") {
			t.Fatal("no crash")
		}
		x.advance(2*time.Minute + time.Second)
		x.requireSweep("wrk_b", false)
		x.advance(5 * time.Second)
		x.requireSweep("wrk_b", true)
	})
}

// ------------------------------------------------------------ outcomes

// A run that failed before any mint records the confirmed absence with a named
// code, and the delivered value ids are untouched by the failed cycle.
func TestRotationFailedCycleLeavesDeliveredValuesAndRecordsNoCandidate(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		rot := x.create(rotPasswordKey, rotUsernameKey)
		x.cycle(rot.ID)
		before := x.entryIDs()
		x.ctl.set(func() { x.ctl.createErr = dynamic.ErrUnreachable })
		x.advance(24*time.Hour + time.Minute)
		x.drain()
		if x.entryIDs() != before {
			t.Fatalf("a failed cycle changed the delivered value ids: %s -> %s", before, x.entryIDs())
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.run_outcome' AND payload LIKE '%"phase":"drop_candidate"%' AND payload LIKE '%"error_code":"no_candidate"%'`); n == 0 {
			t.Fatal("the confirmed absence did not record a no_candidate outcome")
		}
	})
}

// A non-definite pre-start failure moves the rotation's next run so the oldest due
// row cannot head-of-line block every other rotation.
func TestRotationDeferredRotationYieldsToTheNextDue(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		first := x.create(rotPasswordKey, rotUsernameKey)
		x.advance(time.Minute)
		second, err := x.svc.Create(tctx(t), x.actor, x.scope, x.request(rotSecondKey, rotSecondUser))
		if err != nil {
			t.Fatal(err)
		}
		x.advance(25 * time.Hour)
		due, ok, err := x.exec.Runtime.DueRotation(tctx(t), x.now())
		if err != nil || !ok || due.ID != first.ID {
			t.Fatalf("the oldest due rotation is %q (%v, %v), want %s", due.ID, ok, err, first.ID)
		}
		if err := x.exec.Runtime.DeferRotation(tctx(t), due, x.now().Add(5*time.Minute), x.now()); err != nil {
			t.Fatal(err)
		}
		next, ok, err := x.exec.Runtime.DueRotation(tctx(t), x.now())
		if err != nil || !ok || next.ID != second.ID {
			t.Fatalf("after the deferral the due rotation is %q (%v, %v), want %s", next.ID, ok, err, second.ID)
		}
	})
}

// ------------------------------------------------------------ per-org cap

// The per-organization claim cap is soft (the count and the claim are separate
// statements) but holds under a sequential worker: with four live leases in the
// organization a fifth due run waits for one to expire.
func TestRotationPerOrgClaimCap(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		x := newRotExec(t, db)
		pairs := [][2]string{{rotPasswordKey, rotUsernameKey}, {rotSecondKey, rotSecondUser}}
		for i := 3; i <= 5; i++ {
			pw, user := fmt.Sprintf("key_rot_pw%d", i), fmt.Sprintf("key_rot_user%d", i)
			rotationKey(t, db, pw, fmt.Sprintf("ROT_DB_PASSWORD%d", i), "secret")
			rotationKey(t, db, user, fmt.Sprintf("ROT_DB_USER%d", i), "config")
			pairs = append(pairs, [2]string{pw, user})
		}
		for _, p := range pairs {
			x.create(p[0], p[1])
		}
		x.advance(24*time.Hour + time.Minute)
		// Four runs die mid-mint with their leases live: the organization is at its
		// cap, so the fifth due rotation does not start.
		for i := 0; i < 4; i++ {
			x.crashOnce("create:before")
			if !x.crashSweep(fmt.Sprintf("wrk_%d", i)) {
				t.Fatalf("no crash for run %d", i+1)
			}
		}
		x.requireSweep("wrk_late", false)
		if got := queryInt(t, db, "SELECT COUNT(*) FROM rotation_runs"); got != 4 {
			t.Fatalf("%d runs started under a full cap", got)
		}
		// Once the dead leases lapse the cap clears and work resumes.
		x.advance(rotationLeaseTermForTest + time.Second)
		x.requireSweep("wrk_late", true)
	})
}
