package operation

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRequestAdmissionChargesOnceAcrossConcurrentRetries(t *testing.T) {
	var charges atomic.Int64
	refusal := errors.New("refused")
	ctx := WithRequestAdmission(context.Background(), func(id string) error {
		if id != "live-session" {
			t.Errorf("untrusted subject %q", id)
		}
		charges.Add(1)
		return refusal
	})
	if err := AdmitRequest(ctx, ""); err != nil || charges.Load() != 0 {
		t.Fatal("machine or unresolved caller charged a session bucket")
	}
	var workers sync.WaitGroup
	for range 20 {
		workers.Go(func() {
			if err := AdmitRequest(ctx, "live-session"); !errors.Is(err, refusal) {
				t.Errorf("accounting refusal lost: %v", err)
			}
		})
	}
	workers.Wait()
	if charges.Load() != 1 {
		t.Fatalf("request charged %d times", charges.Load())
	}
}
