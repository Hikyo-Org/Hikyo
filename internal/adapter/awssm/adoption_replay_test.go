package awssm

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func TestPlanExposesHeldOwnershipLossForFreshAdoption(t *testing.T) {
	for _, target := range []adapter.Target{jsonTarget(), perKeyTarget()} {
		for _, state := range []adapter.LedgerState{adapter.Owned, adapter.Dispatched} {
			for _, owner := range []string{"", "another-target"} {
				t.Run(fmt.Sprintf("%s/%s/owner=%s", target.Destination.Kind, state, owner), func(t *testing.T) {
					api := newFakeAPI()
					name := "prod/app"
					if target.Destination.Kind == adapter.PerKey {
						name = "prod/APP_DATABASE_URL"
					}
					tags := map[string]string{}
					if owner != "" {
						tags[adapter.SentinelName] = owner
					}
					api.secrets[name] = &fakeSecret{tags: tags, stages: map[string][]string{"legacy": {awsCurrent}}, values: map[string]string{"legacy": "external"}}
					plan, err := (&Module{API: api}).Plan(t.Context(), adapter.PlanRequest{Target: target, Manifest: manifest(), Ledger: []adapter.LedgerEntry{{Surface: adapter.Secret, EffectiveName: name, State: state}}, Gate: func(context.Context) error { return nil }})
					if err != nil {
						t.Fatal(err)
					}
					matched := false
					for _, change := range plan.Changes {
						if change.EffectiveName == name {
							matched = true
							if change.Disposition != adapter.Conflict {
								t.Fatalf("held ownership loss plan = %+v, want fresh conflict artifact", change)
							}
						}
					}
					if !matched {
						t.Fatal("plan omitted held name")
					}
					if len(api.writes()) != 0 {
						t.Fatalf("value-blind plan mutated provider: %v", api.writes())
					}
				})
			}
		}
	}
}

func TestHeldOwnershipPlanRechecksAuthorityBeforeMetadata(t *testing.T) {
	api := newFakeAPI()
	gates := 0
	_, err := (&Module{API: api}).Plan(t.Context(), adapter.PlanRequest{Target: perKeyTarget(), Manifest: manifest(), Ledger: []adapter.LedgerEntry{{Surface: adapter.Secret, EffectiveName: "prod/APP_DATABASE_URL", State: adapter.Owned}}, Gate: func(context.Context) error {
		gates++
		if gates == 3 {
			return adapter.ErrUnauthorized
		}
		return nil
	}})
	if !errors.Is(err, adapter.ErrUnauthorized) {
		t.Fatalf("metadata gate = %v", err)
	}
	for _, call := range api.calls {
		if strings.HasPrefix(call, "describe:") {
			t.Fatalf("metadata request after revocation: %v", api.calls)
		}
	}
}

func TestInterruptedAdoptionRetainsOnlyApprovedPredecessor(t *testing.T) {
	for _, interruption := range []string{"lost-put-response", "promotion-refusal"} {
		for _, foreign := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/foreign=%v", interruption, foreign), func(t *testing.T) {
				api, journal := newFakeAPI(), newFakeJournal()
				api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{}, stages: map[string][]string{"legacy": {awsCurrent}}, values: map[string]string{"legacy": "approved legacy"}}
				journal.states["prod/app"] = adapter.Owned
				req := adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "adoption"}
				req.Ledger[0].AdoptionPending = true
				req.Ledger[0].AdoptionWitness = testAdoptionWitness(t, api, "prod/app")
				var provider API = api
				if interruption == "lost-put-response" {
					provider = &lostWriteResponseAPI{fakeAPI: api, operation: "put"}
				} else {
					api.failOnce["stage-"+awsCurrent+":prod/app"] = &ResponseError{Status: 503}
				}
				if _, err := (&Module{API: provider}).Sync(t.Context(), req, journal); !errors.Is(err, adapter.ErrIndeterminate) {
					t.Fatalf("interruption = %v, want indeterminate", err)
				}
				if api.current("prod/app") != "approved legacy" || journal.states["prod/app"] != adapter.Dispatched || len(api.secrets["prod/app"].values) != 2 {
					t.Fatal("fixture did not stage exactly one value while retaining legacy current")
				}
				if foreign {
					api.externalPut("prod/app", "foreign", "later external value")
				}
				// The production ledger no longer exposes AdoptionPending after
				// an ambiguous outcome changes Owned to Dispatched.
				req.Ledger = journal.ledger()
				before := len(api.writes())
				_, err := (&Module{API: api}).Sync(t.Context(), req, journal)
				if foreign {
					if !errors.Is(err, adapter.ErrConflict) || len(api.writes()) != before || api.current("prod/app") != "later external value" {
						t.Fatalf("replay overrode later external edit: err=%v writes=%v", err, api.writes()[before:])
					}
					return
				}
				if err != nil {
					t.Fatalf("approved unchanged predecessor could not resume: %v", err)
				}
				secret := api.secrets["prod/app"]
				token := idempotencyToken(req.JobID, req.Target.ID, req.Target.Generation, "prod/app")
				if len(secret.values) != 2 || secret.tags[VersionTag] != token || !slices.Contains(secret.stages[token], awsCurrent) || !slices.Contains(secret.stages[token], CurrentStage) || journal.states["prod/app"] != adapter.Owned {
					t.Fatalf("resume duplicated a value or failed ownership: %+v", secret)
				}
			})
		}
	}
}

