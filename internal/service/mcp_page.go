package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// This file holds the bounded keyset page reads the MCP adapter maps its five
// tools onto (#629). Each mirrors its unbounded sibling exactly -- same
// authorization operation, same transaction, same in-transaction identity
// resolution -- and differs only by pushing the cursor and limit into the store
// query, so no whole collection is materialized to slice a limit afterwards.
// The transport owns the opaque cursor; a method here takes only the decoded
// keyset position and the page size.

// ListPage is the bounded keyset read behind hikyo_list_environments. The
// stable order matches List exactly: display order then UNIQUE environment
// name. afterDisplayOrder is -1 and afterName is "" for the first page.
func (s *Environments) ListPage(ctx context.Context, actor Actor, scope domain.Scope, afterDisplayOrder int64, afterName string, limit int) ([]Environment, error) {
	var rows []store.Environment
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, p, err := authorize(ctx, az, actor, authz.OpEnvList, scope, time.Now().UTC())
		if err != nil {
			return err
		}
		rows, err = r.Environments().ListPage(ctx, p, afterDisplayOrder, afterName, limit)
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]Environment, 0, len(rows))
	for _, row := range rows {
		out = append(out, environmentOf(row))
	}
	return out, nil
}

// ListPage is the bounded keyset read behind hikyo_list_definitions. The stable
// order is the UNIQUE key name; afterName is "" for the first page. Presence is
// resolved per page key, never by listing the project's presence rows. The
// schema revision rides along exactly as the unbounded List returns it.
func (s *Keys) ListPage(ctx context.Context, actor Actor, scope domain.Scope, afterName string, limit int) ([]Key, int64, error) {
	var out []Key
	var revision int64
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, p, err := authorize(ctx, az, actor, authz.OpKeyList, scope, time.Now().UTC())
		if err != nil {
			return err
		}
		rows, err := r.Catalogue().ListPage(ctx, p, afterName, limit)
		if err != nil {
			return err
		}
		revision, err = r.Catalogue().SchemaRevision(ctx, p)
		if err != nil {
			return err
		}
		out = make([]Key, 0, len(rows))
		for _, row := range rows {
			presence, err := r.Catalogue().PresenceForKey(ctx, p, row.ID)
			if err != nil {
				return err
			}
			key, err := keyOf(row, presence)
			if err != nil {
				return err
			}
			out = append(out, key)
		}
		return nil
	})
	return out, revision, err
}

// ListPage is the bounded keyset read behind hikyo_inspect_configuration. It
// pages the catalogue by the UNIQUE key name and resolves each page key's cell
// with a point read; reveal is always false, so `config` plaintext appears and
// a `secret` cell carries only its classification and set/absent presence.
func (s *Values) ListPage(ctx context.Context, actor Actor, scope domain.Scope, afterName string, limit int) ([]ValueCell, error) {
	sealer, err := sealerFor(ctx, s.DB, s.Keyring, actor, authz.OpValueList, scope)
	if err != nil {
		return nil, err
	}
	var out []ValueCell
	err = tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		_, p, err := authorize(ctx, az, actor, authz.OpValueList, scope, time.Now().UTC())
		if err != nil {
			return err
		}
		keys, err := r.Catalogue().ListPage(ctx, p, afterName, limit)
		if err != nil {
			return err
		}
		out = make([]ValueCell, 0, len(keys))
		for _, key := range keys {
			cell := ValueCell{KeyID: key.ID, Name: key.Name, Classification: key.Classification}
			entry, err := r.Values().Get(ctx, p, key.ID)
			switch {
			case errors.Is(err, domain.ErrNotFound):
				// `absent`: not an error, not a fallback.
			case err != nil:
				return err
			default:
				cell.Set = true
				cell.UpdatedAt = entry.UpdatedAt
				cell.UpdatedBy = entry.UpdatedBy
				// `config` plaintext rides `read`; `secret` plaintext is never
				// opened on this reveal-false path.
				if key.Classification == string(schema.Config) {
					plain, err := openCell(sealer, entry)
					if err != nil {
						return err
					}
					cell.Value = plain
					cell.Revealed = true
				}
			}
			out = append(out, cell)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PendingDraftsPage is the bounded keyset read behind hikyo_list_pending_changes.
// The owner is the caller resolved inside the transaction, and the stable order
// is the UNIQUE draft key_id; afterKeyID is "" for the first page. `config`
// draft plaintext may appear; a `secret` or unset draft never carries material.
func (s *Revisions) PendingDraftsPage(ctx context.Context, actor Actor, scope domain.Scope, afterKeyID string, limit int) ([]PendingDraft, error) {
	sealer, err := sealerFor(ctx, s.DB, s.Keyring, actor, authz.OpValuePendingList, scope)
	if err != nil {
		return nil, err
	}
	var out []PendingDraft
	err = tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		out = nil
		caller, p, err := authorize(ctx, az, actor, authz.OpValuePendingList, scope, s.now())
		if err != nil {
			return err
		}
		changes, err := r.Pending().ListForOwnerInEnvironmentPage(ctx, p, string(caller.Principal), afterKeyID, limit)
		if err != nil {
			return err
		}
		out = make([]PendingDraft, 0, len(changes))
		for _, change := range changes {
			key, err := r.Catalogue().GetInProject(ctx, p, change.KeyID)
			if err != nil {
				return fmt.Errorf("service: pending change %s references key %s: %w", change.ID, change.KeyID, err)
			}
			presence, err := r.Catalogue().PresenceForKey(ctx, p, change.KeyID)
			if err != nil {
				return err
			}
			draft, err := pendingDraftView(change, key, presence, sealer)
			if err != nil {
				return err
			}

			out = append(out, draft)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// HistoryPage is the bounded keyset read behind hikyo_list_revisions. The stable
// order is the UNIQUE revision, descending; beforeRevision is a sentinel above
// the newest revision for the first page. It returns revision metadata only, no
// historical values and no change token.
func (s *Revisions) HistoryPage(ctx context.Context, actor Actor, scope domain.Scope, beforeRevision int64, limit int) ([]RevisionView, error) {
	var out []RevisionView
	err := tx.Read(ctx, s.DB, func(ctx context.Context, r store.ReadRepos, az *authz.TxAuthorizer) error {
		out = nil
		_, p, err := authorize(ctx, az, actor, authz.OpRevisionList, scope, s.now())
		if err != nil {
			return err
		}
		snapshots, err := r.Snapshots().ListPage(ctx, p, beforeRevision, limit)
		if err != nil {
			return err
		}
		for _, snapshot := range snapshots {
			changes, err := r.Snapshots().Changes(ctx, p, snapshot.Revision)
			if err != nil {
				return err
			}
			name, err := revisionPublisherName(ctx, az, snapshot.PublishedBy)
			if err != nil {
				return err
			}
			out = append(out, RevisionView{
				Revision: snapshot.Revision, SchemaRevision: snapshot.SchemaRevision,
				PublishedBy: snapshot.PublishedBy, PublishedByName: name, PublishedAt: snapshot.PublishedAt,
				ChangedKeys:    changedKeys(changes),
				PayloadPresent: snapshot.PayloadPresent(), CollectedPolicy: snapshot.CollectionPolicy(),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
