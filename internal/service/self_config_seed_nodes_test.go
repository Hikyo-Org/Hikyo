package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/operation"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/tlstest"
)

func selfConfigNodeSeedValues(port, budget string) map[string]string {
	return map[string]string{"HIKYO_LISTEN": "127.0.0.1:" + port, "HIKYO_OPERATIONAL_LISTEN": "127.0.0.1:9090", "HIKYO_ADMISSION_BUDGET_MIB": budget, "HIKYO_BACKUP_DIR": "/tmp/private-node-seed"}
}

func TestSelfConfigNodeSeedAdoptionImportsEncryptedLocalInputs(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			cfg := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "seed.db")}
			if engine == "postgres" {
				cfg = selfConfigPostgres(t)
			}
			s, local := unmanagedSelfConfigFixture(t, cfg)
			values := selfConfigNodeSeedValues("8080", "256")
			s.SeedNode = func() (map[string]string, error) { return maps.Clone(values), nil }
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := s.attestNodeSeed(operation.WithNetwork(t.Context()), *s.seed); err == nil {
				t.Fatal("network context minted node seed authority")
			}
			if _, err := s.prepareAdoptionSeed(operation.WithNetwork(t.Context()), nil); err == nil {
				t.Fatal("network context used host adoption authority")
			}
			actor, session := selfConfigSession(t, s, local)
			preview, err := s.PreviewAdoption(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			seed, err := s.prepareAdoptionSeed(t.Context(), &actor)
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := runtimeconfig.Prepare(seed.values)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := bundle.NodeValues(s.NodeID)
			if err != nil || !maps.Equal(actual, values) {
				t.Fatalf("node seed did not preserve exact values: %v", err)
			}
			if _, err := bundle.NodeValues("another-node"); err == nil {
				t.Fatal("missing node inherited another node's values")
			}
			at, err := s.runtimeTimestamp(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			err = tx.Read(t.Context(), s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
				_, p, err := authorize(ctx, az, actor, authz.OpSelfConfigPreview, domain.Scope{}, s.now())
				if err != nil {
					return err
				}
				if _, err := r.SelfConfig().HostSeedInputs(ctx, p, at); err == nil {
					t.Fatal("human preview proof obtained closed host seed discovery")
				}
				inputs, err := r.SelfConfig().SeedInputs(ctx, p, s.NodeID, at)
				if err != nil {
					return err
				}
				if len(inputs) != 1 || bytes.Contains(inputs[0].Ciphertext, []byte("private-node-seed")) || bytes.Contains(inputs[0].Ciphertext, []byte("HIKYO_LISTEN")) {
					t.Fatal("node seed was not stored as ciphertext")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "adopt", OwnerInstanceID: preview.OwnerInstanceID, SchemaVersion: runtimeconfig.SchemaVersion, PreviewToken: preview.PreviewToken})
			status, err := s.Adopt(t.Context(), actor, SelfConfigAdoptRequest{PreviewToken: preview.PreviewToken, IdempotencyKey: "node-seed-adopt"})
			if err != nil || !status.Managed {
				t.Fatalf("adoption: %v", err)
			}
			if err := s.attestNodeSeed(t.Context(), *s.seed); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("adopted node allowed seed replacement: %v", err)
			}
			resolved, err := s.ResolveRuntimeBundle(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			actual, err = resolved.NodeValues(s.NodeID)
			if err != nil || !maps.Equal(actual, values) {
				t.Fatalf("published node values differ: %v", err)
			}
		})
	}
}

