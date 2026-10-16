package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

func init() {
	corpus = append(corpus,
		scenario{"advisory_environment_topology_mutations", scenarioAdvisoryTopologyMutations},
		scenario{"advisory_environment_topology_failures", scenarioAdvisoryTopologyFailures},
		scenario{"advisory_environment_topology_isolation", scenarioAdvisoryTopologyIsolation},
		scenario{"advisory_environment_topology_definitions", scenarioAdvisoryTopologyDefinitions},
		scenario{"advisory_environment_topology_revoked_sessions", scenarioAdvisoryTopologyRevokedSessions},
	)
}

func scenarioAdvisoryTopologyMutations(t *testing.T, db *store.DB) {
	for _, mutation := range []string{"create", "clone", "rename", "reorder", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			who, scope, _, envs, _ := valueFixture(t, db, "topology"+mutation)
			actor := service.LocalPrincipal(who)
			dev := mustEnv(t, envs, actor, scope, "dev")
			prod := mustEnv(t, envs, actor, scope, "prod")
			envs.Advisory = service.NewAdvisory()
			events := watchTopology(t, db, envs.Advisory, actor, scope)
			var err error
			switch mutation {
			case "create":
				_, err = envs.Create(t.Context(), actor, scope, "new", nil)
			case "clone":
				_, _, err = envs.Clone(t.Context(), actor, scope, "cloned", string(dev.Env), nil)
			case "rename":
				_, err = envs.Rename(t.Context(), actor, dev, "renamed", nil)
			case "reorder":
				_, err = envs.Reorder(t.Context(), actor, scope, []string{string(prod.Env), string(dev.Env)})
			case "delete":
				err = envs.Delete(t.Context(), actor, dev)
			}
			if err != nil {
				t.Fatal(err)
			}
			awaitTopology(t, events, scope)
			// A topology notification is useful only after the listing commits.
			list, err := envs.List(t.Context(), actor, scope)
			if err != nil {
				t.Fatal(err)
			}
			switch mutation {
			case "create", "clone":
				if len(list) != 3 {
					t.Fatalf("committed listing has %d environments, want 3", len(list))
				}
			case "rename":
				if list[0].Name != "renamed" {
					t.Fatalf("rename not committed: %+v", list)
				}
			case "reorder":
				if list[0].ID != string(prod.Env) || list[1].ID != string(dev.Env) {
					t.Fatalf("reorder not committed: %+v", list)
				}
			case "delete":
				if len(list) != 1 || list[0].ID != string(prod.Env) {
					t.Fatalf("delete not committed: %+v", list)
				}
				if _, err := envs.Get(t.Context(), actor, dev); !errors.Is(err, domain.ErrNotFound) {
					t.Fatalf("deleted environment still resolves: %v", err)
				}
			}
		})
	}
}

func scenarioAdvisoryTopologyFailures(t *testing.T, db *store.DB) {
	who, scope, _, envs, _ := valueFixture(t, db, "topologyfailures")
	actor := service.LocalPrincipal(who)
	dev := mustEnv(t, envs, actor, scope, "dev")
	prod := mustEnv(t, envs, actor, scope, "prod")
	denied := service.LocalPrincipal(newPrincipal(t, db, "usr_topology_denied", nil))
	envs.Advisory = service.NewAdvisory()
	events := watchTopology(t, db, envs.Advisory, actor, scope)
	// The missing clone source aborts after inserting its destination, proving
	// a rolled-back write cannot notify readers about an uncommitted topology.
	for _, attempt := range []func() error{
		func() error { _, err := envs.Create(t.Context(), actor, scope, "dev", nil); return err },
		func() error {
			_, _, err := envs.Clone(t.Context(), actor, scope, "rolled-back", "env_missing", nil)
			return err
		},
		func() error { _, err := envs.Rename(t.Context(), actor, dev, "prod", nil); return err },
		func() error {
			_, err := envs.Reorder(t.Context(), actor, scope, []string{string(dev.Env), string(dev.Env)})
			return err
		},
		func() error { return envs.Delete(t.Context(), denied, dev) },
		func() error { _, err := envs.Create(t.Context(), denied, scope, "unauthorized", nil); return err },
		func() error {
			_, _, err := envs.Clone(t.Context(), denied, scope, "unauthorized-clone", string(dev.Env), nil)
			return err
		},
		func() error { _, err := envs.Rename(t.Context(), denied, dev, "unauthorized", nil); return err },
		func() error {
			_, err := envs.Reorder(t.Context(), denied, scope, []string{string(prod.Env), string(dev.Env)})
			return err
		},
	} {
		if err := attempt(); err == nil {
			t.Fatal("failed mutation unexpectedly succeeded")
		}
	}
	list, err := envs.List(t.Context(), actor, scope)
	if err != nil || len(list) != 2 || list[0].Name != "dev" || list[1].Name != "prod" {
		t.Fatalf("failed writes changed topology: %+v, %v", list, err)
	}
	// The successful rename is an ordered barrier after every failed attempt.
	// Any earlier notification is observable before this barrier's event.
	if _, err := envs.Rename(t.Context(), actor, dev, "barrier", nil); err != nil {
		t.Fatal(err)
	}
	awaitTopology(t, events, scope)
	select {
	case ev := <-events:
		t.Fatalf("failed mutation emitted an extra event: %+v", ev)
	case <-time.After(100 * time.Millisecond):
	}
}

