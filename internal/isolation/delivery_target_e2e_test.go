package isolation

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/deliverytarget"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Delivery-target condition reporting (#788, k8s-condition-reporting ADR):
// the report, tombstone, list and purge driven through the real service with
// real minted workload credentials.

var dtNow = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

// reportingWorkload creates a workload service account in env's project,
// mints one credential for it and grants it `read` plus
// `report-delivery-status` on env. The grants come from the org member
// manager: no human holds the atom, so the grant-unheld rule rules out a
// project-scope manager.
func reportingWorkload(t *testing.T, db *store.DB, name string, env domain.Scope) (service.ServiceAccountView, service.MintResult) {
	t.Helper()
	ident := identitySvc(db)
	admin := service.LocalPrincipal(identAdmin)
	project := domain.Scope{Org: env.Org, Project: env.Project}
	sa, err := ident.CreateServiceAccount(t.Context(), admin, project, name, domain.ClassWorkload)
	if err != nil {
		t.Fatal(err)
	}
	minted, err := ident.MintCredential(t.Context(), admin, project, sa.ID, service.MintRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range []domain.Capability{domain.CapRead, domain.CapReportDeliveryStatus} {
		grantWorkload(t, db, sa.Principal, capability, env)
	}
	return sa, minted
}

func grantWorkload(t *testing.T, db *store.DB, p domain.PrincipalID, capability domain.Capability, env domain.Scope) {
	t.Helper()
	if _, err := grantSvcWithAuth(db).Create(t.Context(), service.LocalPrincipal(orgAdmin), service.GrantSpec{
		Target: p, Capability: capability, Scope: env,
	}); err != nil {
		t.Fatalf("grant %s(%s) to %s: %v", capability, env.Env, p, err)
	}
}

func dtService(t *testing.T, db *store.DB, now time.Time) *service.Delivery {
	t.Helper()
	del := deliverySvc(t, db)
	del.Now = func() time.Time { return now }
	return del
}

// dtReport is a valid vocabulary-1 report for target n, observed at gen.
func dtReport(n int, gen int64, at time.Time) deliverytarget.Report {
	return deliverytarget.Report{
		Vocabulary: 1,
		Target: deliverytarget.Target{
			ClusterID:   "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f00",
			InstanceUID: "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0f01",
			Namespace:   "apps", Name: fmt.Sprintf("target-%d", n),
			UID: fmt.Sprintf("0193f0b4-1f2a-7c31-9c1e-%012d", n),
		},
		Generation: gen, ObservedGeneration: gen, ReportedAt: at,
		ReportIntervalSeconds: 300, Lifecycle: "Synced",
		Conditions: []deliverytarget.Condition{
			{Type: "Ready", Status: "True", Reason: "Reconciled", ObservedGeneration: gen},
			{Type: "Synced", Status: "True", Reason: "Delivered", ObservedGeneration: gen},
		},
		Reporter: deliverytarget.ReporterKubernetesOperator, ReporterVersion: "1.2.0",
	}
}

func dtKey(n int) service.DeliveryTargetKey {
	r := dtReport(n, 1, dtNow)
	return service.DeliveryTargetKey{ClusterID: r.Target.ClusterID, InstanceUID: r.Target.InstanceUID, UID: r.Target.UID}
}

func dtRows(t *testing.T, db *store.DB, where string) int64 {
	t.Helper()
	return queryInt(t, db, "SELECT COUNT(*) FROM delivery_target_reports WHERE "+where)
}

func dtEvents(t *testing.T, db *store.DB, typ, where string) int64 {
	t.Helper()
	q := "SELECT COUNT(*) FROM audit_tenant_events WHERE type = '" + typ + "'"
	if where != "" {
		q += " AND " + where
	}
	return queryInt(t, db, q)
}

func dtList(t *testing.T, db *store.DB, actor service.Actor, env domain.EnvID, now time.Time) service.DeliveryTargetList {
	t.Helper()
	list, err := dtService(t, db, now).ListTargets(t.Context(), actor, envScope(env))
	if err != nil {
		t.Fatalf("list delivery targets in %s: %v", env, err)
	}
	return list
}

// runDeliveryTargetAuditLifecycle gives every identity.delivery_target_* type a
// real emitter for the registry-emitter closure check: a new row, a vocabulary
// refusal on it, a tombstone, and the scheduled 30-day purge of a second row.
// identityFixtures has already run in the audit suite's identity lifecycle.
func runDeliveryTargetAuditLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	_, minted := reportingWorkload(t, db, "audited-reporter", envScope(envA1))
	scope := envScope(envA1)
	del := dtService(t, db, dtNow)
	for _, n := range []int{1, 2} {
		if err := del.ReportTarget(t.Context(), minted.Value, scope, dtReport(n, 1, dtNow)); err != nil {
			t.Fatalf("identity.delivery_target_created: %v", err)
		}
	}
	bad := dtReport(1, 2, dtNow.Add(time.Second))
	bad.Conditions[0].Reason = "NotInVocabulary"
	if err := del.ReportTarget(t.Context(), minted.Value, scope, bad); !errors.Is(err, service.ErrReportVocabulary) {
		t.Fatalf("identity.delivery_target_refused: %v", err)
	}
	if err := del.TombstoneTarget(t.Context(), minted.Value, scope, dtKey(1)); err != nil {
		t.Fatalf("identity.delivery_target_tombstoned: %v", err)
	}
	if err := dtService(t, db, dtNow.Add(deliverytarget.PurgeAfter+time.Hour)).PurgeExpiredTargets(t.Context()); err != nil {
		t.Fatalf("identity.delivery_target_purged: %v", err)
	}
}

