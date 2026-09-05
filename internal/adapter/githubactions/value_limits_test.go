package githubactions

import (
	"errors"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func TestGitHubVariableLimitCountsUTF8Bytes(t *testing.T) {
	for _, row := range []struct {
		name, value string
		refused     bool
	}{
		{"ASCII_MAX", strings.Repeat("x", 48000), false},
		{"ASCII_OVER", strings.Repeat("x", 48001), true},
		{"UNICODE_MAX", strings.Repeat("雪", 16000), false},
		{"UNICODE_OVER", strings.Repeat("雪", 16001), true},
	} {
		t.Run(row.name, func(t *testing.T) {
			manifest := []adapter.ManifestEntry{{KeyID: "key_config", CanonicalName: row.name, Classification: adapter.ConfigClassification, Value: row.value}}
			api := &fakeAPI{id: 42}
			if row.refused {
				api.resolveErrors = []error{errors.New("oversize manifest reached provider identity lookup")}
			}
			journal := newFakeJournal()
			_, err := (&Module{API: api, Seal: fakeSeal}).Sync(t.Context(), adapter.SyncRequest{Target: testTarget(), Manifest: manifest}, journal)
			if !row.refused {
				if err != nil {
					t.Fatalf("exact 48000-byte variable refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), row.name) || !strings.Contains(err.Error(), "48000-byte") {
				t.Fatalf("oversize variable error = %v, want named 48000-byte refusal", err)
			}
			if len(api.resolveErrors) != 1 || len(api.writes) != 0 || len(journal.states) != 0 || len(journal.completions) != 0 {
				t.Fatal("oversize variable touched provider or journal")
			}
			if err := validateManifest("", manifest, false); err != nil {
				t.Fatalf("value-blind Plan preflight inspected size: %v", err)
			}
		})
	}
}

func TestGitHubSecretLimitCountsPlaintextBytes(t *testing.T) {
	for _, row := range []struct {
		name, value string
		refused     bool
	}{
		{"ASCII_MAX", strings.Repeat("x", 47952), false},
		{"ASCII_OVER", strings.Repeat("x", 47953), true},
		{"UNICODE_MAX", strings.Repeat("雪", 15984), false},
		{"UNICODE_OVER", strings.Repeat("雪", 15985), true},
	} {
		t.Run(row.name, func(t *testing.T) {
			manifest := []adapter.ManifestEntry{{KeyID: "key_secret", CanonicalName: row.name, Classification: adapter.SecretClassification, Value: row.value}}
			api := &fakeAPI{id: 42}
			if row.refused {
				api.resolveErrors = []error{errors.New("oversize manifest reached provider identity lookup")}
			}
			journal := newFakeJournal()
			_, err := (&Module{API: api, Seal: fakeSeal}).Sync(t.Context(), adapter.SyncRequest{Target: testTarget(), Manifest: manifest}, journal)
			if !row.refused {
				if err != nil {
					t.Fatalf("exact 47952-byte secret refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), row.name) || !strings.Contains(err.Error(), "47952-byte") {
				t.Fatalf("oversize secret error = %v, want named 47952-byte refusal", err)
			}
			if len(api.resolveErrors) != 1 || len(api.writes) != 0 || len(journal.states) != 0 || len(journal.completions) != 0 {
				t.Fatal("oversize secret touched provider or journal")
			}
			if err := validateManifest("", manifest, false); err != nil {
				t.Fatalf("value-blind Plan preflight inspected size: %v", err)
			}
		})
	}
}
