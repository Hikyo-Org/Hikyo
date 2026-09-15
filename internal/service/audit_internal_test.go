package service

import (
	"context"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

// fakeTrail is a seq-ordered audit trail backing a fillPage read closure: it
// returns rows with Seq strictly above the cursor, ascending, capped at Limit —
// the same shape PageTenant/PageInstance produce for AuditPageBySeq.
type fakeTrail struct {
	rows  []store.AuditEvent
	reads int
}

func (t *fakeTrail) read(_ context.Context, f store.AuditFilter) ([]store.AuditEvent, error) {
	t.reads++
	out := make([]store.AuditEvent, 0, f.Limit)
	for _, e := range t.rows {
		if e.Seq <= f.AfterSeq {
			continue
		}
		out = append(out, e)
		if len(out) >= f.Limit {
			break
		}
	}
	return out, nil
}

func trailByActor(n int, everyNthAlice int) *fakeTrail {
	t := &fakeTrail{rows: make([]store.AuditEvent, n)}
	for i := range t.rows {
		t.rows[i].Seq = int64(i + 1)
		if everyNthAlice > 0 && (i+1)%everyNthAlice == 0 {
			t.rows[i].Actor.ID = "usr_alice"
		} else {
			t.rows[i].Actor.ID = "usr_bob"
		}
	}
	return t
}

// TestFillPageDensePaging pins the sparse-page fix: a selective filter fills the
// page from multiple store reads instead of returning a near-empty one, resumes
// without skipping or duplicating, ends with Exhausted, and yields under the
// per-request scan budget rather than walking the whole trail.
func TestFillPageDensePaging(t *testing.T) {
	ctx := context.Background()

	// 100 rows, every 10th matches alice → 10 matches. limit 5, ceiling 100.
	trail := trailByActor(100, 10)
	f := store.AuditFilter{Limit: 5, Actor: "usr_alice"}
	page, err := fillPage(ctx, f, 100, trail.read, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 5 {
		t.Fatalf("dense page = %d events, want the full limit 5 (not a sparse read)", len(page.Events))
	}
	if page.Exhausted {
		t.Fatal("5 of 10 matches returned: must not be exhausted")
	}
	if page.Events[0].Seq != 10 || page.Events[4].Seq != 50 {
		t.Fatalf("matched seqs = %d..%d, want 10..50", page.Events[0].Seq, page.Events[4].Seq)
	}
	if page.NextSeq != 50 {
		t.Fatalf("cursor = %d, want the last RETURNED seq 50", page.NextSeq)
	}

	// Resume: the second page picks up the remaining 5 with no skip or dup.
	f2 := f
	f2.AfterSeq = page.NextSeq
	page2, err := fillPage(ctx, f2, 100, trail.read, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Events) != 5 || page2.Events[0].Seq != 60 || page2.Events[4].Seq != 100 {
		t.Fatalf("resume page seqs = %v, want 60..100", seqs(page2.Events))
	}
	if !page2.Exhausted {
		t.Fatal("last 5 matches at the ceiling: must be exhausted")
	}

	// A filter that matches nothing must not walk the whole trail in one request:
	// the scan budget yields an empty page with Exhausted=false and an advanced
	// cursor, and does NOT read every chunk to the end.
	big := trailByActor(1000000, 0) // no alice anywhere
	miss := store.AuditFilter{Limit: 5, Actor: "usr_alice"}
	pageMiss, err := fillPage(ctx, miss, 1000000, big.read, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pageMiss.Events) != 0 || pageMiss.Exhausted {
		t.Fatalf("budget-capped page = %d events exhausted=%v, want 0/false", len(pageMiss.Events), pageMiss.Exhausted)
	}
	if pageMiss.NextSeq == 0 {
		t.Fatal("budget-capped page must advance the cursor so the caller can resume")
	}
	if big.reads > 16 {
		t.Fatalf("scan budget breached: %d reads, want <= 16", big.reads)
	}
}

// TestFillPageKeepFilter pins the service-side keep pass (the ActorName path):
// keep runs after the store filter, keep-dropped rows still advance the scanned
// cursor, the page fills to the limit from multiple reads, and NextSeq is the
// last RETURNED seq. Here the store filter is unset and keep is the only
// selector — the shape the actor_name glob takes at runtime.
func TestFillPageKeepFilter(t *testing.T) {
	ctx := context.Background()

	// 100 rows, every 10th is alice → 10 kept. Resolve id->name and glob "Al*"
	// via the same MatchesActorName the runtime keep uses.
	trail := trailByActor(100, 10)
	name := map[string]string{"usr_alice": "Alice", "usr_bob": "Bob"}
	nameFilter := store.AuditFilter{ActorName: "Al*"}
	keep := func(e store.AuditEvent) (bool, error) {
		return nameFilter.MatchesActorName(name[e.Actor.ID]), nil
	}

	f := store.AuditFilter{Limit: 5} // no store-side field set; keep selects
	page, err := fillPage(ctx, f, 100, trail.read, keep)
	if err != nil {
		t.Fatal(err)
	}
	if got := seqs(page.Events); len(got) != 5 || got[0] != 10 || got[4] != 50 {
		t.Fatalf("kept seqs = %v, want 10..50 (full limit from keep pass)", got)
	}
	if page.NextSeq != 50 {
		t.Fatalf("cursor = %d, want the last RETURNED seq 50", page.NextSeq)
	}
	if page.Exhausted {
		t.Fatal("5 of 10 kept: must not be exhausted")
	}
}

func seqs(es []store.AuditEvent) []int64 {
	out := make([]int64, len(es))
	for i, e := range es {
		out[i] = e.Seq
	}
	return out
}

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
