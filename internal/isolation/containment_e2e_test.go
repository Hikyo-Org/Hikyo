package isolation

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/authn"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Principal containment, slice S1 (incident-containment ADR, O1 option B): the
// state, its gates, and the one recovery lookup. These tests drive the real
// services on both engines.

const containedPassword = "correct horse battery staple"

type containmentSubject struct {
	db       *store.DB
	auth     *service.Auth
	admin    admin
	subject  domain.PrincipalID
	account  string
	username string
	token    string
	handle   []byte
	email    string
}

// containmentFixture creates a bootstrap administrator and a second human who
// holds two instance capabilities (instance-config and manage-members), a
// password, a live session, a passkey user handle and a verified email.
func containmentFixture(t *testing.T, db *store.DB) *containmentSubject {
	t.Helper()
	ctx := t.Context()
	adm := bootstrapAdmin(t, db, adminOpts{username: "operator", displayName: "Operator", password: containedPassword, login: true})
	s := &containmentSubject{db: db, auth: adm.auth, admin: adm, subject: "usr_contained", account: "acc_usr_contained", username: "contained-user", handle: []byte("handle-contained-0123456789abcdef"), email: "contained@example.test"}
	err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		if err := az.CreateHumanPrincipal(ctx, s.subject, time.Now()); err != nil {
			return err
		}
		if err := az.CreateAccount(ctx, authz.Account{ID: s.account, PrincipalID: s.subject, Username: s.username, DisplayName: "Contained", CreatedAt: time.Now()}); err != nil {
			return err
		}
		for i, c := range []domain.Capability{domain.CapInstanceConfig, domain.CapManageMembers} {
			if err := az.CreateGrant(ctx, "grant_contained_"+string(rune('a'+i)), s.subject, domain.Grant{Capability: c}, time.Now()); err != nil {
				return err
			}
		}
		_, err := az.SetWebAuthnUserHandle(ctx, s.account, s.handle)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	execRaw(t, db, `UPDATE accounts SET email = '`+s.email+`', email_verified_at = `+ts+` WHERE id = '`+s.account+`'`)
	s.establish(t, containedPassword)
	login, err := s.auth.LocalLogin(ctx, s.username, containedPassword, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	s.token = login.SessionToken
	return s
}

func (s *containmentSubject) establish(t *testing.T, password string) {
	t.Helper()
	reset, err := s.auth.BreakGlassResetCredential(t.Context(), string(s.subject), "file")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.auth.EstablishCredential(t.Context(), reset.Authority, password); err != nil {
		t.Fatal(err)
	}
}

func (s *containmentSubject) contain(t *testing.T) {
	t.Helper()
	containPrincipalForTest(t, s.db, s.subject)
}

func (s *containmentSubject) release(t *testing.T) {
	t.Helper()
	releaseContainmentForTest(t, s.db, s.subject)
}

// containPrincipalForTest and releaseContainmentForTest are the only callers of
// the setters in S1: no production path contains a principal until the incident
// service lands.
func containPrincipalForTest(t *testing.T, db *store.DB, p domain.PrincipalID) {
	t.Helper()
	err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return az.ContainPrincipal(ctx, p, time.Now(), "inc_test")
	})
	if err != nil {
		t.Fatal(err)
	}
}

