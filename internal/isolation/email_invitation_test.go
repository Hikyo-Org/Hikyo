package isolation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mime/quotedprintable"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mailtest"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/server"
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
	// 999ns past a microsecond, as Linux clocks give: truncation and rounding then differ.
	r := &emailInvitationRig{t: t, db: db, sink: sink, now: time.Now().UTC().Truncate(time.Microsecond).Add(999 * time.Nanosecond)}
	r.clock = &r.now
	r.svc = &service.EmailInvitations{
		DB: db, Now: func() time.Time { return *r.clock }, ExternalOrigin: "https://hikyo.test", ExternalOriginExplicit: true,
		Mail:   func(context.Context) (*runtimeconfig.Bundle, error) { return bundle, nil },
		Budget: service.NewBudgetWithClock(func() time.Time { return *r.clock }),
		Sends:  service.NewMailSends(),
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
	r.svc.Sends.Wait()
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

// tenantEvents counts the inviting organisation's trail.
func (r *emailInvitationRig) tenantEvents() int64 {
	r.t.Helper()
	return queryInt(r.t, r.db, `SELECT COUNT(*) FROM audit_tenant_events WHERE org_id = 'org_a'`)
}

// targetState is everything an invitation must leave untouched while pending.
type targetState struct {
	generation int64
	sessions   int64
	grants     string
	footprint  string
	orgCount   int
}

func (r *emailInvitationRig) state(p domain.PrincipalID) targetState {
	r.t.Helper()
	var st targetState
	err := tx.Read(r.t.Context(), r.db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
		var err error
		if st.generation, err = az.PrincipalGeneration(ctx, p); err != nil {
			return err
		}
		grants, err := az.GrantsForResetTarget(ctx, p)
		if err != nil {
			return err
		}
		foot, err := az.PrincipalFootprintOrgs(ctx, p)
		if err != nil {
			return err
		}
		st.grants, st.footprint, st.orgCount = fmt.Sprint(grants), fmt.Sprint(foot), len(foot)
		return nil
	})
	if err != nil {
		r.t.Fatal(err)
	}
	st.sessions = queryInt(r.t, r.db, `SELECT COUNT(*) FROM sessions WHERE principal_id = '`+string(p)+`'`)
	return st
}