func TestDeliveryTargetLifecycle(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		sa, minted := reportingWorkload(t, db, "reporter", envScope(envA1))
		scope := envScope(envA1)
		del := dtService(t, db, dtNow)
		principal := "principal_id = '" + string(sa.Principal) + "'"

		if err := del.ReportTarget(t.Context(), minted.Value, scope, dtReport(1, 1, dtNow)); err != nil {
			t.Fatalf("first report: %v", err)
		}
		// An accepted repeat report updates the row and emits nothing.
		if err := del.ReportTarget(t.Context(), minted.Value, scope, dtReport(1, 1, dtNow.Add(time.Minute))); err != nil {
			t.Fatalf("repeat report: %v", err)
		}
		if got := dtEvents(t, db, "identity.delivery_target_created", ""); got != 1 {
			t.Fatalf("created events = %d, want 1 (repeat reports are not audited)", got)
		}

		// Rotation: mint a new credential, revoke the old one, report again.
		// The row is keyed by principal, so it stays one row.
		ident := identitySvc(db)
		rotated, err := ident.MintCredential(t.Context(), service.LocalPrincipal(identAdmin), prjScope(), sa.ID, service.MintRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if err := ident.RevokeCredential(t.Context(), service.LocalPrincipal(identAdmin), prjScope(), sa.ID, minted.Credential.ID); err != nil {
			t.Fatal(err)
		}
		// The revoked credential is an authentication failure (401).
		if err := del.ReportTarget(t.Context(), minted.Value, scope, dtReport(1, 1, dtNow.Add(2*time.Minute))); !errors.Is(err, domain.ErrUnauthenticated) {
			t.Fatalf("revoked credential report = %v, want 401", err)
		}
		if err := del.ReportTarget(t.Context(), rotated.Value, scope, dtReport(1, 1, dtNow.Add(2*time.Minute))); err != nil {
			t.Fatalf("report under the rotated credential: %v", err)
		}
		if got := dtRows(t, db, principal); got != 1 {
			t.Fatalf("rows after rotation = %d, want 1", got)
		}
		// An expired credential is 401 too.
		if expires := rotated.Credential.ExpiresAt; !expires.IsZero() {
			late := dtService(t, db, expires.Add(time.Minute))
			if err := late.ReportTarget(t.Context(), rotated.Value, scope, dtReport(1, 1, expires)); !errors.Is(err, domain.ErrUnauthenticated) {
				t.Fatalf("expired credential report = %v, want 401", err)
			}
		} else {
			t.Fatal("the default mint is indefinite; the expiry leg needs a finite credential")
		}

		list := dtList(t, db, service.LocalPrincipal(alice), envA1, dtNow.Add(3*time.Minute))
		if len(list.Targets) != 1 || list.Targets[0].State != deliverytarget.StateReported {
			t.Fatalf("list = %+v, want one reported row", list.Targets)
		}
		// A stale row reads stale, never healthy: 2 x 5 min + 5 min grace.
		if got := dtList(t, db, service.LocalPrincipal(alice), envA1, dtNow.Add(20*time.Minute)).Targets[0].State; got != deliverytarget.StateStale {
			t.Fatalf("state after 20 min = %q, want stale", got)
		}

		// Revoking every credential turns the principal's rows reporter-revoked.
		if err := ident.RevokeCredential(t.Context(), service.LocalPrincipal(identAdmin), prjScope(), sa.ID, rotated.Credential.ID); err != nil {
			t.Fatal(err)
		}
		if got := dtList(t, db, service.LocalPrincipal(alice), envA1, dtNow.Add(3*time.Minute)).Targets[0].State; got != deliverytarget.StateReporterRevoked {
			t.Fatalf("state with no live credential = %q, want reporter-revoked", got)
		}

		// Principal deletion removes its rows in the same transaction.
		if err := ident.DeleteServiceAccount(t.Context(), service.LocalPrincipal(identAdmin), prjScope(), sa.ID); err != nil {
			t.Fatal(err)
		}
		if got := dtRows(t, db, principal); got != 0 {
			t.Fatalf("rows after principal deletion = %d, want 0", got)
		}
	})
}

