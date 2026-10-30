package githubactions

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

const observedVariableName = "HIKYO_CONTRACT_SYNTHETIC_ORG_CRLF"

func syntheticObservedClient(t *testing.T, transport http.RoundTripper, emit func(map[string]any)) (*observedContractClient, adapter.Destination) {
	t.Helper()
	client, err := NewTestClient("https://api.github.com", "github_pat_synthetic_only", &http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	cfg := githubContractConfig{owner: "acme", organizationID: 9, repositoryID: 11}
	observed := observeContractClient(client, cfg, emit)
	destination := adapter.Destination{Kind: adapter.Organization, Owner: cfg.owner, NumericID: cfg.organizationID, Visibility: "selected", SelectedRepositoryIDs: []int64{cfg.repositoryID}}
	observed.own(destination, adapter.Variable, observedVariableName)
	return observed, destination
}

func TestContractObserverVariableReplacement204And404(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()
			calls := 0
			var records []map[string]any
			client, destination := syntheticObservedClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.Method != http.MethodPut || request.URL.Path != "/orgs/acme/actions/variables/"+observedVariableName+"/repositories" {
					t.Fatalf("unexpected variable replacement route: %s %s", request.Method, request.URL.Path)
				}
				var body struct {
					IDs []int64 `json:"selected_repository_ids"`
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil || !slices.Equal(body.IDs, []int64{11}) {
					t.Fatalf("replacement recipients=%v error=%v", body.IDs, err)
				}
				return response(status, `{"message":"untrusted remote text"}`, http.Header{
					"X-Github-Request-Id": {"ABCD:1:2:3:4"}, "X-Accepted-Github-Permissions": {"organization_variables=write; metadata=read"},
				}), nil
			}), func(record map[string]any) { records = append(records, record) })
			err := client.ReplaceSelectedRepositories(t.Context(), destination, adapter.Variable, observedVariableName)
			if (status == http.StatusNoContent && err != nil) || (status == http.StatusNotFound && !IsStatus(err, status)) {
				t.Fatalf("replacement error=%v, status=%d", err, status)
			}
			if calls != 1 || len(records) != 1 {
				t.Fatalf("calls=%d records=%d, want exactly one without retry/dispatch", calls, len(records))
			}
			record := records[0]
			category := "success"
			if status == http.StatusNotFound {
				category = "http_404_ambiguous"
				if err.Error() != (&ResponseError{Status: status}).Error() {
					t.Fatalf("status-only refusal changed: %v", err)
				}
			}
			for key, want := range map[string]any{
				"kind": "provider_operation", "operation": "replace_selected_repositories", "method": http.MethodPut,
				"owner": "acme", "organization_id": int64(9), "name": observedVariableName, "visibility": "selected",
				"selected_repository_ids": []int64{11}, "metadata_source": "owned_call_arguments", "status": status,
				"request_id": "ABCD:1:2:3:4", "accepted_permissions": "organization_variables=write; metadata=read", "error_category": category,
			} {
				if !reflect.DeepEqual(record[key], want) {
					t.Errorf("%s=%v, want %v", key, record[key], want)
				}
			}
			start, startErr := time.Parse(time.RFC3339Nano, record["started_at"].(string))
			finish, finishErr := time.Parse(time.RFC3339Nano, record["finished_at"].(string))
			if startErr != nil || finishErr != nil || finish.Before(start) {
				t.Fatal("missing or unordered UTC operation times")
			}
		})
	}
}

