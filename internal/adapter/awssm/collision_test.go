package awssm

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func TestDuplicateManifestNamesRefusedBeforeAnyProviderCall(t *testing.T) {
	for _, kind := range []adapter.DestinationKind{adapter.JSONObject, adapter.PerKey} {
		for _, operation := range []string{"plan", "sync"} {
			for _, order := range []string{"forward", "reverse"} {
				t.Run(string(kind)+"/"+operation+"/"+order, func(t *testing.T) {
					api, journal := newFakeAPI(), newFakeJournal()
					module := &Module{API: api}
					target := jsonTarget()
					if kind == adapter.PerKey {
						target = perKeyTarget()
					}
					entries := []adapter.ManifestEntry{
						{KeyID: "key_secret", CanonicalName: "TOKEN", Classification: adapter.SecretClassification, Value: "secret-value-canary"},
						{KeyID: "key_config", CanonicalName: "TOKEN", Classification: adapter.ConfigClassification, Value: "config-value-canary"},
					}
					if order == "reverse" {
						slices.Reverse(entries)
					}
					var err error
					if operation == "plan" {
						var result adapter.Plan
						result, err = module.Plan(t.Context(), adapter.PlanRequest{Target: target, Manifest: entries, Gate: func(context.Context) error { return nil }})
						if len(result.Changes) != 0 {
							t.Fatal("duplicate manifest returned a usable plan")
						}
					} else {
						var result adapter.SyncResult
						result, err = module.Sync(t.Context(), adapter.SyncRequest{Target: target, Manifest: entries, JobID: "job_collision"}, journal)
						if len(result.Changes) != 0 || len(result.Conflicts) != 0 {
							t.Fatal("duplicate manifest returned partial results")
						}
					}
					if err == nil || !strings.Contains(err.Error(), "collides case-insensitively") {
						t.Fatal("duplicate manifest was not refused by the collision guard")
					}
					for _, entry := range entries {
						if strings.Contains(err.Error(), entry.Value) {
							t.Fatal("collision refusal disclosed a value")
						}
					}
					if len(api.calls) != 0 || len(api.secrets) != 0 || len(journal.intents) != 0 || len(journal.outcomes) != 0 || len(journal.states) != 0 || journal.refusals != 0 {
						t.Fatalf("collision reached identity, metadata, writes or durable effects: calls=%v", api.calls)
					}
				})
			}
		}
	}
}