func TestSelfConfigNodeSeedChangeInvalidatesReviewedAdoption(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "seed-race.db")}
			if engine == store.EnginePostgres {
				cfg = selfConfigPostgres(t)
			}
			s, local := unmanagedSelfConfigFixture(t, cfg)
			s.SeedNode = func() (map[string]string, error) { return selfConfigNodeSeedValues("8080", "256"), nil }
			if err := s.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			actor, session := selfConfigSession(t, s, local)
			preview, err := s.PreviewAdoption(t.Context(), actor)
			if err != nil {
				t.Fatal(err)
			}
			selfConfigReauthenticate(t, s, session, SelfConfigReauthTarget{Action: "adopt", OwnerInstanceID: preview.OwnerInstanceID, SchemaVersion: runtimeconfig.SchemaVersion, PreviewToken: preview.PreviewToken})
			// A restarted node can observe changed startup inputs between review
			// and import. Its new attestation must invalidate the old ceremony.
			restarted := &SelfConfig{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth, NodeID: s.NodeID, Seed: s.Seed, SeedNode: func() (map[string]string, error) { return selfConfigNodeSeedValues("8081", "512"), nil }}
			if err := restarted.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Adopt(t.Context(), actor, SelfConfigAdoptRequest{PreviewToken: preview.PreviewToken, IdempotencyKey: "changed-node-seed"}); !errors.Is(err, domain.ErrConflict) {
				t.Fatalf("changed node inputs accepted reviewed preview: %v", err)
			}
			status, err := s.Status(t.Context(), actor)
			if err != nil || status.Managed {
				t.Fatalf("failed adoption created a binding: %v", err)
			}
		})
	}
}