func TestDeliveryTargetRefusals(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		sa, minted := reportingWorkload(t, db, "refused-reporter", envScope(envA1))
		scope := envScope(envA1)
		del := dtService(t, db, dtNow)
		row := "principal_id = '" + string(sa.Principal) + "'"
		if err := del.ReportTarget(t.Context(), minted.Value, scope, dtReport(1, 2, dtNow)); err != nil {
			t.Fatal(err)
		}
		snapshot := func() string {
			return queryString(t, db, "SELECT observed_generation || '/' || reported_at || '/' || conditions FROM delivery_target_reports WHERE "+row)
		}
		accepted := snapshot()

		// Ordering (409): audited, dropped, never recorded on the row.
		for name, report := range map[string]deliverytarget.Report{
			"lower observed_generation": dtReport(1, 1, dtNow.Add(time.Minute)),
			"not-later reported_at":     dtReport(1, 2, dtNow),
			"future reported_at":        dtReport(1, 2, dtNow.Add(deliverytarget.MaxFutureSkew+time.Second)),
		} {
			if err := del.ReportTarget(t.Context(), minted.Value, scope, report); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("%s = %v, want 409", name, err)
			}
		}
		if got := snapshot(); got != accepted {
			t.Fatalf("an out-of-order report changed the row: %q -> %q", accepted, got)
		}
		if got := dtRows(t, db, row+" AND refusal_cause IS NOT NULL"); got != 0 {
			t.Fatal("an ordering refusal was recorded on the row")
		}
		if got := dtEvents(t, db, "identity.delivery_target_refused", `payload LIKE '%"cause":"ordering"%'`); got != 3 {
			t.Fatalf("ordering refusal events = %d, want 3", got)
		}

		// Vocabulary (422): a grammatical type or reason outside the
		// advertised vocabulary names the field, stores no report data, and
		// records only the closed cause on the existing row.
		for _, tc := range []struct {
			field  string
			mutate func(*deliverytarget.Report)
		}{
			{"conditions[1].type", func(r *deliverytarget.Report) { r.Conditions[1].Type = "example.com/Healthy" }},
			{"conditions[0].reason", func(r *deliverytarget.Report) { r.Conditions[0].Reason = "Fine" }},
		} {
			execRaw(t, db, "UPDATE delivery_target_reports SET refusal_cause = NULL, refused_at = NULL WHERE "+row)
			bad := dtReport(1, 3, dtNow.Add(time.Minute))
			tc.mutate(&bad)
			err := dtService(t, db, dtNow.Add(time.Minute)).ReportTarget(t.Context(), minted.Value, scope, bad)
			var detail interface{ SafeDetail() string }
			if !errors.Is(err, service.ErrReportVocabulary) || !errors.As(err, &detail) || detail.SafeDetail() != tc.field {
				t.Fatalf("vocabulary refusal = %v, want 422 naming %s", err, tc.field)
			}
			if got := snapshot(); got != accepted {
				t.Fatalf("a vocabulary-refused report stored data: %q -> %q", accepted, got)
			}
			if got := dtRows(t, db, row+" AND refusal_cause = 'vocabulary' AND refused_at IS NOT NULL"); got != 1 {
				t.Fatalf("%s: the vocabulary refusal cause was not recorded on the existing row", tc.field)
			}
			if got := dtEvents(t, db, "identity.delivery_target_refused", `payload LIKE '%"field":"`+tc.field+`"%'`); got != 1 {
				t.Fatalf("%s: refusal events naming the field = %d, want 1", tc.field, got)
			}
		}
		// A string outside the condition grammar is a 400: no event, no mark.
		refusals := dtEvents(t, db, "identity.delivery_target_refused", "")
		ungrammatical := dtReport(1, 3, dtNow.Add(time.Minute))
		ungrammatical.Conditions[0].Type = "not a condition"
		if err := del.ReportTarget(t.Context(), minted.Value, scope, ungrammatical); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("ungrammatical condition type = %v, want 400", err)
		}
		if got := dtEvents(t, db, "identity.delivery_target_refused", ""); got != refusals || snapshot() != accepted {
			t.Fatal("a grammar refusal touched the trail or the row")
		}
		list := dtList(t, db, service.LocalPrincipal(alice), envA1, dtNow.Add(2*time.Minute))
		if got := list.Targets[0]; got.State != deliverytarget.StateRefused || got.RefusalCause != deliverytarget.RefusalVocabulary {
			t.Fatalf("list state = %q/%q, want refused/vocabulary", got.State, got.RefusalCause)
		}
		// A vocabulary refusal with no existing row touches nothing.
		fresh := dtReport(9, 1, dtNow)
		fresh.Lifecycle = "Healthy"
		if err := del.ReportTarget(t.Context(), minted.Value, scope, fresh); !errors.Is(err, service.ErrReportVocabulary) {
			t.Fatalf("new-row vocabulary refusal = %v", err)
		}
		if got := dtRows(t, db, row); got != 1 {
			t.Fatalf("rows after a refused new report = %d, want 1", got)
		}
		// The next accepted report clears the refusal.
		if err := dtService(t, db, dtNow.Add(2*time.Minute)).ReportTarget(t.Context(), minted.Value, scope, dtReport(1, 3, dtNow.Add(time.Minute))); err != nil {
			t.Fatal(err)
		}
		if got := dtRows(t, db, row+" AND refusal_cause IS NULL"); got != 1 {
			t.Fatal("an accepted report did not clear the recorded refusal")
		}

		// Size (413): authorized, audited under no row.
		if err := del.RefuseOversizeReport(t.Context(), minted.Value, scope); !errors.Is(err, service.ErrReportTooLarge) {
			t.Fatalf("oversize = %v, want 413", err)
		}
		if got := dtEvents(t, db, "identity.delivery_target_refused", `payload LIKE '%"cause":"size"%' AND payload NOT LIKE '%target_id%'`); got != 1 {
			t.Fatalf("size refusal events without a row = %d, want 1", got)
		}

		// Quota (named 409): 100 rows per principal.
		for n := 2; n <= deliverytarget.MaxRowsPerPrincipal; n++ {
			if err := del.ReportTarget(t.Context(), minted.Value, scope, dtReport(n, 1, dtNow)); err != nil {
				t.Fatalf("report %d: %v", n, err)
			}
		}
		if err := del.ReportTarget(t.Context(), minted.Value, scope, dtReport(deliverytarget.MaxRowsPerPrincipal+1, 1, dtNow)); !errors.Is(err, domain.ErrLimitExceeded) {
			t.Fatalf("report past the quota = %v, want the named 409", err)
		}
		if got := dtRows(t, db, row); got != deliverytarget.MaxRowsPerPrincipal {
			t.Fatalf("rows = %d, want the %d quota", got, deliverytarget.MaxRowsPerPrincipal)
		}
		list = dtList(t, db, service.LocalPrincipal(alice), envA1, dtNow)
		if len(list.Reporters) != 1 || list.Reporters[0].QuotaRefusedAt.IsZero() {
			t.Fatalf("reporters = %+v, want the quota-refused notice", list.Reporters)
		}
	})
}

