package audit

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/google/uuid"
)

func refusalTestSample(t *testing.T) RefusalSample {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return RefusalSample{Stage: RefusalArtifact, SourceIP: "192.0.2.1", RequestID: id.String(), Operation: "whoami"}
}

func TestRefusalWindowBoundsAndSchema(t *testing.T) {
	w := NewRefusalWindow()
	for i := 0; i < 1000; i++ {
		if err := w.Add(refusalTestSample(t)); err != nil {
			t.Fatal(err)
		}
	}
	if len(w.Samples) != 32 || w.Counts[0] != 1000 {
		t.Fatalf("unbounded or lost accounting: %d samples %d count", len(w.Samples), w.Counts[0])
	}
	e := Event{ID: "evt_test", Type: EventRequestRefused, SchemaVersion: 1, OccurredAt: time.Now().UTC(), Actor: Actor{Class: ActorUnauthenticated}, Outcome: OutcomeFailure, Origin: OriginSystem, Payload: w.RefusalPayload(RefusalArtifact, 120)}
	if err := Validate(e, TrailInstance, domain.Scope{}); err != nil {
		t.Fatal(err)
	}
	e.Payload["unsampled_count"] = int64(0)
	if err := Validate(e, TrailInstance, domain.Scope{}); err == nil {
		t.Fatal("false unsampled count accepted")
	}
	w.Counts[0] = math.MaxInt64
	if err := w.Add(refusalTestSample(t)); err == nil || w.Counts[0] != math.MaxInt64 {
		t.Fatal("counter saturation wrapped or was silent")
	}
	for _, bad := range []string{"unbounded operation prose", "hik_1_wl_abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ", "/arbitrary/path?secret=canary"} {
		s := refusalTestSample(t)
		s.Operation = bad
		if s.Validate() == nil {
			t.Fatal("untrusted operation accepted")
		}
	}
}

func TestRefusalReceiptSharesOwnershipWithoutCredentialData(t *testing.T) {
	s := refusalTestSample(t)
	ctx := WithRefusalReceipt(context.Background(), s.SourceIP, s.RequestID)
	NoteRefusal(ctx, RefusalArtifact)
	NoteRefusal(ctx, RefusalAdmission)
	SetRefusalOperation(ctx, "whoami")
	got, ok := PendingRefusal(ctx)
	if !ok || got.Stage != RefusalAdmission || got.Operation != "whoami" {
		t.Fatal("request receipt lost")
	}
	MarkDedicatedRefusal(ctx)
	if _, ok := PendingRefusal(ctx); ok {
		t.Fatal("durable dedicated receipt duplicated")
	}
}

func TestIncidentalArtifactProbeRetainsAdmissionButNotAuthenticationOutcome(t *testing.T) {
	s := refusalTestSample(t)
	ctx := WithRefusalReceipt(context.Background(), s.SourceIP, s.RequestID)
	probe := WithRefusalArtifactProbe(ctx)
	NoteRefusal(probe, RefusalArtifact)
	if _, ok := PendingRefusal(ctx); ok {
		t.Fatal("incidental dead cookie became a final authentication refusal")
	}
	NoteRefusal(probe, RefusalAdmission)
	if got, ok := PendingRefusal(ctx); !ok || got.Stage != RefusalAdmission {
		t.Fatal("probe hid final admission refusal")
	}
}
