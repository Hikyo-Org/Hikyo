package githubactions

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func TestArtifactRoutesMatchDestinationAPI(t *testing.T) {
	operations := []struct {
		name, method, suffix, body string
		status                     int
		call                       func(*testing.T, *Client, adapter.Destination) error
	}{
		{"list-secrets", http.MethodGet, "/secrets?per_page=100&page=1", `{"total_count":1,"secrets":[{"name":"TOKEN"}]}`, http.StatusOK, func(t *testing.T, c *Client, d adapter.Destination) error {
			names, err := c.ListSecretNames(t.Context(), d)
			if err == nil && !slices.Equal(names, []string{"TOKEN"}) {
				t.Fatalf("secret names = %v", names)
			}
			return err
		}},
		{"public-key", http.MethodGet, "/secrets/public-key", `{"key_id":"key-1","key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`, http.StatusOK, func(t *testing.T, c *Client, d adapter.Destination) error {
			key, err := c.PublicKey(t.Context(), d)
			if err == nil && key.ID != "key-1" {
				t.Fatalf("public key ID = %q", key.ID)
			}
			return err
		}},
		{"put-secret", http.MethodPut, "/secrets/TOKEN", "", http.StatusCreated, func(t *testing.T, c *Client, d adapter.Destination) error {
			_, err := c.PutSecret(t.Context(), d, "TOKEN", "sealed", "key-1")
			return err
		}},
		{"delete-secret", http.MethodDelete, "/secrets/TOKEN", "", http.StatusNoContent, func(t *testing.T, c *Client, d adapter.Destination) error {
			return c.DeleteSecret(t.Context(), d, "TOKEN")
		}},
		{"create-variable", http.MethodPost, "/variables", "", http.StatusCreated, func(t *testing.T, c *Client, d adapter.Destination) error {
			_, err := c.CreateVariable(t.Context(), d, "MODE", "synthetic")
			return err
		}},
		{"update-variable", http.MethodPatch, "/variables/MODE", "", http.StatusNoContent, func(t *testing.T, c *Client, d adapter.Destination) error {
			_, err := c.UpdateVariable(t.Context(), d, "MODE", "synthetic")
			return err
		}},
		{"delete-variable", http.MethodDelete, "/variables/MODE", "", http.StatusNoContent, func(t *testing.T, c *Client, d adapter.Destination) error {
			return c.DeleteVariable(t.Context(), d, "MODE")
		}},
	}
	for _, origin := range []struct{ name, url, prefix string }{
		{"github", "https://api.github.com", ""},
		{"ghes", "https://github.example/api/v3", "/api/v3"},
	} {
		for _, destination := range []struct {
			name, path string
			value      adapter.Destination
		}{
			{"repository", "/repos/team/repo/actions", adapter.Destination{Kind: adapter.Repository, Owner: "team", Name: "repo"}},
			{"organization", "/orgs/team/actions", adapter.Destination{Kind: adapter.Organization, Owner: "team", Visibility: "all"}},
			{"environment", "/repos/team/repo/environments/prod%2F100%25", adapter.Destination{Kind: adapter.Environment, Owner: "team", Name: "repo", Environment: "prod/100%"}},
		} {
			for _, operation := range operations {
				t.Run(origin.name+"/"+destination.name+"/"+operation.name, func(t *testing.T) {
					var requests []string
					want := operation.method + " " + origin.prefix + destination.path + operation.suffix
					client, err := NewTestClient(origin.url, "github_pat_route_fixture", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						got := req.Method + " " + req.URL.RequestURI()
						requests = append(requests, got)
						if got != want {
							return response(http.StatusNotFound, `{}`), nil
						}
						return response(operation.status, operation.body), nil
					})})
					if err != nil {
						t.Fatal(err)
					}
					if err := operation.call(t, client, destination.value); err != nil {
						t.Fatalf("operation refused: %v; requests = %v, want %q", err, requests, want)
					}
					if !slices.Equal(requests, []string{want}) {
						t.Fatalf("requests = %v, want one request to %q", requests, want)
					}
				})
			}
		}
	}
}

