package isolation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// Repeated snapshot inserts must rebind the complete tenant chain and retain
// per-call proof checks, even after the transaction has prepared their SQL.
func TestSnapshotInsertsRetainProofBindingAndRollback(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		rollback := errors.New("rollback populated snapshot transaction")
		var expired authz.Proof
		write := func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
			for i, scope := range []domain.Scope{scopeEnv(orgA, prjA1, envA1), scopeEnv(orgA, prjA2, envA2)} {
				proof, err := az.Authorize(ctx, authz.Identity{Principal: custodian}, authz.OpValuePublish, scope)
				if err != nil {
					return err
				}
				expired = proof
				id := fmt.Sprintf("snp_stmt_%d", i)
				if err := repos.Snapshots().Insert(ctx, proof, store.NewSnapshot{ID: id, Revision: 20, SchemaRevision: 1, PublishedBy: string(custodian), PublishedAt: time.Now().UTC()}); err != nil {
					return err
				}
				key := []string{keyA1, keyA2}[i]
				entry := store.NewSnapshotEntry{ID: fmt.Sprintf("sne_stmt_%d", i), SnapshotID: id, KeyID: key, KeyName: "SHARED_KEY", Classification: "config", Ciphertext: []byte{byte(i + 1)}, ValueEntryID: fmt.Sprintf("val_stmt_%d", i)}
				if err := repos.Snapshots().InsertEntry(ctx, proof, entry); err != nil {
					return err
				}
				if err := repos.Snapshots().InsertChange(ctx, proof, 20, key, "SHARED_KEY", store.RevisionChangeAdded); err != nil {
					return err
				}
				// The statements now exist. A proof for a read operation and an
				// absent proof must still fail before SQL executes on that path.
				read, err := az.Authorize(ctx, authz.Identity{Principal: custodian}, authz.OpEnvRead, scope)
				if err != nil {
					return err
				}
				for _, denied := range []authz.Proof{nil, read} {
					if err := repos.Snapshots().InsertEntry(ctx, denied, entry); err == nil || !strings.HasPrefix(err.Error(), "authz:") {
						t.Fatalf("snapshot entry proof refusal = %v", err)
					}
					if err := repos.Snapshots().InsertChange(ctx, denied, 21, key, "SHARED_KEY", store.RevisionChangeAdded); err == nil || !strings.HasPrefix(err.Error(), "authz:") {
						t.Fatalf("lineage proof refusal = %v", err)
					}
				}
			}
			return nil
		}
		err := tx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
			if err := write(ctx, repos, az); err != nil {
				return err
			}
			return rollback
		})
		if !errors.Is(err, rollback) {
			t.Fatalf("populated rollback: %v", err)
		}
		for _, query := range []string{
			`SELECT COUNT(*) FROM snapshots WHERE id IN ('snp_stmt_0','snp_stmt_1')`,
			`SELECT COUNT(*) FROM snapshot_entries WHERE id IN ('sne_stmt_0','sne_stmt_1')`,
			`SELECT COUNT(*) FROM revision_key_changes WHERE revision=20`,
		} {
			if n := queryInt(t, db, query); n != 0 {
				t.Fatalf("rollback retained %d rows", n)
			}
		}
		if err := tx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
			if err := repos.Snapshots().InsertChange(ctx, expired, 21, keyA2, "SHARED_KEY", store.RevisionChangeAdded); err == nil || !strings.HasPrefix(err.Error(), "authz:") {
				t.Fatalf("expired proof refusal = %v", err)
			}
			return write(ctx, repos, az)
		}); err != nil {
			t.Fatal(err)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM snapshot_entries WHERE (id='sne_stmt_0' AND org_id='org_a' AND project_id='prj_a1' AND environment_id='env_a1' AND key_id='key_a1') OR (id='sne_stmt_1' AND org_id='org_a' AND project_id='prj_a2' AND environment_id='env_a2' AND key_id='key_a2')`); n != 2 {
			t.Fatalf("rebound snapshot scope count=%d", n)
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM revision_key_changes WHERE revision=20 AND ((project_id='prj_a1' AND environment_id='env_a1' AND key_id='key_a1') OR (project_id='prj_a2' AND environment_id='env_a2' AND key_id='key_a2'))`); n != 2 {
			t.Fatalf("rebound lineage scope count=%d", n)
		}
	})
}
