package bridgerevocation

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
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
	// The file is decoded here, not through the package's reader, so whole-file overlays of an
	// earlier bridgerevocation.go still build against this test.
	raw, err := os.ReadFile(filepath.Join(checker.Directory, floorFile))
	var set struct {
		Floors []struct {
			Sequence int64                  `json:"sequence"`
			Digest   releaseidentity.Digest `json:"digest"`
		} `json:"floors"`
	}
	if err != nil || json.Unmarshal(raw, &set) != nil || len(set.Floors) != 1 || set.Floors[0].Sequence != 9 || set.Floors[0].Digest != high {
		t.Fatalf("a lower raise must leave the floor at 9: %s %v", raw, err)
	}
}
