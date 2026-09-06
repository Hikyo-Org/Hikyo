package isolation

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mailtest"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// runSelfConfigAuditLifecycle supplies actual service emitters to registry
// closure: explicit adoption, publication and apply, a real TLS SMTP delivery,
// restored-incarnation fencing, host recovery and reauthenticated resumption.
func runSelfConfigAuditLifecycle(t *testing.T, engine store.Engine) *store.DB {
	t.Helper()
	db := openSelfConfigAuditDB(t, engine)
	auth := authService(t, db)
	var clock atomic.Int64
	// Advance only the authentication clock through distinct TOTP steps. Keep
	// evidence in the recent past and runtime heartbeats on the real store clock.
	clock.Store(time.Now().UTC().Add(-5 * time.Minute).UnixNano())
	now := func() time.Time { return time.Unix(0, clock.Load()).UTC() }
	advance := func() { clock.Add(int64(30 * time.Second)) }
	auth.Now = now
	const password = "self configuration audit lifecycle password"
	admin := bootstrapAdmin(t, db, adminOpts{username: "self-config-admin", displayName: "Configuration Admin", password: password, auth: auth})
	sink := mailtest.New(t, "implicit")
	managed := &service.SelfConfig{DB: db, Keyring: auth.Keyring, Auth: auth, NodeID: "audit-local", Seed: func() (map[string]string, error) {
		return map[string]string{
			"HIKYO_UPDATE_CHANNEL": "off",
			"HIKYO_MAIL_ADDR":      sink.Addr, "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_FROM": "Hikyo <hikyo@example.com>",
			"HIKYO_MAIL_USER": "audit-operator", "HIKYO_MAIL_PASSWORD": " audit password\n", "HIKYO_MAIL_CA_PEM": sink.CAPEM, "HIKYO_MAIL_ALLOWED_CIDRS": "127.0.0.1/32",
		}, nil
	}}
	// Bootstrap deliberately predates wiring SelfConfig, exercising an existing
	// installation's explicit adoption rather than manufacturing an audit record.
	auth.SelfConfig = managed
	login, err := auth.LocalLogin(t.Context(), "self-config-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := auth.EnrolTOTPStart(t.Context(), login.SessionToken, password)
	if err != nil {
		t.Fatal(err)
	}
	advance()
	confirmed, err := auth.EnrolTOTPConfirm(t.Context(), login.SessionToken, totpCode(t, uri, now()))
	if err != nil {
		t.Fatal(err)
	}
	advance()
	elevated, err := auth.StepUpTOTP(t.Context(), confirmed.SessionToken, totpCode(t, uri, now()))
	if err != nil {
		t.Fatal(err)
	}
	token := elevated.SessionToken
	reauthenticate := func(target service.SelfConfigReauthTarget) service.Actor {
		t.Helper()
		intent, err := service.NewSelfConfigReauthIntent(target)
		if err != nil {
			t.Fatal(err)
		}
		advance()
		opened, err := auth.ReauthTOTP(t.Context(), token, intent, totpCode(t, uri, now()))
		if err != nil {
			t.Fatal(err)
		}
		if !opened.SingleDecision {
			t.Fatal("configuration ceremony was not a single decision")
		}
		token = opened.SessionToken
		return service.Bearer(token)
	}
	preview, err := managed.PreviewAdoption(t.Context(), service.Bearer(token))
	if err != nil {
		t.Fatal(err)
	}
	actor := reauthenticate(service.SelfConfigReauthTarget{Action: "adopt", OwnerInstanceID: preview.OwnerInstanceID, SchemaVersion: runtimeconfig.SchemaVersion, PreviewToken: preview.PreviewToken})
	adopted, err := managed.Adopt(t.Context(), actor, service.SelfConfigAdoptRequest{PreviewToken: preview.PreviewToken, IdempotencyKey: "audit-adopt"})
	if err != nil {
		t.Fatal(err)
	}
	if !adopted.Managed || adopted.Binding == nil {
		t.Fatal("adoption omitted managed binding")
	}
	// Creating the system organization changes the administrator's grant
	// generation. Authenticate anew instead of reusing the invalidated session.
	login, err = auth.LocalLogin(t.Context(), "self-config-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	advance()
	elevated, err = auth.StepUpTOTP(t.Context(), login.SessionToken, totpCode(t, uri, now()))
	if err != nil {
		t.Fatal(err)
	}
	token = elevated.SessionToken
	if err := managed.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	scope := domain.Scope{Org: domain.OrgID(adopted.Binding.OrgID), Project: domain.ProjectID(adopted.Binding.ProjectID), Env: domain.EnvID(adopted.Binding.EnvironmentID)}
	local := service.LocalPrincipal(admin.boot.PrincipalID)
	values := &service.Values{DB: db, Keyring: auth.Keyring, Auth: auth}
	staged, err := values.Set(t.Context(), local, scope, "HIKYO_UPDATE_CHANNEL", "nightly", nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions := &service.Revisions{DB: db, Keyring: auth.Keyring, Auth: auth, Now: now}
	if _, err := revisions.PublishPlanned(t.Context(), local, scope, service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
		t.Fatal(err)
	}
	bundle, err := managed.Capture(t.Context())
	if err != nil || bundle.UpdateChannel() != "off" {
		t.Fatalf("publication activated candidate: %v", err)
	}

	startWorker := func() func() {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() { defer close(done); managed.Run(ctx) }()
		return func() { cancel(); <-done }
	}
	actor = reauthenticate(service.SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: adopted.OwnerInstanceID, Revision: 2, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 1})
	func() {
		stop := startWorker()
		defer stop()
		if _, err := managed.Apply(t.Context(), actor, service.SelfConfigApplyRequest{Revision: 2, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 1, IdempotencyKey: "audit-apply"}); err != nil {
			t.Fatal(err)
		}
		if err := managed.ReconcileRuntime(t.Context()); err != nil {
			t.Fatal(err)
		}
	}()
	bundle, err = managed.Capture(t.Context())
	if err != nil || bundle.UpdateChannel() != "nightly" {
		t.Fatalf("apply did not activate candidate: %v", err)
	}
	actor = reauthenticate(service.SelfConfigReauthTarget{Action: "mail-test", OwnerInstanceID: adopted.OwnerInstanceID, Revision: 2, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 2, To: "recipient@example.com"})
	func() {
		stop := startWorker()
		defer stop()
		sent, err := managed.TestMail(t.Context(), actor, service.SelfConfigMailTestRequest{Revision: 2, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 2, To: "recipient@example.com"})
		if err != nil {
			t.Fatal(err)
		}
		if !sent.Sent || len(sink.Messages()) != 1 {
			t.Fatal("test delivery did not reach the TLS SMTP sink")
		}
	}()
	// Restore fixture input: a backed-up binding carries an older incarnation.
	// The production runtime detects it and emits the fence with its own proof.
	execRaw(t, db, "UPDATE self_config_binding SET incarnation = 'audit-backup-incarnation'")
	if err := managed.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := managed.Capture(t.Context()); !errors.Is(err, service.ErrSelfConfigUnavailable) {
		t.Fatalf("restored credentials remained active: %v", err)
	}
	recovered, err := managed.Recover(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Generation != 3 || !recovered.Suspended {
		t.Fatalf("host recovery cleared suspension: %+v", recovered)
	}
	actor = reauthenticate(service.SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: adopted.OwnerInstanceID, Revision: 1, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 3, ConfirmRestoredCredentials: true})
	func() {
		stop := startWorker()
		defer stop()
		if _, err := managed.Apply(t.Context(), actor, service.SelfConfigApplyRequest{Revision: 1, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: 3, ConfirmRestoredCredentials: true, IdempotencyKey: "audit-resume"}); err != nil {
			t.Fatal(err)
		}
		if err := managed.ReconcileRuntime(t.Context()); err != nil {
			t.Fatal(err)
		}
	}()
	status, err := managed.Status(t.Context(), service.Bearer(token))
	if err != nil {
		t.Fatal(err)
	}
	if status.Generation != 4 || status.State != "active" {
		t.Fatalf("confirmed recovery did not resume: %+v", status)
	}
	// A prepared external rollout uses real publication, Apply and distinct TOTP
	// ceremonies. Only the external deployment transport is deterministic here.
	owner, incarnation, err := db.RecoveryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	managed.Deployment = newAuditDeployment(owner, incarnation)
	staged, err = values.Set(t.Context(), local, scope, config.ManagedBootstrapSourcesKey, `{"version":1,"database_source":"replacement"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := revisions.PublishPlanned(t.Context(), local, scope, service.PublishRequest{VersionIDs: []string{staged.VersionID}}); err != nil {
		t.Fatal(err)
	}
	status, err = managed.Status(t.Context(), service.Bearer(token))
	if err != nil {
		t.Fatal(err)
	}
	req := service.SelfConfigApplyRequest{Revision: *status.LatestRevision, SchemaVersion: runtimeconfig.SchemaVersion, ExpectedGeneration: status.Generation, IdempotencyKey: "audit-bootstrap", PrepareOnly: true}
	func() {
		stop := startWorker()
		defer stop()
		prepared, err := managed.Apply(t.Context(), service.Bearer(token), req)
		if err != nil || prepared.Job == nil || !prepared.Job.Prepared || prepared.Job.PlanDigest == "" {
			t.Fatalf("bootstrap preparation: %+v, %v", prepared.Job, err)
		}
		req.PlanDigest = prepared.Job.PlanDigest
	}()
	req.PrepareOnly = false
	actor = reauthenticate(service.SelfConfigReauthTarget{Action: "apply", OwnerInstanceID: owner, Revision: req.Revision, SchemaVersion: req.SchemaVersion, ExpectedGeneration: req.ExpectedGeneration, PlanDigest: req.PlanDigest})
	applied, err := managed.Apply(t.Context(), actor, req)
	if err != nil {
		t.Fatal(err)
	}
	// Model a convergence timeout through the worker's checked store door. The
	// controller has not replaced this node, so no application ack is fabricated.
	err = tx.Write(t.Context(), db, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		p, err := az.SelfConfigRuntimeAuthority(ctx, "")
		if err != nil {
			return err
		}
		return r.SelfConfig().FinishJob(ctx, p, applied.Job.ID, "partial", "convergence_timeout", time.Now().UTC())
	})
	if err != nil {
		t.Fatal(err)
	}
	restore := service.SelfConfigDeploymentRestoreRequest{Revision: req.Revision, ExpectedGeneration: applied.Generation, SchemaVersion: req.SchemaVersion, PlanDigest: req.PlanDigest}
	const restoreEvent = "self_config.deployment_restore_requested"
	if _, err := managed.RestoreDeployment(t.Context(), actor, restore); err == nil {
		t.Fatal("spent Apply factor authorized deployment restore")
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = '"+restoreEvent+"'"); got != 0 {
		t.Fatal("refused restore emitted success")
	}
	actor = reauthenticate(service.SelfConfigReauthTarget{Action: "rollout-restore", OwnerInstanceID: owner, Revision: restore.Revision, SchemaVersion: restore.SchemaVersion, ExpectedGeneration: restore.ExpectedGeneration, PlanDigest: restore.PlanDigest})
	restored, err := managed.RestoreDeployment(t.Context(), actor, restore)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Generation != applied.Generation || !restored.Job.DeploymentRestorePending {
		t.Fatal("restore changed desired generation or claimed external completion")
	}
	if _, err := managed.RestoreDeployment(t.Context(), actor, restore); err != nil {
		t.Fatal(err)
	}
	if got := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = '"+restoreEvent+"'"); got != 1 {
		t.Fatalf("restore receipt count %d, want one after idempotent retry", got)
	}
	return db
}
