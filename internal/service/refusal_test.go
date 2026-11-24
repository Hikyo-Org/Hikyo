package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/audit"
	"github.com/Hikyo-Org/hikyo/internal/domain"
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

// A self-surface call with no bearer answers unauthenticated like resolve does,
// so it must also reach the request's security signal (#1034).
func TestBearerlessSelfSurfaceCallRecordsArtifactRefusal(t *testing.T) {
	ctx := audit.WithRefusalReceipt(t.Context(), "192.0.2.7", "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fee")
	if _, err := (Actor{}).resolveSelf(ctx, nil, time.Now()); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatalf("bearer-less call: %v", err)
	}
	if got, ok := audit.PendingRefusal(ctx); !ok || got.Stage != audit.RefusalArtifact {
		t.Fatalf("bearer-less self-surface refusal not recorded: %+v ok=%v", got, ok)
	}
	local := audit.WithRefusalReceipt(t.Context(), "192.0.2.7", "0193f0b4-1f2a-7c31-9c1e-2a4b6d8e0fed")
	if _, err := LocalPrincipal("usr_local").resolveSelf(local, nil, time.Now()); err != nil {
		t.Fatalf("local principal: %v", err)
	}
	if _, ok := audit.PendingRefusal(local); ok {
		t.Fatal("an admitted local principal left a refusal")
	}
}