func scenarioAdvisoryTopologyIsolation(t *testing.T, db *store.DB) {
	who, scope, values, envs, keys := valueFixture(t, db, "topologyisolation")
	actor := service.LocalPrincipal(who)
	dev := mustEnv(t, envs, actor, scope, "dev")
	mustKey(t, keys, actor, scope, "NOTICE", string(schema.Config), schema.DefaultPresenceRules())
	envs.Advisory = service.NewAdvisory()
	values.Advisory = envs.Advisory
	revisions := &service.Revisions{DB: db, Advisory: envs.Advisory}
	reader := newPrincipal(t, db, "usr_topology_reader", []grantSpec{{"read", scope}})
	events := watchTopology(t, db, envs.Advisory, service.LocalPrincipal(reader), scope)
	ownerEvents := watchTopology(t, db, envs.Advisory, actor, scope)
	// Revocation after connect must remove project-wide topology timing even
	// when an environment read grant remains usable for ordinary cell events.
	execConformance(t, db, `UPDATE grants SET env_id = $1 WHERE principal_id = $2`, string(dev.Env), string(reader))
	if _, err := revisions.Watch(t.Context(), service.LocalPrincipal(reader), scope); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("environment-only reader subscribed to project topology: %v", err)
	}
	// A rule-based directory read is deliberately weaker than a project-wide
	// advisory subscription, even though it can list its selected environment.
	selected := newPrincipal(t, db, "usr_topology_selected", nil)
	if _, err := (&service.Rules{DB: db}).Create(t.Context(), service.LocalPrincipal(admin), service.RuleSpec{
		Target: selected, Capability: domain.CapRead, Org: scope.Org,
		Where: domain.Where{Projects: []domain.ProjectID{scope.Project}, EnvMode: domain.AxisOnly,
			Envs: map[domain.ProjectID][]domain.EnvID{scope.Project: {dev.Env}}, KeyMode: domain.AxisAll},
	}); err != nil {
		t.Fatal(err)
	}
	if list, err := envs.List(t.Context(), service.LocalPrincipal(selected), scope); err != nil || len(list) != 1 {
		t.Fatalf("selected reader cannot navigate its environment: %+v, %v", list, err)
	}
	if _, err := revisions.Watch(t.Context(), service.LocalPrincipal(selected), scope); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("navigation-only rule subscribed to project topology: %v", err)
	}
	sibling, err := (&service.Projects{DB: db}).Create(t.Context(), actor, scope.Org, "topology-sibling")
	if err != nil {
		t.Fatal(err)
	}
	siblingScope := domain.Scope{Org: scope.Org, Project: domain.ProjectID(sibling.ID)}
	if _, err := envs.Create(t.Context(), actor, siblingScope, "hidden-sibling", nil); err != nil {
		t.Fatal(err)
	}
	otherWho, otherScope, _, otherEnvs, _ := valueFixture(t, db, "topologyforeign")
	otherEnvs.Advisory = envs.Advisory
	if _, err := revisions.Watch(t.Context(), service.LocalPrincipal(reader), otherScope); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign project watch = %v", err)
	}
	if _, err := otherEnvs.Create(t.Context(), service.LocalPrincipal(otherWho), otherScope, "hidden", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := envs.Rename(t.Context(), actor, dev, "visible-to-owner", nil); err != nil {
		t.Fatal(err)
	}
	awaitTopology(t, ownerEvents, scope)
	if _, err := values.Set(t.Context(), actor, dev, "NOTICE", "authorized-cell-barrier", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case ev, ok := <-events:
		if !ok || ev.Type != service.AdvisoryStaged || ev.EnvironmentID != string(dev.Env) {
			t.Fatalf("reader must receive only its authorized cell event, not topology: %+v, open=%t", ev, ok)
		}
	case <-time.After(time.Second):
		t.Fatal("authorized environment event did not arrive after filtered topology")
	}
}