func TestContractObserverCreate201ThenRecipient404(t *testing.T) {
	var methods []string
	var records []map[string]any
	client, destination := syntheticObservedClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		methods = append(methods, request.Method)
		if len(methods) == 1 && request.Method == http.MethodPost && request.URL.Path == "/orgs/acme/actions/variables" {
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body["name"] != observedVariableName || body["visibility"] != "selected" || body["value"] != "private-value" || body["selected_repository_ids"] != nil {
				t.Fatal("create request changed")
			}
			return response(http.StatusCreated, ""), nil
		}
		if len(methods) != 2 || request.Method != http.MethodPut || request.URL.Path != "/orgs/acme/actions/variables/"+observedVariableName+"/repositories" {
			t.Fatal("unexpected retry, dispatch, or operation")
		}
		return response(http.StatusNotFound, `{"message":"private-value"}`), nil
	}), func(record map[string]any) { records = append(records, record) })
	created, err := client.CreateVariable(t.Context(), destination, observedVariableName, "private-value")
	if err != nil || created.Status != http.StatusCreated {
		t.Fatalf("create status=%d error=%v", created.Status, err)
	}
	if err := client.ReplaceSelectedRepositories(t.Context(), destination, adapter.Variable, observedVariableName); !IsStatus(err, http.StatusNotFound) {
		t.Fatalf("replacement error=%v, want unchanged 404", err)
	}
	if !slices.Equal(methods, []string{http.MethodPost, http.MethodPut}) || len(records) != 2 || records[0]["status"] != http.StatusCreated || records[1]["status"] != http.StatusNotFound || records[0]["name"] != records[1]["name"] {
		t.Fatalf("uninterpretable create/replacement sequence: methods=%v records=%v", methods, records)
	}
	encoded, _ := json.Marshal(records)
	if strings.Contains(string(encoded), "private-value") || strings.Contains(string(encoded), "github_pat_") {
		t.Fatal("observer emitted a value or credential")
	}
}

func TestContractObserverRedactsUntrustedAndReflectedHeaders(t *testing.T) {
	for _, row := range []struct {
		name, value, requestID, permissions string
	}{
		{"token", "private-value", "github_pat_synthetic_only", "Bearer github_pat_synthetic_only"},
		{"remote text", "private-value", "https://signed.invalid/?token=secret", "organization_variables=write\nsecret"},
		{"oversize", "private-value", strings.Repeat("A", 129), strings.Repeat("organization_variables=write;", 20)},
		{"reflected value matching grammar", "ABCD", "ABCD:1:2:3:4", "unknown_permission=write"},
		{"reflected permission matching grammar", "write", "invalid-id", "organization_variables=write"},
	} {
		t.Run(row.name, func(t *testing.T) {
			var records []map[string]any
			client, destination := syntheticObservedClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				return response(http.StatusCreated, "private-body", http.Header{
					"X-Github-Request-Id": {row.requestID}, "X-Accepted-Github-Permissions": {row.permissions},
					"Authorization": {"Bearer github_pat_synthetic_only"}, "Location": {"https://signed.invalid/?token=secret"},
				}), nil
			}), func(record map[string]any) { records = append(records, record) })
			if _, err := client.CreateVariable(t.Context(), destination, observedVariableName, row.value); err != nil {
				t.Fatal(err)
			}
			if len(records) != 1 || records[0]["request_id"] != nil || records[0]["accepted_permissions"] != nil || records[0]["request_id_redacted"] != true || records[0]["accepted_permissions_redacted"] != true {
				t.Fatal("unsafe metadata was not redacted")
			}
			encoded, _ := json.Marshal(records)
			for _, unsafe := range []string{row.requestID, row.permissions, "private-body", "github_pat_synthetic_only", "signed.invalid"} {
				if strings.Contains(string(encoded), unsafe) {
					t.Fatal("untrusted or secret-tainted metadata escaped")
				}
			}
		})
	}
}

type unreadableContractBody struct{ io.ReadCloser }

func (unreadableContractBody) Read([]byte) (int, error) { panic("observer read a body") }

func TestContractObserverPassesThroughWithoutReadingBodiesOrErrors(t *testing.T) {
	remoteErr := errors.New("private error with signed URL and github_pat_synthetic_only")
	response := &http.Response{StatusCode: http.StatusBadGateway, Body: unreadableContractBody{}}
	calls := 0
	var records []map[string]any
	var original *http.Request
	client, destination := syntheticObservedClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request != original {
			t.Fatal("observer changed request")
		}
		return response, remoteErr
	}), func(record map[string]any) { records = append(records, record) })
	ctx := client.operationContext(t.Context(), destination, adapter.Variable, observedVariableName, "create_variable", nil)
	original, _ = http.NewRequestWithContext(ctx, http.MethodPost, "https://api.github.com/orgs/acme/actions/variables", unreadableContractBody{})
	got, err := client.observer.RoundTrip(original)
	if got != response || err != remoteErr || calls != 1 || len(records) != 1 || records[0]["error_category"] != "transport_error" {
		t.Fatal("observer altered transport outcome")
	}
	encoded, _ := json.Marshal(records)
	if strings.Contains(string(encoded), remoteErr.Error()) {
		t.Fatal("observer emitted raw error")
	}
}

