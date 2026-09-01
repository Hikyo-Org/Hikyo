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

	// A sparse filter over a fully scanned page: two matches, but the cursor is
	// the last SCANNED seq and the window is not exhausted (scanned == limit).
	page := filterPage(scanned, store.AuditFilter{Limit: 3, Actor: "usr_alice"})
	if len(page.Events) != 2 {
		t.Fatalf("matched events = %d, want 2", len(page.Events))
	}
	if page.NextSeq != 12 {
		t.Fatalf("cursor = %d, want the last scanned seq 12 (not the last matched)", page.NextSeq)
	}
	if page.Exhausted {
		t.Fatal("a full page (scanned == limit) must not report exhausted")
	}

	// A short page (fewer scanned than the limit) reaches the end of the trail.
	short := filterPage(scanned, store.AuditFilter{Limit: 10})
	if !short.Exhausted {
		t.Fatal("scanned < limit must report exhausted")
	}
	if short.NextSeq != 12 {
		t.Fatalf("cursor = %d, want 12", short.NextSeq)
	}

	// An empty scan leaves the cursor where the caller asked to resume.
	empty := filterPage(nil, store.AuditFilter{Limit: 10, AfterSeq: 7})
	if !empty.Exhausted || empty.NextSeq != 7 {
		t.Fatalf("empty scan: exhausted=%v next=%d, want true/7", empty.Exhausted, empty.NextSeq)
	}
}
