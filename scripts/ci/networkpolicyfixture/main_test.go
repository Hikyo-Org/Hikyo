package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVariableResponsesPreserveValuesAsSafeJSON(t *testing.T) {
	fixture := &gitlabFixture{values: make(map[string]variable)}
	value := variable{Key: "NP_SHARED_KEY", Value: "</script><img src=x onerror=alert(1)>&\u2028\u2029", Scope: "*"}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodGet} {
		t.Run(method, func(t *testing.T) {
			path := "/api/v4/projects/42/variables"
			if method != http.MethodPost {
				path += "/" + value.Key
			}
			body, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(method, path, bytes.NewReader(body))
			request.Header.Set("PRIVATE-TOKEN", "synthetic-network-policy")
			response := httptest.NewRecorder()
			fixture.ServeHTTP(response, request)
			wantStatus := http.StatusOK
			if method == http.MethodPost {
				wantStatus = http.StatusCreated
			}
			if response.Code != wantStatus {
				t.Fatalf("HTTP %d, want %d", response.Code, wantStatus)
			}
			if response.Header().Get("Content-Type") != "application/json" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatalf("unsafe response headers: %v", response.Header())
			}
			if strings.ContainsAny(response.Body.String(), "<>&\u2028\u2029") {
				t.Fatalf("unescaped HTML/script characters in response: %q", response.Body.String())
			}
			var observed variable
			if err := json.Unmarshal(response.Body.Bytes(), &observed); err != nil {
				t.Fatal(err)
			}
			if observed != value {
				t.Fatalf("JSON roundtrip changed provider value: %#v", observed)
			}
		})
	}
}
