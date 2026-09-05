package updater

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnixControlRefusesSubmissionAndPreservesHistoricalOutcomes(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "hikyo-updater-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "updater.sock")
	journal := &Journal{Path: filepath.Join(dir, "state.json")}
	runner := &recordingRunner{}
	control := &ControlServer{
		Executor: Executor{Config: fixtureConfig(t, BackendFlux), Runner: runner},
		Journal:  journal,
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: control.Handler()}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Shutdown(context.Background())
		_ = listener.Close()
	})

	client := NewClient(socket)
	if _, err := client.Capability(t.Context()); !errors.Is(err, ErrRemoteApplyDisabled) {
		t.Fatalf("helper capability=%v, want disabled", err)
	}
	jobID := "upd_0198aa00-0000-7000-8000-000000000001"
	if err := journal.Create(Job{ID: jobID, Backend: BackendFlux, State: StateSucceeded}); err != nil {
		t.Fatal(err)
	}
	queuedID := "upd_0198aa00-0000-7000-8000-000000000002"
	if err := journal.Create(Job{ID: queuedID, Backend: BackendFlux, State: StateQueued}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Submit(t.Context(), Request{ID: queuedID, Version: "1.2.3"}); !errors.Is(err, ErrRemoteApplyDisabled) {
		t.Fatalf("old queued submission=%v, want disabled", err)
	}
	queued, err := client.Job(t.Context(), queuedID)
	if err != nil || queued.State != StateQueued || len(runner.calls) != 0 {
		t.Fatalf("queued history changed or executed: job=%+v calls=%v err=%v", queued, runner.calls, err)
	}
	pending, err := client.PendingOutcomes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != jobID {
		t.Fatalf("pending outcomes = %#v, want job %s", pending, jobID)
	}
	if err := client.AcknowledgeOutcome(t.Context(), jobID); err != nil {
		t.Fatal(err)
	}
	job, err := journal.Get(jobID)
	if err != nil {
		t.Fatal(err)
	}
	if !job.OutcomeReported {
		t.Fatal("outcome acknowledgement was not durable")
	}
	if _, err := client.Submit(t.Context(), Request{
		ID: "../../unsafe", Version: "1.2.4",
		ReleaseURL: "https://github.com/Hikyo-Org/hikyo/releases/tag/v1.2.4", RequestedBy: "usr_admin",
	}); err == nil {
		t.Fatal("helper accepted a non-canonical job id")
	}
	if _, err := client.Submit(t.Context(), Request{
		ID: "upd_0198aa00-0000-7000-8000-000000000002", Version: "1.2.4",
		ReleaseURL: "https://attacker.example/v1.2.4", RequestedBy: "usr_admin",
	}); !errors.Is(err, ErrRemoteApplyDisabled) {
		t.Fatalf("foreign release authority error = %v, want ErrRemoteApplyDisabled", err)
	}
}

// This transport models an old helper that still accepts legacy jobs. The new
// client must refuse before it can advertise capability or enqueue a command.
type acceptingLegacyTransport struct{ calls int }

func (transport *acceptingLegacyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.calls++
	status, body := http.StatusOK, `{"backend":"flux"}`
	if request.Method == http.MethodPost {
		status, body = http.StatusAccepted, `{"id":"upd_legacy","backend":"flux","state":"queued"}`
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestRetiredClientCannotContactAcceptingLegacyHelper(t *testing.T) {
	transport := &acceptingLegacyTransport{}
	client := &Client{http: &http.Client{Transport: transport}}
	if _, err := client.Capability(t.Context()); !errors.Is(err, ErrRemoteApplyDisabled) {
		t.Fatalf("capability error=%v, want local retirement refusal", err)
	}
	if _, err := client.Submit(t.Context(), Request{ID: "upd_legacy", Version: "1.2.3"}); !errors.Is(err, ErrRemoteApplyDisabled) {
		t.Fatalf("submit error=%v, want local retirement refusal", err)
	}
	if transport.calls != 0 {
		t.Fatalf("legacy helper received %d requests", transport.calls)
	}
}

func TestRetiredControlEndpointsRefuseBeforeReadingJournal(t *testing.T) {
	handler := (&ControlServer{}).Handler()
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/v1/capability"}, {http.MethodPost, "/v1/jobs"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, strings.NewReader(`{}`)))
		if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "remote-apply-disabled") {
			t.Fatalf("%s %s: status=%d body=%s", endpoint.method, endpoint.path, response.Code, response.Body.String())
		}
	}
}
