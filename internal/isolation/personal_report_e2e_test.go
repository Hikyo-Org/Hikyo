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

// grantOrgCap gives a principal one org-wide capability in org A.
func grantOrgCap(t *testing.T, db *store.DB, id string, principal domain.PrincipalID, capability domain.Capability) {
	t.Helper()
	execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, project_id, env_id, created_at) `+
		`VALUES ('`+id+`', '`+string(principal)+`', '`+string(capability)+`', 'org_a', NULL, NULL, `+ts+`)`)
	execRaw(t, db, `INSERT INTO grant_origins (id, grant_id, kind, subject, created_at) `+
		`VALUES ('gor_`+id+`', '`+id+`', 'manual', '`+string(principal)+`', `+ts+`)`)
}

func revokeCeremonyGrant(t *testing.T, db *store.DB, username, capability string) {
	t.Helper()
	id := "g_cer_" + username + "_" + capability
	execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id = '`+id+`'`)
	execRaw(t, db, `DELETE FROM grants WHERE id = '`+id+`'`)
}

func countFor(c service.PersonalCounts, keyID string) (service.PersonalCount, bool) {
	for _, item := range c.Items {
		if item.KeyID == keyID {
			return item, true
		}
	}
	return service.PersonalCount{}, false
}

// The matrix marks count holders, notices and requirements per key, never a
// value, and the denominator is the chokepoint's own read-and-reveal
// evaluation (personal-values ADR, One complete surface).
func TestPersonalValueCountsMarksAndEligible(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		e := personalFixture(t, db, "pv-counts")
		ctx := t.Context()
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)
		keyB := "key_" + ceremonySecretB

		counts, err := e.pv.Counts(ctx, service.Bearer(e.adminTok), envScope(envA1))
		if err != nil {
			t.Fatal(err)
		}
		a, ok := countFor(counts, e.keyA)
		if !ok || len(counts.Items) != 1 || a.Name != ceremonySecretA || a.Holders != 1 || a.Notices != 0 || a.Required {
			t.Fatalf("counts: items=%d a=%+v", len(counts.Items), a)
		}
		// The owner and the invited administrator both hold read and reveal.
		eligible := counts.Eligible
		if eligible < 2 {
			t.Fatalf("eligible = %d, want the owner and the administrator at least", eligible)
		}

		// Reveal without read is not eligible: the formula, not a reveal
		// grant alone, decides.
		revokeCeremonyGrant(t, db, "pv-counts", "read")
		if counts, err = e.pv.Counts(ctx, service.Bearer(e.adminTok), envScope(envA1)); err != nil || counts.Eligible != eligible-1 {
			t.Fatalf("eligible after the owner lost read = %d (err=%v), want %d", counts.Eligible, err, eligible-1)
		}

		// A requirement and an administrator's removal show as marks.
		execRaw(t, db, `INSERT INTO key_personal_required_environments (org_id, project_id, key_id, environment_id) VALUES ('org_a', 'prj_a1', '`+keyB+`', 'env_a1')`)
		e.adminTok = personalReauth(t, e.admin.auth, e.adminTok, e.adminDev, e.keyA)
		if err := e.pv.DeleteMember(ctx, service.Bearer(e.adminTok), envScope(envA1), ceremonySecretA, "pv-counts"); err != nil {
			t.Fatal(err)
		}
		counts, err = e.pv.Counts(ctx, service.Bearer(e.adminTok), envScope(envA1))
		if err != nil {
			t.Fatal(err)
		}
		a, _ = countFor(counts, e.keyA)
		b, ok := countFor(counts, keyB)
		if len(counts.Items) != 2 || a.Holders != 0 || a.Notices != 1 || !ok || !b.Required || b.Holders != 0 {
			t.Fatalf("marks after removal: items=%d a=%+v b=%+v", len(counts.Items), a, b)
		}

		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'personal_value.counts_read' AND env_id = 'env_a1'`); n != 3 {
			t.Fatalf("counts_read events = %d, want one per read", n)
		}
		// Without manage-members the matrix is nonexistent.
		adminID := queryString(t, db, `SELECT principal_id FROM accounts WHERE username = 'pv-counts-admin'`)
		manage := `SELECT id FROM grants WHERE principal_id = '` + adminID + `' AND capability = 'manage-members'`
		execRaw(t, db, `DELETE FROM grant_origins WHERE grant_id IN (`+manage+`)`)
		execRaw(t, db, `DELETE FROM grants WHERE id IN (`+manage+`)`)
		if _, err := e.pv.Counts(ctx, service.Bearer(e.adminTok), envScope(envA1)); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("counts without manage-members = %v", err)
		}
	})
}

// The offboarding report lists the shared keys a person is recorded as
// receiving, matched through a developer credential's recorded authority, and
// the service accounts they created: names only, with its evidence window and
// fixed limits (personal-values ADR, Offboarding 3 and 4).
func TestPersonalOffboardingReport(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		const member = "pv-offboard"
		e := personalFixture(t, db, member)
		ctx := t.Context()
		owner := e.admin.boot.PrincipalID
		e.setOwn(t, ceremonySecretA, e.keyA, personalPlain)

		// The developer credential receives SECRET_B as a shared value and
		// SECRET_A as the member's own personal value, which is never listed.
		minted, _ := mintDeveloper(t, db, &e.ceremonyEnv, time.Hour, true)
		// Minting previewed both keys under the member's own principal; only
		// the authority match finds the credential's later receipt.
		credentialFetch := time.Now().UTC().Truncate(time.Microsecond)
		if _, err := deliverySvc(t, db).Fetch(ctx, minted.Value, envScope(envA1), "", service.FetchOptions{}); err != nil {
			t.Fatal(err)
		}
		authority := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'identity.disclosure' AND authority_id = '`+string(owner)+`' AND actor_id <> '`+string(owner)+`'`)
		if authority == 0 {
			t.Fatal("fixture: no disclosure recorded under the member's authority")
		}
		execRaw(t, db, `INSERT INTO principals (id, kind, class, created_at) VALUES ('mch_pv_offboard', 'machine', 'workload', `+ts+`)`)
		execRaw(t, db, `INSERT INTO service_accounts (id, principal_id, org_id, project_id, name, kind, created_at, created_by) `+
			`VALUES ('sa_pv_offboard', 'mch_pv_offboard', 'org_a', 'prj_a1', 'deploy-bot', 'workload', `+ts+`, '`+string(owner)+`')`)

		// The member leaves: no grant remains. SECRET_B is rotated afterwards.
		for _, c := range []string{"read", "edit", "publish", "reveal", "project-settings"} {
			revokeCeremonyGrant(t, db, member, c)
		}
		publishValue(t, e.values, service.LocalPrincipal(custodian), scopeEnv(orgA, prjA1, envA1), ceremonySecretB, "plaintext-rotated-"+ceremonySecretB)

		adminID := domain.PrincipalID(queryString(t, db, `SELECT principal_id FROM accounts WHERE username = '`+member+`-admin'`))
		admin := service.Bearer(e.adminTok)
		project := scopeProject(orgA, prjA1)
		// manage-members alone is not enough: the report reads the audit trail.
		if _, err := e.pv.OffboardingReport(ctx, admin, project, member); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("report without audit-read = %v", err)
		}
		grantOrgCap(t, db, "g_pv_offboard_audit", adminID, domain.CapAuditRead)

		envName := queryString(t, db, `SELECT name FROM environments WHERE id = 'env_a1'`)
		for _, ask := range []string{member, string(owner)} {
			report, err := e.pv.OffboardingReport(ctx, admin, project, ask)
			if err != nil {
				t.Fatalf("report for %s: %v", ask, err)
			}
			if report.Principal != owner || report.Username != member {
				t.Fatalf("report subject = %s/%s", report.Principal, report.Username)
			}
			if len(report.Received) != 2 {
				t.Fatalf("received = %+v", report.Received)
			}
			for _, x := range report.Received {
				if x.EnvironmentID != envA1 || x.EnvironmentName != envName || x.LastReceivedAt.IsZero() {
					t.Fatalf("receipt = %+v", x)
				}
				// SECRET_B changed after receipt; SECRET_A was only revealed
				// as the fixture's shared value and never changed since.
				if want := x.KeyName == ceremonySecretB; x.ChangedSince != want || (x.KeyName != ceremonySecretA && x.KeyName != ceremonySecretB) {
					t.Fatalf("receipt %s changed_since=%v", x.KeyName, x.ChangedSince)
				}
				if x.KeyName == ceremonySecretB && x.LastReceivedAt.Before(credentialFetch) {
					t.Fatalf("receipt %s misses the developer credential's disclosure", x.KeyName)
				}
				if x.LastReceivedAt.Before(report.EvidenceFrom) {
					t.Fatalf("receipt %s predates the evidence window", x.KeyName)
				}
			}
			if len(report.ServiceAccounts) != 1 || report.ServiceAccounts[0].Name != "deploy-bot" || report.ServiceAccounts[0].ID != "sa_pv_offboard" {
				t.Fatalf("service accounts = %+v", report.ServiceAccounts)
			}
			if !slices.Equal(report.Limits, service.OffboardingLimits) || report.EvidenceFrom.After(report.GeneratedAt) {
				t.Fatalf("limits or window: from=%v generated=%v", report.EvidenceFrom, report.GeneratedAt)
			}
			for _, want := range []string{"cannot undo", "backups"} {
				if !slices.ContainsFunc(report.Limits, func(l string) bool { return strings.Contains(l, want) }) {
					t.Fatalf("limits lack %q", want)
				}
			}
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'personal_value.offboarding_report_read' AND object_id = '`+string(owner)+`' AND actor_id = '`+string(adminID)+`'`); n != 2 {
			t.Fatalf("offboarding_report_read events = %d, want 2", n)
		}

		// A nonexistent member and a principal with no tie to the project
		// answer alike.
		for _, ask := range []string{"nobody-at-all", "usr_bob"} {
			if _, err := e.pv.OffboardingReport(ctx, admin, project, ask); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("report for %s = %v, want the nonexistent answer", ask, err)
			}
		}
		// The invited administrator holds grants here: an empty report, not 404.
		if r, err := e.pv.OffboardingReport(ctx, admin, project, member+"-admin"); err != nil || len(r.Received) != 0 || len(r.ServiceAccounts) != 0 {
			t.Fatalf("report for a member with nothing received: err=%v", err)
		}
		// The report never carries a value.
		for _, table := range []string{"audit_tenant_events", "audit_instance_events"} {
			if n := queryInt(t, db, `SELECT COUNT(*) FROM `+table+` WHERE payload LIKE '%`+personalPlain+`%' OR payload LIKE '%plaintext-%'`); n != 0 {
				t.Fatalf("%s holds a value", table)
			}
		}
	})
}
