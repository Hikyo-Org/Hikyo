package admission

import (
	"errors"
	"testing"
	"time"
)

func TestLiveSessionMemory(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	l := replacementLimiter(t, fixedClock(&now), 64*1024)
	session := []byte("verifier")
	if _, known := l.KnownLiveSession(session); known {
		t.Fatal("an unseen session was known")
	}
	l.NoteLiveSession(session, "ses_a")
	if id, known := l.KnownLiveSession(session); !known || id != "ses_a" {
		t.Fatalf("known session = %q, %v", id, known)
	}
	l.ForgetSession(session)
	if _, known := l.KnownLiveSession(session); known {
		t.Fatal("a forgotten session stayed known")
	}
}

func TestLiveSessionMemoryDropsTheLeastRecentlyProven(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	l := replacementLimiter(t, fixedClock(&now), 64*1024)
	verifier := func(index int) []byte { return []byte{byte(index), byte(index >> 8), byte(index >> 16)} }
	known := func(index int) bool {
		_, ok := l.KnownLiveSession(verifier(index))
		return ok
	}
	for index := range MaxTrackedSubjects {
		now = now.Add(time.Second)
		l.NoteLiveSession(verifier(index), "ses")
	}
	// Proving a tracked session again at the bound evicts nothing and refreshes it.
	now = now.Add(time.Second)
	l.NoteLiveSession(verifier(0), "ses")
	if len(l.liveSessions) != MaxTrackedSubjects || !known(1) {
		t.Fatalf("re-proving evicted: tracked %d", len(l.liveSessions))
	}
	now = now.Add(time.Second)
	l.NoteLiveSession(verifier(MaxTrackedSubjects), "ses")
	if len(l.liveSessions) != MaxTrackedSubjects || known(1) || !known(0) || !known(MaxTrackedSubjects) {
		t.Fatal("eviction did not drop exactly the least recently proven session")
	}
}

func TestInheritCountersKeepsLiveSessionsAndRefusesADirtyCandidate(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	previous := replacementLimiter(t, fixedClock(&now), 64*1024)
	next := replacementLimiter(t, fixedClock(&now), 64*1024)
	session := []byte("verifier")
	previous.NoteLiveSession(session, "ses_a")
	if err := next.InheritCounters(previous); err != nil {
		t.Fatal(err)
	}
	if id, known := next.KnownLiveSession(session); !known || id != "ses_a" {
		t.Fatal("a generation handoff forgot a live session")
	}
	dirty := replacementLimiter(t, fixedClock(&now), 64*1024)
	dirty.NoteLiveSession(session, "ses_a")
	if err := dirty.InheritCounters(previous); !errors.Is(err, ErrCountersNotEmpty) {
		t.Fatalf("dirty candidate = %v", err)
	}
}
