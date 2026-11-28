package bridgerevocation_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/bridgerevocation"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust/testfixture"
)

// barrierFetcher answers once every runner has read the stored list, so all of them
// compare against the same stale floor and then race to store theirs.
type barrierFetcher struct {
	list, signature []byte
	arrived         *sync.WaitGroup
	release         <-chan struct{}
}

func (b barrierFetcher) Fetch(context.Context) ([]byte, []byte, error) {
	b.arrived.Done()
	<-b.release
	return b.list, b.signature, nil
}

// Concurrent runs must never lower the stored sequence floor: the last writer used to
// win whatever its sequence.
func TestConcurrentChecksNeverLowerTheStoredSequence(t *testing.T) {
	f := testfixture.New(t)
	snapshot := f.Snapshot(t)
	const runners = 8
	for round := range 10 {
		directory := t.TempDir()
		var arrived sync.WaitGroup
		arrived.Add(runners)
		release := make(chan struct{})
		var done sync.WaitGroup
		for sequence := int64(1); sequence <= runners; sequence++ {
			list, signature := signedList(t, f, sequence)
			checker := bridgerevocation.Checker{Mode: bridgerevocation.Online, Fetcher: barrierFetcher{list, signature, &arrived, release}, Directory: directory, Now: func() time.Time { return testNow }}
			done.Add(1)
			go func() {
				defer done.Done()
				if _, err := checker.Check(t.Context(), snapshot); err != nil {
					t.Error(err)
				}
			}()
		}
		arrived.Wait()
		close(release)
		done.Wait()
		stored := bridgerevocation.Checker{Mode: bridgerevocation.Off, Directory: directory, Now: func() time.Time { return testNow }}
		result, err := stored.Check(t.Context(), snapshot)
		if err != nil || result.Sequence != runners {
			t.Fatalf("round %d: the stored floor must be the highest sequence %d, got %d: %v", round, runners, result.Sequence, err)
		}
	}
}