func TestSelfConfigNodeSeedHAKeepsDifferentInputs(t *testing.T) {
	s, local := unmanagedSelfConfigFixture(t, selfConfigPostgres(t))
	s.NodeID = "replica-a"
	first := selfConfigNodeSeedValues("8080", "256")
	second := selfConfigNodeSeedValues("8082", "512")
	s.SeedNode = func() (map[string]string, error) { return maps.Clone(first), nil }
	peer := &SelfConfig{DB: s.DB, Keyring: s.Keyring, Auth: s.Auth, NodeID: "replica-b", Seed: s.Seed, SeedNode: func() (map[string]string, error) { return maps.Clone(second), nil }}
	at, err := s.DB.Coordination().Now(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{s.NodeID, peer.NodeID} {
		if err := s.DB.Coordination().UpsertNode(t.Context(), store.HANode{NodeID: id, BinaryVersion: "node-seed-test", SchemaVersion: 1, RootKeyFingerprint: "shared-test-root", StartedAt: at, HeartbeatAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	actor, _ := selfConfigSession(t, s, local)
	if _, err := s.PreviewAdoption(t.Context(), actor); !errors.Is(err, store.ErrSelfConfigSeedDisagreement) {
		t.Fatalf("missing peer seed accepted: %v", err)
	}
	if err := peer.LoadRuntime(t.Context()); err != nil {
		t.Fatal(err)
	}
	seed, err := s.prepareAdoptionSeed(t.Context(), &actor)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := runtimeconfig.Prepare(seed.values)
	if err != nil {
		t.Fatal(err)
	}
	for node, want := range map[string]map[string]string{s.NodeID: first, peer.NodeID: second} {
		got, err := bundle.NodeValues(node)
		if err != nil || !maps.Equal(got, want) {
			t.Fatalf("HA node %s did not retain its own values: %v", node, err)
		}
	}
	// Membership is fixed by exact references at the final import transaction.
	if err := s.DB.Coordination().UpsertNode(t.Context(), store.HANode{NodeID: "late-node", BinaryVersion: "node-seed-test", SchemaVersion: 1, RootKeyFingerprint: "shared-test-root", StartedAt: at, HeartbeatAt: at.Add(time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.prepareAdoptionSeed(t.Context(), &actor); !errors.Is(err, store.ErrSelfConfigSeedDisagreement) {
		t.Fatalf("late node silently omitted: %v", err)
	}
}

func TestHostAdoptionReadsFreshServerSeedWithoutEvaluatingCommandDefaults(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "host-seed.db")}
			if engine == store.EnginePostgres {
				cfg = selfConfigPostgres(t)
			}
			server, _ := unmanagedSelfConfigFixture(t, cfg)
			server.Installer = &installerProbe{activateFailures: make(map[string]int)}
			server.Seed = func() (map[string]string, error) {
				return map[string]string{"HIKYO_ARGON2_TIME": "4", "HIKYO_EXTERNAL_ORIGIN": "https://live.example", "HIKYO_UPDATE_CHANNEL": "nightly"}, nil
			}
			node := selfConfigNodeSeedValues("45790", "512")
			server.SeedNode = func() (map[string]string, error) { return maps.Clone(node), nil }
			rejectCommandDefaults := func() (map[string]string, error) {
				t.Error("host evaluated CLI configuration")
				return nil, errors.New("command defaults must not be read")
			}
			host := &SelfConfig{DB: server.DB, Keyring: server.Keyring, NodeID: server.NodeID, Seed: rejectCommandDefaults, SeedNode: rejectCommandDefaults}
			if _, err := host.prepareAdoptionSeed(t.Context(), nil); !errors.Is(err, store.ErrSelfConfigSeedDisagreement) {
				t.Fatalf("missing server seed accepted: %v", err)
			}
			if err := server.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			seed, err := host.prepareAdoptionSeed(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := runtimeconfig.Prepare(seed.values)
			if err != nil {
				t.Fatal(err)
			}
			got, err := bundle.NodeValues(server.NodeID)
			if err != nil || !maps.Equal(got, node) || bundle.OwnerValues()["HIKYO_ARGON2_TIME"] != "4" || bundle.OwnerValues()["HIKYO_EXTERNAL_ORIGIN"] != "https://live.example" || bundle.UpdateChannel() != "nightly" {
				t.Fatal("host imported defaults instead of exact server settings")
			}
			// Host preparation reads only: the server sees the exact same authenticated
			// token and references rather than an attestation rewritten by the command.
			again, err := host.prepareAdoptionSeed(t.Context(), nil)
			if err != nil || again.token != seed.token {
				t.Fatalf("host seed read changed server attestation: %v", err)
			}
			host.NodeID = "command-default-node"
			discovered, err := host.prepareAdoptionSeed(t.Context(), nil)
			if err != nil || discovered.token != seed.token {
				t.Fatalf("closed host could not discover exact standalone server identity: %v", err)
			}
			host.NodeID = server.NodeID
			if engine == store.EngineSQLite {
				host.Now = func() time.Time { return server.now().Add(time.Minute) }
				if _, err := host.prepareAdoptionSeed(t.Context(), nil); !errors.Is(err, store.ErrSelfConfigSeedDisagreement) {
					t.Fatalf("stale server seed accepted: %v", err)
				}
			}
		})
	}
}

func TestSelfConfigSeedEnvelopeFitsAllBoundedValues(t *testing.T) {
	owner, node := map[string]string{}, map[string]string{}
	for _, key := range runtimeconfig.Catalogue() {
		owner[key.Name] = strings.Repeat("<", schema.MaxValueBytes)
	}
	for _, key := range config.ManagedNodeKeys() {
		node[key] = strings.Repeat("<", config.MaxManagedNodeValueBytes)
	}
	raw, err := encodeSelfConfigNodeSeed(strings.Repeat("o", 64), strings.Repeat("i", 64), strings.Repeat("n", 63), node, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw)+1024 > store.MaxSelfConfigSeedInputBytes {
		t.Fatal("transport bound cannot contain current catalogue with worst-case JSON escaping and AEAD framing")
	}
}

func TestHostAdoptionPreservesLargeMailAndTLSSeed(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "large-seed.db")}
			if engine == store.EnginePostgres {
				cfg = selfConfigPostgres(t)
			}
			server, _ := unmanagedSelfConfigFixture(t, cfg)
			cert, key, _ := tlstest.MintServerCert(t, "127.0.0.1")
			owner := map[string]string{"HIKYO_MAIL_ADDR": "smtp.example:465", "HIKYO_MAIL_TLS": "implicit", "HIKYO_MAIL_USER": "user", "HIKYO_MAIL_PASSWORD": strings.Repeat("<", schema.MaxValueBytes), "HIKYO_MAIL_FROM": "sender@example.com", "HIKYO_MAIL_CA_PEM": strings.Repeat(string(cert), schema.MaxValueBytes/len(cert))}
			server.Seed = func() (map[string]string, error) { return maps.Clone(owner), nil }
			node := selfConfigNodeSeedValues("45791", "512")
			node["HIKYO_TLS_CERT_PEM"], node["HIKYO_TLS_KEY_PEM"] = string(cert), string(key)
			server.SeedNode = func() (map[string]string, error) { return maps.Clone(node), nil }
			if err := server.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			host := &SelfConfig{DB: server.DB, Keyring: server.Keyring, NodeID: "local", SeedNode: server.SeedNode}
			seed, err := host.prepareAdoptionSeed(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range owner {
				if seed.values[name] != value {
					t.Fatal("large owner seed lost exact bytes")
				}
			}
			bundle, err := runtimeconfig.Prepare(seed.values)
			if err != nil {
				t.Fatal(err)
			}
			got, err := bundle.NodeValues(server.NodeID)
			if err != nil || !maps.Equal(got, node) {
				t.Fatal("large envelope changed TLS node seed")
			}
		})
	}
}

