//line /owned/internal/isolation/oauth2_e2e_test.go:1
package badline

import (
	"context"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// This production file claims, through the directive above, a path that is in
// the reviewed harness inventory. The raw handle is reported, but this fixture
// cannot prove the defence: its package base never matches the claimed path's
// package, so it would be refused anyway. The unit test
// TestInventoriedPathClaimedByLineDirectiveIsRefused proves that authority
// follows the file on disk, never a //line claim.
func rawRead(ctx context.Context, db *store.DB) error {
	_, err := db.SQLiteRead().ExecContext(ctx, "DELETE FROM grants")
	return err
}