func watchTopology(t *testing.T, db *store.DB, advisory *service.Advisory, actor service.Actor, scope domain.Scope) <-chan service.AdvisoryEvent {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	events, err := (&service.Revisions{DB: db, Advisory: advisory}).Watch(ctx, actor, scope)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func awaitTopology(t *testing.T, events <-chan service.AdvisoryEvent, scope domain.Scope) service.AdvisoryEvent {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("stream closed before topology event")
			}
			if ev.Type == "environments.changed" {
				want := service.AdvisoryEvent{Type: service.AdvisoryEnvironmentsChanged, ProjectID: string(scope.Project)}
				if ev != want {
					t.Fatalf("topology event disclosed fields or wrong project: %+v, want %+v", ev, want)
				}
				return ev
			}
		case <-timer.C:
			t.Fatal("successful environment mutation emitted no environments.changed event")
		}
	}
}

func scenarioAdvisoryTopologyDefinitions(t *testing.T, db *store.DB) {
	who, scope, _, envs, _ := valueFixture(t, db, "topologydefinitions")
	actor := service.LocalPrincipal(who)
	mustEnv(t, envs, actor, scope, "dev")
	svc := &service.Definitions{DB: db, Keyring: sharedKeyring(t, db), Advisory: service.NewAdvisory()}
	events := watchTopology(t, db, svc.Advisory, actor, scope)
	for _, change := range []string{"create", "rename", "delete-all"} {
		t.Run(change, func(t *testing.T) {
			raw, err := svc.Export(t.Context(), actor, scope, false)
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := definitions.Parse(raw)
			if err != nil {
				t.Fatal(err)
			}
			bundle := canonical.WireBundle()
			switch change {
			case "create":
				bundle.Environments = append(bundle.Environments, definitions.Environment{Name: "created-by-bundle"})
			case "rename":
				bundle.Environments[0].Name = "renamed-by-bundle"
			case "delete-all":
				bundle.Environments = nil
			}
			raw, err = json.Marshal(bundle)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := svc.Plan(t.Context(), actor, scope, raw, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Apply(t.Context(), actor, scope, plan.ID, service.ApplyOptions{AllowDelete: change == "delete-all"}); err != nil {
				t.Fatal(err)
			}
			awaitTopology(t, events, scope)
		})
	}
}

func scenarioAdvisoryTopologyRevokedSessions(t *testing.T, db *store.DB) {
	for _, kind := range []crypto.ArtifactType{crypto.ArtifactCLISession, crypto.ArtifactBrowserSession} {
		t.Run(string(kind), func(t *testing.T) {
			label := "topologyrevoked" + string(kind)
			who, scope, _, envs, _ := valueFixture(t, db, label)
			actor := service.LocalPrincipal(who)
			dev := mustEnv(t, envs, actor, scope, "dev")
			reader := newPrincipal(t, db, "usr_"+label+"_reader", []grantSpec{{"read", scope}})
			artifact, verifier, err := crypto.NewArtifact(kind)
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			sessionType := "cli"
			if kind == crypto.ArtifactBrowserSession {
				sessionType = "browser"
			}
			err = tx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) error {
				generation, err := az.PrincipalGeneration(ctx, reader)
				if err != nil {
					return err
				}
				epoch, err := az.CredentialEpoch(ctx)
				if err != nil {
					return err
				}
				return az.MintSession(ctx, authz.NewSession{
					ID: "ses_" + label, PrincipalID: reader, Verifier: verifier,
					Artifact: sessionType, SessionGeneration: generation, CredentialEpoch: epoch,
					AuthMethod: "local-password", Factors: `["password"]`,
					AuthenticatedAt: now, CreatedAt: now,
					IdleExpiresAt: now.Add(time.Hour), AbsoluteExpiresAt: now.Add(24 * time.Hour),
				})
			})
			if err != nil {
				t.Fatal(err)
			}
			envs.Advisory = service.NewAdvisory()
			events := watchTopology(t, db, envs.Advisory, service.Bearer(artifact), scope)
			// Positive control: the real bearer receives project metadata before
			// revocation, then the same stream closes on its next topology event.
			if _, err := envs.Rename(t.Context(), actor, dev, "before-revocation", nil); err != nil {
				t.Fatal(err)
			}
			awaitTopology(t, events, scope)
			if err := (&service.Auth{DB: db}).Logout(t.Context(), artifact); err != nil {
				t.Fatal(err)
			}
			if _, err := envs.Rename(t.Context(), actor, dev, "after-revocation", nil); err != nil {
				t.Fatal(err)
			}
			select {
			case ev, ok := <-events:
				if ok {
					t.Fatalf("revoked session received topology: %+v", ev)
				}
			case <-time.After(time.Second):
				t.Fatal("revoked session stream did not close on topology event")
			}
		})
	}
}
