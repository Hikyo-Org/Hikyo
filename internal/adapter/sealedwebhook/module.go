package sealedwebhook

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/crypto/sealedhook"
)

// ConnectionVersion is reported by TestConnection.
const ConnectionVersion = "sealed-webhook/v1"

// envelopeLifetime is how long a sealed envelope stays acceptable. It is well
// under sealedhook.MaxLifetime and longer than any delivery round trip.
const envelopeLifetime = time.Minute

// probeRoute names a connection probe, which belongs to no target yet.
const probeRoute = "probe"

// Receiver reasons with a defined Hikyo meaning (docs/spec/sealed-webhook.md).
const (
	ReasonUnauthorized  = "unauthorized"
	ReasonExistsUnowned = "exists_unowned"
)

// Module implements the four-operation adapter seam for one endpoint.
type Module struct {
	API      API
	Endpoint *Endpoint
	// Binding is the tenant's write-only adapter credential. It travels only
	// inside the recipient-encrypted payload.
	Binding string
	Now     func() time.Time
}

var _ adapter.Module = (*Module)(nil)

func (m *Module) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Module) ready() error {
	if m.API == nil || m.Endpoint == nil {
		return errors.New("sealed-webhook: module is not configured")
	}
	if m.Binding == "" {
		return adapter.ErrProviderAuth
	}
	return nil
}

// ValidateConfig accepts only the activated endpoint's exact origin.
func (m *Module) ValidateConfig(cfg adapter.Config) error {
	if m.Endpoint == nil {
		return errors.New("sealed-webhook: module is not configured")
	}
	origin, err := CanonicalOrigin(cfg.Origin)
	if err != nil {
		return err
	}
	if origin != m.Endpoint.origin {
		return errors.New("sealed-webhook: origin is not an instance-admin configured endpoint")
	}
	return nil
}

// Namespace validates the only destination shape: an organization-kind
// destination whose owner names the receiver namespace.
func Namespace(d adapter.Destination) (string, error) {
	if d.Kind != adapter.Organization || d.Name != "" || d.Environment != "" || d.Visibility != "" || len(d.SelectedRepositoryIDs) != 0 || d.RepositoryID != 0 {
		return "", errors.New("sealed-webhook: destination must be kind organization with only the receiver namespace as owner")
	}
	if !sealedhook.ValidID(d.Owner) {
		return "", errors.New("sealed-webhook: namespace must match [a-z0-9][a-z0-9._-]{0,62}")
	}
	return d.Owner, nil
}

func (m *Module) destinationID(d adapter.Destination) (string, int64, error) {
	namespace, err := Namespace(d)
	if err != nil {
		return "", 0, err
	}
	return namespace, sealedhook.DestinationID(m.Endpoint.fingerprint, namespace), nil
}

// verifyDestination fails closed when the endpoint's trust boundary moved
// since the target was bound (a new fingerprint derives a new identity).
func (m *Module) verifyDestination(target adapter.Target) (string, error) {
	namespace, id, err := m.destinationID(target.Destination)
	if err != nil {
		return "", err
	}
	if target.Destination.NumericID != id {
		return "", fmt.Errorf("%w: endpoint trust boundary changed since this target was bound", adapter.ErrDestinationID)
	}
	return namespace, nil
}

