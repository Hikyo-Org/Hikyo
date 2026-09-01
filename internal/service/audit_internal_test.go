package service

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// TestFilterPageScanWindow pins the scan-window contract: the cursor advances
// over SCANNED rows (matched or not) so a sparse filter never re-reads or skips,
// and Exhausted reflects the scanned count against the page limit, not the
// matched count.
func TestFilterPageScanWindow(t *testing.T) {
	scanned := []store.AuditEvent{
		{Seq: 10},
		{Seq: 11},
		{Seq: 12},
	}
	scanned[0].Actor.ID = "usr_alice"
	scanned[1].Actor.ID = "usr_bob"
	scanned[2].Actor.ID = "usr_alice"

	// A sparse filter over a fully scanned page under a ceiling that admits it
	// all: two matches, cursor is the last SCANNED seq, and the window is not
	// exhausted (scanned == limit, cursor below the ceiling).
	page := filterPage(scanned, store.AuditFilter{Limit: 3, Actor: "usr_alice"}, 100)
	if len(page.Events) != 2 {
		t.Fatalf("matched events = %d, want 2", len(page.Events))
	}
	if page.NextSeq != 12 {
		t.Fatalf("cursor = %d, want the last scanned seq 12 (not the last matched)", page.NextSeq)
	}
	if page.UpperSeq != 100 {
		t.Fatalf("upper seq = %d, want the pinned ceiling 100", page.UpperSeq)
	}
	if page.Exhausted {
		t.Fatal("a full page below the ceiling must not report exhausted")
	}

	// A short page (fewer scanned than the limit) reaches the end of the trail.
	short := filterPage(scanned, store.AuditFilter{Limit: 10}, 100)
	if !short.Exhausted || short.NextSeq != 12 {
		t.Fatalf("short page: exhausted=%v next=%d, want true/12", short.Exhausted, short.NextSeq)
	}

	// An empty scan leaves the cursor where the caller asked to resume.
	empty := filterPage(nil, store.AuditFilter{Limit: 10, AfterSeq: 7}, 100)
	if !empty.Exhausted || empty.NextSeq != 7 {
		t.Fatalf("empty scan: exhausted=%v next=%d, want true/7", empty.Exhausted, empty.NextSeq)
	}

	// The ceiling is the crux: a full page (scanned == limit) whose last row is
	// AT the ceiling is exhausted — no row remains between the cursor and the
	// pinned top, even though the store could return more (this reader's own
	// audit.query event, or a concurrent write).
	atCeiling := filterPage(scanned, store.AuditFilter{Limit: 3}, 12)
	if !atCeiling.Exhausted || atCeiling.NextSeq != 12 {
		t.Fatalf("cursor at ceiling: exhausted=%v next=%d, want true/12", atCeiling.Exhausted, atCeiling.NextSeq)
	}

	// A row past the ceiling mid-page stops the scan there: the cursor is the
	// last row WITHIN the ceiling and the run is exhausted. seq 12 is above the
	// ceiling 11, so it is neither returned nor advanced past.
	pastMid := filterPage(scanned, store.AuditFilter{Limit: 3}, 11)
	if !pastMid.Exhausted {
		t.Fatal("a row past the ceiling must end the run")
	}
	if pastMid.NextSeq != 11 {
		t.Fatalf("cursor = %d, want 11 (the last row within the ceiling)", pastMid.NextSeq)
	}

	// The first scanned row already past the ceiling: nothing kept, cursor
	// unmoved, exhausted.
	firstPast := filterPage(scanned, store.AuditFilter{Limit: 3, AfterSeq: 9}, 9)
	if !firstPast.Exhausted || firstPast.NextSeq != 9 || len(firstPast.Events) != 0 {
		t.Fatalf("first row past ceiling: exhausted=%v next=%d events=%d, want true/9/0",
			firstPast.Exhausted, firstPast.NextSeq, len(firstPast.Events))
	}
}
