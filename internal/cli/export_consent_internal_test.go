package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func TestExportConsentUsesSnapshotMetadata(t *testing.T) {
	for _, tc := range []struct {
		name     string
		revision int64
		path     string
		gone     bool
	}{
		{"latest", 0, "/revisions/latest", false},
		{"historical", 7, "/revisions/7", false},
		{"collected", 7, "/revisions/7", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodGet || r.URL.RequestURI() != "/env"+tc.path || r.Header.Get("Authorization") != "Bearer human-session" {
					t.Errorf("unexpected metadata request: %s %s", r.Method, r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				if tc.gone {
					w.WriteHeader(http.StatusGone)
					_, _ = w.Write([]byte(`{"error":{"code":"gone","message":"revision payload collected"}}`))
					return
				}
				// Names can have been renamed or reused in the current catalogue.
				// Consent must bind the snapshot's IDs, without a catalogue lookup.
				_ = json.NewEncoder(w).Encode(apigen.RevisionDetail{Revision: 7, Keys: []apigen.SnapshotKey{
					{KeyId: "key_historical", Name: "OLD_NAME", Classification: apigen.KeyClassificationSecret},
					{KeyId: "key_config", Name: "CONFIG", Classification: apigen.KeyClassificationConfig},
				}})
			}))
			defer srv.Close()
			client := &Client{Entry: TrustEntry{Origin: srv.URL}, Bearer: "human-session", HTTP: srv.Client()}
			keys, err := exportSecretKeyIDs(t.Context(), client, "/env", tc.revision)
			if tc.gone {
				if err == nil || len(keys) != 0 {
					t.Fatalf("collected revision consent = %v, %v", keys, err)
				}
			} else if err != nil || !slices.Equal(keys, []string{"key_historical"}) {
				t.Fatalf("consent keys = %v, %v", keys, err)
			}
			if requests != 1 {
				t.Fatalf("metadata requests = %d, want 1", requests)
			}
		})
	}
}
