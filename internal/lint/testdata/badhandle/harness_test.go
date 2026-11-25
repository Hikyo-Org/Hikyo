package badhandle

import (
	"context"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// A test file outside the reviewed harness inventory (driverFiles) gets no
// ambient authority: its raw handle is reported like a production file's.
func TestRawHandleOutsideTheInventory(t *testing.T) {
	var db *store.DB
	_, _ = db.SQLiteRead().ExecContext(context.Background(), "DELETE FROM grants") // want "calls store\\.DB\\.SQLiteRead" "names driver type database/sql\\.DB"
}
