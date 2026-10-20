package service

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Interleave keys and reverse environment order so equivalence also checks
// that bucketing preserves the store's order rather than map iteration order.
func cataloguePresenceFixture(keyCount, envCount int, dense bool) ([]store.CatalogueKey, []store.KeyPresence) {
	keys := make([]store.CatalogueKey, keyCount)
	for i := range keys {
		keys[i] = store.CatalogueKey{
			ID: fmt.Sprintf("key_%04d", i), Name: fmt.Sprintf("KEY_%04d", i),
			Declaration: `{"rule":{"type":"string"}}`, Classification: string(schema.Config),
			RequiredMode: string(schema.PresenceExplicit), ForbiddenMode: string(schema.PresenceExplicit),
		}
	}
	var rows []store.KeyPresence
	for e := envCount - 1; e >= 0; e-- {
		for i, key := range keys {
			if !dense && (i%10 != 0 || e >= 2) {
				continue
			}
			rule := store.PresenceRuleRequired
			if e%2 == 1 {
				rule = store.PresenceRuleForbidden
			}
			rows = append(rows, store.KeyPresence{KeyID: key.ID, EnvironmentID: fmt.Sprintf("env_%02d", e), Rule: rule})
		}
	}
	return keys, rows
}

func TestPresenceIndexEquivalence(t *testing.T) {
	for _, dense := range []bool{false, true} {
		t.Run(fmt.Sprintf("dense=%t", dense), func(t *testing.T) {
			keys, rows := cataloguePresenceFixture(config.DefaultKeysPerProject, MaxEnvironmentsPerProject, dense)
			index := indexPresenceByKey(rows)
			for _, key := range keys {
				want, err := keyOf(key, rows)
				if err != nil {
					t.Fatal(err)
				}
				got, err := keyOf(key, index[key.ID])
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("key %s: indexed = %+v, %v; scan = %+v", key.ID, got, err, want)
				}
			}
		})
	}
}

func TestPresenceIndexModesAndSnapshotLifetime(t *testing.T) {
	rows := []store.KeyPresence{
		{KeyID: "other", EnvironmentID: "env_other", Rule: store.PresenceRuleRequired},
		{KeyID: "key", EnvironmentID: "env_b", Rule: store.PresenceRuleRequired},
		{KeyID: "key", EnvironmentID: "env_c", Rule: store.PresenceRuleForbidden},
		{KeyID: "key", EnvironmentID: "ignored", Rule: "unknown"},
		{KeyID: "key", EnvironmentID: "env_a", Rule: store.PresenceRuleRequired},
	}
	index := indexPresenceByKey(rows)
	for _, required := range []schema.PresenceMode{schema.PresenceNone, schema.PresenceAll, schema.PresenceExplicit} {
		for _, forbidden := range []schema.PresenceMode{schema.PresenceNone, schema.PresenceAll, schema.PresenceExplicit} {
			got := presenceOf("key", string(required), string(forbidden), index["key"])
			want := schema.PresenceRules{Required: schema.Presence{Mode: required}, Forbidden: schema.Presence{Mode: forbidden}}
			if required == schema.PresenceExplicit {
				want.Required.Environments = []string{"env_b", "env_a"}
			}
			if forbidden == schema.PresenceExplicit {
				want.Forbidden.Environments = []string{"env_c"}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("modes %s/%s: got %+v, want %+v", required, forbidden, got, want)
			}
		}
	}
	if got := indexPresenceByKey(nil); len(got) != 0 {
		t.Fatalf("empty index = %v", got)
	}
	if got := index["missing"]; got != nil {
		t.Fatalf("missing key rows = %v", got)
	}
	rows[1].EnvironmentID = "env_new"
	if index["key"][0].EnvironmentID != "env_b" {
		t.Fatal("existing snapshot changed with its source rows")
	}
	if fresh := indexPresenceByKey(rows); fresh["key"][0].EnvironmentID != "env_new" {
		t.Fatal("rebuilt snapshot did not see changed rows")
	}
}

type presenceCatalogueReader struct {
	store.CatalogueReader // Unexpected reads, including per-key reads, fail the test.
	keys                  []store.CatalogueKey
	rows                  []store.KeyPresence
	calls                 []string
}