// TestConnection sends a probe envelope and requires a verified ack.
func (m *Module) TestConnection(ctx context.Context, req adapter.ConnectionRequest) (adapter.Connection, error) {
	if err := m.ready(); err != nil {
		return adapter.Connection{}, err
	}
	if req.Gate == nil {
		return adapter.Connection{}, adapter.ErrUnauthorized
	}
	if req.AllowEnvironmentCreate {
		return adapter.Connection{}, errors.New("sealed-webhook: receivers have no environments to create")
	}
	namespace, id, err := m.destinationID(req.Destination)
	if err != nil {
		return adapter.Connection{}, err
	}
	if req.Destination.NumericID != 0 && req.Destination.NumericID != id {
		return adapter.Connection{}, fmt.Errorf("%w: endpoint trust boundary changed since this target was bound", adapter.ErrDestinationID)
	}
	key, err := sealedhook.NewProbeKey()
	if err != nil {
		return adapter.Connection{}, err
	}
	sealed, err := m.seal(namespace, probeRoute, sealedhook.Source{}, sealedhook.OpProbe, nil, nil, key)
	if err != nil {
		return adapter.Connection{}, err
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	ack, err := m.API.Deliver(ctx, sealed)
	if err != nil {
		return adapter.Connection{}, err
	}
	if err := ackError(ack, "probe"); err != nil {
		return adapter.Connection{}, err
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Connection{}, err
	}
	return adapter.Connection{Version: ConnectionVersion, DestinationID: id}, nil
}

// ackError maps a verified non-success ack to the seam's error vocabulary.
func ackError(ack sealedhook.Ack, what string) error {
	switch ack.Status {
	case sealedhook.AckApplied, sealedhook.AckAlreadyApplied:
		return nil
	case sealedhook.AckConflict:
		return fmt.Errorf("%w: %s (%s)", adapter.ErrConflict, what, ack.Reason)
	}
	if ack.Reason == ReasonUnauthorized {
		return fmt.Errorf("%w: receiver refused the binding for %s", adapter.ErrProviderAuth, what)
	}
	return fmt.Errorf("sealed-webhook: receiver rejected %s (%s)", what, ack.Reason)
}

// Plan is value-blind and network-free: a receiver exposes no read, so an
// unclaimed name's existence is unknown until the receiver answers a sync.
func (m *Module) Plan(ctx context.Context, req adapter.PlanRequest) (adapter.Plan, error) {
	if m.Endpoint == nil {
		return adapter.Plan{}, errors.New("sealed-webhook: module is not configured")
	}
	if err := adapter.ValidateSealedWebhookManifest(req.Target.NamePrefix, req.Manifest, false); err != nil {
		return adapter.Plan{}, err
	}
	if req.Gate == nil {
		return adapter.Plan{}, adapter.ErrUnauthorized
	}
	if err := req.Gate(ctx); err != nil {
		return adapter.Plan{}, err
	}
	if _, err := m.verifyDestination(req.Target); err != nil {
		return adapter.Plan{}, err
	}
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.Plan{}, err
	}
	changes := adapter.PlanChanges(adapter.DesiredRows(req.Target.NamePrefix, req.Manifest, true), ledger, nil)
	for i, change := range changes {
		if _, claimed := ledger[adapter.NewLedgerKey(change.Surface, change.EffectiveName)]; !claimed && change.Disposition == adapter.Create {
			changes[i].Disposition = adapter.Unknown
		}
	}
	return adapter.Plan{Changes: changes}, nil
}

func (m *Module) seal(namespace, route string, source sealedhook.Source, op sealedhook.Op, names []sealedhook.Name, values []sealedhook.Value, key string) (Delivery, error) {
	if names == nil {
		names = []sealedhook.Name{}
	}
	sealed, err := m.Endpoint.sealer.Seal(sealedhook.Draft{
		TargetID: m.Endpoint.id, InstanceID: m.Endpoint.instanceID, Namespace: namespace, Route: route,
		Generation: m.Endpoint.generation, Source: source, Op: op, Names: names, Binding: m.Binding,
		Values: values, IdempotencyKey: key, IssuedAt: m.now(), Lifetime: envelopeLifetime,
	})
	if err != nil {
		return Delivery{}, err
	}
	return Delivery{Body: sealed.Body, Digest: sealed.Digest, IdempotencyKey: sealed.IdempotencyKey}, nil
}

type syncContext struct {
	namespace string
	route     string
	source    sealedhook.Source
	target    adapter.Target
	revision  int64
}

func (m *Module) delivery(sc syncContext, op sealedhook.Op, surface adapter.Surface, name, value string) (Delivery, error) {
	key := sealedhook.IdempotencyKey(sealedhook.IdempotencyParts{
		TargetID: m.Endpoint.id, Fingerprint: m.Endpoint.fingerprint, Route: sc.route, Generation: sc.target.Generation,
		Revision: sc.revision, Op: op, Surface: string(surface), Name: name,
	})
	names := []sealedhook.Name{{Surface: string(surface), Name: name}}
	var values []sealedhook.Value
	if op == sealedhook.OpUpsert {
		values = []sealedhook.Value{{Surface: string(surface), Name: name, Value: value}}
	}
	return m.seal(sc.namespace, sc.route, sc.source, op, names, values, key)
}