func TestHostAdoptionRechecksStandaloneDiscoveryBeforeBinding(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "host-seed-membership.db")}
			if engine == store.EnginePostgres {
				cfg = selfConfigPostgres(t)
			}
			server, local := unmanagedSelfConfigFixture(t, cfg)
			server.SeedNode = func() (map[string]string, error) { return selfConfigNodeSeedValues("45792", "256"), nil }
			if err := server.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			reviewed, err := server.prepareAdoptionSeed(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reviewed.hostSeedDiscovery {
				t.Fatal("host selection mode was not retained")
			}
			peer := &SelfConfig{DB: server.DB, Keyring: server.Keyring, Auth: server.Auth, NodeID: "late-standalone", Seed: server.Seed, SeedNode: server.SeedNode}
			if err := peer.LoadRuntime(t.Context()); err != nil {
				t.Fatal(err)
			}
			err = tx.Write(t.Context(), server.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
				caller, _, err := authorize(ctx, az, local, authz.OpSelfConfigAdopt, domain.Scope{}, server.now())
				if err != nil {
					return err
				}
				_, err = server.provision(ctx, r, az, caller, reviewed, "host-discovery-race", server.now())
				return err
			})
			if !errors.Is(err, store.ErrSelfConfigSeedDisagreement) {
				t.Fatalf("second fresh standalone was ignored at final binding: %v", err)
			}
			status, err := server.Status(t.Context(), local)
			if err != nil || status.Managed {
				t.Fatalf("ambiguous import committed binding: %v", err)
			}
		})
	}
}

func TestAdoptionBindingRechecksSeedFreshnessWithCurrentDatabaseClock(t *testing.T) {
	for _, engine := range []store.Engine{store.EngineSQLite, store.EnginePostgres} {
		for _, host := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/host=%v", engine, host), func(t *testing.T) {
				cfg := store.Config{Engine: engine, Path: filepath.Join(t.TempDir(), "host-seed-clock.db")}
				if engine == store.EnginePostgres {
					cfg = selfConfigPostgres(t)
				}
				server, local := unmanagedSelfConfigFixture(t, cfg)
				server.SeedNode = func() (map[string]string, error) { return selfConfigNodeSeedValues("45793", "256"), nil }
				if err := server.LoadRuntime(t.Context()); err != nil {
					t.Fatal(err)
				}
				var actor *Actor
				if !host {
					actor = &local
				}
				reviewed, err := server.prepareAdoptionSeed(t.Context(), actor)
				if err != nil {
					t.Fatal(err)
				}
				at, err := server.runtimeTimestamp(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				stale := at.Add(-time.Minute)
				// Model a review timestamp captured before a long wait without making
				// the suite sleep: the ciphertext and reference stay exact, but their
				// authenticated input row and proposed binding timestamp are now old.
				err = tx.Write(t.Context(), server.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
					proof, err := az.SelfConfigSeedAuthority(ctx)
					if err != nil {
						return err
					}
					inputs, err := r.SelfConfig().SeedInputs(ctx, proof, server.NodeID, at)
					if err != nil {
						return err
					}
					input := inputs[0]
					input.UpdatedAt = stale
					return r.SelfConfig().PutSeedInput(ctx, proof, input)
				})
				if err != nil {
					t.Fatal(err)
				}
				err = tx.Write(t.Context(), server.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
					caller, _, err := authorize(ctx, az, local, authz.OpSelfConfigAdopt, domain.Scope{}, server.now())
					if err != nil {
						return err
					}
					_, err = server.provision(ctx, r, az, caller, reviewed, "host-stale-clock", stale)
					return err
				})
				if !errors.Is(err, store.ErrSelfConfigSeedDisagreement) {
					t.Fatalf("old binding timestamp revived expired seed: %v", err)
				}
				status, err := server.Status(t.Context(), local)
				if err != nil || status.Managed {
					t.Fatalf("expired import committed binding: %v", err)
				}
			})
		}
	}
}
