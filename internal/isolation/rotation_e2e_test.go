package isolation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Scheduled credential rotation configuration (#148, slice S1). Everything
// traverses the real service, authorizer and store on both engines; only the
// external PostgreSQL provider is replaced (and S1 never calls it). Every actor
// is a real signed-in session with a publish ceremony window: a rotation is a
// human act, and a LocalPrincipal has no session to consume a ceremony from.

const (
	rotPasswordKey = "key_rot_pw"
	rotUsernameKey = "key_rot_user"
	rotSecondKey   = "key_rot_pw2"
	rotSecondUser  = "key_rot_user2"
	rotAdminSecret = "rotation-admin-credential-do-not-log"
	// rotPublisher holds read and publish and nothing else: the principal that may
	// pause and delete a rotation but never create or resume one.
	rotPublisher = domain.PrincipalID("usr_rot_publisher")
	// rotNarrow holds the create formula through a key-narrowed rule only.
	rotNarrow = domain.PrincipalID("usr_rot_narrow")
)

type rotationFixture struct {
	t        *testing.T
	db       *store.DB
	svc      *service.CredentialRotations
	dyn      *service.Dynamic
	actor    service.Actor // alice, signed in, ceremony window open
	scope    domain.Scope
	env      domain.EnvID
	provider string
	now      time.Time
	seq      int
}

func rotationKey(t *testing.T, db *store.DB, id, name, classification string) {
	t.Helper()
	execRaw(t, db, `INSERT INTO keys (id, org_id, project_id, name, folder_path, classification, description, deprecated, deprecation_note, declaration, required_mode, forbidden_mode, group_id, created_at) VALUES ('`+id+`', 'org_a', 'prj_a1', '`+name+`', '', '`+classification+`', '', FALSE, '', '{"rule":{"type":"string"}}', 'none', 'none', NULL, `+ts+`)`)
}

func newRotationFixture(t *testing.T, db *store.DB) *rotationFixture {
	t.Helper()
	ctx := tctx(t)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_rot_manage','usr_alice','manage-identities','org_a',NULL,NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_rot_reveal','usr_alice','reveal','org_a',NULL,NULL,`+ts+`)`)
	rotationKey(t, db, rotPasswordKey, "ROT_DB_PASSWORD", "secret")
	rotationKey(t, db, rotUsernameKey, "ROT_DB_USER", "config")
	rotationKey(t, db, rotSecondKey, "ROT_DB_PASSWORD2", "secret")
	rotationKey(t, db, rotSecondUser, "ROT_DB_USER2", "config")

	// A fresh environment of its own, so protected or approval-covered state set up
	// by one test never leaks into another through the shared audit database.
	env, err := (&service.Environments{DB: db, Keyring: probeKeyring(t, db)}).Create(ctx, service.LocalPrincipal(alice), prjScope(), "rotation", nil)
	if err != nil {
		t.Fatalf("create rotation environment: %v", err)
	}
	f := &rotationFixture{
		t: t, db: db, env: domain.EnvID(env.ID),
		scope: domain.Scope{Org: orgA, Project: prjA1, Env: domain.EnvID(env.ID)},
		// The real clock: sessions and ceremony windows expire in real time, and the
		// service resolves the caller's session at its own clock.
		now: time.Now().UTC().Truncate(time.Second),
	}
	f.dyn = &service.Dynamic{
		DB: db, Keyring: probeKeyring(t, db), Runtime: store.NewDynamicRuntime(db),
		LeaseDeadline: 5 * time.Second,
		ProviderFactory: func(dynamic.Kind, string, string, string) (dynamic.Provider, error) {
			return fakeDynamicProvider{ctl: &fakeDynamicControl{}}, nil
		},
	}
	provider, err := f.dyn.Configure(ctx, service.LocalPrincipal(alice), prjScope(), service.CreateDynamicProviderRequest{
		Kind: string(dynamic.KindPostgres), Origin: "postgres://admin@db.rotation.example:5432/app",
		GrantRole: "app_rw", Credential: []byte(rotAdminSecret),
	})
	if err != nil {
		t.Fatalf("configure provider: %v", err)
	}
	f.provider = provider.ID
	auth := authService(t, db)
	// An environment's effective ceremony window falls back to the instance
	// default, which is zero in this harness unless set.
	auth.ReauthWindow = 15 * time.Minute
	f.svc = &service.CredentialRotations{DB: db, Auth: auth, Now: func() time.Time { return f.now }}
	f.actor = f.session(alice, true)
	return f
}

// session mints a signed-in session for principal and, when withWindow is set,
// an unbound reauthentication window over the rotation environment, which any
// publish ceremony intent consumes.
func (f *rotationFixture) session(principal domain.PrincipalID, withWindow bool) service.Actor {
	f.t.Helper()
	return service.Bearer(f.sessionToken(principal, withWindow))
}

// sessionToken is session's bearer artifact, for a test that drives the router.
func (f *rotationFixture) sessionToken(principal domain.PrincipalID, withWindow bool) string {
	f.t.Helper()
	f.seq++
	artifact, verifier, err := crypto.NewArtifact(crypto.ArtifactCLISession)
	if err != nil {
		f.t.Fatal(err)
	}
	now := time.Now().UTC()
	generation := int64(queryInt(f.t, f.db, "SELECT session_generation FROM principals WHERE id = '"+string(principal)+"'"))
	sessionID := fmt.Sprintf("ses_rot_%s_%d", principal, f.seq)
	if err := tx.Write(f.t.Context(), f.db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		if err := az.MintSession(ctx, authz.NewSession{
			ID: sessionID, PrincipalID: principal, Verifier: verifier, Artifact: "cli",
			SessionGeneration: generation, CredentialEpoch: epoch,
			AuthMethod: "local-passkey", Factors: `["webauthn","mfa"]`,
			AuthenticatedAt: now, CreatedAt: now, IdleExpiresAt: now.Add(30 * time.Hour),
			AbsoluteExpiresAt: now.Add(48 * time.Hour), SourceIP: "127.0.0.1", UserAgent: "rotation-e2e",
		}); err != nil {
			return err
		}
		if !withWindow {
			return nil
		}
		return az.OpenReauthWindow(ctx, authz.NewReauthWindow{
			ID: fmt.Sprintf("raw_rot_%s_%d", principal, f.seq), SessionID: sessionID, EnvironmentID: string(f.env),
			FactorClass: "totp", SingleDecision: false, AuthenticatedAt: now,
			WindowExpiresAt: now.Add(24 * time.Hour), HardExpiresAt: now.Add(48 * time.Hour),
			CredentialEpoch: epoch, CreatedAt: now,
		})
	}); err != nil {
		f.t.Fatalf("mint session: %v", err)
	}
	return artifact
}

func (f *rotationFixture) request(key, user string) service.CreateRotationRequest {
	return service.CreateRotationRequest{
		ProviderID: f.provider, KeyID: key, UsernameKeyID: user,
		IntervalSeconds: 24 * 3600, RevocationDelaySeconds: 3600,
	}
}

func (f *rotationFixture) create(key, user string) service.RotationView {
	f.t.Helper()
	v, err := f.svc.Create(tctx(f.t), f.actor, f.scope, f.request(key, user))
	if err != nil {
		f.t.Fatalf("create rotation: %v", err)
	}
	return v
}

func (f *rotationFixture) state(id string) service.RotationView {
	f.t.Helper()
	v, err := f.svc.Get(tctx(f.t), f.actor, f.scope, id)
	if err != nil {
		f.t.Fatalf("get rotation: %v", err)
	}
	return v
}

// list returns the first page of rotations (every test fixture fits in one).
func (f *rotationFixture) list(actor service.Actor, scope domain.Scope, includeDisabled bool) ([]service.RotationView, error) {
	f.t.Helper()
	page, err := f.svc.List(tctx(f.t), actor, scope, service.RotationListRequest{IncludeDisabled: includeDisabled})
	return page.Items, err
}

func (f *rotationFixture) transition(actor service.Actor, name, id string) (service.RotationView, error) {
	f.t.Helper()
	req := service.RotationStateRequest{RotationID: id}
	switch name {
	case "pause":
		return f.svc.Pause(tctx(f.t), actor, f.scope, req)
	case "resume":
		return f.svc.Resume(tctx(f.t), actor, f.scope, req)
	case "delete":
		return f.svc.Delete(tctx(f.t), actor, f.scope, req)
	}
	f.t.Fatalf("unknown transition %q", name)
	return service.RotationView{}, nil
}

// rotationHuman inserts a human principal that holds exactly the capabilities
// named, at org A.
func rotationHuman(t *testing.T, db *store.DB, id domain.PrincipalID, caps ...string) {
	t.Helper()
	execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('`+string(id)+`', 'human', `+ts+`)`)
	for _, c := range caps {
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_`+string(id)+`_`+c+`','`+string(id)+`','`+c+`','org_a',NULL,NULL,`+ts+`)`)
	}
}