// Sync converges the full manifest one effect at a time. Every effect is
// sealed before Prepare, so no plaintext-bearing work happens after the
// provider-write fence is taken except the single POST.
func (m *Module) Sync(ctx context.Context, req adapter.SyncRequest, journal adapter.Journal) (adapter.SyncResult, error) {
	if journal == nil {
		return adapter.SyncResult{}, errors.New("sealed-webhook: durable journal is required")
	}
	if err := m.ready(); err != nil {
		return adapter.SyncResult{}, err
	}
	if err := adapter.ValidateSealedWebhookManifest(req.Target.NamePrefix, req.Manifest, true); err != nil {
		return adapter.SyncResult{}, err
	}
	if req.Source.Revision < 1 || req.Source.OrgID == "" || req.Source.ProjectID == "" || req.Source.EnvironmentID == "" {
		return adapter.SyncResult{}, errors.New("sealed-webhook: sync requires a pinned source revision")
	}
	inspect := adapter.Effect{Surface: adapter.Secret, EffectiveName: "*", Disposition: adapter.Update}
	if err := journal.Gate(ctx, inspect); err != nil {
		return adapter.SyncResult{}, err
	}
	namespace, err := m.verifyDestination(req.Target)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	sc := syncContext{
		namespace: namespace, route: req.Target.ID, target: req.Target, revision: req.Source.Revision,
		source: sealedhook.Source{Org: req.Source.OrgID, Project: req.Source.ProjectID, Environment: req.Source.EnvironmentID, Revision: req.Source.Revision},
	}
	ledger, err := adapter.IndexLedger(req.Ledger)
	if err != nil {
		return adapter.SyncResult{}, err
	}
	desiredRows := adapter.DesiredRows(req.Target.NamePrefix, req.Manifest, !req.Teardown)
	completed := adapter.CompletedNames(req.Completed)
	result := adapter.SyncResult{}
	for _, row := range desiredRows {
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
			if state, err = journal.Reserve(ctx, effect); err != nil {
				return result, err
			}
			record = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: state}
			ledger[key] = record
		}
		delivery, err := m.delivery(sc, sealedhook.OpUpsert, row.Surface, row.EffectiveName, row.Value)
		if err != nil {
			return result, err
		}
		change := adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: effect.Disposition}
		sent, err := m.dispatch(ctx, journal, effect, state, record.Missing, delivery)
		if err != nil {
			return result, err
		}
		completion, outErr := upsertCompletion(sent.ack, sent.err, state, record.Missing, row)
		if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
			return result, finishErr
		}
		if outErr != nil {
			if completion.Conflict {
				result.Conflicts = append(result.Conflicts, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Conflict})
			} else {
				result.Failed = append(result.Failed, change)
			}
			return result, outErr
		}
		ledger[key] = adapter.LedgerEntry{Surface: row.Surface, EffectiveName: row.EffectiveName, State: adapter.Owned}
		result.Changes = append(result.Changes, change)
	}

	reservations, prunes := adapter.Undesired(desiredRows, ledger)
	for _, row := range reservations {
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
		if err := journal.Gate(ctx, effect); err != nil {
			return result, err
		}
		if err := journal.ReleaseReservation(ctx, effect); err != nil {
			return result, err
		}
		result.Changes = append(result.Changes, adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete})
	}
	for _, row := range prunes {
		effect := adapter.Effect{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
		change := adapter.Change{Surface: row.Surface, EffectiveName: row.EffectiveName, Disposition: adapter.Delete}
		delivery, err := m.delivery(sc, sealedhook.OpPrune, row.Surface, row.EffectiveName, "")
		if err != nil {
			return result, err
		}
		sent, err := m.dispatch(ctx, journal, effect, row.State, false, delivery)
		if err != nil {
			return result, err
		}
		completion, outErr := pruneCompletion(sent.ack, sent.err, row)
		if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
			return result, finishErr
		}
		if outErr != nil {
			result.Failed = append(result.Failed, change)
			return result, outErr
		}
		result.Changes = append(result.Changes, change)
	}
	return result, nil
}

