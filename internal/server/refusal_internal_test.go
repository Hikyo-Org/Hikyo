package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
)

type refusalRecorderFunc func(context.Context, audit.RefusalSample) error

func (f refusalRecorderFunc) Record(ctx context.Context, s audit.RefusalSample) error {
	return f(ctx, s)
}

func TestRefusalSettlesBeforeWireAndNeverEchoesSinkError(t *testing.T) {
	for _, sinkFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[sinkFails], func(t *testing.T) {
			w := httptest.NewRecorder()
			calls := 0
			a := &API{Refusals: refusalRecorderFunc(func(ctx context.Context, s audit.RefusalSample) error {
				calls++
				if w.Body.Len() != 0 || w.Flushed {
					t.Fatal("wire escaped before accounting")
				}
				if s.Stage != audit.RefusalConflict || s.SourceIP != "192.0.2.103" || s.RequestID == "spoofed" || s.Operation != "unmatched" {
					t.Fatalf("untrusted metadata: %+v", s)
				}
				if err := s.Validate(); err != nil {
					t.Fatal(err)
				}
				if sinkFails {
					return errors.New("SECRET-SINK-ERROR")
				}
				return nil
			})}
			r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/whoami?secret=SECRET-QUERY", nil)
			r.RemoteAddr = "192.0.2.103:1234"
			r.Header.Set("X-Forwarded-For", "198.51.100.1")
			r.Header.Set(RefusalRequestIDHeader, "spoofed")
			r.Header.Set("Authorization", "Bearer SECRET-BEARER")
			r.AddCookie(&http.Cookie{Name: browserSessionCookie, Value: "SECRET-COOKIE"})
			New(a, nil).ServeHTTP(w, r)
			if calls != 1 || w.Code != http.StatusUnauthorized {
				t.Fatalf("calls %d response %d", calls, w.Code)
			}
			control := httptest.NewRecorder()
			refuseUnauthenticated(control)
			if w.Body.String() != control.Body.String() {
				t.Fatal("refusal response changed")
			}

		})
	}
}

func TestPublicCapRefusalIsObservedBeforeRouting(t *testing.T) {
	var sample audit.RefusalSample
	a := &API{Refusals: refusalRecorderFunc(func(_ context.Context, s audit.RefusalSample) error { sample = s; return nil })}
	started := make(chan struct{}, MaxInFlightRequests)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	h := a.observeRefusals(boundPublicRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	})))
	var wg sync.WaitGroup
	for i := 0; i < MaxInFlightRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest(http.MethodGet, "/unmatched", nil)
			r.RemoteAddr = "192.0.2.103:1234"
			h.ServeHTTP(httptest.NewRecorder(), r)
		}()
	}
	for i := 0; i < MaxInFlightRequests; i++ {
		<-started
	}
	r := httptest.NewRequest(http.MethodGet, "/unmatched", nil)
	r.RemoteAddr = "192.0.2.103:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	once.Do(func() { close(release) })
	wg.Wait()
	if w.Code != http.StatusTooManyRequests || sample.Stage != audit.RefusalAdmission || w.Header().Get("Retry-After") == "" {
		t.Fatal("public cap bypassed refusal observer")
	}
}