func TestRotationConfigurationLifecycle(t *testing.T) { forEngines(t, runRotationLifecycle) }

func runRotationLifecycle(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)

	created, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotPasswordKey, rotUsernameKey))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.State != store.RotationStateActive || created.Generation != 1 ||
		created.AuthorityPrincipalID != string(alice) || created.CreatedBy != string(alice) ||
		created.KeyID != rotPasswordKey || created.UsernameKeyID != rotUsernameKey ||
		created.IntervalSeconds != 86400 || created.RevocationDelaySeconds != 3600 ||
		created.CredentialExpiresAt != "" {
		t.Fatalf("created rotation = %+v", created)
	}
	if want := f.now.Add(24 * time.Hour); !mustParseStamp(t, created.NextRunAt).Equal(want) {
		t.Fatalf("next_run_at = %s want %s", created.NextRunAt, want)
	}

	// A live rotation owns its keys, whichever role they play: neither key can be
	// driven twice, and a key cannot switch role across rotations either.
	if _, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotPasswordKey, rotSecondUser)); !errors.Is(err, service.ErrRotationKeyInUse) {
		t.Fatalf("duplicate password key = %v", err)
	}
	if _, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotSecondKey, rotUsernameKey)); !errors.Is(err, service.ErrRotationKeyInUse) {
		t.Fatalf("duplicate username key = %v", err)
	}

	listed, err := f.list(f.actor, f.scope, false)
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("list = %+v, %v", listed, err)
	}
	if got, err := f.svc.Get(ctx, f.actor, f.scope, created.ID); err != nil || got.ID != created.ID {
		t.Fatalf("get = %+v, %v", got, err)
	}

	// Pause: stale generation is refused; the right one moves state and clears
	// the schedule.
	if _, err := f.svc.Pause(ctx, f.actor, f.scope, service.RotationStateRequest{RotationID: created.ID, ExpectedGeneration: 7}); !errors.Is(err, service.ErrRotationStale) {
		t.Fatalf("stale pause = %v", err)
	}
	paused, err := f.svc.Pause(ctx, f.actor, f.scope, service.RotationStateRequest{RotationID: created.ID, ExpectedGeneration: 1})
	if err != nil || paused.State != store.RotationStatePaused || paused.Generation != 2 || paused.NextRunAt != "" {
		t.Fatalf("pause = %+v, %v", paused, err)
	}
	if _, err := f.svc.Pause(ctx, f.actor, f.scope, service.RotationStateRequest{RotationID: created.ID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("pause of a paused rotation = %v", err)
	}

	// Resume recomputes next_run_at from the resume time, not the old schedule.
	f.now = f.now.Add(10 * time.Hour)
	resumed, err := f.svc.Resume(ctx, f.actor, f.scope, service.RotationStateRequest{RotationID: created.ID})
	if err != nil || resumed.State != store.RotationStateActive || resumed.Generation != 3 {
		t.Fatalf("resume = %+v, %v", resumed, err)
	}
	if want := f.now.Add(24 * time.Hour); !mustParseStamp(t, resumed.NextRunAt).Equal(want) {
		t.Fatalf("resumed next_run_at = %s want %s", resumed.NextRunAt, want)
	}

	// Delete is soft; it frees the keys and a second delete conflicts.
	deleted, err := f.svc.Delete(ctx, f.actor, f.scope, service.RotationStateRequest{RotationID: created.ID})
	if err != nil || deleted.State != store.RotationStateDisabled || deleted.NextRunAt != "" {
		t.Fatalf("delete = %+v, %v", deleted, err)
	}
	if _, err := f.svc.Delete(ctx, f.actor, f.scope, service.RotationStateRequest{RotationID: created.ID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second delete = %v", err)
	}
	if _, err := f.svc.Resume(ctx, f.actor, f.scope, service.RotationStateRequest{RotationID: created.ID}); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("resume of a deleted rotation = %v", err)
	}
	// The default list hides deleted history; asking for it shows it.
	if rows, err := f.list(f.actor, f.scope, false); err != nil || len(rows) != 0 {
		t.Fatalf("default list after delete = %+v, %v", rows, err)
	}
	if rows, err := f.list(f.actor, f.scope, true); err != nil || len(rows) != 1 || rows[0].State != store.RotationStateDisabled {
		t.Fatalf("list with disabled = %+v, %v", rows, err)
	}
	if again, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotPasswordKey, rotUsernameKey)); err != nil || again.ID == created.ID {
		t.Fatalf("recreate over a deleted rotation = %+v, %v", again, err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 2 {
		t.Fatalf("disabled history must stay: %d rows", got)
	}

	// Every action is audited with a payload that names the delegation and
	// nothing secret.
	for _, action := range []string{"created", "paused", "resumed", "deleted"} {
		if queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.configured' AND payload LIKE '%\"action\":\""+action+"\"%'") == 0 {
			t.Errorf("no rotation.configured audit row for %s", action)
		}
	}
	payloads := queryStrings(t, db, "SELECT payload FROM audit_tenant_events WHERE type='rotation.configured'")
	for _, forbidden := range []string{rotAdminSecret, "password\":", "candidate", "ciphertext"} {
		if strings.Contains(payloads, forbidden) {
			t.Errorf("rotation audit payload contains %q: %s", forbidden, payloads)
		}
	}
	if !strings.Contains(payloads, `"authority_principal_id":"usr_alice"`) || !strings.Contains(payloads, `"username_key_id":"`+rotUsernameKey+`"`) {
		t.Errorf("audit payload does not record the authority principal and both keys: %s", payloads)
	}
}