// TestDeliveryTargetUniformNotFound: a cross-tenant report and a report after
// the grant was removed are indistinguishable from a nonexistent environment,
// and neither touches a row.
func TestDeliveryTargetUniformNotFound(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		sa, minted := reportingWorkload(t, db, "tenant-reporter", envScope(envA1))
		del := dtService(t, db, dtNow)
		if err := del.ReportTarget(t.Context(), minted.Value, envScope(envA1), dtReport(1, 1, dtNow)); err != nil {
			t.Fatal(err)
		}
		before := dtRows(t, db, "1 = 1")
		missing := del.ReportTarget(t.Context(), minted.Value, envScope("env_missing"), dtReport(1, 2, dtNow.Add(time.Minute)))
		crossTenant := del.ReportTarget(t.Context(), minted.Value, scopeEnv(orgB, prjB1, envB1), dtReport(1, 2, dtNow.Add(time.Minute)))
		assertUniformNotFound(t, crossTenant, missing)
		// `read` alone is not the atom: env_prod is readable, not reportable.
		grantWorkload(t, db, sa.Principal, domain.CapRead, envScope(envProd))
		readOnly := del.ReportTarget(t.Context(), minted.Value, envScope(envProd), dtReport(1, 2, dtNow.Add(time.Minute)))
		assertUniformNotFound(t, readOnly, missing)

		if err := grantSvcWithAuth(db).Revoke(t.Context(), service.LocalPrincipal(orgAdmin), service.GrantSpec{
			Target: sa.Principal, Capability: domain.CapReportDeliveryStatus, Scope: envScope(envA1),
		}); err != nil {
			t.Fatal(err)
		}
		removed := del.ReportTarget(t.Context(), minted.Value, envScope(envA1), dtReport(1, 2, dtNow.Add(time.Minute)))
		assertUniformNotFound(t, removed, missing)
		tombstone := del.TombstoneTarget(t.Context(), minted.Value, envScope(envA1), dtKey(1))
		assertUniformNotFound(t, tombstone, missing)
		oversize := del.RefuseOversizeReport(t.Context(), minted.Value, envScope(envA1))
		assertUniformNotFound(t, oversize, missing)
		if got := dtRows(t, db, "1 = 1"); got != before {
			t.Fatalf("rows = %d after authorization refusals, want %d", got, before)
		}
		if got := dtRows(t, db, "refusal_cause IS NOT NULL"); got != 0 {
			t.Fatal("an authorization refusal was recorded on a row")
		}
		// Without the grant the row reads reporter-revoked.
		if got := dtList(t, db, service.LocalPrincipal(alice), envA1, dtNow).Targets[0].State; got != deliverytarget.StateReporterRevoked {
			t.Fatalf("state after the grant was removed = %q, want reporter-revoked", got)
		}
	})
}

