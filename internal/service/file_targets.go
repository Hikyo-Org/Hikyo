package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Generic file synchronization (#164), the server half.
//
// A file target is a PULL-class destination: it binds one environment, one
// key selection (resolved to immutable ids with the #157 selection rules) and
// one workload service account. It deliberately stores no path, format, mode
// or owner; those stay in the client's local config, so the server never
// receives or writes a host path. There is no outbox job, ledger, provider
// lease or credential: the bound account fetches through the normal delivery
// surface with `target=<id>`, and that fetch is the only way values leave.
//
// The binding is what makes the identity target-scoped. A bound account's
// delivery is narrowed to the selection (and must name the target); it cannot
// fall back to the environment-wide delivery its `read` grant would otherwise
// allow. Removing the binding would restore that wider delivery, so delete
// refuses while the account can still authenticate: revoke its credentials,
// or delete the account (which cascades to the target) instead.

// FileTarget intentionally shares a store-owned type with transport mapping.
// This sanctioned seam follows the system-architecture ADR; boundary_test.go
// enforces that handlers never import internal/store directly.
type FileTarget = store.FileTarget

// FileTargetKey is the store's selected-key pair, shared the same way.
type FileTargetKey = store.FileTargetKey

// FileTargetReport is the store's report row, shared the same way.
type FileTargetReport = store.FileTargetReport

// FileTargetInput creates a file target.
type FileTargetInput struct {
	EnvironmentID    string
	Name             string
	ServiceAccountID string
	KeyIDs           []string
	KeySelection     AdapterKeySelection
}

// File-target report states: the closed vocabulary a client may assert.
const (
	FileTargetApplied = "applied"
	FileTargetCurrent = "current"
	FileTargetOffline = "offline"
	FileTargetRefused = "refused"
	FileTargetFailed  = "failed"
)

// fileTargetReportSkew is how far a client clock may run ahead of the server
// (the condition-reporting ADR's bound).
const fileTargetReportSkew = 5 * time.Minute

var fileTargetNameGrammar = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// FileTargets owns file-target configuration.
type FileTargets struct {
	DB  *store.DB
	Now func() time.Time
}

func (s *FileTargets) now() time.Time { return nowOr(s.Now) }

// Create binds a workload service account to an environment and key set.
func (s *FileTargets) Create(ctx context.Context, actor Actor, scope domain.Scope, in FileTargetInput) (FileTarget, error) {
	if err := requireProjectScope(scope, "file target create requires project scope, environment and service account", in.EnvironmentID, in.ServiceAccountID); err != nil {
		return FileTarget{}, err
	}
	if !fileTargetNameGrammar.MatchString(in.Name) {
		return FileTarget{}, invalidDetail("file target name %q must match ^[a-z][a-z0-9-]{0,62}$", in.Name)
	}
	id, err := newID("ftg")
	if err != nil {
		return FileTarget{}, err
	}
	var out FileTarget
	err = tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, p, err := authorize(ctx, az, actor, authz.OpFileTargetCreate, scope, now)
		if err != nil {
			return err
		}
		keyIDs, err := resolveFileTargetKeys(ctx, r, p, in.KeyIDs, in.KeySelection)
		if err != nil {
			return err
		}
		sa, err := az.ServiceAccountAt(ctx, scope, in.ServiceAccountID)
		if err != nil {
			return err
		}
		if sa.Kind != domain.ClassWorkload {
			return invalidDetail("service account %s is a %s account; a file target binds a workload account", sa.ID, sa.Kind)
		}
		row := store.FileTarget{
			ID: id, EnvironmentID: in.EnvironmentID, Name: in.Name,
			ServiceAccountID: sa.ID, PrincipalID: string(sa.PrincipalID),
			AuthorityPrincipalID: string(caller.Principal), CreatedAt: now,
		}
		if err := r.FileTargets().Create(ctx, p, row, keyIDs); err != nil {
			if errors.Is(err, domain.ErrConflict) {
				return &detailErr{
					detail: "the service account is already bound to a file target, or the name is taken in this environment",
					err:    err,
				}
			}
			return err
		}
		if out, err = loadFileTarget(ctx, r, p, id); err != nil {
			return err
		}
		return insertFileTargetConfigured(ctx, r, p, caller.Principal, "create", out)
	})
	return out, err
}

