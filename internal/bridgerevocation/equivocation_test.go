package bridgerevocation_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// Two runs that both read an empty store and fetch different lists for the same sequence
// race to store them: the first wins and the second must refuse, not return its own list
// as if it were the stored one.
func TestConcurrentChecksFetchingDifferentListsAtOneSequenceRefuseTheSecond(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	bridge := releaseidentity.Hash([]byte("bridge"))
	for round := range 10 {
		directory := t.TempDir()
		var arrived sync.WaitGroup
		arrived.Add(2)
		release := make(chan struct{})
		errs := make(chan error, 2)
		for _, revoked := range [][]releaseidentity.Digest{nil, {bridge}} {
			list, signature := signedList(t, f, 5, revoked...)
			checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: barrierFetcher{list, signature, &arrived, release}, Directory: directory, Now: func() time.Time { return testNow }}
			go func() {
				_, err := checker.Check(t.Context(), snapshot)
				errs <- err
			}()
		}
		arrived.Wait()
		close(release)
		var refused int
		for range 2 {
			if err := <-errs; err != nil {
				if !strings.Contains(err.Error(), "equivocation") {
					t.Fatalf("round %d: unexpected error %v", round, err)
				}
				refused++
			}
		}
		if refused != 1 {
			t.Fatalf("round %d: exactly one of two conflicting lists at one sequence must be refused, got %d", round, refused)
		}
	}
}
