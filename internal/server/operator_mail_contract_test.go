package server_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/mail"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

type unavailableInstanceMail struct{ err error }

func (unavailableInstanceMail) Configured(context.Context, service.Actor) (bool, error) {
	return false, nil
}
func (s unavailableInstanceMail) Test(context.Context, service.Actor, string, string) error {
	return s.err
}

// Authorized operator sends expose unavailable mail through the API's closed
// 503 policy. An SMTP failure must not leak the relay's response or recipient.
func TestInstanceMailUnavailableWire(t *testing.T) {
	var uniformBody string
	for name, cause := range map[string]error{"unconfigured": mail.ErrDisabled, "delivery failure": mail.ErrDelivery} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(server.New(stubReady{}, &server.API{
				Auth: stubAuth{}, Orgs: stubOrgs{},
				Mail: unavailableInstanceMail{err: fmt.Errorf("private relay refused recipient: %w", cause)},
			}, nil))
			t.Cleanup(srv.Close)
			response, payload := call(t, srv, http.MethodPost, api.PathPrefix+"/instance/mail/test", "hik_1_cli_x",
				map[string]any{"to": "recipient@example.com", "proof": "fresh proof"})
			if response.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, body = %s; want 503", response.StatusCode, payload)
			}
			body := decodeError(t, payload)
			if body.Error.Code != apigen.ErrorCodeServiceUnavailable || body.Error.Detail != nil {
				t.Fatalf("unavailable mail escaped closed wire policy: %#v", body)
			}
			if strings.Contains(string(payload), "private relay") || strings.Contains(string(payload), "recipient@example.com") {
				t.Fatal("mail failure disclosed delivery details")
			}
			if uniformBody == "" {
				uniformBody = string(payload)
			} else if string(payload) != uniformBody {
				t.Fatalf("unavailable mail responses differ: %s versus %s", payload, uniformBody)
			}
		})
	}
}
