package isolation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"mime/quotedprintable"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mailtest"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// The email invitation of an existing account (#1207,
// docs/spec/email-invitations.md): send, list, revoke, preview and accept.

func TestEmailInvitations(t *testing.T) { forEngines(t, runEmailInvitations) }

type emailInvitationRig struct {
	t     *testing.T
	db    *store.DB
	svc   *service.EmailInvitations
	sink  *mailtest.Sink
	now   time.Time
	clock *time.Time
}

func newEmailInvitationRig(t *testing.T, db *store.DB, configured bool) *emailInvitationRig {
	t.Helper()
	sink := mailtest.New(t, "implicit")
	cfg := map[string]string{}
	if configured {
		cfg = map[string]string{"HIKYO_MAIL_ADDR": sink.Addr, "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_FROM": "hikyo@example.test", "HIKYO_MAIL_CA_PEM": sink.CAPEM, "HIKYO_MAIL_ALLOWED_CIDRS": "127.0.0.0/8"}
	}
	bundle, err := runtimeconfig.Prepare(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := &emailInvitationRig{t: t, db: db, sink: sink, now: time.Now().UTC()}
	r.clock = &r.now
	r.svc = &service.EmailInvitations{
		DB: db, Now: func() time.Time { return *r.clock }, ExternalOrigin: "https://hikyo.test",
		Mail:   func(context.Context) (*runtimeconfig.Bundle, error) { return bundle, nil },
		Budget: service.NewBudgetWithClock(func() time.Time { return *r.clock }),
	}
	return r
}

// account seeds a human with a verified email address.
func (r *emailInvitationRig) account(id, email string) domain.PrincipalID {
	r.t.Helper()
	execRaw(r.t, r.db, fmt.Sprintf(`INSERT INTO principals (id, kind, created_at) VALUES ('%s', 'human', %s)`, id, ts))
	execRaw(r.t, r.db, fmt.Sprintf(`INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('acc_%s', '%s', '%s', '%s', '%s', %s, %s)`,
		id, id, id, id, email, tsMicro, tsMicro))
	return domain.PrincipalID(id)
}

// token reads the invitation link out of the nth captured message.
func (r *emailInvitationRig) token(n int) string {
	r.t.Helper()
	msgs := r.sink.Messages()
	if len(msgs) <= n {
		r.t.Fatalf("only %d messages captured, want index %d", len(msgs), n)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(msgs[n].Data))
	if err != nil {
		r.t.Fatal(err)
	}
	var body io.Reader = parsed.Body
	if strings.EqualFold(parsed.Header.Get("Content-Transfer-Encoding"), "quoted-printable") {
		body = quotedprintable.NewReader(body)
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		r.t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "https://hikyo.test/invitations/accept#") {
			u, err := url.Parse(line)
			if err != nil {
				r.t.Fatal(err)
			}
			frag, err := url.ParseQuery(u.Fragment)
			if err != nil {
				r.t.Fatal(err)
			}
			return frag.Get("token")
		}
	}
	r.t.Fatalf("invitation link absent from mail body")
	return ""
}

func (r *emailInvitationRig) send(actor domain.PrincipalID, email string) (service.EmailInvitationReceipt, error) {
	r.t.Helper()
	res, err := r.svc.Send(r.t.Context(), service.LocalPrincipal(actor), service.EmailInvitationSendSpec{Org: orgA, Email: email, Template: domain.TemplateViewer})
	r.svc.WaitForMail()
	return res, err
}