// UpdateKeys replaces the key selection under a generation compare-and-swap.
func (s *FileTargets) UpdateKeys(ctx context.Context, actor Actor, scope domain.Scope, id string, expectedGeneration int64, keyIDs []string, selection AdapterKeySelection) (FileTarget, error) {
	if err := requireProjectScope(scope, "file target update requires project scope and target", id); err != nil {
		return FileTarget{}, err
	}
	if expectedGeneration <= 0 {
		return FileTarget{}, invalidDetail("file target update requires the expected generation")
	}
	var out FileTarget
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, p, err := authorize(ctx, az, actor, authz.OpFileTargetUpdate, scope, now)
		if err != nil {
			return err
		}
		current, err := r.FileTargets().Get(ctx, p, id)
		if err != nil {
			return err
		}
		resolved, err := resolveFileTargetKeys(ctx, r, p, keyIDs, selection)
		if err != nil {
			return err
		}
		ok, err := r.FileTargets().ReplaceKeys(ctx, p, current, expectedGeneration, resolved, string(caller.Principal), now)
		if err != nil {
			return err
		}
		if !ok {
			return &detailErr{
				detail: fmt.Sprintf("file target %s is at generation %d, not %d; re-read it and retry", id, current.Generation, expectedGeneration),
				err:    fmt.Errorf("%w: stale file target generation", domain.ErrConflict),
			}
		}
		if out, err = loadFileTarget(ctx, r, p, id); err != nil {
			return err
		}
		return insertFileTargetConfigured(ctx, r, p, caller.Principal, "update", out)
	})
	return out, err
}

// Delete removes a file target. It refuses while the bound service account
// can still authenticate, because removing the binding would widen that
// account's delivery back to the whole environment.
func (s *FileTargets) Delete(ctx context.Context, actor Actor, scope domain.Scope, id string) error {
	if err := requireProjectScope(scope, "file target delete requires project scope and target", id); err != nil {
		return err
	}
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, p, err := authorize(ctx, az, actor, authz.OpFileTargetDelete, scope, now)
		if err != nil {
			return err
		}
		current, err := r.FileTargets().Get(ctx, p, id)
		if err != nil {
			return err
		}
		epoch, err := az.CredentialEpoch(ctx)
		if err != nil {
			return err
		}
		live, err := az.LiveMachineCredentialCount(ctx, current.ServiceAccountID, epoch, now)
		if err != nil {
			return err
		}
		if live > 0 {
			detail := fmt.Sprintf("service account %s still holds %d live credential(s); removing the target would widen its delivery to the whole environment. "+
				"Revoke its credentials first, or delete the service account (which removes this target with it)", current.ServiceAccountID, live)
			return &detailErr{detail: detail, err: fmt.Errorf("%w: %s", domain.ErrConflict, detail)}
		}
		current.Keys, err = r.FileTargets().Keys(ctx, p, id)
		if err != nil {
			return err
		}
		deleted, err := r.FileTargets().Delete(ctx, p, id)
		if err != nil {
			return err
		}
		if !deleted {
			return domain.ErrNotFound
		}
		return insertFileTargetConfigured(ctx, r, p, caller.Principal, "delete", current)
	})
}

// Get reads one file target with its resolved key selection.
func (s *FileTargets) Get(ctx context.Context, actor Actor, scope domain.Scope, id string) (FileTarget, error) {
	if err := requireProjectScope(scope, "file target show requires project scope and target", id); err != nil {
		return FileTarget{}, err
	}
	var out FileTarget
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpFileTargetInspect, scope, s.now())
		if err != nil {
			return err
		}
		if out, err = loadFileTarget(ctx, r, p, id); err != nil {
			return err
		}
		return insertInspected(ctx, r, p, caller.Principal, audit.EventFileTargetInspected, audit.Object{Type: "file-target", ID: id}, 1)
	})
	return out, err
}