func releaseContainmentForTest(t *testing.T, db *store.DB, p domain.PrincipalID) {
	t.Helper()
	err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		return az.ReleasePrincipalContainment(ctx, p)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// reach reports whether each gated read still reaches the subject.
type containmentReach struct {
	login, session, generation, chokepoint, census, passkey, email bool
}

func (s *containmentSubject) reach(t *testing.T) containmentReach {
	t.Helper()
	ctx := t.Context()
	var r containmentReach
	_, err := s.auth.LocalLogin(ctx, s.username, containedPassword, service.ArtifactCLI)
	r.login = err == nil
	_, err = s.auth.Identity(ctx, s.token)
	r.session = err == nil
	err = tx.Write(ctx, s.db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		_, err := az.PrincipalGeneration(ctx, s.subject)
		r.generation = err == nil
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		_, err = az.Authorize(ctx, authz.Identity{Principal: s.subject}, authz.OpOrgList, domain.Scope{})
		r.chokepoint = err == nil
		if err != nil && !errors.Is(err, domain.ErrUnauthorized) {
			return err
		}
		holders, err := az.ManageMembersHolders(ctx, "")
		r.census = slices.Contains(holders, s.subject)
		if err != nil {
			return err
		}
		_, err = az.AccountByWebAuthnUserHandle(ctx, s.handle)
		r.passkey = err == nil
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		_, err = az.AccountByEmail(ctx, s.email)
		r.email = err == nil
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestContainmentRefusesEveryAuthorityAndReleaseNeverRevivesASession(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := containmentFixture(t, db)
		ctx := t.Context()
		all := containmentReach{login: true, session: true, generation: true, chokepoint: true, census: true, passkey: true, email: true}
		if got := s.reach(t); got != all {
			t.Fatalf("before containment: %+v, want every path open", got)
		}
		generation := func() int64 {
			return queryInt(t, db, `SELECT session_generation FROM principals WHERE id = '`+string(s.subject)+`'`)
		}
		gen := generation()
		sessions := queryInt(t, db, `SELECT COUNT(*) FROM sessions WHERE principal_id = '`+string(s.subject)+`'`)

		s.contain(t)
		if got := s.reach(t); got != (containmentReach{}) {
			t.Fatalf("contained: %+v, want every path closed", got)
		}
		// The refusal has the same shape as an unknown user, so a contained
		// account is not an oracle for existence.
		_, contained := s.auth.LocalLogin(ctx, s.username, containedPassword, service.ArtifactCLI)
		_, unknown := s.auth.LocalLogin(ctx, "no-such-user", containedPassword, service.ArtifactCLI)
		if !errors.Is(contained, domain.ErrUnauthenticated) || !errors.Is(unknown, domain.ErrUnauthenticated) || contained.Error() != unknown.Error() {
			t.Fatalf("contained login %v differs from unknown-user login %v", contained, unknown)
		}
		// Containment advances the generation (so nothing minted before it can
		// outlive it) but removes and revokes no row: grants and session rows stay.
		if got := generation(); got != gen+1 {
			t.Fatalf("containment moved the session generation %d -> %d, want one advance", gen, got)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM grants WHERE principal_id = '`+string(s.subject)+`'`) != 2 ||
			queryInt(t, db, `SELECT COUNT(*) FROM sessions WHERE principal_id = '`+string(s.subject)+`'`) != sessions {
			t.Fatal("containment removed or revoked authority")
		}
		// A second contain of the same principal is refused, not silently a no-op.
		err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			return az.ContainPrincipal(ctx, s.subject, time.Now(), "inc_other")
		})
		if !errors.Is(err, authn.ErrContainmentUnchanged) {
			t.Fatalf("a second contain: %v, want ErrContainmentUnchanged", err)
		}
		if got := generation(); got != gen+1 {
			t.Fatalf("a refused contain moved the generation to %d", got)
		}

		s.release(t)
		if got := generation(); got != gen+2 {
			t.Fatalf("release left the generation at %d, want %d", got, gen+2)
		}
		// Release restores sign-in and the lookups, never a session minted
		// before containment: O4.
		want := all
		want.session = false
		if got := s.reach(t); got != want {
			t.Fatalf("after release: %+v, want everything but the pre-containment session", got)
		}
		fresh, err := s.auth.LocalLogin(ctx, s.username, containedPassword, service.ArtifactCLI)
		if err != nil {
			t.Fatalf("a fresh sign-in after release: %v", err)
		}
		if _, err := s.auth.Identity(ctx, fresh.SessionToken); err != nil {
			t.Fatalf("the fresh session is not live: %v", err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM principals WHERE id = '`+string(s.subject)+`' AND containment_state = 'none' AND contained_at IS NULL AND contained_incident_id IS NULL`) != 1 {
			t.Fatal("release left containment columns set")
		}
		// Releasing a principal that is not contained is refused too.
		err = tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			return az.ReleasePrincipalContainment(ctx, s.subject)
		})
		if !errors.Is(err, authn.ErrContainmentUnchanged) {
			t.Fatalf("releasing an uncontained principal: %v, want ErrContainmentUnchanged", err)
		}
	})
}