func jsonKeys(t *testing.T, raw string) []string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return slices.Sorted(maps.Keys(m))
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
		if err != nil || len(list) != 1 || list[0].Email != "Nobody@example.test" || list[0].InvitedBy != admin ||
			!list[0].CreatedAt.Equal(res.CreatedAt) || !list[0].ExpiresAt.Equal(res.ExpiresAt) {
			t.Fatalf("list = %+v, %v", list, err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type = 'member.email_invited' AND org_id = 'org_a' AND payload NOT LIKE '%account%' AND payload NOT LIKE '%matched%'`); n != 1 {
			t.Fatalf("org-trail invited events without a match fact = %d, want 1", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'member.email_invitation_dispatch' AND payload LIKE '%"matched":false%'`); n != 1 {
			t.Fatalf("instance dispatch (unmatched) = %d, want 1", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'member.email_invitation_mail_outcome' AND outcome = 'success' AND payload LIKE '%"mail_sent":false%'`); n != 1 {
			t.Fatalf("unmatched no-mail outcomes = %d, want 1", n)
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
		if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'member.email_invitation_mail_outcome' AND outcome = 'success' AND payload LIKE '%"mail_sent":true%'`) != 1 {
			t.Fatal("no delivered-mail outcome recorded")
		}
		before := rig.grantsOf(invitee)
		// A live developer credential of the person must not outlive the
		// acceptance, which advances their generation.
		developer := seedSecurityDeveloper(t, db, sessionWithWindows(t, db, invitee), envScope(envA1), "invitee")
		var gen0 int64
		_ = tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			var e error
			gen0, e = az.PrincipalGeneration(ctx, invitee)
			return e
		})

		// Someone else's session, and a read-only preview, change nothing.
		trail := rig.tenantEvents()
		_, err = rig.svc.Preview(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "preview by another account", err)
		_, err = rig.svc.Accept(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "accept by another account", err)
		if rig.tenantEvents() != trail {
			t.Fatal("a refused acceptance wrote to the inviting organisation's trail")
		}
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
		if authenticateCaller(t, db, developer).Principal != "" {
			t.Fatal("the person's developer credential survived the acceptance")
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

	t.Run("a_failed_send_is_counted_in_the_scrape_across_a_generation_replacement", func(t *testing.T) {
		sends := service.NewMailSends()
		var calls atomic.Int64
		gate := make(chan struct{})
		replacement := *rig.svc
		replacement.Sends = sends
		// The first call per send is the configured-mailer predicate; the
		// second is the background delivery, which fails once released.
		replacement.Mail = func(ctx context.Context) (*runtimeconfig.Bundle, error) {
			if calls.Add(1)%2 == 1 {
				return rig.svc.Mail(ctx)
			}
			<-gate
			return nil, errors.New("smtp down")
		}
		scrape := func(m *server.Metrics) string {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/metrics", nil)
			req.RemoteAddr = "127.0.0.1:1234"
			server.NewOperational(nil, mcpOperationalHealth{}, m).ServeHTTP(rec, req)
			for _, line := range strings.Split(rec.Body.String(), "\n") {
				if strings.HasPrefix(line, "hikyo_email_invitation_mail_failures_total ") {
					return strings.TrimPrefix(line, "hikyo_email_invitation_mail_failures_total ")
				}
			}
			t.Fatal("the failure counter is absent from the scrape")
			return ""
		}
		first := server.NewMetrics(nil)
		first.SetEmailInvitationFailureSource(sends.Failures)

		if _, err := replacement.Send(ctx, service.LocalPrincipal(admin), service.EmailInvitationSendSpec{Org: orgA, Email: "other@example.test", Template: domain.TemplateViewer}); err != nil {
			t.Fatal(err)
		}
		// The generation that started the send is replaced while it is in flight.
		second := server.NewMetrics(nil)
		second.SetEmailInvitationFailureSource(sends.Failures)
		if got := scrape(second); got != "0" {
			t.Fatalf("failures before the send finished = %s", got)
		}
		close(gate)
		sends.Wait()
		if got := scrape(second); got != "1" {
			t.Fatalf("failures after a late failure on a replaced generation = %s, want 1", got)
		}
		if got := scrape(first); got != "1" {
			t.Fatalf("the replaced generation's scrape = %s, want 1", got)
		}
		execRaw(t, db, `DELETE FROM org_email_invitations`)
	})

	t.Run("an_unverified_address_is_unmatched", func(t *testing.T) {
		execRaw(t, db, `INSERT INTO principals (id, kind, created_at) VALUES ('usr_ei_unverified', 'human', `+ts+`)`)
		execRaw(t, db, `INSERT INTO accounts (id, principal_id, username, display_name, email, created_at) VALUES ('acc_usr_ei_unverified', 'usr_ei_unverified', 'ei-unverified', 'ei-unverified', 'unverified@example.test', `+tsMicro+`)`)
		before := len(rig.sink.Messages())
		if _, err := rig.send(admin, "unverified@example.test"); err != nil {
			t.Fatal(err)
		}
		rig.svc.Sends.Wait()
		if len(rig.sink.Messages()) != before {
			t.Fatal("an unverified address was mailed")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM org_email_invitations WHERE email = 'unverified@example.test' AND account_id IS NOT NULL`); n != 0 {
			t.Fatalf("an unverified address bound %d invitations to its account", n)
		}
		execRaw(t, db, `DELETE FROM org_email_invitations`)
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
		trail := rig.tenantEvents()
		_, err := rig.svc.Accept(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "a superseded epoch", err)
		if rig.tenantEvents() != trail {
			t.Fatal("an epoch refusal wrote to the inviting organisation's trail")
		}
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
		trail = rig.tenantEvents()
		_, err = rig.svc.Accept(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "an inviter who lost the authority", err)
		if rig.tenantEvents() != trail {
			t.Fatal("a lost-authority acceptance wrote a grant.denied to the inviting organisation's trail")
		}
		if rig.refusals("inviter-unauthorized") != 1 || rig.rows() != 1 || rig.grantsOf(other) != grantsBefore {
			t.Fatal("the lost-authority refusal changed state")
		}
		_, err = rig.svc.Preview(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "preview after the authority is gone", err)
		if rig.tenantEvents() != trail {
			t.Fatal("a lost-authority preview wrote to the inviting organisation's trail")
		}
		// The unmatched and expired causes leave the trail alone as well.
		execRaw(t, db, `UPDATE org_email_invitations SET account_id = NULL`)
		_, err = rig.svc.Accept(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "an unmatched row", err)
		execRaw(t, db, `UPDATE org_email_invitations SET expires_at = '2020-01-01T00:00:00.000000Z'`)
		_, err = rig.svc.Accept(ctx, service.LocalPrincipal(other), token)
		wantNotFound(t, "an expired row", err)
		if rig.tenantEvents() != trail {
			t.Fatal("an unmatched or expired acceptance wrote to the inviting organisation's trail")
		}
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

	t.Run("matched_and_unmatched_sends_look_the_same", func(t *testing.T) {
		execRaw(t, db, `DELETE FROM org_email_invitations`)
		watermark := queryInt(t, db, `SELECT COALESCE(MAX(seq), 0) FROM audit_tenant_events`)
		matched, err := rig.send(admin, "other@example.test")
		if err != nil {
			t.Fatal(err)
		}
		unmatched, err := rig.send(admin, "stranger@example.test")
		if err != nil {
			t.Fatal(err)
		}
		list, err := rig.svc.List(ctx, service.LocalPrincipal(admin), orgA)
		if err != nil || len(list) != 2 {
			t.Fatalf("list = %+v, %v", list, err)
		}
		payload := func(id string) string {
			return queryString(t, db, `SELECT payload FROM audit_tenant_events WHERE type = 'member.email_invited' AND object_id = '`+id+`'`)
		}
		if !slices.Equal(jsonKeys(t, payload(matched.ID)), jsonKeys(t, payload(unmatched.ID))) {
			t.Fatalf("org-trail payload keys differ: %s vs %s", payload(matched.ID), payload(unmatched.ID))
		}
		for _, needle := range []string{"acc_usr_ei_other", "usr_ei_other"} {
			if n := queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE org_id = 'org_a' AND seq > `+fmt.Sprint(watermark)+` AND (payload LIKE '%`+needle+`%' OR object_id LIKE '%`+needle+`%' OR actor_id LIKE '%`+needle+`%')`); n != 0 {
				t.Fatalf("the org trail names the matched account (%s) in %d events", needle, n)
			}
		}
		// The match fact lives on the instance trail only.
		if queryInt(t, db, `SELECT COUNT(*) FROM audit_instance_events WHERE type = 'member.email_invitation_dispatch' AND payload LIKE '%"matched":true%'`) == 0 {
			t.Fatal("matched dispatch intent missing")
		}
		execRaw(t, db, `DELETE FROM org_email_invitations`)
	})

	t.Run("nothing_about_the_person_changes_while_pending", func(t *testing.T) {
		target := rig.account("usr_ei_pending", "pending@example.test")
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_ei_pending', 'usr_ei_pending', 'read', 'org_a', `+ts+`)`)
		sessionWithWindows(t, db, target)
		before := rig.state(target)
		if before.sessions == 0 {
			t.Fatal("no session to protect")
		}
		if _, err := rig.send(admin, "pending@example.test"); err != nil {
			t.Fatal(err)
		}
		if got := rig.state(target); got != before {
			t.Fatalf("send changed the person: %+v -> %+v", before, got)
		}
		if _, err := rig.send(admin, "pending@example.test"); err != nil {
			t.Fatal(err)
		}
		if got := rig.state(target); got != before {
			t.Fatalf("re-send changed the person: %+v -> %+v", before, got)
		}
		execRaw(t, db, `DELETE FROM org_email_invitations`)
	})

	t.Run("credential_reset_follows_the_accepted_footprint", func(t *testing.T) {
		resetter := rig.account("usr_ei_resetter", "resetter@example.test")
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_ei_resetter', 'usr_ei_resetter', 'credential-reset', 'org_a', `+ts+`)`)
		auth := authService(t, db)
		multi := rig.account("usr_ei_multi", "multi@example.test")
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_ei_multi', 'usr_ei_multi', 'read', 'org_b', `+ts+`)`)
		solo := rig.account("usr_ei_solo", "solo@example.test")
		bResetter := rig.account("usr_ei_b_resetter", "b-resetter@example.test")
		execRaw(t, db, `INSERT INTO grants (id, principal_id, capability, org_id, created_at) VALUES ('g_ei_b_resetter', 'usr_ei_b_resetter', 'credential-reset', 'org_b', `+ts+`)`)
		// Before accepting: the inviting org's holder reaches neither person
		// (the multi person's only org is org_b, the solo person has none), and
		// org_b's holder reaches the person whose only org it is.
		if _, err := auth.ResetCredential(ctx, service.LocalPrincipal(resetter), string(solo), "response"); err == nil {
			t.Fatal("an org credential-reset holder reset a person with no org before they accepted")
		}
		if _, err := auth.ResetCredential(ctx, service.LocalPrincipal(resetter), string(multi), "response"); err == nil {
			t.Fatal("an org credential-reset holder reset a person of another org before they accepted")
		}
		if _, err := auth.ResetCredential(ctx, service.LocalPrincipal(bResetter), string(multi), "response"); err != nil {
			t.Fatalf("the holder of the person's only org could not reset them before acceptance: %v", err)
		}
		for _, p := range []struct {
			who  domain.PrincipalID
			mail string
		}{{multi, "multi@example.test"}, {solo, "solo@example.test"}} {
			before := len(rig.sink.Messages())
			if _, err := rig.send(admin, p.mail); err != nil {
				t.Fatal(err)
			}
			if _, err := rig.svc.Accept(ctx, service.LocalPrincipal(p.who), rig.token(before)); err != nil {
				t.Fatal(err)
			}
		}
		// After: the acceptance made the multi person belong to two orgs, so
		// neither org's holder reaches them alone; the solo person's only org is
		// now the inviting one.
		if _, err := auth.ResetCredential(ctx, service.LocalPrincipal(resetter), string(multi), "response"); err == nil {
			t.Fatal("an org credential-reset holder reset a person who also belongs to another org")
		}
		if _, err := auth.ResetCredential(ctx, service.LocalPrincipal(bResetter), string(multi), "response"); err == nil {
			t.Fatal("the other org's holder still reaches a person the acceptance made multi-org")
		}
		if _, err := auth.ResetCredential(ctx, service.LocalPrincipal(resetter), string(solo), "response"); err != nil {
			t.Fatalf("an org credential-reset holder could not reset a person whose only org is theirs: %v", err)
		}
	})

	t.Run("deleting_the_org_removes_its_pending_rows", func(t *testing.T) {
		org, err := (&service.Orgs{DB: db}).Create(ctx, service.LocalPrincipal(root), "ei-doomed", true, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := rig.svc.Send(ctx, service.LocalPrincipal(root), service.EmailInvitationSendSpec{Org: domain.OrgID(org.ID), Email: "doomed@example.test", Template: domain.TemplateViewer}); err != nil {
			t.Fatal(err)
		}
		rig.svc.Sends.Wait()
		if n := queryInt(t, db, `SELECT COUNT(*) FROM org_email_invitations WHERE org_id = '`+string(org.ID)+`'`); n != 1 {
			t.Fatalf("pending rows = %d, want 1", n)
		}
		if err := (&service.Orgs{DB: db}).Delete(ctx, service.LocalPrincipal(root), domain.OrgID(org.ID)); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM org_email_invitations WHERE org_id = '`+string(org.ID)+`'`); n != 0 {
			t.Fatalf("pending rows after org deletion = %d, want 0", n)
		}
	})

	t.Run("erasing_the_matched_account_leaves_the_row_listed_and_unusable", func(t *testing.T) {
		execRaw(t, db, `DELETE FROM org_email_invitations`)
		victim := rig.account("usr_ei_erased", "erased@example.test")
		before := len(rig.sink.Messages())
		if _, err := rig.send(admin, "erased@example.test"); err != nil {
			t.Fatal(err)
		}
		token := rig.token(before)
		listed, err := rig.svc.List(ctx, service.LocalPrincipal(admin), orgA)
		if err != nil || len(listed) != 1 {
			t.Fatalf("list before erasure = %+v, %v", listed, err)
		}
		if _, err := authService(t, db).ApplyPrivacySubject(ctx, string(victim), "erase", ""); err != nil {
			t.Fatal(err)
		}
		after, err := rig.svc.List(ctx, service.LocalPrincipal(admin), orgA)
		if err != nil || len(after) != 1 || after[0] != listed[0] {
			t.Fatalf("list after erasure = %+v, %v; want %+v", after, err, listed)
		}
		_, err = rig.svc.Accept(ctx, service.LocalPrincipal(victim), token)
		wantNotFound(t, "accept after erasure", err)
		execRaw(t, db, `DELETE FROM org_email_invitations`)
	})

	t.Run("an_unstated_origin_refuses_before_anything_is_written_or_sent", func(t *testing.T) {
		derived := newEmailInvitationRig(t, db, true)
		derived.svc.ExternalOriginExplicit = false
		derived.account("usr_ei_derived", "derived@example.test")
		for _, addr := range []string{"derived@example.test", "nobody@example.test", "not an address"} {
			_, err := derived.send(admin, addr)
			var detail interface{ SafeDetail() string }
			if !errors.As(err, &detail) || detail.SafeDetail() != "no-public-origin" || !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("send(%q) without a stated origin = %v", addr, err)
			}
		}
		if _, err := derived.send(reader, "derived@example.test"); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("authorization precedes the predicate: %v", err)
		}
		derived.svc.Sends.Wait()
		if derived.rows() != 0 || len(derived.sink.Messages()) != 0 {
			t.Fatalf("a refused send wrote %d rows and mailed %d", derived.rows(), len(derived.sink.Messages()))
		}
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
