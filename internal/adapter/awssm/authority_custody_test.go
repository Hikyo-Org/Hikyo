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

// Revocation lands while the preceding provider request is in flight. The
// next provider request must perform a new durable authorization check.
type revokeAfterRequestAPI struct {
	*fakeAPI
	journal *fakeJournal
	after   string
	err     error
	stopped int
}

func (api *revokeAfterRequestAPI) revoke(operation string, err error) error {
	if err == nil && api.after == operation {
		api.journal.gateErr = api.err
		api.stopped = len(api.calls)
	}
	return err
}

func (api *revokeAfterRequestAPI) CreateSecret(ctx context.Context, input CreateSecretInput) error {
	return api.revoke("create", api.fakeAPI.CreateSecret(ctx, input))
}

func (api *revokeAfterRequestAPI) RestoreSecret(ctx context.Context, name string) error {
	return api.revoke("restore", api.fakeAPI.RestoreSecret(ctx, name))
}

func (api *revokeAfterRequestAPI) TagSecret(ctx context.Context, name string, tags map[string]string) error {
	return api.revoke("tag", api.fakeAPI.TagSecret(ctx, name, tags))
}

func (api *revokeAfterRequestAPI) PutSecretValue(ctx context.Context, name, token, value string) error {
	return api.revoke("put", api.fakeAPI.PutSecretValue(ctx, name, token, value))
}

func (api *revokeAfterRequestAPI) UpdateSecretVersionStage(ctx context.Context, name, stage, moveTo, removeFrom string) error {
	return api.revoke(stage, api.fakeAPI.UpdateSecretVersionStage(ctx, name, stage, moveTo, removeFrom))
}

func TestRevocationStopsEverySubsequentAWSRequest(t *testing.T) {
	for _, operation := range []string{"create", "restore", "tag", "put", awsCurrent, CurrentStage} {
		for _, gateErr := range []error{adapter.ErrUnauthorized, adapter.ErrSuperseded} {
			t.Run(fmt.Sprintf("%s/%v", operation, gateErr), func(t *testing.T) {
				api, journal := newFakeAPI(), newFakeJournal()
				req := adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "revoked"}
				if operation == "restore" || operation == awsCurrent {
					initial := req
					initial.JobID = "initial"
					if _, err := (&Module{API: api}).Sync(t.Context(), initial, journal); err != nil {
						t.Fatal(err)
					}
					api.secrets["prod/app"].deleted = operation == "restore"
				}
				if operation == "tag" {
					api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{}, stages: map[string][]string{"foreign": {awsCurrent}}, values: map[string]string{"foreign": "unmanaged"}}
					journal.states["prod/app"] = adapter.Owned
				}
				req.Ledger = journal.ledger()
				if operation == "tag" {
					req.Ledger[0].AdoptionPending = true
				}
				wrapped := &revokeAfterRequestAPI{fakeAPI: api, journal: journal, after: operation, err: gateErr}
				_, err := (&Module{API: wrapped}).Sync(t.Context(), req, journal)
				if !errors.Is(err, gateErr) {
					t.Fatalf("revocation after %s = %v, want %v", operation, err, gateErr)
				}
				if wrapped.stopped == 0 || len(api.calls) != wrapped.stopped {
					t.Fatalf("requests sent after revocation: %v", api.calls[wrapped.stopped:])
				}
				if state := journal.states["prod/app"]; state != adapter.Owned && state != adapter.Dispatched {
					t.Fatalf("partial resource custody lost: %q", state)
				}
			})
		}
	}
}