// A delegation and a passkey session minted before containment stay dead after
// release, a contained issuer's delegation is refused while contained, and a
// fresh sign-in after release mints working authority again.
func TestContainmentReleaseNeverRevivesDelegationsOrSessions(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		f := ceremonyFixture(t, db, "contain-delegation")
		subject := f.admin.boot.PrincipalID
		delegation, session := mintDeveloperWithPasskey(t, db, &f, time.Hour)
		delivery := deliverySvc(t, db)
		fetch := func(bearer string) error {
			_, err := delivery.FetchAs(ctx, service.Bearer(bearer), envScope(envA1), "", service.FetchOptions{})
			return err
		}
		if err := fetch(delegation.Value); err != nil {
			t.Fatalf("pre-containment delegation: %v", err)
		}
		if authenticateCaller(t, db, session).Principal == "" {
			t.Fatal("pre-containment session is not live")
		}

		// A password proof awaiting its second factor is also pre-containment
		// authority: it mints a session without consulting the generation.
		pending, err := f.admin.auth.LocalLogin(ctx, "contain-delegation", ceremonyPassword, service.ArtifactBrowser)
		if err != nil || pending.Challenge == nil {
			t.Fatalf("pending proof fixture: %+v, %v", pending, err)
		}
		pendingProofs := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM login_challenges WHERE consumed_at IS NULL`)
		}
		if pendingProofs() == 0 {
			t.Fatal("fixture left no pending password proof")
		}

		containPrincipalForTest(t, db, subject)
		if got := pendingProofs(); got != 0 {
			t.Fatalf("containment left %d pending password proofs", got)
		}
		for name, bearer := range map[string]string{"delegation": delegation.Value, "session": session} {
			if authenticateCaller(t, db, bearer).Principal != "" {
				t.Fatalf("a contained issuer's %s authenticated", name)
			}
		}
		if err := fetch(delegation.Value); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("a contained issuer's delegation delivered: %v", err)
		}
		if _, err := discoverableLogin(t, f.admin.auth, ctx, f.device); err == nil {
			t.Fatal("a contained account signed in with its passkey")
		}

		releaseContainmentForTest(t, db, subject)
		for name, bearer := range map[string]string{"delegation": delegation.Value, "session": session} {
			if authenticateCaller(t, db, bearer).Principal != "" {
				t.Fatalf("release revived the pre-containment %s", name)
			}
		}
		if err := fetch(delegation.Value); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("release revived the pre-containment delegation: %v", err)
		}
		login, err := discoverableLogin(t, f.admin.auth, ctx, f.device)
		if err != nil {
			t.Fatalf("a fresh passkey sign-in after release: %v", err)
		}
		f.admin.token = login.SessionToken
		fresh, _ := mintDeveloperWithPasskey(t, db, &f, time.Hour)
		if err := fetch(fresh.Value); err != nil {
			t.Fatalf("a delegation minted after release: %v", err)
		}
	})
}

// Effective authority needs BOTH states: privacy active AND containment none.
// Releasing either one while the other still holds must not restore anything.
func TestContainmentAndPrivacyBothRequired(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := containmentFixture(t, db)
		ctx := t.Context()
		closed := containmentReach{}
		s.contain(t)
		if _, err := s.auth.ApplyPrivacySubject(ctx, string(s.subject), "restrict", ""); err != nil {
			t.Fatal(err)
		}
		s.release(t)
		if got := s.reach(t); got != closed {
			t.Fatalf("restricted after containment release: %+v, want closed", got)
		}
		s.contain(t)
		if _, err := s.auth.ApplyPrivacySubject(ctx, string(s.subject), "release", ""); err != nil {
			t.Fatal(err)
		}
		if got := s.reach(t); got != closed {
			t.Fatalf("contained after privacy release: %+v, want closed", got)
		}
		s.release(t)
		if _, err := s.auth.LocalLogin(ctx, s.username, containedPassword, service.ArtifactCLI); err != nil {
			t.Fatalf("both released: %v", err)
		}
	})
}

// Reset issuance and establishment must work for a contained account (they
// carry no grants), while the account still cannot sign in afterwards, and a
// restricted account stays out of the recovery lookup.
func TestContainmentRecoveryLookupServesResetAndEstablishmentOnly(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := containmentFixture(t, db)
		ctx := t.Context()
		s.contain(t)
		const replacement = "an entirely different passphrase"
		s.establish(t, replacement)
		if _, err := s.auth.LocalLogin(ctx, s.username, replacement, service.ArtifactCLI); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("contained account signed in after establishment: %v", err)
		}
		s.release(t)
		if _, err := s.auth.LocalLogin(ctx, s.username, replacement, service.ArtifactCLI); err != nil {
			t.Fatalf("the established credential did not survive containment: %v", err)
		}
		if _, err := s.auth.LocalLogin(ctx, s.username, containedPassword, service.ArtifactCLI); err == nil {
			t.Fatal("the pre-reset password still works")
		}

		// Privacy stays gated in the recovery lookup.
		if _, err := s.auth.ApplyPrivacySubject(ctx, string(s.subject), "restrict", ""); err != nil {
			t.Fatal(err)
		}
		if _, err := s.auth.BreakGlassResetCredential(ctx, string(s.subject), "file"); err == nil {
			t.Fatal("reset reached a privacy-restricted account")
		}
	})
}

// Containment consumes every outstanding establishment authority of the
// account in its own transaction, so an authority minted before containment is
// unusable after it. One minted afterwards (a reset issued to a contained
// account) works, and the account still cannot sign in. EstablishCredential
// additionally refuses, for a contained account, any authority created before
// contained_at even if it is still unconsumed.
func TestContainmentConsumesOutstandingAuthorities(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := containmentFixture(t, db)
		ctx := t.Context()
		unconsumed := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities WHERE account_id = '`+s.account+`' AND consumed_at IS NULL`)
		}
		before, err := s.auth.BreakGlassResetCredential(ctx, string(s.subject), "file")
		if err != nil {
			t.Fatal(err)
		}
		if unconsumed() != 1 {
			t.Fatalf("fixture: %d outstanding authorities, want 1", unconsumed())
		}
		s.contain(t)
		if unconsumed() != 0 {
			t.Fatalf("containment left %d authorities outstanding", unconsumed())
		}
		if err := s.auth.EstablishCredential(ctx, before.Authority, "a password set from a pre-containment authority"); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("a pre-containment authority established a credential: %v", err)
		}

		// An authority issued to the contained account works, and signing in
		// still fails, because the account lookup is gated.
		const replacement = "a password established while contained"
		s.establish(t, replacement)
		if _, err := s.auth.LocalLogin(ctx, s.username, replacement, service.ArtifactCLI); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("a contained account signed in: %v", err)
		}

		// Defense in depth: an authority that is still unconsumed but older than
		// contained_at (a restore, a bypassed sweep) is refused uniformly.
		stale, err := s.auth.BreakGlassResetCredential(ctx, string(s.subject), "file")
		if err != nil {
			t.Fatal(err)
		}
		execRaw(t, db, `UPDATE credential_authorities SET created_at = `+ts+` WHERE account_id = '`+s.account+`' AND consumed_at IS NULL`)
		if err := s.auth.EstablishCredential(ctx, stale.Authority, "a password from a stale authority"); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("an unconsumed pre-containment authority was accepted: %v", err)
		}
		if unconsumed() != 1 {
			t.Fatalf("the refused authority was consumed: %d outstanding", unconsumed())
		}
		// The refusal is the one an invalid authority gets.
		staleErr := s.auth.EstablishCredential(ctx, stale.Authority, "a password from a stale authority")
		invalidErr := s.auth.EstablishCredential(ctx, "hk_not_an_authority", "a password from a stale authority")
		if staleErr == nil || invalidErr == nil || staleErr.Error() != invalidErr.Error() {
			t.Fatalf("stale authority refusal %v differs from invalid authority refusal %v", staleErr, invalidErr)
		}

		s.release(t)
		if _, err := s.auth.LocalLogin(ctx, s.username, replacement, service.ArtifactCLI); err != nil {
			t.Fatalf("the credential established while contained: %v", err)
		}
	})
}

