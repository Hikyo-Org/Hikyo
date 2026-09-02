package isolation

import (
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestMemberInviteSQLite(t *testing.T)   { runMemberInvite(t, seededDB(t, openSQLite)) }
func TestMemberInvitePostgres(t *testing.T) { runMemberInvite(t, seededDB(t, openPostgres)) }

// runMemberInvite (#568, human-auth ADR § Identity linking): a manage-members
// holder invites a human at organisation or instance scope; the account, the
// optional template grants and the credential-establishment authority commit
// in ONE transaction; the invitee establishes a password with the authority
// and signs in like anyone else; a taken username is a conflict that leaves
// nothing behind; and both trails carry the fact — member.invited on the
// scope's trail, the mint on the instance trail with issuer `invitation`.
func runMemberInvite(t *testing.T, db *store.DB) {
	factorAdmin := bootstrapFactorAdmin(t, db)
	auth, password := factorAdmin.auth, factorAdmin.password
	base := time.Now().UTC()
	clk := base
	auth.Now = func() time.Time { return clk }
	grants := &service.Grants{DB: db, Auth: auth, Now: func() time.Time { return clk }}
	ctx := t.Context()

	// The bootstrap administrator holds `operator` (manage-members at instance
	// scope), which covers org_a by inheritance. Step up: membership writes
	// are administrative.
	login, err := auth.LocalLogin(ctx, "factor-admin", password, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := auth.EnrolTOTPStart(ctx, login.SessionToken, password)
	if err != nil {
		t.Fatalf("enrol start: %v", err)
	}
	clk = base.Add(30 * time.Second)
	confirmed, err := auth.EnrolTOTPConfirm(ctx, login.SessionToken, totpCode(t, uri, clk))
	if err != nil {
		t.Fatalf("enrol confirm: %v", err)
	}
	clk = base.Add(60 * time.Second)
	stepped, err := auth.StepUpTOTP(ctx, confirmed.SessionToken, totpCode(t, uri, clk))
	if err != nil {
		t.Fatalf("step-up: %v", err)
	}
	admin := service.Bearer(stepped.SessionToken)

	// Organisation invite with a template: account + grants + authority.
	inv, err := grants.InviteMember(ctx, admin, service.InviteSpec{
		Scope: domain.Scope{Org: "org_a"}, Username: "  dana  ", DisplayName: "Dana",
		Template: domain.TemplateEditor, Delivery: "response",
	})
	if err != nil {
		t.Fatalf("invite into org_a: %v", err)
	}
	if inv.GrantsCreated != 2 {
		t.Errorf("editor at org expands to read+edit; grants_created = %d", inv.GrantsCreated)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM accounts WHERE username = 'dana' AND display_name = 'Dana'"); n != 1 {
		t.Errorf("trimmed username / display name not stored: %d rows", n)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM grants WHERE principal_id = '"+string(inv.PrincipalID)+"' AND org_id = 'org_a'"); n != 2 {
		t.Errorf("org grants for the invitee = %d, want 2", n)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'member.invited' AND org_id = 'org_a' AND payload LIKE '%\"template\":\"editor\"%'"); n != 1 {
		t.Errorf("member.invited on the org trail = %d, want 1", n)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'auth.credential_authority_minted' AND payload LIKE '%\"issued_by\":\"invitation\"%'"); n != 1 {
		t.Errorf("invitation mint on the instance trail = %d, want 1", n)
	}
	// The authority is session-less and establishes only a password; the
	// invitee then logs in with it — and cannot reuse the authority.
	const danaPassword = "dana's first password, long enough"
	if err := auth.EstablishCredential(ctx, inv.Authority, danaPassword); err != nil {
		t.Fatalf("establish with the invitation authority: %v", err)
	}
	if _, err := auth.LocalLogin(ctx, "dana", danaPassword, service.ArtifactCLI); err != nil {
		t.Fatalf("the invitee cannot log in with the established credential: %v", err)
	}
	if err := auth.EstablishCredential(ctx, inv.Authority, "a second password, long enough"); err == nil {
		t.Error("a consumed invitation authority was accepted again")
	}

	// A taken username is a conflict, and the failed transaction leaves no
	// orphan principal, account or grant behind.
	principalsBefore := queryInt(t, db, "SELECT COUNT(*) FROM principals")
	_, err = grants.InviteMember(ctx, admin, service.InviteSpec{
		Scope: domain.Scope{Org: "org_a"}, Username: "dana", Template: domain.TemplateViewer, Delivery: "response",
	})
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate username: %v, want domain.ErrConflict", err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM principals"); n != principalsBefore {
		t.Errorf("a refused invitation left a principal behind: %d → %d", principalsBefore, n)
	}

	// Instance invite without a template: a grantless account that can sign
	// in and see nothing, recorded on the instance trail.
	sam, err := grants.InviteMember(ctx, admin, service.InviteSpec{Username: "sam", Delivery: "response"})
	if err != nil {
		t.Fatalf("invite at instance scope: %v", err)
	}
	if sam.GrantsCreated != 0 {
		t.Errorf("no template → no grants; got %d", sam.GrantsCreated)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM grants WHERE principal_id = '"+string(sam.PrincipalID)+"'"); n != 0 {
		t.Errorf("grantless invite wrote %d grant rows", n)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM audit_instance_events WHERE type = 'member.invited'"); n != 1 {
		t.Errorf("member.invited on the instance trail = %d, want 1", n)
	}

	// Malformed addresses are invalid, never widened or narrowed.
	if _, err := grants.InviteMember(ctx, admin, service.InviteSpec{
		Scope: domain.Scope{Org: "org_a", Project: "prj_a"}, Username: "x", Delivery: "response",
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("project-scoped invite: %v, want domain.ErrInvalid", err)
	}
	if _, err := grants.InviteMember(ctx, admin, service.InviteSpec{
		Scope: domain.Scope{Org: "org_a"}, Username: "   ", Delivery: "response",
	}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("blank username: %v, want domain.ErrInvalid", err)
	}

	// The invitee holds no manage-members anywhere: inviting is refused
	// uniformly, and nothing is written.
	danaLogin, err := auth.LocalLogin(ctx, "dana", danaPassword, service.ArtifactCLI)
	if err != nil {
		t.Fatal(err)
	}
	before := queryInt(t, db, "SELECT COUNT(*) FROM accounts")
	if _, err := grants.InviteMember(ctx, service.Bearer(danaLogin.SessionToken), service.InviteSpec{
		Scope: domain.Scope{Org: "org_a"}, Username: "eve", Delivery: "response",
	}); err == nil {
		t.Error("an editor invited a member")
	} else if !errors.Is(err, domain.ErrUnauthorized) && !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("editor invite refusal: %v, want a uniform ErrUnauthorized/ErrNotFound", err)
	}
	if n := queryInt(t, db, "SELECT COUNT(*) FROM accounts"); n != before {
		t.Errorf("a refused invitation created an account")
	}
}
