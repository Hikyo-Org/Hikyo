package operation

import (
	"context"
	"errors"
	"testing"
)

func TestPreauthenticationAdmissionRetainsOneRefusalAcrossNestedReads(t *testing.T) {
	refusal := errors.New("overloaded")
	charges := 0
	ctx := WithPreauthenticationAdmission(context.Background(), func() error { charges++; return refusal })
	for range 3 {
		if !errors.Is(AdmitPreauthentication(ctx), refusal) {
			t.Fatal("nested resolution lost refusal")
		}
	}
	if charges != 1 {
		t.Fatalf("charged %d times", charges)
	}
}

type liveSessionsStub map[string]string

func (s liveSessionsStub) KnownLiveSession(verifier []byte) (string, bool) {
	id, known := s[string(verifier)]
	return id, known
}
func (s liveSessionsStub) NoteLiveSession(verifier []byte, sessionID string) {
	s[string(verifier)] = sessionID
}
func (s liveSessionsStub) ForgetSession(verifier []byte) { delete(s, string(verifier)) }

func TestSessionPresentationSpendsOneLimit(t *testing.T) {
	source, budget := 0, 0
	sessions := liveSessionsStub{}
	base := WithLiveSessions(t.Context(), sessions)
	request := func() context.Context {
		ctx := WithPreauthenticationAdmission(base, func() error { source++; return nil })
		return WithRequestAdmission(ctx, func(id string) error {
			if id != "ses_a" {
				t.Errorf("budget charged to %q", id)
			}
			budget++
			return nil
		})
	}
	verifier := []byte("verifier")
	if err := AdmitSessionPresentation(request(), verifier); err != nil || source != 1 || budget != 0 {
		t.Fatalf("unknown session: err=%v source=%d budget=%d", err, source, budget)
	}
	NoteSessionLive(base, verifier, "ses_a")
	if err := AdmitSessionPresentation(request(), verifier); err != nil || source != 1 || budget != 1 {
		t.Fatalf("known session: err=%v source=%d budget=%d", err, source, budget)
	}
	ForgetSession(base, verifier)
	if err := AdmitSessionPresentation(request(), verifier); err != nil || source != 2 || budget != 1 {
		t.Fatalf("forgotten session: err=%v source=%d budget=%d", err, source, budget)
	}
}

func TestKnownSessionOverBudgetIsRefusedBeforeItsRead(t *testing.T) {
	refusal := errors.New("over budget")
	sessions := liveSessionsStub{"verifier": "ses_a"}
	ctx := WithRequestAdmission(WithLiveSessions(t.Context(), sessions), func(string) error { return refusal })
	if err := AdmitSessionPresentation(ctx, []byte("verifier")); !errors.Is(err, refusal) {
		t.Fatalf("err=%v", err)
	}
}

func TestSessionPresentationWithoutMemoryStillCharges(t *testing.T) {
	charges := 0
	ctx := WithPreauthenticationAdmission(t.Context(), func() error { charges++; return nil })
	NoteSessionLive(ctx, []byte("verifier"), "ses_a")
	if err := AdmitSessionPresentation(ctx, []byte("verifier")); err != nil || charges != 1 {
		t.Fatalf("err=%v charges=%d", err, charges)
	}
}