// List reads every file target of the project.
func (s *FileTargets) List(ctx context.Context, actor Actor, scope domain.Scope) ([]FileTarget, error) {
	if err := requireProjectScope(scope, "file target list requires project scope"); err != nil {
		return nil, err
	}
	var out []FileTarget
	err := tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		caller, p, err := authorize(ctx, az, actor, authz.OpFileTargetInspect, scope, s.now())
		if err != nil {
			return err
		}
		if out, err = r.FileTargets().List(ctx, p); err != nil {
			return err
		}
		ids := make([]string, len(out))
		for i := range out {
			ids[i] = out[i].ID
		}
		selections, err := r.FileTargets().KeysForTargets(ctx, p, ids)
		if err != nil {
			return err
		}
		for i := range out {
			out[i].Keys = selections[out[i].ID]
		}
		return insertInspected(ctx, r, p, caller.Principal, audit.EventFileTargetInspected, audit.Object{Type: "file-target-list", ID: string(scope.Project)}, int64(len(out)))
	})
	return out, err
}

func loadFileTarget(ctx context.Context, r store.Repos, p authz.Proof, id string) (FileTarget, error) {
	out, err := r.FileTargets().Get(ctx, p, id)
	if err != nil {
		return FileTarget{}, err
	}
	out.Keys, err = r.FileTargets().Keys(ctx, p, id)
	return out, err
}

// resolveFileTargetKeys applies the #157 selection rules: explicit ids and
// names are the operator's list, patterns and classification sweep the
// catalogue, and an empty result is refused.
func resolveFileTargetKeys(ctx context.Context, r store.Repos, p authz.Proof, explicit []string, selection AdapterKeySelection) ([]string, error) {
	catalogue, err := r.Catalogue().List(ctx, p)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(catalogue))
	for _, k := range catalogue {
		known[k.ID] = true
	}
	for _, id := range explicit {
		if !known[id] {
			return nil, invalidDetail("key %s does not exist in this project", id)
		}
	}
	return resolveKeySelection(catalogue, explicit, selection)
}

func insertFileTargetConfigured(ctx context.Context, r store.Repos, p authz.Proof, principal domain.PrincipalID, mutation string, t FileTarget) error {
	ev, err := domainEvent(ctx, audit.EventFileTargetConfigured, principal, audit.Object{Type: "file-target", ID: t.ID}, audit.Payload{
		"mutation": mutation, "environment_id": t.EnvironmentID, "service_account_id": t.ServiceAccountID,
		"generation": t.Generation, "key_count": len(t.Keys),
	})
	if err != nil {
		return err
	}
	return r.Audit().InsertTenant(ctx, p, ev)
}

// fileTargetSelection is the delivery-side half of the binding. It returns the
// key-id set a fetch is narrowed to and the target's generation, or nil and
// zero for an unbound caller.
func fileTargetSelection(ctx context.Context, r store.Repos, p authz.Proof, principal domain.PrincipalID, scope domain.Scope, requested string) (map[string]bool, int64, error) {
	bound, err := r.FileTargets().ForPrincipal(ctx, p, string(principal))
	switch {
	case errors.Is(err, store.ErrNotFound):
		if requested != "" {
			// Naming a target this caller is not bound to is the uniform
			// nonexistent answer: it discloses nothing about the target.
			return nil, 0, domain.ErrNotFound
		}
		return nil, 0, nil
	case err != nil:
		return nil, 0, err
	}
	if requested == "" {
		return nil, 0, invalidDetail("this service account is bound to file target %s; fetch with target=%s", bound.ID, bound.ID)
	}
	if requested != bound.ID || bound.EnvironmentID != string(scope.Env) {
		return nil, 0, domain.ErrNotFound
	}
	keys, err := r.FileTargets().Keys(ctx, p, bound.ID)
	if err != nil {
		return nil, 0, err
	}
	only := make(map[string]bool, len(keys))
	for _, k := range keys {
		only[k.KeyID] = true
	}
	return only, bound.Generation, nil
}

