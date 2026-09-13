package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/service"
)

type runtimeStatusStub struct {
	status service.RuntimeStatus
	err    error
}

func (s runtimeStatusStub) RuntimeStatus(context.Context) (service.RuntimeStatus, error) {
	return s.status, s.err
}

func TestRuntimeStatusPublicContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source RuntimeStatusSource
		code   int
		body   string
	}{
		{"ready", runtimeStatusStub{status: service.RuntimeStatus{State: "ready"}}, 200, `{"phase":null,"state":"ready"}`},
		{"backup", runtimeStatusStub{status: service.RuntimeStatus{State: "maintenance", Phase: "backup"}}, 200, `{"phase":"backup","state":"maintenance"}`},
		{"recovery", runtimeStatusStub{status: service.RuntimeStatus{State: "recovery-required"}}, 200, `{"phase":null,"state":"recovery-required"}`},
		{"storage failure", runtimeStatusStub{err: errors.New("private database path")}, 503, ""},
		{"missing source", nil, 503, ""},
		{"invalid phase", runtimeStatusStub{status: service.RuntimeStatus{State: "maintenance", Phase: "private stage"}}, 503, ""},
		{"inconsistent ready", runtimeStatusStub{status: service.RuntimeStatus{State: "ready", Phase: "backup"}}, 503, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := NewPublic(nil, &API{Runtime: tc.source}, nil, PublicOptions{})
			r := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/status", nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.code || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("status=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
			}
			if tc.body != "" && strings.TrimSpace(w.Body.String()) != tc.body {
				t.Fatalf("body=%s want=%s", w.Body.String(), tc.body)
			}
			if tc.code == 503 && (strings.Contains(w.Body.String(), "private") || w.Header().Get("Retry-After") != "2") {
				t.Fatalf("unsafe unavailable response: %s", w.Body.String())
			}
		})
	}
}
