package app

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"strings"
	"sync"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

// devFakeProvider is the --dev-only in-process stand-in for a deployment
// provider (#157). It exists for one reason: a browser flow cannot exercise
// the adapters surface against a real provider, because adapter egress
// requires HTTPS with certificate validation to a public address and no such
// provider exists inside the e2e harness. The fake keeps the whole rest of
// the path real: configuration, ceremonies, the outbox, the ownership ledger,
// INTENT/OUTCOME journaling and the audit trail all run exactly as they do
// against Forgejo or GitHub; only the HTTP peer is replaced by an in-memory
// name store per origin.
//
// It is refused at boot outside --dev (config.go), so it can never stand in
// for a provider on an instance that is not an evaluation instance.
type devFakeProvider struct {
	mu     sync.Mutex
	stores map[string]*devFakeStore
}

type devFakeStore struct {
	secrets   map[string]string
	variables map[string]string
}

func newDevFakeProvider() *devFakeProvider {
	return &devFakeProvider{stores: make(map[string]*devFakeStore)}
}

// hasState keeps a mode switch from abandoning simulated remote ownership.
// The provider belongs to the application owner, not a replaceable graph.
func (p *devFakeProvider) hasState() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, store := range p.stores {
		if len(store.secrets) != 0 || len(store.variables) != 0 {
			return true
		}
	}
	return false
}

func (p *devFakeProvider) store(origin string, destination adapter.Destination) *devFakeStore {
	p.mu.Lock()
	defer p.mu.Unlock()
	key := origin + "\x00" + string(destination.Kind) + "\x00" + destination.Owner + "\x00" + destination.Name + "\x00" + destination.Environment
	s, ok := p.stores[key]
	if !ok {
		s = &devFakeStore{secrets: map[string]string{}, variables: map[string]string{}}
		p.stores[key] = s
	}
	return s
}

// factory yields one module per lease. The credential is accepted when it is
// non-empty and not the literal "revoked", which lets a flow exercise the
// auth-refusal path deterministically. The e2e-possible-capture and
// e2e-owned-missing credentials simulate provider races through the real
// journal protocol below; they carry no authority outside this --dev fake.
func (p *devFakeProvider) factory(_ adapter.Provider, config adapter.Config, credential string) (*adapter.ModuleLease, error) {
	if credential == "" {
		return nil, adapter.ErrProviderAuth
	}
	return adapter.NewModuleLease(&devFakeModule{provider: p, origin: config.Origin, credential: credential}, func() {})
}

type devFakeModule struct {
	provider   *devFakeProvider
	origin     string
	credential string
}

func (m *devFakeModule) ValidateConfig(config adapter.Config) error {
	if !strings.HasPrefix(config.Origin, "https://") {
		return errors.New("dev fake provider: origin must be an https origin")
	}
	return nil
}

func devFakeDestinationID(destination adapter.Destination) int64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(string(destination.Kind) + "/" + destination.Owner + "/" + destination.Name + "/" + destination.Environment))
	return int64(h.Sum32()%1_000_000) + 1
}

func (m *devFakeModule) TestConnection(ctx context.Context, request adapter.ConnectionRequest) (adapter.Connection, error) {
	if request.Gate != nil {
		if err := request.Gate(ctx); err != nil {
			return adapter.Connection{}, err
		}
	}
	if m.credential == "revoked" {
		return adapter.Connection{}, adapter.ErrProviderAuth
	}
	if request.Destination.Owner == "" {
		return adapter.Connection{}, errors.New("dev fake provider: destination owner is required")
	}
	connection := adapter.Connection{Version: "dev-fake", DestinationID: devFakeDestinationID(request.Destination)}
	if request.Destination.Kind == adapter.Environment {
		connection.RepositoryID = devFakeDestinationID(adapter.Destination{Kind: adapter.Repository, Owner: request.Destination.Owner, Name: request.Destination.Name})
	}
	return connection, nil
}

func (m *devFakeModule) Plan(ctx context.Context, request adapter.PlanRequest) (adapter.Plan, error) {
	if request.Gate != nil {
		if err := request.Gate(ctx); err != nil {
			return adapter.Plan{}, err
		}
	}
	if err := adapter.ValidateManifest(request.Target.NamePrefix, request.Manifest); err != nil {
		return adapter.Plan{}, err
	}
	ledger, err := adapter.IndexLedger(request.Ledger)
	if err != nil {
		return adapter.Plan{}, err
	}
	store := m.provider.store(m.origin, request.Target.Destination)
	m.provider.mu.Lock()
	names := make([]string, 0, len(store.secrets))
	for name := range store.secrets {
		names = append(names, name)
	}
	m.provider.mu.Unlock()
	desired := adapter.DesiredRows(request.Target.NamePrefix, request.Manifest, true)
	return adapter.Plan{Changes: adapter.PlanChanges(desired, ledger, adapter.NameSet(names))}, nil
}