// A privacy-restricted principal is invisible to the recovery lookup, but its
// outstanding authorities must still be consumed by containment: otherwise the
// sequence restricted, contained, released, privacy-released would leave a
// pre-containment authority redeemable.
func TestContainmentSweepsAuthoritiesOfARestrictedPrincipal(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := containmentFixture(t, db)
		ctx := t.Context()
		unconsumed := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities WHERE account_id = '`+s.account+`' AND consumed_at IS NULL`)
		}
		before, err := s.auth.BreakGlassResetCredential(ctx, string(s.subject), "file")
		if err != nil {
			t.Fatal(err)
		}
		if unconsumed() != 1 {
			t.Fatalf("fixture: %d outstanding authorities, want 1", unconsumed())
		}
		setPrivacy := func(state string) {
			execRaw(t, db, `UPDATE principals SET privacy_state = '`+state+`' WHERE id = '`+string(s.subject)+`'`)
		}
		setPrivacy("restricted")
		s.contain(t)
		if unconsumed() != 0 {
			t.Fatalf("containing a restricted principal left %d authorities outstanding", unconsumed())
		}
		s.release(t)
		setPrivacy("active")
		if err := s.auth.EstablishCredential(ctx, before.Authority, "a password set from a pre-containment authority"); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("a pre-containment authority survived restricted, contained, released: %v", err)
		}
	})
}

// holdWriteWhile runs a write transaction that applies hold and then stays open
// until the returned release is called. On sqlite that holds the single writer;
// on postgres the principal row lock hold takes. Either way a concurrent write
// transaction that touches the same principal waits for the commit.
func holdWriteWhile(t *testing.T, db *store.DB, hold func(context.Context, *authz.TxAuthorizer) error) func() error {
	t.Helper()
	held, done, release := make(chan struct{}), make(chan error, 1), make(chan struct{})
	go func() {
		first := true
		done <- tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			if err := hold(ctx, az); err != nil {
				return err
			}
			if first {
				first = false
				close(held)
			}
			<-release
			return nil
		})
	}()
	select {
	case <-held:
	case err := <-done:
		t.Fatalf("the holding transaction ended early: %v", err)
	}
	return func() error {
		close(release)
		return <-done
	}
}

// writerWaiters counts write transactions blocked behind another one: sqlite
// connection-pool waits on the single writer, or postgres sessions waiting on a
// lock. Both are observable without a production hook.
func writerWaiters(t *testing.T, db *store.DB) int64 {
	t.Helper()
	if db.Engine() == store.EnginePostgres {
		return queryInt(t, db, `SELECT COUNT(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`)
	}
	return db.SQLiteWrite().Stats().WaitCount
}

// runBehindHolder runs fn on its own goroutine while a write transaction applying
// hold owns the writer lock, waits until fn is observably blocked behind that
// holder (so its read and match phases have finished and its write phase is
// waiting), then commits the holder and returns fn's result. fn's write phase
// therefore resolves its inputs after the holder's change landed. If fn ends
// before it was ever seen blocked, the interleaving did not happen and the test
// fails instead of passing vacuously.
func runBehindHolder(t *testing.T, db *store.DB, hold func(context.Context, *authz.TxAuthorizer) error, fn func() error) error {
	t.Helper()
	release := holdWriteWhile(t, db, hold)
	base := writerWaiters(t, db)
	result := make(chan error, 1)
	go func() { result <- fn() }()
	deadline := time.After(30 * time.Second)
	for writerWaiters(t, db) <= base {
		select {
		case err := <-result:
			_ = release()
			t.Fatalf("fn finished without ever blocking behind the holder (err=%v): the interleaving did not occur", err)
		case <-deadline:
			_ = release()
			t.Fatal("fn never blocked behind the holder within 30s")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := release(); err != nil {
		t.Fatalf("holder: %v", err)
	}
	return <-result
}

// Password sign-in resolves the account in a read phase and mints in a write
// phase. Containment landing in between must mint nothing: the shared session
// sink reads the gated generation, which a contained principal does not have.
func TestContainmentBetweenLoginReadAndWriteMintsNothing(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := containmentFixture(t, db)
		sessions := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM sessions WHERE principal_id = '`+string(s.subject)+`'`)
		}
		before := sessions()
		contain := func(ctx context.Context, az *authz.TxAuthorizer) error {
			return az.ContainPrincipal(ctx, s.subject, time.Now(), "inc_test")
		}
		err := runBehindHolder(t, db, contain, func() error {
			_, err := s.auth.LocalLogin(t.Context(), s.username, containedPassword, service.ArtifactCLI)
			return err
		})
		if err == nil {
			t.Fatal("a sign-in racing containment succeeded")
		}
		if got := sessions(); got != before {
			t.Fatalf("a sign-in racing containment minted a session: %d -> %d", before, got)
		}
		s.release(t)
		if _, err := s.auth.LocalLogin(t.Context(), s.username, containedPassword, service.ArtifactCLI); err != nil {
			t.Fatalf("sign-in after release: %v", err)
		}
	})
}