func TestLegacyInterruptedAdoptionWithoutApprovedMarkerFailsClosed(t *testing.T) {
	api, journal := newFakeAPI(), newFakeJournal()
	req := adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "old-adoption"}
	token := idempotencyToken(req.JobID, req.Target.ID, req.Target.Generation, "prod/app")
	api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{adapter.SentinelName: testTarget}, stages: map[string][]string{"legacy": {awsCurrent}, token: {PendingStage}}, values: map[string]string{"legacy": "unproved legacy", token: "staged value"}}
	journal.states["prod/app"] = adapter.Dispatched
	req.Ledger = journal.ledger()
	if _, err := (&Module{API: api}).Sync(t.Context(), req, journal); !errors.Is(err, adapter.ErrConflict) || len(api.writes()) != 0 {
		t.Fatalf("missing approved-predecessor marker inferred consent: err=%v writes=%v", err, api.writes())
	}
}

func testAdoptionWitness(t *testing.T, api API, name string) string {
	t.Helper()
	meta, err := api.DescribeSecret(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	return adoptionWitness(meta)
}

func TestAdoptionBindsReviewedResourceAndVersion(t *testing.T) {
	for _, scenario := range []string{"unchanged", "external-edit", "replacement", "missing-witness", "empty-current"} {
		t.Run(scenario, func(t *testing.T) {
			api, journal := newFakeAPI(), newFakeJournal()
			secret := &fakeSecret{tags: map[string]string{}, stages: map[string][]string{"reviewed": {awsCurrent}}, values: map[string]string{"reviewed": "reviewed synthetic"}}
			if scenario == "empty-current" {
				secret.stages, secret.values = map[string][]string{}, map[string]string{}
			}
			api.secrets["prod/app"] = secret
			provider := &wrongResourceMetadataAPI{fakeAPI: api, mutate: func(*SecretMetadata) {}}
			module := &Module{API: provider}
			plan, err := module.Plan(t.Context(), adapter.PlanRequest{Target: jsonTarget(), Manifest: manifest(), Gate: func(context.Context) error { return nil }})
			if err != nil || len(plan.Changes) != 1 || plan.Changes[0].Disposition != adapter.Conflict || plan.Changes[0].ObservedProviderWitness == "" {
				t.Fatalf("plan omitted exact adoption witness: %+v, %v", plan, err)
			}
			journal.states["prod/app"] = adapter.Owned
			req := adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "initial-adoption"}
			req.Ledger[0].AdoptionPending = true
			req.Ledger[0].AdoptionWitness = plan.Changes[0].ObservedProviderWitness
			switch scenario {
			case "external-edit":
				api.externalPut("prod/app", "unreviewed", "later synthetic")
			case "replacement":
				provider.mutate = func(meta *SecretMetadata) { meta.ARN = strings.TrimSuffix(meta.ARN, "AbCdEf") + "GhIjKl" }
			case "missing-witness":
				req.Ledger[0].AdoptionWitness = ""
			}
			_, err = module.Sync(t.Context(), req, journal)
			if scenario == "unchanged" || scenario == "empty-current" {
				if err != nil || len(api.writes()) == 0 || journal.states["prod/app"] != adapter.Owned {
					t.Fatalf("unchanged reviewed resource did not converge: %v", err)
				}
				return
			}
			if !errors.Is(err, adapter.ErrConflict) || len(api.writes()) != 0 || journal.states["prod/app"] != adapter.Owned {
				t.Fatalf("unreviewed resource/version was mutated or custody lost: %v calls=%v", err, api.writes())
			}
		})
	}
}