func TestRotationCreateRefusals(t *testing.T) { forEngines(t, runRotationCreateRefusals) }

func runRotationCreateRefusals(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	bad := func(name string, mutate func(*service.CreateRotationRequest), want error) {
		t.Helper()
		req := f.request(rotPasswordKey, rotUsernameKey)
		mutate(&req)
		if _, err := f.svc.Create(ctx, f.actor, f.scope, req); !errors.Is(err, want) {
			t.Errorf("%s = %v, want %v", name, err, want)
		}
	}
	bad("interval below minimum", func(r *service.CreateRotationRequest) { r.IntervalSeconds = 3599 }, domain.ErrInvalid)
	bad("interval above maximum", func(r *service.CreateRotationRequest) { r.IntervalSeconds = 90*86400 + 1 }, domain.ErrInvalid)
	bad("delay below minimum", func(r *service.CreateRotationRequest) { r.RevocationDelaySeconds = 299 }, domain.ErrInvalid)
	bad("delay above maximum", func(r *service.CreateRotationRequest) { r.RevocationDelaySeconds = 7*86400 + 1 }, domain.ErrInvalid)
	bad("delay not shorter than interval", func(r *service.CreateRotationRequest) {
		r.IntervalSeconds, r.RevocationDelaySeconds = 3600, 3600
	}, domain.ErrInvalid)
	bad("no provider", func(r *service.CreateRotationRequest) { r.ProviderID = "" }, domain.ErrInvalid)
	bad("no username key", func(r *service.CreateRotationRequest) { r.UsernameKeyID = "" }, domain.ErrInvalid)
	bad("same key twice", func(r *service.CreateRotationRequest) { r.UsernameKeyID = r.KeyID }, domain.ErrInvalid)
	bad("unknown provider", func(r *service.CreateRotationRequest) { r.ProviderID = "dpv_missing" }, service.ErrRotationProviderInactive)
	bad("unknown key", func(r *service.CreateRotationRequest) { r.KeyID = "key_missing" }, domain.ErrNotFound)
	bad("key in another project", func(r *service.CreateRotationRequest) { r.UsernameKeyID = "key_a2" }, domain.ErrNotFound)
	bad("password key is not secret", func(r *service.CreateRotationRequest) { r.KeyID = rotUsernameKey; r.UsernameKeyID = rotSecondUser }, domain.ErrInvalid)
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 0 {
		t.Fatalf("refused creates left %d rows", got)
	}

	// A provider in another project of the same org is not this project's
	// provider: it answers like an unknown one.
	other, err := f.dyn.Configure(ctx, service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA2}, service.CreateDynamicProviderRequest{
		Kind: string(dynamic.KindPostgres), Origin: "postgres://admin@db.other.example:5432/app",
		GrantRole: "app_rw", Credential: []byte(rotAdminSecret),
	})
	if err != nil {
		t.Fatalf("configure cross-project provider: %v", err)
	}
	bad("cross-project provider", func(r *service.CreateRotationRequest) { r.ProviderID = other.ID }, service.ErrRotationProviderInactive)

	// A provider whose admin credential was revoked cannot mint, so it cannot be
	// the target of a new rotation.
	if err := f.dyn.RevokeCredential(ctx, service.LocalPrincipal(alice), prjScope(), f.provider); err != nil {
		t.Fatal(err)
	}
	bad("provider without credential", func(*service.CreateRotationRequest) {}, service.ErrRotationProviderInactive)

	// A deleted provider is a soft tombstone and reads as absent.
	if _, err := f.dyn.Delete(ctx, service.LocalPrincipal(alice), prjScope(), f.provider, false); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	bad("tombstoned provider", func(*service.CreateRotationRequest) {}, service.ErrRotationProviderInactive)

	// Access: the creator must hold edit, publish AND reveal. Without reveal the
	// uniform not-found answers, and nothing is written.
	execRaw(t, db, `DELETE FROM grants WHERE id='g_rot_reveal'`)
	if _, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotPasswordKey, rotUsernameKey)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("create without reveal = %v", err)
	}
	// A bare reader can inspect nothing it cannot read and create nothing.
	readerActor := f.session(reader, true)
	if _, err := f.svc.Create(ctx, readerActor, f.scope, f.request(rotPasswordKey, rotUsernameKey)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("reader create = %v", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 0 {
		t.Fatalf("denied creates left %d rows", got)
	}
}

// A rotation is a standing delegation consumed through a publish ceremony: a
// session without an open window cannot create or resume one, because no
// per-publish ceremony will ever gate the unattended publishes that follow.
func TestRotationRequiresCeremony(t *testing.T) { forEngines(t, runRotationRequiresCeremony) }

func runRotationRequiresCeremony(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	noWindow := f.session(alice, false)
	_, err := f.svc.Create(ctx, noWindow, f.scope, f.request(rotPasswordKey, rotUsernameKey))
	if err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("create without a ceremony window = %v, want a ceremony refusal", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 0 {
		t.Fatalf("a refused ceremony left %d rows", got)
	}
	created := f.create(rotPasswordKey, rotUsernameKey)
	if _, err := f.transition(f.actor, "pause", created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.transition(noWindow, "resume", created.ID); err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("resume without a ceremony window = %v, want a ceremony refusal", err)
	}
	if got := f.state(created.ID); got.State != store.RotationStatePaused {
		t.Fatalf("a refused resume changed the state to %s", got.State)
	}
	// Pause and delete only reduce what the rotation does and need no ceremony.
	if _, err := f.transition(noWindow, "delete", created.ID); err != nil {
		t.Fatalf("delete without a ceremony window: %v", err)
	}
}

// A scheduled rotation publishes unattended, so an environment whose publishes
// need a ceremony or an approval cannot be its target, by name, at create and at
// resume.
func TestRotationRefusesAttendedEnvironments(t *testing.T) {
	forEngines(t, runRotationRefusesAttendedEnvironments)
}

func runRotationRefusesAttendedEnvironments(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	created := f.create(rotPasswordKey, rotUsernameKey)
	if _, err := f.transition(f.actor, "pause", created.ID); err != nil {
		t.Fatal(err)
	}

	execRaw(t, db, `UPDATE environments SET protected=TRUE WHERE id='`+string(f.env)+`'`)
	if _, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotSecondKey, rotSecondUser)); !errors.Is(err, service.ErrRotationProtectedEnvironment) {
		t.Fatalf("create in a protected environment = %v", err)
	}
	if _, err := f.transition(f.actor, "resume", created.ID); !errors.Is(err, service.ErrRotationProtectedEnvironment) {
		t.Fatalf("resume in a protected environment = %v", err)
	}
	execRaw(t, db, `UPDATE environments SET protected=FALSE WHERE id='`+string(f.env)+`'`)

	approvals := &service.Approvals{DB: db, Auth: authService(t, db), Keyring: probeKeyring(t, db)}
	if _, err := approvals.CreatePolicy(ctx, service.LocalPrincipal(orgAdmin), prjScope(), service.ApprovalPolicyInput{
		EnvironmentID: string(f.env), MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true,
		Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(custodian)}},
	}); err != nil {
		t.Fatalf("create approval policy: %v", err)
	}
	if _, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotSecondKey, rotSecondUser)); !errors.Is(err, service.ErrRotationApprovalPolicy) {
		t.Fatalf("create in an approval-covered environment = %v", err)
	}
	if _, err := f.transition(f.actor, "resume", created.ID); !errors.Is(err, service.ErrRotationApprovalPolicy) {
		t.Fatalf("resume in an approval-covered environment = %v", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 1 {
		t.Fatalf("refused calls changed the row count to %d", got)
	}
}

func TestRotationTenantIsolation(t *testing.T) { forEngines(t, runRotationTenantIsolation) }

func runRotationTenantIsolation(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	created := f.create(rotPasswordKey, rotUsernameKey)
	// Bob holds the same grants in org B. Every address into org A answers with
	// the uniform not-found, for reads, writes and a rotation that exists.
	bobActor := f.session(bob, true)
	if _, err := f.list(bobActor, f.scope, false); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-org list = %v", err)
	}
	if _, err := f.svc.Get(ctx, bobActor, f.scope, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-org get = %v", err)
	}
	for name, run := range map[string]func() error{
		"pause":  func() error { _, err := f.transition(bobActor, "pause", created.ID); return err },
		"resume": func() error { _, err := f.transition(bobActor, "resume", created.ID); return err },
		"delete": func() error { _, err := f.transition(bobActor, "delete", created.ID); return err },
		"create": func() error {
			_, err := f.svc.Create(ctx, bobActor, f.scope, f.request(rotSecondKey, rotSecondUser))
			return err
		},
	} {
		if err := run(); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("cross-org %s = %v", name, err)
		}
	}
	// Addressing a real rotation through a sibling environment of the same
	// project must not find it either.
	sibling := domain.Scope{Org: orgA, Project: prjA1, Env: envProd}
	if _, err := f.svc.Get(ctx, f.actor, sibling, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("sibling environment get = %v", err)
	}
	if rows, err := f.list(f.actor, sibling, true); err != nil || len(rows) != 0 {
		t.Errorf("sibling environment list = %+v, %v", rows, err)
	}
	if got, err := f.svc.Get(ctx, f.actor, f.scope, created.ID); err != nil || got.State != store.RotationStateActive || got.Generation != 1 {
		t.Errorf("rotation changed under refused calls: %+v, %v", got, err)
	}
}

