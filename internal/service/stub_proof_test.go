package service

import (
	"context"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
)

// realProof authorizes the adapter fixture's principal against the fixture
// database and returns the proof. It is for tests whose stub repositories
// never read the proof; tests of real authorization mint proofs in their own
// transaction. The returned proof belongs to an already-committed transaction;
// the stub repositories in these tests never verify it. Take one proof per test
// and reuse it: each call opens a new fixture database.
func realProof(t *testing.T) authz.Proof {
	t.Helper()
	db := adapterServiceDB(t)
	var proof authz.Proof
	// WriteResult would return the proof out of its transaction, which txresult
	// refuses; the stub repositories only pass it along.
	err := storetx.Write(t.Context(), db, func(ctx context.Context, _ store.Repos, az *authz.TxAuthorizer) (err error) {
		_, proof, err = authorize(ctx, az, LocalPrincipal("usr_adapter"), authz.OpAdapterConfigure, adapterScope, time.Now())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return proof
}
