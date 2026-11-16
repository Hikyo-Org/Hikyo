package isolation

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/dynamic"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Scheduled credential rotation configuration (#148, slice S1). Everything
// traverses the real service, authorizer and store on both engines; only the
// external PostgreSQL provider is replaced (and S1 never calls it).

const (
	rotPasswordKey = "key_rot_pw"
	rotUsernameKey = "key_rot_user"
	rotSecondKey   = "key_rot_pw2"
	rotSecondUser  = "key_rot_user2"
	rotAdminSecret = "rotation-admin-credential-do-not-log"
)

type rotationFixture struct {
	db       *store.DB
	svc      *service.CredentialRotations
	actor    service.Actor
	scope    domain.Scope
	provider string
	now      time.Time
}

func rotationKey(t *testing.T, db *store.DB, id, name, classification string) {
	t.Helper()
	execRaw(t, db, `INSERT INTO keys (id, org_id, project_id, name, folder_path, classification, description, deprecated, deprecation_note, declaration, required_mode, forbidden_mode, group_id, created_at) VALUES ('`+id+`', 'org_a', 'prj_a1', '`+name+`', '', '`+classification+`', '', FALSE, '', '{"rule":{"type":"string"}}', 'none', 'none', NULL, `+ts+`)`)
}

func newRotationFixture(t *testing.T, db *store.DB) *rotationFixture {
	t.Helper()
	ctx := tctx(t)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_rot_manage','usr_alice','manage-identities','org_a','prj_a1',NULL,`+ts+`)`)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_rot_reveal','usr_alice','reveal','org_a',NULL,NULL,`+ts+`)`)
	rotationKey(t, db, rotPasswordKey, "ROT_DB_PASSWORD", "secret")
	rotationKey(t, db, rotUsernameKey, "ROT_DB_USER", "config")
	rotationKey(t, db, rotSecondKey, "ROT_DB_PASSWORD2", "secret")
	rotationKey(t, db, rotSecondUser, "ROT_DB_USER2", "config")

	dyn := &service.Dynamic{
		DB: db, Keyring: probeKeyring(t, db), Runtime: store.NewDynamicRuntime(db),
		LeaseDeadline: 5 * time.Second,
		ProviderFactory: func(dynamic.Kind, string, string, string) (dynamic.Provider, error) {
			return fakeDynamicProvider{ctl: &fakeDynamicControl{}}, nil
		},
	}
	provider, err := dyn.Configure(ctx, service.LocalPrincipal(alice), domain.Scope{Org: orgA, Project: prjA1}, service.CreateDynamicProviderRequest{
		Kind: string(dynamic.KindPostgres), Origin: "postgres://admin@db.rotation.example:5432/app",
		GrantRole: "app_rw", Credential: []byte(rotAdminSecret),
	})
	if err != nil {
		t.Fatalf("configure provider: %v", err)
	}
	f := &rotationFixture{
		db: db, actor: service.LocalPrincipal(alice), provider: provider.ID,
		scope: domain.Scope{Org: orgA, Project: prjA1, Env: envA1},
		now:   time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC),
	}
	f.svc = &service.CredentialRotations{DB: db, Now: func() time.Time { return f.now }}
	return f
}

func (f *rotationFixture) request(key, user string) service.CreateRotationRequest {
	return service.CreateRotationRequest{
		ProviderID: f.provider, KeyID: key, UsernameKeyID: user,
		IntervalSeconds: 24 * 3600, RevocationDelaySeconds: 3600,
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
		created.IntervalSeconds != 86400 || created.RevocationDelaySeconds != 3600 {
		t.Fatalf("created rotation = %+v", created)
	}
	if want := f.now.Add(24 * time.Hour); !mustParseStamp(t, created.NextRunAt).Equal(want) {
		t.Fatalf("next_run_at = %s want %s", created.NextRunAt, want)
	}

	// A live rotation owns its keys: neither key can be driven twice.
	if _, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotPasswordKey, rotSecondUser)); !errors.Is(err, service.ErrRotationKeyInUse) {
		t.Fatalf("duplicate password key = %v", err)
	}
	if _, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotSecondKey, rotUsernameKey)); !errors.Is(err, service.ErrRotationKeyInUse) {
		t.Fatalf("duplicate username key = %v", err)
	}

	listed, err := f.svc.List(ctx, f.actor, f.scope)
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

	// Resume refuses when the recorded authority lost reveal, even though the
	// caller can still publish.
	if _, err := f.svc.Pause(ctx, f.actor, f.scope, service.RotationStateRequest{RotationID: created.ID}); err != nil {
		t.Fatal(err)
	}
	// Park every reveal grant the principal holds (the shared audit fixture seeds
	// a broader one) and restore exactly those rows afterwards.
	parked := strings.TrimSuffix(queryStrings(t, db, `SELECT '''' || id || ''',' FROM grants WHERE principal_id='usr_alice' AND capability='reveal' ORDER BY id`), ",")
	execRaw(t, db, `UPDATE grants SET capability='read' WHERE id IN (`+parked+`)`)
	if _, err := f.svc.Resume(ctx, f.actor, f.scope, service.RotationStateRequest{RotationID: created.ID}); !errors.Is(err, service.ErrRotationAuthorityLost) {
		t.Fatalf("resume without authority = %v", err)
	}
	execRaw(t, db, `UPDATE grants SET capability='reveal' WHERE id IN (`+parked+`)`)

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
	if !strings.Contains(payloads, `"authority_principal_id":"usr_alice"`) {
		t.Errorf("audit payload does not record the authority principal: %s", payloads)
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
	bad("password key is not secret", func(r *service.CreateRotationRequest) { r.KeyID = rotUsernameKey; r.UsernameKeyID = rotSecondUser }, domain.ErrInvalid)
	bad("unknown provider", func(r *service.CreateRotationRequest) { r.ProviderID = "dpv_missing" }, domain.ErrNotFound)
	bad("unknown key", func(r *service.CreateRotationRequest) { r.KeyID = "key_missing" }, domain.ErrNotFound)
	bad("key in another project", func(r *service.CreateRotationRequest) { r.UsernameKeyID = "key_a2" }, domain.ErrNotFound)
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 0 {
		t.Fatalf("refused creates left %d rows", got)
	}

	// A provider whose admin credential was revoked cannot mint, so it cannot be
	// the target of a new rotation.
	dyn := &service.Dynamic{DB: db, Keyring: probeKeyring(t, db), Runtime: store.NewDynamicRuntime(db)}
	if err := dyn.RevokeCredential(ctx, f.actor, domain.Scope{Org: orgA, Project: prjA1}, f.provider); err != nil {
		t.Fatal(err)
	}
	bad("provider without credential", func(*service.CreateRotationRequest) {}, domain.ErrInvalid)

	// Access: the creator must hold edit, publish AND reveal. Without reveal the
	// uniform not-found answers, and nothing is written.
	execRaw(t, db, `DELETE FROM grants WHERE id='g_rot_reveal'`)
	if _, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotPasswordKey, rotUsernameKey)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("create without reveal = %v", err)
	}
	// A bare reader can inspect nothing it cannot read and create nothing.
	reader := service.LocalPrincipal(domain.PrincipalID("usr_reader"))
	if _, err := f.svc.Create(ctx, reader, f.scope, f.request(rotPasswordKey, rotUsernameKey)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("reader create = %v", err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 0 {
		t.Fatalf("denied creates left %d rows", got)
	}
}

func TestRotationTenantIsolation(t *testing.T) { forEngines(t, runRotationTenantIsolation) }

func runRotationTenantIsolation(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	created, err := f.svc.Create(ctx, f.actor, f.scope, f.request(rotPasswordKey, rotUsernameKey))
	if err != nil {
		t.Fatal(err)
	}
	// Bob holds the same grants in org B. Every address into org A answers with
	// the uniform not-found, for reads, writes and a rotation that exists.
	bobActor := service.LocalPrincipal(bob)
	if _, err := f.svc.List(ctx, bobActor, f.scope); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-org list = %v", err)
	}
	if _, err := f.svc.Get(ctx, bobActor, f.scope, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("cross-org get = %v", err)
	}
	for name, run := range map[string]func() error{
		"pause": func() error {
			_, err := f.svc.Pause(ctx, bobActor, f.scope, service.RotationStateRequest{RotationID: created.ID})
			return err
		},
		"resume": func() error {
			_, err := f.svc.Resume(ctx, bobActor, f.scope, service.RotationStateRequest{RotationID: created.ID})
			return err
		},
		"delete": func() error {
			_, err := f.svc.Delete(ctx, bobActor, f.scope, service.RotationStateRequest{RotationID: created.ID})
			return err
		},
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
	sibling := domain.Scope{Org: orgA, Project: prjA1, Env: "env_prod"}
	if _, err := f.svc.Get(ctx, f.actor, sibling, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("sibling environment get = %v", err)
	}
	if rows, err := f.svc.List(ctx, f.actor, sibling); err != nil || len(rows) != 0 {
		t.Errorf("sibling environment list = %+v, %v", rows, err)
	}
	if got, err := f.svc.Get(ctx, f.actor, f.scope, created.ID); err != nil || got.State != store.RotationStateActive || got.Generation != 1 {
		t.Errorf("rotation changed under refused calls: %+v, %v", got, err)
	}
}

func TestRotationCreateRefusesMachine(t *testing.T) { forEngines(t, runRotationCreateRefusesMachine) }

func runRotationCreateRefusesMachine(t *testing.T, db *store.DB) {
	ctx := tctx(t)
	f := newRotationFixture(t, db)
	ident := identitySvc(db)
	sa, err := ident.CreateServiceAccount(ctx, f.actor, prjScope(), "rot-machine", domain.ClassWorkload)
	if err != nil {
		t.Fatal(err)
	}
	minted, err := ident.MintCredential(ctx, f.actor, prjScope(), sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for i, capability := range []string{"edit", "publish", "reveal", "read"} {
		execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_rot_sa_`+string(rune('a'+i))+`','`+string(sa.Principal)+`','`+capability+`','org_a','prj_a1','env_a1',`+ts+`)`)
	}
	_, err = f.svc.Create(ctx, service.Bearer(minted.Value), f.scope, f.request(rotPasswordKey, rotUsernameKey))
	if err == nil {
		t.Fatal("a machine principal created a rotation")
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM rotations"); got != 0 {
		t.Fatalf("machine create left %d rows", got)
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