// Containment landing between the recovery path's read phase and its write phase
// (the account was resolved ungated-by-state in phase 1) must refuse through the
// uniform unknown-subject path in phase 3: no code spent, no authority minted, no
// generation advance of its own. A control run with nothing interleaved proves
// the same recovery otherwise succeeds. The interleaving is real concurrency
// ordered by the writer lock rather than a hook: the holder owns the write lock
// while ConsumeRecoveryCode runs its read and match phases, then commits, and the
// blocked write phase must re-resolve the account.
func TestContainmentBetweenRecoveryReadAndWriteRefuses(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := containmentFixture(t, db)
		ctx := t.Context()
		codes, _, err := s.auth.GenerateRecoveryCodes(ctx, s.token, containedPassword)
		if err != nil || len(codes) < 2 {
			t.Fatalf("recovery codes: %d, %v", len(codes), err)
		}
		consume := func(code string) func() error {
			return func() error {
				_, err := s.auth.ConsumeRecoveryCode(ctx, s.username, code)
				return err
			}
		}
		recover := func(code string, hold func(context.Context, *authz.TxAuthorizer) error) error {
			return runBehindHolder(t, db, hold, consume(code))
		}
		// Control: the same recovery with nothing interleaved succeeds.
		if err := consume(codes[0])(); err != nil {
			t.Fatalf("control recovery: %v", err)
		}
		// The control spent a code and advanced the generation; sign in afresh.
		recoveryAuthorities := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities WHERE account_id = '`+s.account+`' AND issued_by = 'recovery'`)
		}
		generation := func() int64 {
			return queryInt(t, db, `SELECT session_generation FROM principals WHERE id = '`+string(s.subject)+`'`)
		}
		batchVersion := func() int64 {
			return queryInt(t, db, `SELECT row_version FROM recovery_codes WHERE account_id = '`+s.account+`'`)
		}
		authorities, gen, version := recoveryAuthorities(), generation(), batchVersion()

		contain := func(ctx context.Context, az *authz.TxAuthorizer) error {
			return az.ContainPrincipal(ctx, s.subject, time.Now(), "inc_test")
		}
		if err := recover(codes[1], contain); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("recovery after a concurrent containment: %v, want the uniform refusal", err)
		}
		if got := recoveryAuthorities(); got != authorities {
			t.Fatalf("a contained account minted a recovery authority: %d -> %d", authorities, got)
		}
		if got := generation(); got != gen+1 {
			t.Fatalf("generation %d -> %d, want only the containment advance", gen, got)
		}
		if got := batchVersion(); got != version {
			t.Fatalf("a contained account spent a recovery code: batch version %d -> %d", version, got)
		}
		// Nothing was spent: once released, the same code still recovers.
		s.release(t)
		if _, err := s.auth.ConsumeRecoveryCode(ctx, s.username, codes[1]); err != nil {
			t.Fatalf("the unspent code after release: %v", err)
		}
	})
}

// createHuman seeds a human principal and account with no authority.
func createHuman(t *testing.T, db *store.DB, principal domain.PrincipalID, username string) string {
	t.Helper()
	account := "acc_" + string(principal)
	err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		if err := az.CreateHumanPrincipal(ctx, principal, time.Now()); err != nil {
			return err
		}
		return az.CreateAccount(ctx, authz.Account{ID: account, PrincipalID: principal, Username: username, DisplayName: username, CreatedAt: time.Now()})
	})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

// authorizes reports whether the chokepoint admits the principal for op at scope.
func authorizes(t *testing.T, db *store.DB, principal domain.PrincipalID, op authz.Operation, scope domain.Scope) bool {
	t.Helper()
	var allowed bool
	err := tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		_, err := az.Authorize(ctx, authz.Identity{Principal: principal, Class: domain.ClassHuman}, op, scope)
		allowed = err == nil
		if err != nil && !errors.Is(err, domain.ErrNotFound) && !errors.Is(err, domain.ErrUnauthorized) {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return allowed
}

// Every authority source the chokepoint loads is hidden while the principal is
// contained and visible again after release, one source at a time: a temporary
// access grant, a member access rule, the org-scoped manage-members census, and
// the personal-owner fallback. Each fixture holds authority through that source
// alone, so each assertion isolates one gate.
func TestContainmentHidesEachAuthoritySource(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		ctx := t.Context()
		env := scopeEnv(orgA, prjA1, envA1)
		type source struct {
			name  string
			who   domain.PrincipalID
			held  func() bool
			setup func()
		}
		createHuman(t, db, "usr_src_access", "src-access")
		createHuman(t, db, "usr_src_rule", "src-rule")
		createHuman(t, db, "usr_src_census", "src-census")
		sources := []source{
			{name: "access_grants", who: "usr_src_access",
				setup: func() {
					const future = `'2099-01-01T00:00:00.000000Z'`
					execRaw(t, db, `INSERT INTO access_requests(id,org_id,project_id,environment_id,policy_id,policy_version,requester_principal_id,capabilities,duration_seconds,reason,state,created_at,review_expires_at,granted_at,expires_at) `+
						`VALUES ('ar_src_access','org_a','prj_a1','env_a1','policy_fixture',1,'usr_src_access','["read"]',1,'containment fixture','granted',`+ts+`,`+ts+`,`+ts+`,`+future+`)`)
					execRaw(t, db, `INSERT INTO access_grants(id,principal_id,capability,org_id,project_id,env_id,request_id,created_at,expires_at) `+
						`VALUES ('ag_src_access','usr_src_access','read','org_a','prj_a1','env_a1','ar_src_access',`+ts+`,`+future+`)`)
				},
				held: func() bool { return authorizes(t, db, "usr_src_access", authz.OpPersonalValueList, env) }},
			{name: "member access rule", who: "usr_src_rule",
				setup: func() {
					mode, envs := onlyEnvs(envA1)
					operator := service.Bearer(sessionWithWindows(t, db, root))
					if _, err := (&service.Rules{DB: db}).Create(ctx, operator, service.RuleSpec{Target: "usr_src_rule", Capability: domain.CapRead, Org: orgA, Where: whereIn(mode, envs, domain.AxisAll)}); err != nil {
						t.Fatal(err)
					}
				},
				held: func() bool { return authorizes(t, db, "usr_src_rule", authz.OpPersonalValueList, env) }},
			{name: "org manage-members census", who: "usr_src_census",
				setup: func() {
					err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
						return az.CreateGrant(ctx, "grant_src_census", "usr_src_census", domain.Grant{Capability: domain.CapManageMembers, Scope: domain.Scope{Org: orgA}}, time.Now())
					})
					if err != nil {
						t.Fatal(err)
					}
				},
				held: func() bool {
					var holds bool
					err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
						holders, err := az.ManageMembersHolders(ctx, string(orgA))
						holds = slices.Contains(holders, "usr_src_census")
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
					return holds
				}},
		}
		for _, src := range sources {
			src.setup()
			if !src.held() {
				t.Fatalf("%s: positive control failed, the fixture holds no authority", src.name)
			}
			containPrincipalForTest(t, db, src.who)
			if src.held() {
				t.Fatalf("%s: a contained principal's authority is still loaded", src.name)
			}
			releaseContainmentForTest(t, db, src.who)
			if !src.held() {
				t.Fatalf("%s: release did not restore the authority", src.name)
			}
		}
	})
}

// The personal-owner fallback lets an owner list their own personal values
// whatever their grants. A contained owner loses it, and a released one has it
// again; the owner's grants are removed so the fallback is the only path.
func TestContainmentHidesThePersonalOwnerFallback(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "personal-fallback")
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		owner := e.admin.boot.PrincipalID
		env := scopeEnv(orgA, prjA1, envA1)
		execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id IN (SELECT id FROM grants WHERE principal_id = '`+string(owner)+`' AND org_id IS NOT NULL)`)
		execRaw(t, db, `DELETE FROM grants WHERE principal_id = '`+string(owner)+`' AND org_id IS NOT NULL`)
		if !authorizes(t, db, owner, authz.OpPersonalValueList, env) {
			t.Fatal("positive control: the owner's fallback does not admit their own list")
		}
		containPrincipalForTest(t, db, owner)
		if authorizes(t, db, owner, authz.OpPersonalValueList, env) {
			t.Fatal("a contained owner kept the personal-owner fallback")
		}
		releaseContainmentForTest(t, db, owner)
		if !authorizes(t, db, owner, authz.OpPersonalValueList, env) {
			t.Fatal("release did not restore the personal-owner fallback")
		}
	})
}