// A machine principal is refused creation and every state transition: a rotation
// is a person's delegation. The create refusal is the specific human-only one,
// so the test cannot pass because the machine merely lacked a grant.
func TestRotationRefusesMachine(t *testing.T) { forEngines(t, runRotationRefusesMachine) }

func runRotationRefusesMachine(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	execRaw(t, db, `UPDATE projects SET machine_reveal=TRUE WHERE id='prj_a1'`)
	ident := identitySvc(db)
	sa, err := ident.CreateServiceAccount(ctx, service.LocalPrincipal(alice), prjScope(), "rot-machine", domain.ClassWorkload)
	if err != nil {
		t.Fatal(err)
	}
	minted, err := ident.MintCredential(ctx, service.LocalPrincipal(alice), prjScope(), sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for i, capability := range []string{"edit", "publish", "reveal", "read"} {
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_rot_sa_`+string(rune('a'+i))+`','`+string(sa.Principal)+`','`+capability+`','org_a','prj_a1','`+string(f.env)+`',`+ts+`)`)
	}
	machine := service.Bearer(minted.Value)
	if _, err := f.svc.Create(ctx, machine, f.scope, f.request(rotPasswordKey, rotUsernameKey)); !errors.Is(err, service.ErrRotationHumanOnly) {
		t.Fatalf("machine create = %v, want the human-only refusal", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 0 {
		t.Fatalf("machine create left %d rows", got)
	}

	// Resume is a human act the service checks itself: a machine holding publish on
	// the environment is refused with the human-only refusal and nothing changes.
	// Pause and delete only ever reduce what the rotation does, so the service
	// admits any caller holding publish; the wire contract admits a human session
	// only (TestRotationOperationsAdmitHumanSessionOnly).
	created := f.create(rotPasswordKey, rotUsernameKey)
	if _, err := f.transition(f.actor, "pause", created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.transition(machine, "resume", created.ID); !errors.Is(err, service.ErrRotationHumanOnly) {
		t.Fatalf("machine resume = %v, want the human-only refusal", err)
	}
	if got := f.state(created.ID); got.State != store.RotationStatePaused || got.Generation != 2 {
		t.Fatalf("a machine resume changed the rotation: %+v", got)
	}
}

// Every rotation operation is human-session only on the wire: a machine
// credential is refused at admission, before any grant is consulted. The service
// checks the class itself only where a ceremony is consumed (create, resume).
func TestRotationOperationsAdmitHumanSessionOnly(t *testing.T) {
	for _, op := range []string{"rotation.create", "rotation.inspect", "rotation.pause", "rotation.resume", "rotation.delete", "rotation.run_now", "rotation.runs"} {
		human, described := api.AuthorizationOperationAdmitsArtifact(op, api.ArtifactHumanSession)
		if !described || !human {
			t.Errorf("%s: human session admitted = %v (described %v)", op, human, described)
		}
		if machine, _ := api.AuthorizationOperationAdmitsArtifact(op, api.ArtifactMachineCredential); machine {
			t.Errorf("%s admits a machine credential", op)
		}
	}
}

// A publish-only principal may pause and delete a rotation (both only reduce
// what it does) but can neither create nor resume one: those need the create
// formula on both keys.
func TestRotationPublishOnlyPrincipal(t *testing.T) { forEngines(t, runRotationPublishOnlyPrincipal) }

func runRotationPublishOnlyPrincipal(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	rotationHuman(t, db, rotPublisher, "read", "publish")
	pub := f.session(rotPublisher, true)

	if _, err := f.svc.Create(ctx, pub, f.scope, f.request(rotPasswordKey, rotUsernameKey)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("publish-only create = %v", err)
	}
	created := f.create(rotPasswordKey, rotUsernameKey)
	paused, err := f.transition(pub, "pause", created.ID)
	if err != nil || paused.State != store.RotationStatePaused {
		t.Fatalf("publish-only pause = %+v, %v", paused, err)
	}
	// Resume would let this principal re-arm alice's delegation without holding
	// the access it exercises.
	if _, err := f.transition(pub, "resume", created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("publish-only resume = %v", err)
	}
	if got := f.state(created.ID); got.State != store.RotationStatePaused {
		t.Fatalf("a refused resume changed the state to %s", got.State)
	}
	deleted, err := f.transition(pub, "delete", created.ID)
	if err != nil || deleted.State != store.RotationStateDisabled {
		t.Fatalf("publish-only delete = %+v, %v", deleted, err)
	}
}

// Resume re-checks the recorded authority, and a denial there names the caller as
// the actor and the recorded principal as the authority, never the reverse.
func TestRotationResumeAuthorityLost(t *testing.T) { forEngines(t, runRotationResumeAuthorityLost) }

func runRotationResumeAuthorityLost(t *testing.T, db *store.DB) {
	f := newRotationFixture(t, db)
	created := f.create(rotPasswordKey, rotUsernameKey)
	if _, err := f.transition(f.actor, "pause", created.ID); err != nil {
		t.Fatal(err)
	}
	custodianActor := f.session(custodian, true)

	// Control: the custodian holds the create formula and alice still does, so the
	// custodian can resume. Pause again to set up the denial.
	if _, err := f.transition(custodianActor, "resume", created.ID); err != nil {
		t.Fatalf("control resume by a fully authorized second person: %v", err)
	}
	if _, err := f.transition(custodianActor, "pause", created.ID); err != nil {
		t.Fatal(err)
	}

	// Park every reveal grant alice holds (the shared audit fixture seeds a broader
	// one) and restore exactly those rows afterwards.
	parked := strings.TrimSuffix(queryStrings(t, db, `SELECT '''' || id || ''',' FROM grants WHERE principal_id='usr_alice' AND capability='reveal' ORDER BY id`), ",")
	execRaw(t, db, `UPDATE grants SET capability='read' WHERE id IN (`+parked+`)`)
	defer execRaw(t, db, `UPDATE grants SET capability='reveal' WHERE id IN (`+parked+`)`)

	if _, err := f.transition(custodianActor, "resume", created.ID); !errors.Is(err, service.ErrRotationAuthorityLost) {
		t.Fatalf("resume after the authority lost reveal = %v", err)
	}
	// The denial is attributed to the person who asked (the custodian), with the
	// recorded principal named as the authority.
	denied := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE outcome='denied' AND actor_id='usr_custodian' AND authority_id='usr_alice'`)
	if denied == 0 {
		t.Fatal("no denied audit row attributes the lost authority to alice with the custodian as the actor")
	}
	if wrong := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE outcome='denied' AND actor_id='usr_alice' AND authority_id='usr_alice'`); wrong != 0 {
		t.Fatalf("%d denial rows blame the authority principal as the actor", wrong)
	}
	if got := f.state(created.ID); got.State != store.RotationStatePaused {
		t.Fatalf("a refused resume changed the state to %s", got.State)
	}
	// Alice, who lost reveal herself, cannot resume her own rotation either.
	if _, err := f.transition(f.actor, "resume", created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("resume by the principal who lost reveal = %v", err)
	}
}

// A key-narrowed member rule applies to create and resume: the caller must hold
// the create formula on BOTH keys, not merely on the environment.
func TestRotationKeyNarrowedCreator(t *testing.T) { forEngines(t, runRotationKeyNarrowedCreator) }

func runRotationKeyNarrowedCreator(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	rotationHuman(t, db, rotNarrow, "read")
	rules := &service.Rules{DB: db}
	envMode, envs := onlyEnvs(f.env)
	grantRule := func(capability domain.Capability, keyIDs ...string) {
		t.Helper()
		items := make([]domain.RuleKeyItem, 0, len(keyIDs))
		for _, id := range keyIDs {
			items = append(items, domain.RuleKeyItem{KeyID: id})
		}
		if _, err := rules.Create(ctx, service.LocalPrincipal(orgAdmin), service.RuleSpec{
			Target: rotNarrow, Capability: capability, Org: orgA,
			Where: whereIn(envMode, envs, domain.AxisOnly, items...),
		}); err != nil {
			t.Fatalf("create %s rule: %v", capability, err)
		}
	}
	// The password key only: the username key is outside the rules.
	for _, c := range []domain.Capability{domain.CapRead, domain.CapEdit, domain.CapPublish, domain.CapReveal} {
		grantRule(c, rotPasswordKey)
	}
	narrow := f.session(rotNarrow, true)
	if _, err := f.svc.Create(ctx, narrow, f.scope, f.request(rotPasswordKey, rotUsernameKey)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("create with the username key outside the rule = %v", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 0 {
		t.Fatalf("a refused key-narrowed create left %d rows", got)
	}
	// Widening the rules to cover the username key admits the same call.
	for _, c := range []domain.Capability{domain.CapRead, domain.CapEdit, domain.CapPublish, domain.CapReveal} {
		grantRule(c, rotUsernameKey)
	}
	// Rule changes invalidate the principal's sessions, so sign in again.
	narrow = f.session(rotNarrow, true)
	created, err := f.svc.Create(ctx, narrow, f.scope, f.request(rotPasswordKey, rotUsernameKey))
	if err != nil {
		t.Fatalf("create with both keys covered: %v", err)
	}
	if created.AuthorityPrincipalID != string(rotNarrow) {
		t.Fatalf("authority principal = %q", created.AuthorityPrincipalID)
	}
}

// Cross-role claims: a key a live rotation drives, in either role, cannot be
// driven by another; deleting the rotation frees both.
func TestRotationKeyClaimsAcrossRoles(t *testing.T) { forEngines(t, runRotationKeyClaimsAcrossRoles) }

func runRotationKeyClaimsAcrossRoles(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	// Rotation B's username key is rotation A's password key, a different role.
	// Both keys are secret-classified so the role swap is a legal request.
	rotationKey(t, db, "key_rot_pw_user", "ROT_DB_USER_SECRET", "secret")
	a := f.create(rotPasswordKey, rotUsernameKey)
	_, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotSecondKey, rotPasswordKey))
	if !errors.Is(err, service.ErrRotationKeyInUse) {
		t.Fatalf("username key that is another rotation's password key = %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "already driven by another live rotation") {
		t.Fatalf("in-use refusal message = %q", err)
	}
	_, err = f.svc.Create(ctx, f.actor, f.scope, f.request("key_rot_pw_user", rotUsernameKey))
	if !errors.Is(err, service.ErrRotationKeyInUse) {
		t.Fatalf("password key that is another rotation's username key = %v", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 1 {
		t.Fatalf("refused creates left %d rows", got)
	}
	if _, err := f.transition(f.actor, "delete", a.ID); err != nil {
		t.Fatal(err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotation_key_claims"); got != 0 {
		t.Fatalf("a deleted rotation still holds %d claims", got)
	}
	if _, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotSecondKey, rotPasswordKey)); err != nil {
		t.Fatalf("create after the keys were freed: %v", err)
	}
}

// A provider that is deleted or loses its admin credential pauses its active
// rotations in the same transaction and says why; resume then refuses until the
// provider is usable again.
func TestRotationProviderLifecyclePausesRotations(t *testing.T) {
	forEngines(t, runRotationProviderLifecyclePausesRotations)
}

func runRotationProviderLifecyclePausesRotations(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	revoked := f.create(rotPasswordKey, rotUsernameKey)
	if err := f.dyn.RevokeCredential(ctx, service.LocalPrincipal(alice), prjScope(), f.provider); err != nil {
		t.Fatalf("revoke provider credential: %v", err)
	}
	if got := f.state(revoked.ID); got.State != store.RotationStatePaused || got.NextRunAt != "" {
		t.Fatalf("rotation after the provider credential was revoked = %+v", got)
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.configured' AND payload LIKE '%"reason":"provider_credential_revoked"%' AND payload LIKE '%"action":"paused"%'`); n != 1 {
		t.Fatalf("revoke pause audit rows = %d, want 1", n)
	}
	if _, err := f.transition(f.actor, "resume", revoked.ID); !errors.Is(err, service.ErrRotationProviderInactive) {
		t.Fatalf("resume against a provider without a credential = %v", err)
	}
	if err := f.dyn.ReplaceCredential(ctx, service.LocalPrincipal(alice), prjScope(), f.provider, []byte(rotAdminSecret)); err != nil {
		t.Fatalf("restore provider credential: %v", err)
	}
	if resumed, err := f.transition(f.actor, "resume", revoked.ID); err != nil || resumed.State != store.RotationStateActive {
		t.Fatalf("resume once the provider can mint = %+v, %v", resumed, err)
	}

	// Deleting the provider pauses the active rotation too, and resume refuses.
	if _, err := f.dyn.Delete(ctx, service.LocalPrincipal(alice), prjScope(), f.provider, false); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
	if got := f.state(revoked.ID); got.State != store.RotationStatePaused {
		t.Fatalf("rotation after the provider was deleted = %+v", got)
	}
	if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='rotation.configured' AND payload LIKE '%"reason":"provider_deleted"%'`); n != 1 {
		t.Fatalf("delete pause audit rows = %d, want 1", n)
	}
	if _, err := f.transition(f.actor, "resume", revoked.ID); !errors.Is(err, service.ErrRotationProviderInactive) {
		t.Fatalf("resume against a deleted provider = %v", err)
	}
}

// A resume re-checks that both keys still exist and the password key is still
// secret-classified, and answers with the named refusal rather than the uniform
// not-found or "authority lost".
func TestRotationResumeKeyMissing(t *testing.T) { forEngines(t, runRotationResumeKeyMissing) }

func runRotationResumeKeyMissing(t *testing.T, db *store.DB) {
	f := newRotationFixture(t, db)
	created := f.create(rotPasswordKey, rotUsernameKey)
	if _, err := f.transition(f.actor, "pause", created.ID); err != nil {
		t.Fatal(err)
	}
	// Reclassified: the password key is no longer secret.
	execRaw(t, db, `UPDATE keys SET classification='config' WHERE id='`+rotPasswordKey+`'`)
	if _, err := f.transition(f.actor, "resume", created.ID); !errors.Is(err, service.ErrRotationKeyMissing) {
		t.Fatalf("resume with a reclassified password key = %v", err)
	}
	execRaw(t, db, `UPDATE keys SET classification='secret' WHERE id='`+rotPasswordKey+`'`)
	// Deleted: the username key is gone.
	execRaw(t, db, `DELETE FROM keys WHERE id='`+rotUsernameKey+`'`)
	if _, err := f.transition(f.actor, "resume", created.ID); !errors.Is(err, service.ErrRotationKeyMissing) {
		t.Fatalf("resume with a deleted username key = %v", err)
	}
	if got := f.state(created.ID); got.State != store.RotationStatePaused {
		t.Fatalf("a refused resume changed the state to %s", got.State)
	}
}

// Deleting an environment refuses by name while a rotation is live and purges the
// rotation history with the environment once none is.
func TestRotationEnvironmentDelete(t *testing.T) { forEngines(t, runRotationEnvironmentDelete) }

func runRotationEnvironmentDelete(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	envs := &service.Environments{DB: db, Keyring: probeKeyring(t, db)}
	created := f.create(rotPasswordKey, rotUsernameKey)
	if err := envs.Delete(ctx, service.LocalPrincipal(alice), f.scope); !errors.Is(err, service.ErrEnvironmentHasLiveRotation) {
		t.Fatalf("delete with an active rotation = %v", err)
	}
	if _, err := f.transition(f.actor, "pause", created.ID); err != nil {
		t.Fatal(err)
	}
	// A paused rotation is still live: it can be resumed.
	if err := envs.Delete(ctx, service.LocalPrincipal(alice), f.scope); !errors.Is(err, service.ErrEnvironmentHasLiveRotation) {
		t.Fatalf("delete with a paused rotation = %v", err)
	}
	if _, err := f.transition(f.actor, "delete", created.ID); err != nil {
		t.Fatal(err)
	}
	// A disabled rotation with run and effect history goes with the environment.
	rotationRunRow(t, db, f, "run_env_done", created.ID, "done", "7", "")
	execRaw(t, db, `INSERT INTO rotation_effects (id, org_id, project_id, environment_id, run_id, kind, intent_audit_id, created_at) VALUES ('rfx_env_1','org_a','prj_a1','`+string(f.env)+`','run_env_done','mint','aud_env_1',`+ts+`)`)
	if err := envs.Delete(ctx, service.LocalPrincipal(alice), f.scope); err != nil {
		t.Fatalf("delete with only a disabled rotation: %v", err)
	}
	for _, table := range []string{"rotations", "rotation_runs", "rotation_effects", "rotation_key_claims"} {
		if got := queryInt(t, db, "SELECT COUNT(*) FROM "+table+" WHERE environment_id='"+string(f.env)+"'"); got != 0 {
			t.Errorf("%s still has %d rows for the deleted environment", table, got)
		}
	}
}

// rotationRunRow inserts a run directly: S1 has no executor, so the run table's
// constraints and the queries that read it are exercised with raw rows.
// publishedRevision "" stores NULL; validUntil "" stores NULL.
func rotationRunRow(t *testing.T, db *store.DB, f *rotationFixture, id, rotationID, phase, publishedRevision, validUntil string) {
	t.Helper()
	if err := rotationRunRowErr(t, db, f, id, rotationID, phase, publishedRevision, validUntil, ts); err != nil {
		t.Fatalf("insert run %s: %v", id, err)
	}
}

func rotationRunRowErr(t *testing.T, db *store.DB, f *rotationFixture, id, rotationID, phase, publishedRevision, validUntil, startedAt string) error {
	t.Helper()
	revision, until := "NULL", "NULL"
	if publishedRevision != "" {
		revision = publishedRevision
	}
	if validUntil != "" {
		until = "'" + validUntil + "'"
	}
	return execRawErr(t, db, `INSERT INTO rotation_runs (id, org_id, project_id, environment_id, rotation_id, generation, run_trigger, phase, candidate_handle, candidate_valid_until, published_revision, revocation_delay_seconds, started_at, phase_at, next_attempt_at) VALUES ('`+id+`','org_a','prj_a1','`+string(f.env)+`','`+rotationID+`',1,'schedule','`+phase+`','role_`+id+`',`+until+`,`+revision+`,3600,`+startedAt+`,`+ts+`,`+ts+`)`)
}

// The run and effect tables carry the S2 executor's invariants as constraints;
// S1 writes nothing to them, so raw rows prove the constraints themselves.
func TestRotationRunConstraints(t *testing.T) { forEngines(t, runRotationRunConstraints) }

func runRotationRunConstraints(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	created := f.create(rotPasswordKey, rotUsernameKey)

	// Published-onward phases require a recorded publish. The rotation has no open
	// run yet, so only the CHECK can refuse these rows, never the one-open-run
	// index.
	for _, phase := range []string{"published", "overlap", "revoking", "revoke_refused", "done"} {
		if err := rotationRunRowErr(t, db, f, "run_nopub_"+phase, created.ID, phase, "", "", ts); err == nil {
			t.Fatalf("phase %s without a published_revision was accepted", phase)
		}
		// The same phase with a recorded publish is accepted; it is closed again so
		// it never occupies the rotation's open-run slot.
		if err := rotationRunRowErr(t, db, f, "run_pub_"+phase, created.ID, phase, "7", "", ts); err != nil {
			t.Fatalf("phase %s with a published_revision was refused: %v", phase, err)
		}
		execRaw(t, db, `UPDATE rotation_runs SET phase='done' WHERE id='run_pub_`+phase+`'`)
	}
	rotationRunRow(t, db, f, "run_open_1", created.ID, "minting", "", "")
	// At most one non-terminal run per rotation.
	if err := rotationRunRowErr(t, db, f, "run_open_2", created.ID, "minted", "", "", ts); err == nil {
		t.Fatal("a second non-terminal run for one rotation was accepted")
	}
	// A run in flight blocks delete.
	if _, err := f.transition(f.actor, "delete", created.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("delete with a run in flight = %v", err)
	}
	if got := f.state(created.ID); got.State != store.RotationStateActive {
		t.Fatalf("a refused delete changed the state to %s", got.State)
	}
	// A terminal run frees the slot, and delete succeeds.
	execRaw(t, db, `UPDATE rotation_runs SET phase='failed' WHERE id='run_open_1'`)
	rotationRunRow(t, db, f, "run_open_3", created.ID, "minting", "", "")
	execRaw(t, db, `UPDATE rotation_runs SET phase='quarantine' WHERE id='run_open_3'`)
	if _, err := f.svc.Delete(ctx, f.actor, f.scope, service.RotationStateRequest{RotationID: created.ID}); err != nil {
		t.Fatalf("delete with only terminal runs: %v", err)
	}

	// Effects: the outcome, its audit id and the finish time are all set or all
	// unset.
	effect := func(id, outcome, outcomeAudit, finished string) error {
		return execRawErr(t, db, `INSERT INTO rotation_effects (id, org_id, project_id, environment_id, run_id, kind, intent_audit_id, outcome_audit_id, outcome, created_at, finished_at) VALUES ('`+id+`','org_a','prj_a1','`+string(f.env)+`','run_open_1','mint','aud_i_`+id+`',`+outcomeAudit+`,`+outcome+`,`+ts+`,`+finished+`)`)
	}
	if err := effect("rfx_ok_open", "NULL", "NULL", "NULL"); err != nil {
		t.Fatalf("an open effect: %v", err)
	}
	if err := effect("rfx_ok_done", "'success'", "'aud_o_1'", ts); err != nil {
		t.Fatalf("a finished effect: %v", err)
	}
	for name, args := range map[string][3]string{
		"outcome without an audit id": {"'success'", "NULL", ts},
		"audit id without an outcome": {"NULL", "'aud_o_2'", "NULL"},
		"outcome without a finish":    {"'failure'", "'aud_o_3'", "NULL"},
		"finish without an outcome":   {"NULL", "NULL", ts},
	} {
		if err := effect("rfx_bad_"+strings.ReplaceAll(name, " ", "_"), args[0], args[1], args[2]); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	// The kind enum admits extend (a credential-expiry extension in S2).
	if err := execRawErr(t, db, `INSERT INTO rotation_effects (id, org_id, project_id, environment_id, run_id, kind, intent_audit_id, created_at) VALUES ('rfx_extend','org_a','prj_a1','`+string(f.env)+`','run_open_1','extend','aud_i_extend',`+ts+`)`); err != nil {
		t.Fatalf("an extend effect: %v", err)
	}
}

// The credential's expiry is derived from the latest run that recorded a publish,
// across every rotation that ever drove the key, so a deleted-and-recreated
// rotation still reports the role that is live in the keys.
func TestRotationCredentialExpiry(t *testing.T) { forEngines(t, runRotationCredentialExpiry) }

func runRotationCredentialExpiry(t *testing.T, db *store.DB) {
	f := newRotationFixture(t, db)
	first := f.create(rotPasswordKey, rotUsernameKey)
	if first.CredentialExpiresAt != "" {
		t.Fatalf("expiry before any run = %q", first.CredentialExpiresAt)
	}
	// An unpublished run has delivered nothing.
	rotationRunRow(t, db, f, "run_exp_unpub", first.ID, "minted", "", "2030-01-01T00:00:00.000000Z")
	if got := f.state(first.ID); got.CredentialExpiresAt != "" {
		t.Fatalf("expiry from an unpublished run = %q", got.CredentialExpiresAt)
	}
	execRaw(t, db, `UPDATE rotation_runs SET phase='failed' WHERE id='run_exp_unpub'`)
	rotationRunRow(t, db, f, "run_exp_done", first.ID, "done", "4", "2026-12-01T00:00:00.000000Z")
	want := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if got := f.state(first.ID); got.CredentialExpiresAt == "" || !mustParseStamp(t, got.CredentialExpiresAt).Equal(want) {
		t.Fatalf("expiry after a published run = %q, want %s", got.CredentialExpiresAt, want)
	}
	// Pause and delete still name the live credential's expiry.
	paused, err := f.transition(f.actor, "pause", first.ID)
	if err != nil || !mustParseStamp(t, paused.CredentialExpiresAt).Equal(want) {
		t.Fatalf("pause = %+v, %v", paused, err)
	}
	deleted, err := f.transition(f.actor, "delete", first.ID)
	if err != nil || !mustParseStamp(t, deleted.CredentialExpiresAt).Equal(want) {
		t.Fatalf("delete = %+v, %v", deleted, err)
	}
	// A later rotation over the same password key reports the same live role.
	second := f.create(rotPasswordKey, rotUsernameKey)
	if second.CredentialExpiresAt == "" || !mustParseStamp(t, second.CredentialExpiresAt).Equal(want) {
		t.Fatalf("recreated rotation expiry = %q, want %s", second.CredentialExpiresAt, want)
	}
}

func mustParseStamp(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatalf("stamp %q: %v", s, err)
	}
	return v
}

// runRotationAuditLifecycle drives rotation.configured through the real service
// so the registry-emitter closure check finds a row for it (#148).
func runRotationAuditLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	runRotationLifecycle(t, db)
	// The shared audit fixture's later subtests probe reveal-less callers, so the
	// grants this lifecycle added for its own principal must not outlive it.
	execRaw(t, db, `DELETE FROM grants WHERE id IN ('g_rot_manage', 'g_rot_reveal')`)
}
