package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/service"
)

func TestRecoveryRendersUniformInternalBody(t *testing.T) {
	var pipeline bytes.Buffer
	api := &API{Log: slog.New(slog.NewJSONHandler(&pipeline, nil))}
	stack := api.Middleware()
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Hikyo-Test-Panic") == "1" {
			panic("chargeDefaultAtEntry called for non-default-expensive operation")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	for i := len(stack) - 1; i >= 0; i-- {
		handler = stack[i](handler)
	}

	panicRequest := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	panicRequest.Header.Set("X-Hikyo-Test-Panic", "1")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, panicRequest)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	// The reference body is what a fault renders today, through the strict
	// server's own error leg; the recovered body must be byte-identical.
	reference := renderContractError(t, http.MethodGet, "/api/v1/meta", errors.New("private fault"))
	if recorder.Body.String() != reference.Body.String() {
		t.Fatalf("recovered body differs from the fault body:\nrecovered: %s\nreference: %s",
			recorder.Body.String(), reference.Body.String())
	}
	var body apigen.Error
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != apigen.ErrorCodeInternal || body.Error.Message != "internal error" || body.Error.Detail != nil {
		t.Fatalf("body = %+v, want the fixed internal body", body)
	}
	for _, want := range []string{"handler panic", "chargeDefaultAtEntry", "stack"} {
		if !strings.Contains(pipeline.String(), want) {
			t.Fatalf("panic did not land in the slog pipeline (%q missing):\n%s", want, pipeline.String())
		}
	}

	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != `{"ok":true}` {
		t.Fatalf("subsequent normal request = %d %q, want 200 %q",
			recorder.Code, recorder.Body.String(), `{"ok":true}`)
	}
}

func TestRecoveryCoversTheLiveRouter(t *testing.T) {
	var pipeline bytes.Buffer
	api := &API{Log: slog.New(slog.NewJSONHandler(&pipeline, nil))}
	handler := NewPublic(nil, api, nil, PublicOptions{})

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/orgs", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	// securityHeaders and workspaceCORS are router-level, one layer UP from the
	// recovery leg, so a recovered refusal carries them like any other answer.
	if recorder.Header().Get("X-Content-Type-Options") != "nosniff" || recorder.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("recovered response lost the security baseline: %v", recorder.Header())
	}
	reference := renderContractError(t, http.MethodGet, "/api/v1/orgs", errors.New("private fault"))
	if recorder.Body.String() != reference.Body.String() {
		t.Fatalf("recovered body differs from the fault body:\nrecovered: %s\nreference: %s",
			recorder.Body.String(), reference.Body.String())
	}
	for _, want := range []string{"handler panic", "GET /api/v1/orgs", "nil pointer"} {
		if !strings.Contains(pipeline.String(), want) {
			t.Fatalf("panic did not land in the slog pipeline (%q missing):\n%s", want, pipeline.String())
		}
	}
}

func TestRecoveryKeepsTheAdvisoryStreamAStream(t *testing.T) {
	api := &API{}
	stack := api.Middleware()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	stream := eventStream{ctx: ctx, events: make(chan service.AdvisoryEvent), retry: advisoryRetryBase}
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The visitor refuses a non-flushing writer outright; running it behind
		// the full stack proves responseWriter satisfies and forwards Flusher.
		if err := stream.VisitWatchProjectEventsResponse(w); err != nil {
			t.Error(err)
		}
	})
	for i := len(stack) - 1; i >= 0; i-- {
		handler = stack[i](handler)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(&deadlineRecorder{ResponseRecorder: recorder}, httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil))

	if !recorder.Flushed {
		t.Fatal("flush did not reach the recorder; the advisory stream would buffer until it ended")
	}
	if !strings.HasPrefix(recorder.Body.String(), "retry: ") {
		t.Fatalf("SSE preamble lost through the stack: %q", recorder.Body.String())
	}
}

func TestRecoveryLeavesACommittedResponseAlone(t *testing.T) {
	api := &API{}
	stack := api.Middleware()
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("partial"))
		panic("fault after the status line")
	})
	for i := len(stack) - 1; i >= 0; i-- {
		handler = stack[i](handler)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil))

	if recorder.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want the already-committed %d", recorder.Code, http.StatusTeapot)
	}
	if recorder.Body.String() != "partial" {
		t.Fatalf("body = %q, want the committed bytes alone", recorder.Body.String())
	}
}
