package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/operation"
)

func TestCredentialResolutionUsesSharedSourceAdmission(t *testing.T) {
	limiter, err := admission.New(admission.Config{ArgonMemoryKiB: 64 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	const source = "192.0.2.83"
	for range admission.MetaPerIPPerMinute - 1 {
		if err := limiter.AdmitDiscovery(source); err != nil {
			t.Fatal(err)
		}
	}
	api := API{Admission: limiter}
	handler := api.admitAuthenticatedRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for range 2 {
			if err := operation.AdmitPreauthentication(r.Context()); err != nil {
				if !errors.Is(err, admission.ErrOverloaded) {
					t.Errorf("unexpected error: %v", err)
				}
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	for i, ip := range []string{source, source, "192.0.2.84"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(audit.WithContext(req.Context(), audit.Context{SourceIP: ip}))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		want := http.StatusOK
		if i == 1 {
			want = http.StatusTooManyRequests
		}
		if rec.Code != want {
			t.Fatalf("request %d returned %d, want %d", i, rec.Code, want)
		}
	}
}