// A network reset reaches a contained single-org target when the caller holds
// credential-reset for that org: the grants that classify the target are still
// read, the authority is minted, and establishing it leaves the account unable
// to sign in until release.
func TestContainmentNetworkResetSucceedsForOrgScopedCaller(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		adm := bootstrapAdmin(t, db, adminOpts{username: "operator", displayName: "Operator", password: containedPassword, login: true})
		ctx := t.Context()
		const target, caller domain.PrincipalID = "usr_reset_target", "usr_reset_caller"
		account := createHuman(t, db, target, "reset-target")
		createHuman(t, db, caller, "reset-caller")
		err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			if err := az.CreateGrant(ctx, "grant_reset_target", target, domain.Grant{Capability: domain.CapRead, Scope: domain.Scope{Org: orgA}}, time.Now()); err != nil {
				return err
			}
			return az.CreateGrant(ctx, "grant_reset_caller", caller, domain.Grant{Capability: domain.CapCredentialReset, Scope: domain.Scope{Org: orgA}}, time.Now())
		})
		if err != nil {
			t.Fatal(err)
		}
		operator := service.Bearer(sessionWithWindows(t, db, caller))
		containPrincipalForTest(t, db, target)
		reset, err := adm.auth.ResetCredential(ctx, operator, string(target), "response")
		if err != nil {
			t.Fatalf("the org-scoped reset of a contained single-org target: %v", err)
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities WHERE account_id = '`+account+`' AND issued_by = 'credential-reset' AND consumed_at IS NULL`) != 1 {
			t.Fatal("the network reset minted no authority")
		}
		const password = "a password established by network reset"
		if err := adm.auth.EstablishCredential(ctx, reset.Authority, password); err != nil {
			t.Fatalf("establishing the network reset authority: %v", err)
		}
		if _, err := adm.auth.LocalLogin(ctx, "reset-target", password, service.ArtifactCLI); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("a contained account signed in after a network reset: %v", err)
		}
		releaseContainmentForTest(t, db, target)
		if _, err := adm.auth.LocalLogin(ctx, "reset-target", password, service.ArtifactCLI); err != nil {
			t.Fatalf("sign-in after release: %v", err)
		}
	})
}

// Each gated lookup refuses a contained account on its own, and still does so
// when the session generation gate is defeated by restoring the old value: no
// single gate is carrying the refusal. Only the recovery lookup reaches it.
func TestContainmentLookupsRefuseWithoutTheGenerationGate(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := containmentFixture(t, db)
		ctx := t.Context()
		gen := queryInt(t, db, `SELECT session_generation FROM principals WHERE id = '`+string(s.subject)+`'`)
		s.contain(t)
		execRaw(t, db, `UPDATE principals SET session_generation = `+strconv.FormatInt(gen, 10)+` WHERE id = '`+string(s.subject)+`'`)

		lookups := map[string]func(context.Context, *authz.TxAuthorizer) error{
			"AccountByUsername": func(ctx context.Context, az *authz.TxAuthorizer) error {
				_, err := az.AccountByUsername(ctx, s.username)
				return err
			},
			"AccountByID": func(ctx context.Context, az *authz.TxAuthorizer) error {
				_, err := az.AccountByID(ctx, s.account)
				return err
			},
			"AccountByPrincipal": func(ctx context.Context, az *authz.TxAuthorizer) error {
				_, err := az.AccountByPrincipal(ctx, s.subject)
				return err
			},
			"AccountByEmail": func(ctx context.Context, az *authz.TxAuthorizer) error {
				_, err := az.AccountByEmail(ctx, s.email)
				return err
			},
			"AccountByWebAuthnUserHandle": func(ctx context.Context, az *authz.TxAuthorizer) error {
				_, err := az.AccountByWebAuthnUserHandle(ctx, s.handle)
				return err
			},
			"PrincipalGeneration": func(ctx context.Context, az *authz.TxAuthorizer) error {
				_, err := az.PrincipalGeneration(ctx, s.subject)
				return err
			},
		}
		for name, lookup := range lookups {
			err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error { return lookup(ctx, az) })
			if !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("%s reached a contained account: %v", name, err)
			}
		}
		// The recovery lookup is the one exception, by ID and by principal.
		err := tx.Write(ctx, db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
			byID, err := az.RecoveryAccountByID(ctx, s.account)
			if err != nil {
				return err
			}
			byPrincipal, err := az.RecoveryAccountByPrincipal(ctx, s.subject)
			if err != nil {
				return err
			}
			if byID.ID != s.account || byPrincipal.ID != s.account {
				t.Fatalf("recovery lookup returned %q and %q", byID.ID, byPrincipal.ID)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("recovery lookup: %v", err)
		}
		// With the generation restored, sign-in and the pre-containment session
		// still fail, at the account lookup and the session resolution.
		if _, err := s.auth.LocalLogin(ctx, s.username, containedPassword, service.ArtifactCLI); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("sign-in with the generation defeated: %v", err)
		}
		if _, err := s.auth.Identity(ctx, s.token); err == nil {
			t.Fatal("the pre-containment session resolved with the generation defeated")
		}
	})
}

// The network reset refuses an instance-capability target by its GRANTS. A
// contained holder keeps those grants (containment removes none), so the reset
// must still classify and refuse it, with the instance-target cause, rather
// than treat the hidden authority as absent and mint a reset authority.
func TestContainmentNetworkResetStillRefusesInstanceCapabilityHolder(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := containmentFixture(t, db)
		ctx := t.Context()
		grantInstanceCapability(t, db, root, domain.CapCredentialReset)
		operator := service.Bearer(sessionWithWindows(t, db, root))
		attempt := func() error {
			_, err := s.auth.ResetCredential(ctx, operator, string(s.subject), "response")
			return err
		}
		const cause = `%instance-capability-target%`
		count := func() int64 {
			return queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.credential_reset_issued' AND payload LIKE '`+cause+`'`)
		}
		if err := attempt(); !errors.Is(err, service.ErrNoResetTarget) || count() != 1 {
			t.Fatalf("uncontained holder: err=%v causes=%d", err, count())
		}
		s.contain(t)
		if err := attempt(); !errors.Is(err, service.ErrNoResetTarget) || count() != 2 {
			t.Fatalf("contained holder was not classified by its grants: err=%v causes=%d", err, count())
		}
		if queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities WHERE account_id = '`+s.account+`' AND issued_by = 'credential-reset'`) != 0 {
			t.Fatal("a network reset authority was minted for a contained instance holder")
		}
	})
}

// A restore carries the containment state over untouched: the restored
// instance must not quietly release a held principal, and restore writes no
// containment receipt of its own.
func TestContainmentSurvivesRestore(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := containmentFixture(t, db)
		s.contain(t)
		held := `SELECT COUNT(*) FROM principals WHERE id = '` + string(s.subject) + `' AND containment_state = 'contained' AND contained_incident_id = 'inc_test' AND contained_at IS NOT NULL`
		if queryInt(t, db, held) != 1 {
			t.Fatal("fixture did not record containment")
		}
		db = restoreIsolationFixture(t, db)
		if queryInt(t, db, held) != 1 {
			t.Fatal("restore did not preserve the contained state")
		}
		if got := queryInt(t, db, `SELECT COUNT(*) FROM principals WHERE containment_state <> 'none'`); got != 1 {
			t.Fatalf("restore changed other principals' containment: %d held", got)
		}
	})
}
