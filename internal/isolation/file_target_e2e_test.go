package isolation

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Generic file destinations (#164), the server half, driven through the real
// services with real minted workload credentials: the binding narrows the
// bound account's delivery, the account cannot fall back to the environment,
// the report is the bound principal's alone, and delete refuses to widen.

var ftNow = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func fileTargetSvc(db *store.DB) *service.FileTargets {
	return &service.FileTargets{DB: db, Now: func() time.Time { return ftNow }}
}

// fileTargetAdmin gives identAdmin the adapter half of the file-target
// formula; it already holds manage-identities on prj_a1.
func fileTargetAdmin(t *testing.T, db *store.DB) service.Actor {
	t.Helper()
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_ft_manage','`+string(identAdmin)+`','manage-adapters','org_a','prj_a1',NULL,`+ts+`)`)
	return service.LocalPrincipal(identAdmin)
}

func deliveredNames(res service.FetchResult) []string {
	var names []string
	for _, k := range res.Keys {
		names = append(names, k.Name)
	}
	slices.Sort(names)
	return names
}

func TestFileTargetLifecycle(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		admin := fileTargetAdmin(t, db)
		svc := fileTargetSvc(db)
		del := dtService(t, db, ftNow)
		env := envScope(envA1)
		sa, minted := reportingWorkload(t, db, "file-sync", env)

		// Unbound: the environment's delivery, and naming a target is 404.
		res, err := del.Fetch(t.Context(), minted.Value, env, "", service.FetchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if got := deliveredNames(res); !slices.Equal(got, []string{"DATABASE_PASSWORD", "DATABASE_URL"}) {
			t.Fatalf("unbound delivery = %v", got)
		}
		if _, err := del.Fetch(t.Context(), minted.Value, env, "", service.FetchOptions{Target: "ftg_unbound"}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("unbound caller naming a target = %v, want not found", err)
		}

		// Only a workload account can be bound.
		auto, err := identitySvc(db).CreateServiceAccount(t.Context(), admin, prjScope(), "ci", domain.ClassAutomation)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Create(t.Context(), admin, prjScope(), service.FileTargetInput{
			EnvironmentID: string(envA1), Name: "ci", ServiceAccountID: auto.ID,
			KeySelection: service.AdapterKeySelection{Names: []string{"DATABASE_URL"}},
		}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("automation account bound: %v", err)
		}
		// Neither half of the formula alone suffices.
		if _, err := svc.List(t.Context(), service.LocalPrincipal(alice), prjScope()); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("list without manage-adapters = %v", err)
		}

		target, err := svc.Create(t.Context(), admin, prjScope(), service.FileTargetInput{
			EnvironmentID: string(envA1), Name: "web-host", ServiceAccountID: sa.ID,
			KeySelection: service.AdapterKeySelection{Names: []string{"DATABASE_URL"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if target.Generation != 1 || len(target.Keys) != 1 || target.Keys[0].Name != "DATABASE_URL" || target.PrincipalID != string(sa.Principal) {
			t.Fatalf("created = %+v", target)
		}
		// One target per account.
		if _, err := svc.Create(t.Context(), admin, prjScope(), service.FileTargetInput{
			EnvironmentID: string(envA1), Name: "second", ServiceAccountID: sa.ID,
			KeySelection: service.AdapterKeySelection{Names: []string{"DATABASE_URL"}},
		}); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("second binding = %v, want conflict", err)
		}

		// Bound: the environment-wide fetch is refused, the target fetch is
		// narrowed, and the unselected secret never crosses.
		if _, err := del.Fetch(t.Context(), minted.Value, env, "", service.FetchOptions{}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("bound account without target = %v, want invalid", err)
		}
		if _, err := del.Fetch(t.Context(), minted.Value, env, "", service.FetchOptions{Target: "ftg_other"}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("bound account naming another target = %v, want not found", err)
		}
		narrowed, err := del.Fetch(t.Context(), minted.Value, env, "", service.FetchOptions{Target: target.ID})
		if err != nil {
			t.Fatal(err)
		}
		if got := deliveredNames(narrowed); !slices.Equal(got, []string{"DATABASE_URL"}) {
			t.Fatalf("target delivery = %v", got)
		}
		if narrowed.FileTargetGeneration != 1 || res.FileTargetGeneration != 0 {
			t.Fatalf("target generation = %d (unbound %d), want 1 (0)", narrowed.FileTargetGeneration, res.FileTargetGeneration)
		}
		if narrowed.ChangeToken == res.ChangeToken {
			t.Fatal("the narrowed manifest kept the environment's change token")
		}
		current, err := del.Fetch(t.Context(), minted.Value, env, narrowed.Cursor, service.FetchOptions{Target: target.ID})
		if err != nil || !current.Current {
			t.Fatalf("conditional target fetch = %+v, %v", current, err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'identity.delivery_fetched' AND payload LIKE '%"file_target":"`+target.ID+`"%'`); n != 2 {
			t.Fatalf("fetch records naming the target = %d, want 2", n)
		}

		// Selection replacement is a generation compare-and-swap.
		if _, err := svc.UpdateKeys(t.Context(), admin, prjScope(), target.ID, 7, nil, service.AdapterKeySelection{Names: []string{"DATABASE_PASSWORD"}}); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("stale update = %v, want conflict", err)
		}
		updated, err := svc.UpdateKeys(t.Context(), admin, prjScope(), target.ID, 1, nil, service.AdapterKeySelection{Names: []string{"DATABASE_URL", "DATABASE_PASSWORD"}})
		if err != nil {
			t.Fatal(err)
		}
		if updated.Generation != 2 || len(updated.Keys) != 2 {
			t.Fatalf("updated = %+v", updated)
		}
		widened, err := del.Fetch(t.Context(), minted.Value, env, narrowed.Cursor, service.FetchOptions{Target: target.ID})
		if err != nil || widened.Current || len(widened.Keys) != 2 || widened.FileTargetGeneration != 2 {
			t.Fatalf("fetch after widening = current %v keys %d, %v", widened.Current, len(widened.Keys), err)
		}

		// Reports: only the bound principal, only in the target's environment;
		// a repeat is not audited.
		report := service.FileTargetReport{State: service.FileTargetApplied, Revision: widened.Revision, Generation: 2, Stamp: "v1-0123456789abcdef0123456789abcdef", ReportedAt: ftNow}
		if err := del.ReportFileTarget(t.Context(), minted.Value, env, target.ID, report); err != nil {
			t.Fatal(err)
		}
		report.ReportedAt = ftNow.Add(time.Minute)
		if err := del.ReportFileTarget(t.Context(), minted.Value, env, target.ID, report); err != nil {
			t.Fatal(err)
		}
		if n := dtEvents(t, db, "file_target.applied", ""); n != 1 {
			t.Fatalf("applied events = %d, want 1 (a repeat report is not audited)", n)
		}
		stale := report
		stale.ReportedAt = ftNow
		stale.State = service.FileTargetFailed
		if err := del.ReportFileTarget(t.Context(), minted.Value, env, target.ID, stale); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("stale report = %v, want conflict", err)
		}
		if n := dtEvents(t, db, "file_target.applied", ""); n != 1 {
			t.Fatalf("stale report emitted event: %d", n)
		}
		_, other := reportingWorkload(t, db, "other-sync", env)
		if err := del.ReportFileTarget(t.Context(), other.Value, env, target.ID, report); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("report by an unbound principal = %v, want not found", err)
		}
		report.State = "healthy"
		if err := del.ReportFileTarget(t.Context(), minted.Value, env, target.ID, report); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("report outside the vocabulary = %v, want invalid", err)
		}
		shown, err := svc.Get(t.Context(), admin, prjScope(), target.ID)
		if err != nil {
			t.Fatal(err)
		}
		if shown.Report == nil || shown.Report.State != service.FileTargetApplied || shown.Report.Generation != 2 || !shown.Report.ReportedAt.Equal(ftNow.Add(time.Minute)) {
			t.Fatalf("shown report = %+v", shown.Report)
		}

		// Delete refuses while the bound account can still authenticate.
		if err := svc.Delete(t.Context(), admin, prjScope(), target.ID); !errors.Is(err, domain.ErrConflict) {
			t.Fatalf("delete with a live credential = %v, want conflict", err)
		}
		if err := identitySvc(db).RevokeCredential(t.Context(), admin, prjScope(), sa.ID, minted.Credential.ID); err != nil {
			t.Fatal(err)
		}
		if err := svc.Delete(t.Context(), admin, prjScope(), target.ID); err != nil {
			t.Fatalf("delete after revocation: %v", err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM file_target_keys WHERE target_id = '`+target.ID+`'`); n != 0 {
			t.Fatalf("selection rows after delete = %d", n)
		}

		// Deleting the service account deletes its target.
		sa2, _ := reportingWorkload(t, db, "file-sync-2", env)
		second, err := svc.Create(t.Context(), admin, prjScope(), service.FileTargetInput{
			EnvironmentID: string(envA1), Name: "web-host", ServiceAccountID: sa2.ID,
			KeySelection: service.AdapterKeySelection{Names: []string{"DATABASE_URL"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := identitySvc(db).DeleteServiceAccount(t.Context(), admin, prjScope(), sa2.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Get(t.Context(), admin, prjScope(), second.ID); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("target after account deletion = %v, want not found", err)
		}
	})
}

// runFileTargetAuditLifecycle gives every file_target.* type a real emitter
// for the registry-emitter closure check.
func runFileTargetAuditLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	admin := fileTargetAdmin(t, db)
	svc := fileTargetSvc(db)
	env := envScope(envA1)
	sa, minted := reportingWorkload(t, db, "audited-file-sync", env)
	target, err := svc.Create(t.Context(), admin, prjScope(), service.FileTargetInput{
		EnvironmentID: string(envA1), Name: "audited", ServiceAccountID: sa.ID,
		KeySelection: service.AdapterKeySelection{Names: []string{"DATABASE_URL"}},
	})
	if err != nil {
		t.Fatalf("file_target.configured: %v", err)
	}
	if _, err := svc.List(t.Context(), admin, prjScope()); err != nil {
		t.Fatalf("file_target.inspected: %v", err)
	}
	report := service.FileTargetReport{State: service.FileTargetApplied, Revision: 1, Generation: 1, ReportedAt: ftNow}
	if err := dtService(t, db, ftNow).ReportFileTarget(t.Context(), minted.Value, env, target.ID, report); err != nil {
		t.Fatalf("file_target.applied: %v", err)
	}
}

func TestFileTargetListKeepsSelectionsSeparate(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		identityFixtures(t, db)
		seedDeliveryCatalogue(t, db)
		admin := fileTargetAdmin(t, db)
		svc := fileTargetSvc(db)
		expected := map[string]string{}
		for _, name := range []string{"DATABASE_URL", "DATABASE_PASSWORD"} {
			sa, _ := reportingWorkload(t, db, name, envScope(envA1))
			target, err := svc.Create(t.Context(), admin, prjScope(), service.FileTargetInput{EnvironmentID: string(envA1), Name: strings.ReplaceAll(strings.ToLower(name), "_", "-"), ServiceAccountID: sa.ID, KeySelection: service.AdapterKeySelection{Names: []string{name}}})
			if err != nil {
				t.Fatal(err)
			}
			expected[target.ID] = name
		}
		targets, err := svc.List(t.Context(), admin, prjScope())
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 2 {
			t.Fatalf("targets = %d", len(targets))
		}
		for _, target := range targets {
			if len(target.Keys) != 1 || target.Keys[0].Name != expected[target.ID] {
				t.Fatalf("wrong selection: %+v", target)
			}
		}
	})
}
