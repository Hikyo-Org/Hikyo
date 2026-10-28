package service

import (
	"context"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
)

func TestRefusalWorkBoundsRejectFullQueueAndHonorDeadline(t *testing.T) {
	s := NewRefusalSignals(nil)
	for i := 0; i < cap(s.slots); i++ {
		s.slots <- struct{}{}
	}
	for i := 0; i < cap(s.waiters); i++ {
		s.waiters <- struct{}{}
	}
	if err := s.Record(t.Context(), audit.RefusalSample{}); err == nil {
		t.Fatal("full queue admitted")
	}
	if got := s.RefusalHealth(); got.Failures != 1 || got.Healthy || got.InFlight != 8 || got.Waiting != 64 {
		t.Fatalf("health: %+v", got)
	}
	<-s.waiters
	start := time.Now()
	if err := s.Record(context.Background(), audit.RefusalSample{}); err == nil {
		t.Fatal("held writers ignored deadline")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("deadline did not bound work: %s", elapsed)
	}
	if got := s.RefusalHealth(); got.Failures != 2 || got.Waiting != 63 {
		t.Fatalf("waiter leaked: %+v", got)
	}
}