func (c *presenceCatalogueReader) List(context.Context, authz.Proof) ([]store.CatalogueKey, error) {
	c.calls = append(c.calls, "keys")
	return c.keys, nil
}
func (c *presenceCatalogueReader) ListPresence(context.Context, authz.Proof) ([]store.KeyPresence, error) {
	c.calls = append(c.calls, "presence")
	return c.rows, nil
}
func (c *presenceCatalogueReader) ListGroups(context.Context, authz.Proof) ([]store.CatalogueGroup, error) {
	c.calls = append(c.calls, "groups")
	return []store.CatalogueGroup{{ID: "group", Name: "database"}}, nil
}
func (c *presenceCatalogueReader) SchemaRevision(context.Context, authz.Proof) (int64, error) {
	c.calls = append(c.calls, "revision")
	return 73, nil
}

type presenceEnvironmentReader struct {
	store.EnvironmentReader
}

func (presenceEnvironmentReader) List(context.Context, authz.Proof) ([]store.Environment, error) {
	return []store.Environment{{ID: "env_b", Name: "production"}, {ID: "env_a", Name: "development"}}, nil
}

func TestBuildCurrentStatePresenceEquivalence(t *testing.T) {
	for _, dense := range []bool{false, true} {
		t.Run(fmt.Sprintf("dense=%t", dense), func(t *testing.T) {
			keys, rows := cataloguePresenceFixture(100, 10, dense)
			// A non-name order proves construction follows catalogue order.
			keys[0], keys[99] = keys[99], keys[0]
			cat := &presenceCatalogueReader{keys: keys, rows: rows}
			cur, err := buildCurrentState(t.Context(), cat, presenceEnvironmentReader{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if cur.SchemaRevision != 73 || len(cur.Keys) != len(keys) || cur.KeyGroups[0].ID != "group" || cur.Environments[0].ID != "env_b" {
				t.Fatalf("snapshot metadata/order changed: %+v", cur)
			}
			if want := []string{"keys", "presence", "groups", "revision"}; !reflect.DeepEqual(cat.calls, want) {
				t.Fatalf("catalogue reads = %v, want %v", cat.calls, want)
			}
			for i, key := range keys {
				want := presenceOf(key.ID, key.RequiredMode, key.ForbiddenMode, rows)
				got := schema.PresenceRules{Required: cur.Keys[i].Required, Forbidden: cur.Keys[i].Forbidden}
				if cur.Keys[i].ID != key.ID || !reflect.DeepEqual(got, want) {
					t.Fatalf("key %d: got %+v, want %s %+v", i, cur.Keys[i], key.ID, want)
				}
			}
			keys[0].Declaration = "invalid"
			if _, err := buildCurrentState(t.Context(), cat, presenceEnvironmentReader{}, nil); err == nil {
				t.Fatal("unreadable declaration was silently accepted")
			}
		})
	}
}

var benchmarkCatalogueKey Key

// Both cases include declaration decoding and key projection; indexed includes
// rebuilding the buckets on every immutable read. No database/production latency
// is measured. Scan retains the pre-#981 algorithm as the comparison baseline.
func BenchmarkCataloguePresence(b *testing.B) {
	for _, size := range []struct{ keys, envs int }{{100, 10}, {config.DefaultKeysPerProject, MaxEnvironmentsPerProject}} {
		for _, dense := range []bool{false, true} {
			keys, rows := cataloguePresenceFixture(size.keys, size.envs, dense)
			for _, indexed := range []bool{false, true} {
				b.Run(fmt.Sprintf("keys=%d/envs=%d/dense=%t/indexed=%t", size.keys, size.envs, dense, indexed), func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						var index map[string][]store.KeyPresence
						if indexed {
							index = indexPresenceByKey(rows)
						}
						for _, key := range keys {
							presence := rows
							if indexed {
								presence = index[key.ID]
							}
							var err error
							benchmarkCatalogueKey, err = keyOf(key, presence)
							if err != nil {
								b.Fatal(err)
							}
						}
					}
				})
			}
		}
	}
}