// sent is the result of one POST: a verified ack or the delivery error.
type sent struct {
	ack sealedhook.Ack
	err error
}

// dispatch runs the fenced gate/prepare/gate sequence and the POST. A
// returned error means the effect was already finished (or never prepared)
// and Sync must return it as is; the delivery outcome is in sent.
func (m *Module) dispatch(ctx context.Context, journal adapter.Journal, effect adapter.Effect, state adapter.LedgerState, missing bool, delivery Delivery) (sent, error) {
	if err := journal.Gate(ctx, effect); err != nil {
		return sent{}, err
	}
	if err := journal.Prepare(ctx, effect, state); err != nil {
		return sent{}, err
	}
	if gateErr := journal.Gate(ctx, effect); gateErr != nil {
		completion := adapter.Completion{Outcome: adapter.OutcomeFailure, State: state}
		if missing {
			completion.Missing, completion.Finding = true, "owned_missing"
		}
		if finishErr := journal.Finish(ctx, effect, completion); finishErr != nil {
			return sent{}, finishErr
		}
		return sent{}, gateErr
	}
	ack, err := m.API.Deliver(ctx, delivery)
	return sent{ack: ack, err: err}, nil
}

// upsertCompletion maps a delivery result onto the ownership ledger:
//   - verified success: owned;
//   - forged ack: the receiver may have applied it, so ownership is kept as
//     dispatched and the job fails terminally for operator attention;
//   - no valid ack: unknown and dispatched, retried under the same key;
//   - verified conflict: a bare reservation is released with a conflict
//     artifact; a claimed name keeps its state;
//   - verified rejection: failure, reservation released.
func upsertCompletion(ack sealedhook.Ack, err error, state adapter.LedgerState, missing bool, row adapter.DesiredRow) (adapter.Completion, error) {
	withMissing := func(c adapter.Completion) adapter.Completion {
		if missing && !c.ReleaseLedger && (c.State == adapter.Owned || c.State == adapter.Dispatched) {
			c.Missing, c.Finding = true, "owned_missing"
		}
		return c
	}
	claimedState := state
	if claimedState == adapter.Reserved {
		claimedState = adapter.Dispatched
	}
	switch {
	case errors.Is(err, adapter.ErrAckForged):
		return withMissing(adapter.Completion{Outcome: adapter.OutcomeFailure, State: claimedState, ProviderStatus: 200}), err
	case err != nil:
		return withMissing(adapter.Completion{Outcome: adapter.OutcomeUnknown, State: claimedState}),
			fmt.Errorf("%w: %s %s: %w", adapter.ErrIndeterminate, row.Surface, row.EffectiveName, err)
	}
	outErr := ackError(ack, string(row.Surface)+" "+row.EffectiveName)
	if outErr == nil {
		return adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Owned, ProviderStatus: 200}, nil
	}
	completion := adapter.Completion{Outcome: adapter.OutcomeFailure, ProviderStatus: 200, Conflict: ack.Status == sealedhook.AckConflict}
	if state == adapter.Reserved {
		completion.ReleaseLedger = true
	} else {
		completion.State = state
	}
	return withMissing(completion), outErr
}

func pruneCompletion(ack sealedhook.Ack, err error, row adapter.LedgerEntry) (adapter.Completion, error) {
	switch {
	case errors.Is(err, adapter.ErrAckForged):
		return adapter.Completion{Outcome: adapter.OutcomeFailure, State: row.State, ProviderStatus: 200}, err
	case err != nil:
		return adapter.Completion{Outcome: adapter.OutcomeUnknown, State: row.State},
			fmt.Errorf("%w: prune %s %s: %w", adapter.ErrIndeterminate, row.Surface, row.EffectiveName, err)
	}
	if outErr := ackError(ack, "prune "+string(row.Surface)+" "+row.EffectiveName); outErr != nil {
		return adapter.Completion{Outcome: adapter.OutcomeFailure, State: row.State, ProviderStatus: 200, Conflict: ack.Status == sealedhook.AckConflict}, outErr
	}
	return adapter.Completion{Outcome: adapter.OutcomeSuccess, State: adapter.Released, ProviderStatus: 200}, nil
}