func TestDeliveredOwnershipCannotCaptureReplacementSecret(t *testing.T) {
	for _, teardown := range []bool{false, true} {
		t.Run(fmt.Sprintf("teardown=%v", teardown), func(t *testing.T) {
			api, journal := newFakeAPI(), newFakeJournal()
			module := &Module{API: api}
			req := adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "initial"}
			if _, err := module.Sync(t.Context(), req, journal); err != nil {
				t.Fatal(err)
			}
			// The remote name was deleted/recreated, or all ownership markers
			// were removed. Historical Owned is not a fresh adoption decision.
			api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{}, stages: map[string][]string{"foreign": {awsCurrent}}, values: map[string]string{"foreign": "replacement value"}}
			req.Ledger, req.JobID, req.Teardown = journal.ledger(), "replacement", teardown
			if teardown {
				req.Manifest = nil
			}
			before := len(api.writes())
			if _, err := module.Sync(t.Context(), req, journal); !errors.Is(err, adapter.ErrConflict) {
				t.Fatalf("replacement resource = %v, want conflict", err)
			}
			if writes := api.writes()[before:]; len(writes) != 0 || api.secrets["prod/app"].deleted || api.secrets["prod/app"].values["foreign"] != "replacement value" {
				t.Fatalf("replacement was captured: writes=%v secret=%+v", writes, api.secrets["prod/app"])
			}
		})
	}
}

func TestPendingAdoptionPermitsWriteButNeverUntaggedPrune(t *testing.T) {
	for _, teardown := range []bool{false, true} {
		t.Run(fmt.Sprintf("teardown=%v", teardown), func(t *testing.T) {
			api, journal := newFakeAPI(), newFakeJournal()
			api.secrets["prod/app"] = &fakeSecret{tags: map[string]string{}, stages: map[string][]string{"foreign": {awsCurrent}}, values: map[string]string{"foreign": "unmanaged"}}
			journal.states["prod/app"] = adapter.Owned
			ledger := journal.ledger()
			ledger[0].AdoptionPending = true
			req := adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), Ledger: ledger, JobID: "adopt", Teardown: teardown}
			if teardown {
				req.Manifest = nil
			}
			_, err := (&Module{API: api}).Sync(t.Context(), req, journal)
			if teardown {
				if !errors.Is(err, adapter.ErrConflict) || len(api.writes()) != 0 || api.secrets["prod/app"].deleted {
					t.Fatalf("adoption deleted unmarked foreign resource: err=%v writes=%v", err, api.writes())
				}
			} else if err != nil || api.secrets["prod/app"].tags[adapter.SentinelName] != testTarget || !slices.Contains(api.writes(), "put:prod/app") {
				t.Fatalf("fresh adoption could not write: err=%v writes=%v", err, api.writes())
			}
		})
	}
}

type wrongResourceMetadataAPI struct {
	*fakeAPI
	mutate func(*SecretMetadata)
}

func (api *wrongResourceMetadataAPI) DescribeSecret(ctx context.Context, id string) (SecretMetadata, error) {
	meta, err := api.fakeAPI.DescribeSecret(ctx, id)
	if err == nil {
		api.mutate(&meta)
	}
	return meta, err
}

func TestProviderMetadataCannotRedirectWritesOrDeletes(t *testing.T) {
	for _, mutation := range []string{"name", "arn-name", "partial-arn"} {
		for _, teardown := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/teardown=%v", mutation, teardown), func(t *testing.T) {
				api, journal := newFakeAPI(), newFakeJournal()
				req := adapter.SyncRequest{Target: jsonTarget(), Manifest: manifest(), JobID: "initial"}
				if _, err := (&Module{API: api}).Sync(t.Context(), req, journal); err != nil {
					t.Fatal(err)
				}
				wrapped := &wrongResourceMetadataAPI{fakeAPI: api, mutate: func(meta *SecretMetadata) {
					switch mutation {
					case "name":
						meta.Name = "other"
					case "arn-name":
						meta.ARN = strings.Replace(meta.ARN, "prod/app-", "other-", 1)
					case "partial-arn":
						meta.ARN = strings.TrimSuffix(meta.ARN, "-AbCdEf")
					}
				}}
				req.Ledger, req.JobID, req.Teardown = journal.ledger(), "wrong-metadata", teardown
				if teardown {
					req.Manifest = nil
				}
				before := len(api.writes())
				if _, err := (&Module{API: wrapped}).Sync(t.Context(), req, journal); !errors.Is(err, adapter.ErrDestinationID) {
					t.Fatalf("redirected provider metadata = %v, want destination refusal", err)
				}
				if len(api.writes()) != before {
					t.Fatalf("wrong resource metadata dispatched a write: %v", api.writes()[before:])
				}
			})
		}
	}
}