// ReportFileTarget authenticates the presented artifact and records one
// file-target report.
func (s *Delivery) ReportFileTarget(ctx context.Context, presented string, scope domain.Scope, targetID string, report FileTargetReport) error {
	actor, err := s.callerActor(ctx, presented)
	if err != nil {
		return err
	}
	return s.ReportFileTargetAs(ctx, actor, scope, targetID, report)
}

// ReportFileTargetAs records the bound client's value-free report: the state
// it reached, the revision and target generation it rendered, and its keyed
// generation stamp. Only the bound principal may report, and only in the
// target's environment; anything else is the uniform nonexistent answer.
func (s *Delivery) ReportFileTargetAs(ctx context.Context, actor Actor, scope domain.Scope, targetID string, report FileTargetReport) error {
	if scope.Env == "" || targetID == "" {
		return invalidDetail("a file-target report requires an environment and a target")
	}
	switch report.State {
	case FileTargetApplied, FileTargetCurrent, FileTargetOffline, FileTargetRefused, FileTargetFailed:
	default:
		return invalidDetail("report state must be applied, current, offline, refused or failed")
	}
	if report.Revision < 0 || report.Generation < 1 {
		return invalidDetail("report revision must be non-negative and generation positive")
	}
	if report.Stamp != "" {
		if err := crypto.ParseStamp(report.Stamp); err != nil {
			return invalidDetail("report stamp must be v1-<32 lowercase hex>")
		}
	}
	if report.ReportedAt.IsZero() {
		return invalidDetail("report reported_at is required")
	}
	report.ReportedAt = store.CanonTime(report.ReportedAt)
	var charged bool
	return tx.Write(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) error {
		now := s.now()
		caller, p, err := authorize(ctx, az, actor, authz.OpFileTargetReport, scope, now)
		if err != nil {
			return err
		}
		if err := s.Budget.chargeOnce(&charged, budgetDeliveryTarget, budgetKeys{Principal: caller.Principal, Org: scope.Org}); err != nil {
			return err
		}
		if report.ReportedAt.After(now.Add(fileTargetReportSkew)) {
			return invalidDetail("report reported_at is more than %s in the future", fileTargetReportSkew)
		}
		current, err := r.FileTargets().Get(ctx, p, targetID)
		if err != nil {
			return err
		}
		if current.PrincipalID != string(caller.Principal) || current.EnvironmentID != string(scope.Env) {
			return domain.ErrNotFound
		}
		if prev := current.Report; prev != nil && report.ReportedAt.Before(prev.ReportedAt) {
			return &detailErr{detail: "report reported_at is older than the last accepted report", err: fmt.Errorf("%w: out-of-order file-target report", domain.ErrConflict)}
		}
		report.ReceivedAt = now
		ok, err := r.FileTargets().RecordReport(ctx, p, targetID, string(caller.Principal), report)
		if err != nil {
			return err
		}
		if !ok {
			return domain.ErrNotFound
		}
		prev := current.Report
		if prev != nil && prev.State == report.State && prev.Revision == report.Revision &&
			prev.Stamp == report.Stamp && prev.Generation == report.Generation {
			return nil // a heartbeat: no event (condition-reporting ADR D8)
		}
		payload := audit.Payload{"state": report.State, "revision": report.Revision, "generation": report.Generation}
		if report.Stamp != "" {
			payload["stamp"] = report.Stamp
		}
		ev, err := domainEvent(ctx, audit.EventFileTargetApplied, caller.Principal, audit.Object{Type: "file-target", ID: targetID}, payload)
		if err != nil {
			return err
		}
		return r.Audit().InsertTenant(ctx, p, ev)
	})
}
