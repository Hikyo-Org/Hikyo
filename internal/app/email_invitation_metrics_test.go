package app

import (
	"context"
	"net"
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

// invitationFailureScrape reads the failed-send count from a generation's /metrics.
func invitationFailureScrape(t *testing.T, graph *applicationGeneration) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/metrics", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	graph.operationalHandler.ServeHTTP(rec, req)
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if value, ok := strings.CutPrefix(line, server.MetricEmailInvitationMailFailures+" "); ok {
			return value
		}
	}
	t.Fatalf("scrape (status %d) has no %s", rec.Code, server.MetricEmailInvitationMailFailures)
	return ""
}

// A background invitation send outlives the generation that started it: one
// still in flight when a replacement generation activates is neither waited
// for nor cancelled by the activation, and its failure is counted on the
// active replacement's scrape (#1207).
func TestEmailInvitationFailureCountedAcrossActivation(t *testing.T) {
	srv, err := Boot(t.Context(), devConfig(t), testLogger())
	if err != nil {
		t.Fatal(err)
	}
	bootstrap, err := srv.owner.current.graph.auth.BootstrapAdmin(t.Context(), "invitation-metric-owner", "Owner", "terminal")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.selfConfig.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	startOwnerServer(t, srv)
	actor := service.LocalPrincipal(bootstrap.PrincipalID)
	org, err := (&service.Orgs{DB: srv.db}).Create(t.Context(), actor, "invitation-metric", true, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	// The administrator's own verified address is a matched send.
	drillExec(t, srv.db, `UPDATE accounts SET email = 'owner@example.test', email_verified_at = created_at WHERE principal_id = ?`, string(bootstrap.PrincipalID))

	// A relay that accepts the connection and answers nothing holds the send
	// in flight until the test closes the connection, which fails the delivery.
	relay, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relay.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		if conn, err := relay.Accept(); err == nil {
			accepted <- conn
		}
	}()
	sink := mailtest.New(t, "implicit")
	bundle, err := runtimeconfig.Prepare(map[string]string{
		"HIKYO_MAIL_ADDR": relay.Addr().String(), "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_FROM": "hikyo@example.test",
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
	var conn net.Conn
	select {
	case conn = <-accepted:
	case <-time.After(10 * time.Second):
		t.Fatal("the matched send never reached the relay")
	}

	first := srv.owner.current.graph
	prepared, err := srv.owner.Prepare(t.Context(), ownerCandidate(t, srv, func(values map[string]string) {
		values["HIKYO_AUDIT_ACCESS_RETAIN_DAYS"] = "30"
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if err := prepared.Activate(t.Context()); err != nil {
		t.Fatal(err)
	}
	active := srv.owner.current.graph
	if active == first {
		t.Fatal("activation did not replace the generation")
	}
	if got := invitationFailureScrape(t, active); got != "0" {
		t.Fatalf("before release: active scrape = %q, want 0 (activation waited for or cancelled the send)", got)
	}

	_ = conn.Close()
	srv.owner.emailSends.Wait()
	if got := invitationFailureScrape(t, active); got != "1" {
		t.Fatalf("active generation scrape = %q, want 1", got)
	}
}
