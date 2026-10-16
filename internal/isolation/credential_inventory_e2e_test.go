package isolation

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// The real network gate must admit the inventory read needed before TOTP
// enrolment. In-process service calls and mocked CLI responses miss this gate.
func TestEnrolmentGateCredentialInventory(t *testing.T) {
	for _, artifact := range []service.Artifact{service.ArtifactCLI, service.ArtifactBrowser} {
		t.Run(string(artifact), func(t *testing.T) {
			forEngines(t, func(t *testing.T, db *store.DB) {
				auth := authService(t, db)
				auth.SecondFactorRequired = true
				administrator := bootstrapAdmin(t, db, adminOpts{
					username: "inventory-admin", displayName: "Inventory Admin",
					password: "inventory ordinary passphrase", auth: auth,
				})
				login, err := auth.LocalLogin(t.Context(), "inventory-admin", administrator.password, artifact)
				if err != nil {
					t.Fatal(err)
				}
				srv := httptest.NewServer(server.New(&server.API{Auth: auth, Admission: auth.Admission}, nil))
				t.Cleanup(srv.Close)
				get := func(path string) *http.Response {
					t.Helper()
					req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+api.PathPrefix+path, nil)
					if err != nil {
						t.Fatal(err)
					}
					if artifact == service.ArtifactBrowser {
						req.AddCookie(&http.Cookie{Name: "__Host-hikyo", Value: login.SessionToken})
					} else {
						req.Header.Set("Authorization", "Bearer "+login.SessionToken)
					}
					response, err := srv.Client().Do(req)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { response.Body.Close() })
					return response
				}
				response := get("/auth/totp")
				if response.StatusCode != http.StatusOK {
					t.Fatalf("gated credential inventory = %d, want 200", response.StatusCode)
				}
				var status apigen.TotpStatus
				if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
					t.Fatal(err)
				}
				if !status.HasPassword || status.Confirmed || status.Pending || status.HasPasskey || status.CanReauthOidc || status.Establish != nil {
					t.Fatalf("password-only credential inventory = %+v", status)
				}
				if response := get("/me/profile"); response.StatusCode != http.StatusNotFound {
					t.Fatalf("gated ordinary account read = %d, want 404", response.StatusCode)
				}
				identity, err := auth.Identity(t.Context(), login.SessionToken)
				if err != nil || !identity.EnrolmentRequired {
					t.Fatalf("inventory lifted or revoked the enrolment gate: %+v, %v", identity, err)
				}
			})
		})
	}
}
