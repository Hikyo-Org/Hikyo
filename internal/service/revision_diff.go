package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

type RevisionDiffRow struct {
	KeyID, Name, Classification, Status string
	Revealed                            bool
	Before, After                       *string
}
type RevisionDiff struct {
	LeftRevision, RightRevision int64
	Items                       []RevisionDiffRow
}

// Diff reads one transaction's two retained snapshots. keyID selects the only
// key whose plaintext may be disclosed; empty means config-only comparison.
// Secret equality is never computed until authorization and ceremony complete.
func (s *Revisions) Diff(ctx context.Context, actor Actor, scope domain.Scope, leftRevision, rightRevision int64, keyID string) (RevisionDiff, error) {
	if leftRevision < 1 || rightRevision < 1 {
		return RevisionDiff{}, fmt.Errorf("%w: diff names two positive revisions", domain.ErrInvalid)
	}
	release, err := s.Budget.acquire(budgetValuesExport, budgetKeys{Org: scope.Org})
	if err != nil {
		return RevisionDiff{}, err
	}
	defer release()
	var rateCharged bool
	sealer, err := sealerFor(ctx, s.DB, s.Keyring, actor, authz.OpValueExport, scope)
	if err != nil {
		return RevisionDiff{}, err
	}
	return tx.WriteResult(ctx, s.DB, func(ctx context.Context, r store.Repos, az *authz.TxAuthorizer) (RevisionDiff, error) {
		caller, p, err := authorize(ctx, az, actor, authz.OpValueExport, scope, s.now())
		if err != nil {
			return RevisionDiff{}, err
		}
		if err := s.Budget.chargeOnce(&rateCharged, budgetExportRate, budgetKeys{Principal: caller.Principal}); err != nil {
			return RevisionDiff{}, err
		}
		left, err := readSnapshot(ctx, r.Snapshots(), p, leftRevision)
		if err != nil {
			return RevisionDiff{}, err
		}
		right, err := readSnapshot(ctx, r.Snapshots(), p, rightRevision)
		if err != nil {
			return RevisionDiff{}, err
		}
		latest, err := r.Snapshots().Latest(ctx, p)
		if err != nil {
			return RevisionDiff{}, err
		}
		sides := []store.Snapshot{left, right}
		entries := [2]map[string]store.SnapshotEntry{{}, {}}
		ids := map[string]bool{}
		for side, snapshot := range sides {
			rows, err := r.Snapshots().Entries(ctx, p, snapshot)
			if err != nil {
				return RevisionDiff{}, err
			}
			for _, entry := range rows {
				entries[side][entry.KeyID] = entry
				ids[entry.KeyID] = true
			}
		}
		if keyID != "" && !ids[keyID] {
			return RevisionDiff{}, domain.ErrNotFound
		}
		result := RevisionDiff{LeftRevision: leftRevision, RightRevision: rightRevision, Items: []RevisionDiffRow{}}
		for id := range ids {
			if keyID != "" && id != keyID {
				continue
			}
			l, hasLeft := entries[0][id]
			rr, hasRight := entries[1][id]
			name := rr.KeyName
			if !hasRight {
				name = l.KeyName
			}
			secret := l.Classification == string(schema.Secret) || rr.Classification == string(schema.Secret)
			row := RevisionDiffRow{KeyID: id, Name: name, Classification: string(schema.Config)}
			if secret {
				row.Classification = string(schema.Secret)
			}
			disclose := keyID != "" && secret
			proofs := [2]authz.Proof{p, p}
			if disclose {
				for side, snapshot := range sides {
					if _, present := entries[side][id]; !present {
						continue
					}
					op := authz.OpValueExportReveal
					if snapshot.Revision != latest.Revision {
						op = authz.OpValueExportRevealHistory
					}
					proofs[side], err = az.Authorize(ctx, caller, op, scope)
					if err != nil {
						return RevisionDiff{}, err
					}
				}
				gate := ceremonyGate(ctx, s.Auth, az, caller, disclosureIntent(PurposeReveal, string(scope.Env)))
				if err := gate([]string{id}); err != nil {
					return RevisionDiff{}, err
				}
			}
			switch {
			case !hasLeft:
				row.Status = "added"
			case !hasRight:
				row.Status = "removed"
			case l.ValueEntryID != rr.ValueEntryID:
				row.Status = "edited"
			default:
				row.Status = "not_edited"
			}
			if !secret || disclose {
				row.Revealed = true
				for side, snapshot := range sides {
					entry, present := entries[side][id]
					if !present {
						continue
					}
					plain, err := sealer.OpenField(snapshotAAD(entry.OrgID, entry.ProjectID, entry.EnvironmentID, entry.KeyID, entry.SnapshotID, entry.ID), entry.Ciphertext)
					if err != nil {
						return RevisionDiff{}, fmt.Errorf("service: snapshot entry %s: %w", entry.ID, err)
					}
					value := string(plain)
					crypto.Zero(plain)
					if side == 0 {
						row.Before = &value
					} else {
						row.After = &value
					}
					if disclose {
						ev, err := domainEvent(ctx, audit.EventValueRevealed, caller.Principal, audit.Object{Type: "key", ID: id}, audit.Payload{"key_id": id, "name": audit.SanitizeFreeText(name), "surface": "revision-diff", "revision": snapshot.Revision})
						if err != nil {
							return RevisionDiff{}, err
						}
						if err := r.Audit().InsertTenant(ctx, proofs[side], ev); err != nil {
							return RevisionDiff{}, err
						}
					}
				}
				if hasLeft && hasRight {
					row.Status = "changed"
					if *row.Before == *row.After {
						row.Status = "unchanged"
					}
				}
			}
			result.Items = append(result.Items, row)
		}
		slices.SortFunc(result.Items, func(a, b RevisionDiffRow) int {
			if a.Name < b.Name {
				return -1
			}
			if a.Name > b.Name {
				return 1
			}
			return 0
		})
		return result, nil
	})
}
