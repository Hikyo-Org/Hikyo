package server

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestPublicRequestAdmissionIsBoundedAndReleasesSlots(t *testing.T) {
	entered := make(chan struct{}, 512)
	release := make(chan struct{})
	completed := make(chan struct{}, 512)
	var running sync.WaitGroup
	t.Cleanup(func() { close(release); running.Wait() })
	h := NewPublic(nil, nil, nil, PublicOptions{HSTS: true, MCP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Hold") == "yes" {
			entered <- struct{}{}
			<-release
		}
		w.WriteHeader(http.StatusNoContent)
	})})
	for range 512 {
		running.Add(1)
		go func() {
			defer running.Done()
			r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
			r.Header.Set("Hold", "yes")
			h.ServeHTTP(httptest.NewRecorder(), r)
			completed <- struct{}{}
		}()
		<-entered
	}
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") == "" {
		t.Fatalf("request 513 = %d with Retry-After %q", recorder.Code, recorder.Header().Get("Retry-After"))
	}
	if recorder.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("overflow lost security headers")
	}
	release <- struct{}{}
	<-completed
	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("released slot still refused: %d", recorder.Code)
	}
}

func TestGlobalBodyBoundMatchesTransportContract(t *testing.T) {
	if MaxRequestBytes != 2<<20 {
		t.Fatalf("global body bound = %d, want 2 MiB", MaxRequestBytes)
	}
}

func TestPublicRequestAdmissionReleasesAfterPanic(t *testing.T) {
	h := boundPublicRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/panic" {
			panic("fixture")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	for range MaxInFlightRequests + 1 {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("request was refused before handler panic")
				}
			}()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/panic", nil))
		}()
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("panic leaked admission slots: %d", w.Code)
	}
}
