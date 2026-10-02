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