// Sync follows the journal protocol the real modules follow: reserve, prepare
// (INTENT), write, finish (OUTCOME), then prune undesired owned names with
// sentinels last. An unowned name already present on the secret surface is a
// conflict refusal, exactly as on Forgejo.
func (m *devFakeModule) Sync(ctx context.Context, request adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	if journal == nil {
		return adapter.SyncResult{}, errors.New("dev fake provider: durable journal is required")
	}
	if m.credential == "revoked" {
		return adapter.SyncResult{}, adapter.ErrProviderAuth
	}
	if err := adapter.ValidateManifest(request.Target.NamePrefix, request.Manifest); err != nil {
		return adapter.SyncResult{}, err
	}
	ledger, err := adapter.IndexLedger(request.Ledger)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	store := m.provider.store(m.origin, request.Target.Destination)
	desired := adapter.DesiredRows(request.Target.NamePrefix, request.Manifest, !request.Teardown)
	completed := adapter.CompletedNames(request.Completed)
	result := adapter.SyncResult{}
	for _, row := range desired {
		key := adapter.NewLedgerKey(row.Surface, row.EffectiveName)
		if completed[key] {
			continue
		}
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Create, KeyID: row.KeyID}
		record, claimed := ledger[key]
		state := record.State
		if claimed && (state == adapter.Owned || state == adapter.Dispatched) && !record.Missing {
			effect.Disposition = adapter.Update
		}
		if !claimed {
			state, err = journal.Reserve(ctx, effect)
			if err != nil {
				return result, err
			}
			ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: state}
		}
		m.provider.mu.Lock()
		_, present := store.secrets[strings.ToUpper(row.EffectiveName)]
		if row.Surface == adapter.Variable {
			_, present = store.variables[strings.ToUpper(row.EffectiveName)]
		}
		m.provider.mu.Unlock()
		if state == adapter.Reserved && present {
			if err := journal.Refuse(ctx, effect); err != nil {
				return result, err
			}
			result.Conflicts = append(result.Conflicts, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Conflict})
			return result, fmt.Errorf("%w: %s %s", adapter.ErrConflict, row.Surface, row.EffectiveName)
		}
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		if err := journal.Prepare(ctx, effect, state); err != nil {
			return result, err
		}
		// A missing owned variable followed by a conflicting recreation mirrors
		// GitHub's PATCH 404 then POST 409 path: ownership stays, the missing
		// bit stays, and each attempted provider write has its own outcome.
		if m.credential == "e2e-owned-missing" && row.KeyID != "" && row.Surface == adapter.Variable && claimed && (state == adapter.Owned || state == adapter.Dispatched) {
			if !record.Missing {
				m.provider.mu.Lock()
				delete(store.variables, strings.ToUpper(row.EffectiveName))
				m.provider.mu.Unlock()
				if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeFailure, State: adapter.Owned, Missing: true, ProviderStatus: http.StatusNotFound, Finding: "owned_missing"}); err != nil {
					return result, err
				}
				effect.Disposition = adapter.Create
				if err := journal.Gate(ctx, effect); err != nil {
					return result, err
				}
				if err := journal.Prepare(ctx, effect, adapter.Owned); err != nil {
					return result, err
				}
			}
			m.provider.mu.Lock()
			store.variables[strings.ToUpper(row.EffectiveName)] = "external-recreation"
			m.provider.mu.Unlock()
			if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeFailure, State: adapter.Owned, Missing: true, Conflict: true, ProviderStatus: http.StatusConflict, Finding: "owned_missing"}); err != nil {
				return result, err
			}
			result.Conflicts = append(result.Conflicts, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Conflict})
			return result, fmt.Errorf("%w: owned_missing variable %s", adapter.ErrConflict, row.EffectiveName)
		}
		m.provider.mu.Lock()
		if row.Surface == adapter.Variable {
			store.variables[strings.ToUpper(row.EffectiveName)] = row.Value
		} else {
			store.secrets[strings.ToUpper(row.EffectiveName)] = row.Value
		}
		m.provider.mu.Unlock()
		// A first secret PUT unexpectedly updated a name created concurrently
		// by another actor. The write landed, but ownership is not acquired.
		if m.credential == "e2e-possible-capture" && row.KeyID != "" && row.Surface == adapter.Secret && !claimed {
			if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, ReleaseLedger: true, Conflict: true, ProviderStatus: http.StatusNoContent, Finding: "possible_capture"}); err != nil {
				return result, err
			}
			result.Conflicts = append(result.Conflicts, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Conflict})
			return result, fmt.Errorf("%w: possible_capture secret %s", adapter.ErrConflict, row.EffectiveName)
		}
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned}); err != nil {
			return result, err
		}
		ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: adapter.Owned}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition})
	}
	reservations, prunes := adapter.Undesired(desired, ledger)
	for _, row := range reservations {
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
		if err := journal.ReleaseReservation(ctx, effect); err != nil {
			return result, err
		}
	}
	for _, row := range prunes {
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		if err := journal.Prepare(ctx, effect, row.State); err != nil {
			return result, err
		}
		m.provider.mu.Lock()
		if row.Surface == adapter.Variable {
			delete(store.variables, strings.ToUpper(row.EffectiveName))
		} else {
			delete(store.secrets, strings.ToUpper(row.EffectiveName))
		}
		m.provider.mu.Unlock()
		if err := journal.Finish(ctx, effect, adapter.Completion{Outcome: adapter.OutcomeSuccess, ReleaseLedger: true}); err != nil {
			return result, err
		}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete})
	}
	return result, nil
}
