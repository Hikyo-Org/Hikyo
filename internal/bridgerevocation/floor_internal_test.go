package bridgerevocation

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// ensureFloor decides to raise the floor before it takes the lock, and a concurrent accepted
// list may raise it higher in between. Raising under the lock must then keep the higher
// floor: the floor never goes down within a trust root.
func TestRaiseFloorNeverLowersAFloorRaisedMeanwhile(t *testing.T) {
	snapshot := testfixture.New(t).Snapshot(t)
	checker := Checker{Directory: t.TempDir()}
	high := releaseidentity.Hash([]byte("list nine"))
	low := releaseidentity.Hash([]byte("list five"))
	if err := checker.raiseFloor(snapshot, 9, high); err != nil {
		t.Fatal(err)
	}
	if err := checker.raiseFloor(snapshot, 5, low); err != nil {
		t.Fatal(err)
	}
	floor, found, err := checker.readFloor()
	if err != nil || !found || floor.Sequence != 9 || floor.Digest != high {
		t.Fatalf("a lower raise must leave the floor at 9: %+v %v %v", floor, found, err)
	}
}