func TestContractObserverIgnoresUnownedOrMismatchedMetadata(t *testing.T) {
	calls, emitted := 0, 0
	client, destination := syntheticObservedClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return response(http.StatusNoContent, ""), nil
	}), func(map[string]any) { emitted++ })
	for _, row := range []struct{ name, target string }{
		{"HIKYO_CONTRACT_UNOWNED", "https://api.github.com/orgs/acme/actions/variables"},
		{"HIKYO_CONTRACT_github_pat_secret", "https://api.github.com/orgs/acme/actions/variables"},
		{observedVariableName, "https://api.github.com/orgs/acme/actions/variables?signature=private"},
		{observedVariableName, "https://other.invalid/orgs/acme/actions/variables"},
	} {
		if strings.Contains(row.name, "github_pat_") {
			client.own(destination, adapter.Variable, row.name)
		}
		ctx := client.operationContext(t.Context(), destination, adapter.Variable, row.name, "create_variable", nil)
		request, _ := http.NewRequestWithContext(ctx, http.MethodPost, row.target, nil)
		if _, err := client.observer.RoundTrip(request); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 4 || emitted != 0 {
		t.Fatalf("calls=%d emitted=%d, want unchanged requests with no unrelated metadata", calls, emitted)
	}
}

type contractClosingTransport struct{ closed bool }

func (*contractClosingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return response(http.StatusNoContent, ""), nil
}

func (c *contractClosingTransport) CloseIdleConnections() { c.closed = true }

func TestContractObserverPreservesTransportCleanupAndRejectsDuplicateHeaders(t *testing.T) {
	transport := &contractClosingTransport{}
	client, _ := syntheticObservedClient(t, transport, func(map[string]any) {})
	client.Forget()
	if !transport.closed {
		t.Fatal("observer swallowed private transport cleanup")
	}
	var record map[string]any
	client, destination := syntheticObservedClient(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response(http.StatusCreated, "", http.Header{
			"X-Github-Request-Id":           {"ABCD:1:2:3:4", "BCDE:2:3:4:5"},
			"X-Accepted-Github-Permissions": {"organization_variables=write", "metadata=read"},
		}), nil
	}), func(got map[string]any) { record = got })
	if _, err := client.CreateVariable(t.Context(), destination, observedVariableName, "private-value"); err != nil {
		t.Fatal(err)
	}
	if record["request_id_redacted"] != true || record["accepted_permissions_redacted"] != true {
		t.Fatal("ambiguous duplicate headers were accepted")
	}
}

func TestContractVariableRecipient404KeepsUnknownCustodyWithoutRetry(t *testing.T) {
	api := &fakeAPI{id: 9, replaceSelectedErr: &ResponseError{Status: http.StatusNotFound}}
	target := testTarget()
	target.Destination = adapter.Destination{Kind: adapter.Organization, Owner: "acme", NumericID: 9, Visibility: "selected", SelectedRepositoryIDs: []int64{11}}
	journal := newFakeJournal()
	result, err := (&Module{API: api}).Sync(t.Context(), adapter.SyncRequest{
		Target: target,
		Completed: []adapter.Change{
			{Surface: adapter.Secret, EffectiveName: adapter.SentinelName}, {Surface: adapter.Variable, EffectiveName: adapter.SentinelName},
		},
		Manifest: []adapter.ManifestEntry{{KeyID: "key_1", CanonicalName: observedVariableName, Classification: adapter.ConfigClassification, Value: "private-value"}},
	}, journal)
	completion := journal.completions["variable:"+observedVariableName]
	if !IsStatus(err, http.StatusNotFound) || len(result.Failed) != 1 || completion.Outcome != adapter.OutcomeUnknown || completion.State != adapter.Dispatched || completion.ReleaseLedger || completion.ProviderStatus != http.StatusCreated {
		t.Fatalf("replacement failure semantics changed: error=%v completion=%+v result=%+v", err, completion, result)
	}
	if !slices.Equal(api.writes, []string{"create-variable:" + observedVariableName, "replace-selected:variable:" + observedVariableName}) || journal.states["variable:"+observedVariableName] != adapter.Dispatched {
		t.Fatalf("unexpected retry or custody release: writes=%v states=%v", api.writes, journal.states)
	}
}