func (r *emailInvitationRig) refusals(cause string) int64 {
	r.t.Helper()
	return queryInt(r.t, r.db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'member.email_invitation_refused' AND payload LIKE '%"cause":"`+cause+`"%'`)
}

func (r *emailInvitationRig) rows() int64 {
	r.t.Helper()
	return queryInt(r.t, r.db, `SELECT COUNT(*) FROM org_email_invitations`)
}

func (r *emailInvitationRig) grantsOf(p domain.PrincipalID) int64 {
	r.t.Helper()
	return queryInt(r.t, r.db, `SELECT COUNT(*) FROM grants WHERE principal_id = '`+string(p)+`' AND org_id = 'org_a'`)
}

func wantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("%s = %v, want the uniform not-found", what, err)
	}
}

func runEmailInvitations(t *testing.T, db *store.DB) {
	rig := newEmailInvitationRig(t, db, true)
	ctx := t.Context()
	admin := orgAdmin
	invitee := rig.account("usr_ei_invitee", "invitee@example.test")
	other := rig.account("usr_ei_other", "other@example.test")
	// bob already belongs to org B only; the footprint gate must not stop the
	// invited acceptance, though it still stops a direct grant.
	execRaw(t, db, fmt.Sprintf(`INSERT INTO accounts (id, principal_id, username, display_name, email, email_verified_at, created_at) VALUES ('acc_ei_bob', 'usr_bob', 'ei-bob', 'bob', 'bob@example.test', %s, %s)`, tsMicro, tsMicro))

	t.Run("unmatched_address_writes_the_same_and_sends_nothing", func(t *testing.T) {
		res, err := rig.send(admin, "Nobody@Example.TEST")
		if err != nil {
			t.Fatal(err)
		}
		if res.Email != "Nobody@example.test" || res.Template != domain.TemplateViewer || !res.ExpiresAt.Equal(store.CanonTime(rig.now.Add(service.EmailInvitationLifetime))) {
			t.Fatalf("receipt %+v", res)
		}
		if len(rig.sink.Messages()) != 0 {
			t.Fatal("an unknown address received mail")
		}
		if rig.rows() != 1 {
			t.Fatalf("rows = %d, want 1", rig.rows())
		}
		list, err := rig.svc.List(ctx, service.LocalPrincipal(admin), orgA)
		if err != nil || len(list) != 1 || list[0].Email != "Nobody@example.test" || list[0].InvitedBy != admin {
			t.Fatalf("list = %+v, %v", list, err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'member.email_invited' AND org_id = 'org_a' AND payload NOT LIKE '%account%' AND payload NOT LIKE '%matched%'`); n != 1 {
			t.Fatalf("org-trail invited events without a match fact = %d, want 1", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'member.email_invitation_dispatch' AND payload LIKE '%"matched":false%'`); n != 1 {
			t.Fatalf("instance dispatch (unmatched) = %d, want 1", n)
		}
		if n := auditCount(t, db, "member.email_invitation_mail_outcome"); n != 0 {
			t.Fatalf("mail outcomes = %d, want 0", n)
		}
		execRaw(t, db, `DELETE FROM org_email_invitations`)
	})

	t.Run("matched_send_mails_once_and_the_person_accepts", func(t *testing.T) {
		res, err := rig.send(admin, "invitee@example.test")
		if err != nil {
			t.Fatal(err)
		}
		if len(rig.sink.Messages()) != 1 || rig.sink.Messages()[0].Recipients[0] != "invitee@example.test" {
			t.Fatalf("mail = %+v", rig.sink.Messages())
		}
		token := rig.token(0)
		if auditCount(t, db, "member.email_invitation_mail_outcome") != 1 {
			t.Fatal("no mail outcome recorded")
		}
		before := rig.grantsOf(invitee)
		var gen0 int64
		_ = tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			var e error
			gen0, e = az.PrincipalGeneration(ctx, invitee)
			return e
		})

		// Someone else's session, and a read-only preview, change nothing.
		_, err = rig.svc.Preview(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "preview by another account", err)
		_, err = rig.svc.Accept(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "accept by another account", err)
		if rig.refusals("other-account") != 2 {
			t.Fatalf("other-account refusals = %d, want 2", rig.refusals("other-account"))
		}
		if rig.rows() != 1 || rig.grantsOf(other) != 0 {
			t.Fatal("a refused request changed state")
		}
		offer, err := rig.svc.Preview(ctx, service.LocalPrincipal(invitee), token)
		if err != nil || offer.Org != orgA || offer.OrgName != "org-a" || offer.Template != domain.TemplateViewer || !offer.ExpiresAt.Equal(res.ExpiresAt) {
			t.Fatalf("preview = %+v, %v", offer, err)
		}
		if rig.rows() != 1 || rig.grantsOf(invitee) != before {
			t.Fatal("preview wrote")
		}

		got, err := rig.svc.Accept(ctx, service.LocalPrincipal(invitee), token)
		if err != nil {
			t.Fatal(err)
		}
		if got.Org != orgA || got.OrgName != "org-a" || got.GrantsCreated == 0 || !got.SignedOut || got.Template != domain.TemplateViewer {
			t.Fatalf("accept = %+v", got)
		}
		if rig.rows() != 0 || rig.grantsOf(invitee) == before {
			t.Fatal("accept did not consume the row and grant")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM grant_origins o JOIN grants g ON g.id = o.grant_id WHERE g.principal_id = 'usr_ei_invitee' AND o.kind = 'manual' AND o.subject = '`+string(admin)+`'`); n == 0 {
			t.Fatal("grants do not carry the inviter's manual origin")
		}
		if auditCount(t, db, "member.email_invitation_accepted") != 1 {
			t.Fatal("no acceptance event")
		}
		var gen1 int64
		_ = tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			var e error
			gen1, e = az.PrincipalGeneration(ctx, invitee)
			return e
		})
		if gen1 <= gen0 {
			t.Fatalf("generation %d -> %d: sessions were not ended", gen0, gen1)
		}
		_, err = rig.svc.Accept(ctx, service.LocalPrincipal(invitee), token)
		wantNotFound(t, "second accept", err)
		if rig.refusals("unknown") == 0 {
			t.Fatal("the spent token left no cause")
		}
	})

	t.Run("a_member_of_another_org_can_accept_but_not_be_granted_directly", func(t *testing.T) {
		grants := &service.Grants{DB: db}
		if _, err := grants.Create(ctx, service.LocalPrincipal(admin), service.GrantSpec{Target: bob, Capability: domain.CapRead, Scope: domain.Scope{Org: orgA}}); !errors.Is(err, service.ErrUnknownPrincipal) {
			t.Fatalf("direct grant to a foreign-footprint account = %v, want ErrUnknownPrincipal", err)
		}
		before := len(rig.sink.Messages())
		if _, err := rig.send(admin, "bob@example.test"); err != nil {
			t.Fatal(err)
		}
		token := rig.token(before)
		got, err := rig.svc.Accept(ctx, service.LocalPrincipal(bob), token)
		if err != nil || got.GrantsCreated == 0 {
			t.Fatalf("accept by a member of org B = %+v, %v", got, err)
		}
		if rig.grantsOf(bob) == 0 {
			t.Fatal("no grant landed")
		}
	})

	t.Run("an_unmatched_row_never_validates", func(t *testing.T) {
		before := len(rig.sink.Messages())
		if _, err := rig.send(admin, "ghost@example.test"); err != nil {
			t.Fatal(err)
		}
		if len(rig.sink.Messages()) != before {
			t.Fatal("unknown address was mailed")
		}
		// Even a holder of the (never mailed) token for the stored verifier has
		// no account bound, so every account is refused. Forge one by taking
		// the verifier of a different row: send to a matched address, then
		// move that token onto the unmatched row.
		execRaw(t, db, `DELETE FROM org_email_invitations`)
		if _, err := rig.send(admin, "other@example.test"); err != nil {
			t.Fatal(err)
		}
		token := rig.token(len(rig.sink.Messages()) - 1)
		execRaw(t, db, `UPDATE org_email_invitations SET account_id = NULL`)
		_, err := rig.svc.Preview(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "preview of an unmatched row", err)
		if rig.refusals("unmatched") != 1 {
			t.Fatalf("unmatched refusals = %d", rig.refusals("unmatched"))
		}
		execRaw(t, db, `DELETE FROM org_email_invitations`)
	})

	t.Run("replace_revoke_and_expiry", func(t *testing.T) {
		before := len(rig.sink.Messages())
		first, err := rig.send(admin, "other@example.test")
		if err != nil {
			t.Fatal(err)
		}
		oldToken := rig.token(before)
		second, err := rig.send(admin, "other@example.test")
		if err != nil {
			t.Fatal(err)
		}
		if rig.rows() != 1 || first.ID != second.ID {
			t.Fatalf("rows=%d ids %s/%s: replace must keep one row", rig.rows(), first.ID, second.ID)
		}
		_, err = rig.svc.Preview(ctx, service.LocalPrincipal(other), oldToken)
		wantNotFound(t, "the replaced link", err)
		newToken := rig.token(before + 1)
		if _, err := rig.svc.Preview(ctx, service.LocalPrincipal(other), newToken); err != nil {
			t.Fatalf("replacement link: %v", err)
		}

		// Expiry: the row is neither listed nor valid, and pruning deletes it.
		*rig.clock = rig.now.Add(service.EmailInvitationLifetime + time.Second)
		_, err = rig.svc.Preview(ctx, service.LocalPrincipal(other), newToken)
		wantNotFound(t, "an expired link", err)
		if rig.refusals("expired") != 1 {
			t.Fatal("expiry cause not recorded")
		}
		list, err := rig.svc.List(ctx, service.LocalPrincipal(admin), orgA)
		if err != nil || len(list) != 0 {
			t.Fatalf("expired row listed: %+v %v", list, err)
		}
		*rig.clock = time.Now().UTC()
		execRaw(t, db, `UPDATE org_email_invitations SET expires_at = '2020-01-01T00:00:00.000000Z'`)
		if err := authService(t, db).ReapSignups(ctx); err != nil {
			t.Fatal(err)
		}
		if rig.rows() != 0 {
			t.Fatal("the hourly run left an expired row")
		}

		// Revoke.
		before = len(rig.sink.Messages())
		res, err := rig.send(admin, "other@example.test")
		if err != nil {
			t.Fatal(err)
		}
		token := rig.token(before)
		if err := rig.svc.Revoke(ctx, service.LocalPrincipal(admin), orgA, res.ID); err != nil {
			t.Fatal(err)
		}
		wantNotFound(t, "revoke twice", rig.svc.Revoke(ctx, service.LocalPrincipal(admin), orgA, res.ID))
		_, err = rig.svc.Preview(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "a revoked link", err)
		if auditCount(t, db, "member.email_invitation_revoked") != 1 {
			t.Fatal("no revoke event")
		}
	})

	t.Run("superseded_epoch_and_lost_inviter_authority_leave_the_row", func(t *testing.T) {
		before := len(rig.sink.Messages())
		if _, err := rig.send(admin, "other@example.test"); err != nil {
			t.Fatal(err)
		}
		token := rig.token(before)
		execRaw(t, db, `UPDATE org_email_invitations SET credential_epoch = credential_epoch + 7`)
		_, err := rig.svc.Accept(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "a superseded epoch", err)
		if rig.refusals("epoch-superseded") != 1 || rig.rows() != 1 {
			t.Fatal("epoch refusal not recorded or row touched")
		}
		execRaw(t, db, `UPDATE org_email_invitations SET credential_epoch = credential_epoch - 7`)

		// The inviter loses the authority between send and acceptance.
		inviter := domain.PrincipalID("usr_ei_inviter")
		execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('usr_ei_inviter', 'human', `+ts+`)`)
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_ei_inv', 'usr_ei_inviter', 'manage-members', 'org_a', `+ts+`)`)
		execRaw(t, db, `DELETE FROM org_email_invitations`)
		before = len(rig.sink.Messages())
		if _, err := rig.send(inviter, "other@example.test"); err != nil {
			t.Fatal(err)
		}
		token = rig.token(before)
		execRaw(t, db, `DELETE FROM grants WHERE id = 'g_ei_inv'`)
		grantsBefore := rig.grantsOf(other)
		_, err = rig.svc.Accept(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "an inviter who lost the authority", err)
		if rig.refusals("inviter-unauthorized") != 1 || rig.rows() != 1 || rig.grantsOf(other) != grantsBefore {
			t.Fatal("the lost-authority refusal changed state")
		}
		_, err = rig.svc.Preview(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "preview after the authority is gone", err)
		execRaw(t, db, `DELETE FROM org_email_invitations`)
	})

	t.Run("a_competing_acceptance_applies_once", func(t *testing.T) {
		before := len(rig.sink.Messages())
		if _, err := rig.send(admin, "other@example.test"); err != nil {
			t.Fatal(err)
		}
		token := rig.token(before)
		var wg sync.WaitGroup
		results := make([]error, 4)
		gotGrants := make([]int, 4)
		for i := range results {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := rig.svc.Accept(ctx, service.LocalPrincipal(other), token)
				results[i], gotGrants[i] = err, res.GrantsCreated
			}()
		}
		wg.Wait()
		won := 0
		for i, err := range results {
			switch {
			case err == nil && gotGrants[i] > 0:
				won++
			case !errors.Is(err, domain.ErrNotFound):
				t.Fatalf("racer %d = %v", i, err)
			}
		}
		if won != 1 {
			t.Fatalf("%d acceptances applied the template, want exactly 1", won)
		}
	})

	t.Run("validation_budget_cap_and_authority", func(t *testing.T) {
		if _, err := rig.send(admin, "not an address"); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("malformed address = %v", err)
		}
		if _, err := rig.svc.Send(ctx, service.LocalPrincipal(admin), service.EmailInvitationSendSpec{Org: orgA, Email: "x@example.test", Template: "nope"}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("unknown template = %v", err)
		}
		if _, err := rig.svc.Send(ctx, service.LocalPrincipal(admin), service.EmailInvitationSendSpec{Org: orgA, Email: "x@example.test"}); !errors.Is(err, domain.ErrInvalid) {
			t.Fatalf("missing template = %v", err)
		}
		if _, err := rig.svc.Send(ctx, service.LocalPrincipal(reader), service.EmailInvitationSendSpec{Org: orgA, Email: "x@example.test", Template: domain.TemplateViewer}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("a non-manager send = %v, want the uniform not-found", err)
		}
		if _, err := rig.svc.Send(ctx, service.LocalPrincipal(admin), service.EmailInvitationSendSpec{Org: orgB, Email: "x@example.test", Template: domain.TemplateViewer}); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("a send into a foreign org = %v", err)
		}
		if _, err := rig.svc.List(ctx, service.LocalPrincipal(reader), orgA); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("a non-manager list = %v", err)
		}

		// The pending cap is independent of the address.
		execRaw(t, db, `DELETE FROM org_email_invitations`)
		for i := range service.EmailInvitationPendingCap {
			execRaw(t, db, fmt.Sprintf(`INSERT INTO org_email_invitations (id, org_id, email, template, inviter_principal_id, account_id, token_verifier, credential_epoch, created_at, expires_at) VALUES ('ein_cap_%d', 'org_a', 'cap%d@example.test', 'viewer', 'usr_orgadmin', NULL, %s, 0, %s, '2099-01-01T00:00:00.000000Z')`,
				i, i, blobLit(db, fmt.Appendf(nil, "cap-verifier-%d", i)), tsMicro))
		}
		for _, addr := range []string{"cap0@example.test", "invitee@example.test", "brandnew@example.test"} {
			if _, err := rig.send(admin, addr); !errors.Is(err, admission.ErrOverloaded) {
				t.Fatalf("send at the cap to %s = %v, want the uniform overload", addr, err)
			}
		}
		execRaw(t, db, `DELETE FROM org_email_invitations`)

		// 30 sends an hour per organisation, then the uniform overload.
		fresh := newEmailInvitationRig(t, db, true)
		for i := range service.BudgetEmailInvitationPerHour {
			if _, err := fresh.send(admin, fmt.Sprintf("budget%d@example.test", i)); err != nil {
				t.Fatalf("send %d = %v", i, err)
			}
		}
		if _, err := fresh.send(admin, "budget-over@example.test"); !errors.Is(err, admission.ErrOverloaded) {
			t.Fatalf("send past the hourly budget = %v", err)
		}
		*fresh.clock = fresh.now.Add(61 * time.Minute)
		if _, err := fresh.send(admin, "budget-later@example.test"); err != nil {
			t.Fatalf("send after the window = %v", err)
		}
		execRaw(t, db, `DELETE FROM org_email_invitations`)
	})

	t.Run("an_unconfigured_mailer_refuses_before_reading_the_address", func(t *testing.T) {
		bare := newEmailInvitationRig(t, db, false)
		for _, addr := range []string{"fine@example.test", "not an address", ""} {
			_, err := bare.send(admin, addr)
			var detail interface{ SafeDetail() string }
			if !errors.As(err, &detail) || detail.SafeDetail() != "mailer-unconfigured" || !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("send(%q) on an unconfigured instance = %v", addr, err)
			}
		}
		if _, err := bare.send(reader, "fine@example.test"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("authorization precedes the predicate: %v", err)
		}
		if bare.rows() != 0 {
			t.Fatal("a refused send wrote a row")
		}
	})
}

// runEmailInvitationAuditLifecycle drives every email invitation event once on
// the shared audit fixture: invited, dispatch and mail outcome, a refusal,
// accepted, and revoked.
func runEmailInvitationAuditLifecycle(t *testing.T, db *store.DB) {
	t.Helper()
	rig := newEmailInvitationRig(t, db, true)
	invitee := rig.account("usr_ei_audit_a", "audit-a@example.test")
	other := rig.account("usr_ei_audit_b", "audit-b@example.test")
	if _, err := rig.send(orgAdmin, "audit-a@example.test"); err != nil {
		t.Fatal(err)
	}
	token := rig.token(0)
	if _, err := rig.svc.Accept(t.Context(), service.LocalPrincipal(other), token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("accept by another account = %v, want not found", err)
	}
	if _, err := rig.svc.Accept(t.Context(), service.LocalPrincipal(invitee), token); err != nil {
		t.Fatal(err)
	}
	res, err := rig.send(orgAdmin, "audit-b@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err := rig.svc.Revoke(t.Context(), service.LocalPrincipal(orgAdmin), orgA, res.ID); err != nil {
		t.Fatal(err)
	}
}