// TestDeliveryTargetListIsolation: the list returns rows of the addressed
// environment only, never another project's or another environment's, and an
// environment the caller cannot read is the uniform nonexistent answer, not an
// empty or redacted list (ADR D7).
func TestDeliveryTargetListIsolation(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('usr_envreader', 'human', `+ts+`)`)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) VALUES ('g_er_read', 'usr_envreader', 'read', 'org_a', 'prj_a1', 'env_a1', `+ts+`)`)
		envReader := service.LocalPrincipal("usr_envreader")
		del := dtService(t, db, dtNow)

		_, a1 := reportingWorkload(t, db, "a1-reporter", envScope(envA1))
		_, prodCredential := reportingWorkload(t, db, "prod-reporter", envScope(envProd))
		_, a2 := reportingWorkload(t, db, "a2-reporter", scopeEnv(orgA, prjA2, envA2))
		for _, report := range []struct {
			credential string
			scope      domain.Scope
		}{
			{a1.Value, envScope(envA1)},
			{prodCredential.Value, envScope(envProd)},
			{a2.Value, scopeEnv(orgA, prjA2, envA2)},
		} {
			if err := del.ReportTarget(t.Context(), report.credential, report.scope, dtReport(1, 1, dtNow)); err != nil {
				t.Fatalf("report into %s: %v", report.scope.Env, err)
			}
		}

		for _, viewer := range []service.Actor{service.LocalPrincipal(alice), envReader} {
			list := dtList(t, db, viewer, envA1, dtNow)
			want := queryString(t, db, "SELECT id FROM delivery_target_reports WHERE environment_id = 'env_a1'")
			if len(list.Targets) != 1 || list.Targets[0].ID != want {
				t.Fatalf("env_a1 list = %+v, want exactly its own row %s", list.Targets, want)
			}
			if len(list.Reporters) != 1 || list.Reporters[0].PrincipalID != list.Targets[0].PrincipalID {
				t.Fatalf("env_a1 reporters = %+v, want only the row's own principal", list.Reporters)
			}
		}

		_, missing := del.ListTargets(t.Context(), service.LocalPrincipal(alice), envScope("env_missing"))
		for name, probe := range map[string]struct {
			actor service.Actor
			scope domain.Scope
		}{
			"unreadable environment in the same project": {envReader, envScope(envProd)},
			"another project": {envReader, scopeEnv(orgA, prjA2, envA2)},
			"another tenant":  {service.LocalPrincipal(bob), envScope(envA1)},
		} {
			_, err := del.ListTargets(t.Context(), probe.actor, probe.scope)
			t.Run(name, func(t *testing.T) { assertUniformNotFound(t, err, missing) })
		}
	})
}
