package app

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

// The inspection of a row the target is still writing must answer, whatever binary asks:
// an error reads as an unreadable row, and a container target then stays pinned for a person.
func TestTargetOwnsControlRowOnlyBetweenFirstWriteAndFinalGate(t *testing.T) {
	for phase, want := range map[upgrade.Phase]bool{
		upgrade.Prepared: true, upgrade.SchemaWriteStarted: true, upgrade.SchemaApplied: true, upgrade.RestoreRequired: true,
		upgrade.Healthy: false, upgrade.BackupPreparing: false,
	} {
		if got := targetOwnsControlRow(phase); got != want {
			t.Errorf("%s: got %v", phase, got)
		}
	}
}