func TestMissingVariablePermissionIsNamedAtSentinelSync(t *testing.T) {
	for _, kind := range []adapter.DestinationKind{adapter.Repository, adapter.Organization} {
		t.Run(string(kind), func(t *testing.T) {
			destination := adapter.Destination{Kind: kind, Owner: "team", NumericID: 42}
			if kind == adapter.Repository {
				destination.Name = "repo"
			} else {
				destination.Visibility = "all"
			}
			api := &fakeAPI{id: 42, variableCreateStatus: map[string]int{adapter.SentinelName: http.StatusForbidden}}
			module := &Module{API: api, Seal: fakeSeal}
			if _, err := module.TestConnection(t.Context(), adapter.ConnectionRequest{Destination: destination, Access: adapter.Access{Credential: "github_pat_no_variables"}, Gate: allow}); err != nil {
				t.Fatalf("read-only TestConnection unexpectedly refused missing variable permission: %v", err)
			}
			if len(api.writes) != 0 {
				t.Fatalf("TestConnection mutated provider: %v", api.writes)
			}
			journal := newFakeJournal()
			result, err := module.Sync(t.Context(), adapter.SyncRequest{Target: adapter.Target{ID: "target_1", Generation: 1, Destination: destination}}, journal)
			if !IsStatus(err, http.StatusForbidden) || !strings.Contains(err.Error(), "Variables: write") {
				t.Fatalf("Sync error = %v, want retained 403 and named Variables: write permission", err)
			}
			if !slices.Equal(api.writes, []string{"put-secret:" + adapter.SentinelName, "create-variable:" + adapter.SentinelName}) {
				t.Fatalf("Sync writes = %v, want only first-sync sentinels", api.writes)
			}
			completion := journal.completions["variable:"+adapter.SentinelName]
			if completion.Outcome != adapter.OutcomeFailure || completion.ProviderStatus != http.StatusForbidden || !completion.ReleaseLedger {
				t.Fatalf("refusal changed journal settlement: %+v", completion)
			}
			if len(result.Failed) != 1 || result.Failed[0].Surface != adapter.Variable || result.Failed[0].EffectiveName != adapter.SentinelName {
				t.Fatalf("Sync failed rows = %+v, want variable sentinel", result.Failed)
			}
		})
	}
}

func TestSyncRefusesEmptyVariableBeforeProviderCalls(t *testing.T) {
	contacts := 0
	client, err := NewTestClient("https://api.github.com", "github_pat_empty_fixture", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		contacts++
		return response(http.StatusInternalServerError, `{}`), nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	manifest := []adapter.ManifestEntry{
		{KeyID: "key_secret", CanonicalName: "EMPTY_SECRET", Classification: adapter.SecretClassification, Value: ""},
		{KeyID: "key_config", CanonicalName: "EMPTY_CONFIG", Classification: adapter.ConfigClassification, Value: ""},
	}
	journal := newFakeJournal()
	_, err = (&Module{API: client}).Sync(t.Context(), adapter.SyncRequest{Target: testTarget(), Manifest: manifest}, journal)
	if err == nil || !strings.Contains(err.Error(), "EMPTY_CONFIG") || !strings.Contains(err.Error(), "empty variable") {
		t.Fatalf("Sync error = %v, want named empty variable refusal", err)
	}
	if contacts != 0 || len(journal.states) != 0 || len(journal.completions) != 0 {
		t.Fatalf("invalid manifest reached provider or journal: contacts=%d states=%v completions=%v", contacts, journal.states, journal.completions)
	}
	if _, err := (&Module{API: &fakeAPI{id: 42}}).Plan(t.Context(), adapter.PlanRequest{Target: testTarget(), Manifest: manifest, Gate: allow}); err != nil {
		t.Fatalf("value-blind Plan refused empty placeholders: %v", err)
	}
	if _, err := (&Module{API: &fakeAPI{id: 42}, Seal: fakeSeal}).Sync(t.Context(), adapter.SyncRequest{Target: testTarget(), Manifest: manifest[:1]}, newFakeJournal()); err != nil {
		t.Fatalf("empty secret was incorrectly refused: %v", err)
	}
}

func TestEnvironmentConnectionUsesCanonicalReadOnlyProbes(t *testing.T) {
	var requests []string
	client, err := NewTestClient("https://api.github.com", "github_pat_environment_fixture", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		request := req.Method + " " + req.URL.RequestURI()
		requests = append(requests, request)
		switch request {
		case "GET /repos/team/repo":
			return response(http.StatusOK, `{"id":42}`), nil
		case "GET /repos/team/repo/environments/prod":
			return response(http.StatusOK, `{"id":73}`), nil
		case "GET /repos/team/repo/environments/prod/secrets?per_page=100&page=1":
			return response(http.StatusOK, `{"total_count":0,"secrets":[]}`), nil
		case "GET /repos/team/repo/environments/prod/secrets/public-key":
			return response(http.StatusOK, `{"key_id":"key-1","key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`), nil
		default:
			return response(http.StatusNotFound, `{}`), nil
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := (&Module{API: client}).TestConnection(t.Context(), adapter.ConnectionRequest{
		Destination: adapter.Destination{Kind: adapter.Environment, Owner: "team", Name: "repo", Environment: "prod", RepositoryID: 42, NumericID: 73},
		Access:      adapter.Access{Credential: "github_pat_environment_fixture"}, Gate: allow,
	})
	if err != nil {
		t.Fatalf("TestConnection() refused: %v; requests = %v", err, requests)
	}
	if connection.RepositoryID != 42 || connection.DestinationID != 73 {
		t.Fatalf("connection lost pinned identity: %+v", connection)
	}
	want := []string{
		"GET /repos/team/repo",
		"GET /repos/team/repo/environments/prod",
		"GET /repos/team/repo/environments/prod/secrets?per_page=100&page=1",
		"GET /repos/team/repo/environments/prod/secrets/public-key",
	}
	if !slices.Equal(requests, want) {
		t.Fatalf("requests = %v, want only identity, secret-list and public-key reads %v", requests, want)
	}
}
