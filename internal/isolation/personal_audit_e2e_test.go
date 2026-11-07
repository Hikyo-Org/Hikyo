package isolation

import (
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// runPersonalAuditLifecycle gives every personal_value event TestAuditCore
// counts a real service emitter. It runs on its own seeded instance because the
// personal fixture's org and accounts would collide with the main audit corpus.
func runPersonalAuditLifecycle(t *testing.T, engine store.Engine) *store.DB {
	t.Helper()
	ctx := t.Context()
	db := seededDB(t, func(t *testing.T) *store.DB { return openSelfConfigAuditDB(t, engine, "_personalaudit") })
	const owner = "personal-audit"
	e := personalFixture(t, db, owner)
	scope := envScope(envA1)

	e.setOwn(t, ceremonySecretA, e.keyA, personalPlain) // personal_value.set
	e.admin.token = personalReauth(t, e.admin.auth, e.admin.token, e.device, e.keyA)
	if _, err := e.pv.Reveal(ctx, service.Bearer(e.admin.token), scope, ceremonySecretA); err != nil {
		t.Fatalf("personal reveal: %v", err)
	}
	minted, _ := mintDeveloper(t, db, &e.ceremonyEnv, time.Hour, true)
	if _, err := deliverySvc(t, db).Fetch(ctx, minted.Value, scope, "", service.FetchOptions{}); err != nil {
		t.Fatalf("personal delivery: %v", err)
	}

	admin := service.Bearer(e.adminTok)
	if _, err := e.pv.Counts(ctx, admin, scope); err != nil {
		t.Fatalf("personal counts: %v", err)
	}
	if _, err := e.pv.Members(ctx, admin, scope, ceremonySecretA); err != nil {
		t.Fatalf("personal members: %v", err)
	}
	adminID := domain.PrincipalID(queryString(t, db, `SELECT principal_id FROM accounts WHERE username = '`+owner+`-admin'`))
	grantOrgCap(t, db, "g_pv_audit_read", adminID, domain.CapAuditRead)
	if _, err := e.pv.OffboardingReport(ctx, admin, scopeProject(orgA, prjA1), owner); err != nil {
		t.Fatalf("offboarding report: %v", err)
	}
	if err := e.pv.Delete(ctx, service.Bearer(e.admin.token), scope, ceremonySecretA); err != nil {
		t.Fatalf("personal delete: %v", err)
	}
	return db
}