// A production Prepare leaves adopted Owned custody intact. A failed Finish
// therefore exposes the same pending adoption on retry, unlike fakeJournal's
// default Reserved-to-Dispatched simplification.
type unfinishedAdoptionJournal struct {
	*fakeJournal
	failFinish bool
}

func (j *unfinishedAdoptionJournal) Prepare(_ context.Context, effect adapter.Effect, _ adapter.LedgerState) error {
	j.intents = append(j.intents, string(effect.Disposition)+":"+effect.EffectiveName)
	return nil
}

func (j *unfinishedAdoptionJournal) Finish(ctx context.Context, effect adapter.Effect, completion adapter.Completion) error {
	if j.failFinish {
		j.failFinish = false
		return errors.New("injected lost journal transaction")
	}
	return j.fakeJournal.Finish(ctx, effect, completion)
}

func TestPromotedAdoptionReplaysOnlyThisJobOnReviewedResource(t *testing.T) {
	for _, interruption := range []string{"finish-failure", "definite-marker-failure"} {
		for _, mutation := range []string{"unchanged", "foreign-current", "replacement", "missing-witness", "malformed-witness", "different-job"} {
			t.Run(interruption+"/"+mutation, func(t *testing.T) {
				api := newFakeAPI()
				api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{}, stages: map[string][]string{"reviewed": {awsCurrent}}, values: map[string]string{"reviewed": "reviewed synthetic"}}
				provider := &wrongResourceMetadataAPI{fakeAPI: api, mutate: func(*SecretMetadata) {}}
				journal := &unfinishedAdoptionJournal{fakeJournal: newFakeJournal(), failFinish: interruption == "finish-failure"}
				journal.states["prod/app"] = adapter.Owned
				req := adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: journal.ledger(), JobID: "promoted-adoption"}
				req.Ledger[0].AdoptionPending = true
				req.Ledger[0].AdoptionWitness = testAdoptionWitness(t, api, "prod/app")
				if interruption == "definite-marker-failure" {
					api.failOnce["stage-"+CurrentStage+":prod/app"] = &ResponseError{Status: 400}
				}
				module := &Module{API: provider}
				if _, err := module.Sync(t.Context(), req, journal); err == nil {
					t.Fatal("injected interruption did not fail")
				}
				token := idempotencyToken(req.JobID, req.Target.ID, req.Target.Generation, "prod/app")
				if !slices.Contains(api.secrets["prod/app"].stages[token], awsCurrent) || len(api.secrets["prod/app"].values) != 2 || journal.states["prod/app"] != adapter.Owned {
					t.Fatal("fixture did not preserve promoted value with pending Owned custody")
				}
				switch mutation {
				case "foreign-current":
					api.externalPut("prod/app", "foreign", "later synthetic")
				case "replacement":
					provider.mutate = func(meta *SecretMetadata) { meta.ARN = strings.TrimSuffix(meta.ARN, "AbCdEf") + "GhIjKl" }
				case "missing-witness":
					req.Ledger[0].AdoptionWitness = ""
				case "malformed-witness":
					req.Ledger[0].AdoptionWitness = "[]"
				case "different-job":
					req.JobID = "unapproved-next-job"
				}
				before := len(api.writes())
				_, err := module.Sync(t.Context(), req, journal)
				if mutation != "unchanged" {
					if !errors.Is(err, adapter.ErrConflict) || len(api.writes()) != before || journal.states["prod/app"] != adapter.Owned {
						t.Fatalf("unsafe replay accepted or custody lost: %v, %v", err, api.writes()[before:])
					}
					return
				}
				secret := api.secrets["prod/app"]
				if err != nil || len(secret.values) != 2 || secret.tags[VersionTag] != token || !slices.Contains(secret.stages[token], CurrentStage) {
					t.Fatalf("same-job metadata repair failed: %v", err)
				}
				for _, call := range api.writes()[before:] {
					if call != "stage-"+CurrentStage+":prod/app" && call != "tag:prod/app" {
						t.Fatalf("replay performed more than metadata repair: %s", call)
					}
				}
			})
		}
	}
}
