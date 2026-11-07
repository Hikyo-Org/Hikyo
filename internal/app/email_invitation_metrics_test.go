package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/mailtest"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

// A failed background invitation send is counted by the owner, so the scrape of
// the generation that started it and of a generation prepared after it both
// report it (#1207). This exercises the binding in prepareGeneration itself.
func TestEmailInvitationFailuresReachEveryGenerationScrape(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	bootstrap, err := srv.owner.current.graph.auth.BootstrapAdmin(t.Context(), "invitation-metric-owner", "Owner", "terminal")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.selfConfig.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	actor := service.LocalPrincipal(bootstrap.PrincipalID)
	org, err := (&service.Orgs{DB: srv.db}).Create(t.Context(), actor, "invitation-metric", true, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	// The administrator's own verified address is a matched send.
	drillExec(t, srv.db, `UPDATE accounts SET email = 'owner@example.test', email_verified_at = created_at WHERE principal_id = ?`, string(bootstrap.PrincipalID))

	first := srv.owner.current.graph
	second, err := srv.owner.prepareGeneration(t.Context(), first.cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(second.closeIdleConnections)

	// A configured mailer whose relay refuses the connection fails the delivery.
	sink := mailtest.New(t, "implicit")
	bundle, err := runtimeconfig.Prepare(map[string]string{
		"HIKYO_MAIL_ADDR": "127.0.0.1:1", "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_FROM": "hikyo@example.test",
		"HIKYO_MAIL_CA_PEM": sink.CAPEM, "HIKYO_MAIL_ALLOWED_CIDRS": "127.0.0.0/8",
	})
	if err != nil {
		t.Fatal(err)
	}
	invitations := &service.EmailInvitations{
		DB: srv.db, ExternalOrigin: "https://hikyo.test", ExternalOriginExplicit: true,
		Mail:   func(context.Context) (*runtimeconfig.Bundle, error) { return bundle, nil },
		Budget: service.NewBudgetWithClock(time.Now),
		Sends:  srv.owner.emailSends,
	}
	if _, err := invitations.Send(t.Context(), actor, service.EmailInvitationSendSpec{Org: domain.OrgID(org.ID), Email: "owner@example.test", Template: domain.TemplateViewer}); err != nil {
		t.Fatal(err)
	}
	srv.owner.emailSends.Wait()

	for name, graph := range map[string]*applicationGeneration{"first": first, "second": second} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/metrics", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		graph.operationalHandler.ServeHTTP(rec, req)
		var got string
		for _, line := range strings.Split(rec.Body.String(), "\n") {
			if value, ok := strings.CutPrefix(line, server.MetricEmailInvitationMailFailures+" "); ok {
				got = value
			}
		}
		if got != "1" {
			t.Fatalf("%s generation scrape: %s = %q, want 1 (status %d)", name, server.MetricEmailInvitationMailFailures, got, rec.Code)
		}
	}
}
