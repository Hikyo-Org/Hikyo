package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// A push to main or dev, or a dispatch for every PR, fans out to every open dev
// PR. The throttled publishers (max-parallel) reconcile them a few at a time, so
// the first pass must already hold each routed head non-success.
func TestDevRevokeBlocksEveryRoutedHeadWithoutThrottledWork(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	f := newDevFixture(t)
	f.c.repo = f.pr.Base.Repo
	f.c.repo.DefaultBranch = "main"
	f.api.responses["repos/"+f.c.repoName] = f.c.repo
	var heads []devWork
	var pulls []pull
	for i := 0; i < 5; i++ {
		p := f.pr
		p.Number = 20 + i
		p.Head.SHA = strings.Repeat(string(rune('a'+i)), 40)
		pulls = append(pulls, p)
		heads = append(heads, devWork{PR: p.Number, Head: p.Head.SHA})
	}
	f.api.responses[f.c.endpoint("pulls?state=open&per_page=100&page=1")] = pulls
	// A routed head that moved or left dev is the publisher's work, not this pass's.
	heads = append(heads, devWork{PR: 99, Head: strings.Repeat("9", 40)})
	f.c.verify = func(context.Context, int, string) (policyDecision, error) {
		t.Error("the first pass reached the verifier")
		return policyDecision{}, nil
	}
	if err := f.q.revokeDev(context.Background(), "push", event{Ref: "refs/heads/main", Repository: f.c.repo}, heads); err != nil {
		t.Fatal(err)
	}
	if len(f.api.writes) != 5 {
		t.Fatalf("writes %d, want one blocking record per routed head", len(f.api.writes))
	}
	seen := map[string]bool{}
	for _, w := range f.api.writes {
		if w["method"] != "POST" || w["status"] != "queued" || w["conclusion"] != nil {
			t.Fatalf("revocation wrote %v, not a blocking pending create", w)
		}
		seen[w["head_sha"].(string)] = true
	}
	for _, h := range heads[:5] {
		if !seen[h.Head] {
			t.Fatalf("head %s was not blocked", h.Head)
		}
	}
	// Cost: repository, open PR list and dev tip are read once; each head is one create.
	reads := 0
	for _, n := range f.api.reads {
		reads += n
	}
	if reads != 3 {
		t.Fatalf("first pass read %d times, want 3 (repository, open PRs, dev tip)", reads)
	}
}

func TestDevRevokeRefusesEventsThatDoNotFanOut(t *testing.T) {
	t.Setenv("GITHUB_REF", "refs/heads/main")
	for name, tc := range map[string]struct {
		event string
		e     event
	}{
		"pull request": {"pull_request_target", event{}},
		"sweep":        {"schedule", event{}},
		"foreign push": {"push", event{Ref: "refs/heads/feature"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newDevFixture(t)
			f.c.repo = f.pr.Base.Repo
			f.c.repo.DefaultBranch = "main"
			f.api.responses["repos/"+f.c.repoName] = f.c.repo
			tc.e.Repository = f.c.repo
			if err := f.q.revokeDev(context.Background(), tc.event, tc.e, []devWork{{PR: 7, Head: f.pr.Head.SHA}}); err == nil || len(f.api.writes) != 0 {
				t.Fatalf("err %v, writes %d: a non-fan-out event revoked", err, len(f.api.writes))
			}
		})
	}
}

// ownedRecord builds an owned dev check record for the fixture's head.
func ownedRecord(f *queueFixture, id int64, status, conclusion, title string, started time.Time) check {
	v := check{ID: id, Name: devCheckName, HeadSHA: f.pr.Head.SHA, ExternalID: newBinding(f.c.repo.ID, f.pr, nil).externalID(), Status: status, Conclusion: conclusion}
	v.App.ID, v.App.Slug = actionsAppID, "github-actions"
	v.Output.Title = title
	v.StartedAt = started.UTC().Format(time.RFC3339)
	if status == "completed" {
		v.CompletedAt = v.StartedAt
	}
	return v
}

func passFixture(t *testing.T) (*queueFixture, time.Time) {
	t.Helper()
	f := newDevFixture(t)
	f.q.dco = func(context.Context, pull) (int, error) { return 0, nil }
	start := time.Now().Add(-30 * time.Second).Truncate(time.Second)
	f.q.queueDeadline = start.Add(admissionSeconds * time.Second)
	f.c.expectedHead = f.pr.Head.SHA
	return f, start
}

func successPublished(f *queueFixture) bool {
	newest := newestQueueCheck(f.api.checks)
	return newest != nil && newest.Conclusion == "success"
}

// An older pass can still be running when a push revokes its head. Its terminal
// success must not rebind the revocation record: its inputs predate the event.
// The reconcile that follows the revocation starts later and may publish. The
// boundary is the pass start plus revocationSkew: equal is older, one second more is newer.
func TestDevRevocationSticksAgainstAnOlderPassSuccessWrite(t *testing.T) {
	for name, tc := range map[string]struct {
		offset      time.Duration
		unreadable  bool
		wantSuccess bool
		wantStale   bool
	}{
		"revocation long after the pass began":     {offset: 20 * time.Second, wantStale: true},
		"revocation one second past the skew":      {offset: revocationSkew + time.Second, wantStale: true},
		"revocation exactly at the skew boundary":  {offset: revocationSkew, wantSuccess: true},
		"revocation written before the pass began": {offset: -5 * time.Second, wantSuccess: true},
		"revocation time unreadable":               {unreadable: true},
	} {
		t.Run(name, func(t *testing.T) {
			f, start := passFixture(t)
			f.api.checks = []check{ownedRecord(f, 800, "queued", "", revocationTitle, start.Add(tc.offset))}
			f.api.nextID = 900
			if tc.unreadable {
				f.api.checks[0].StartedAt = ""
			}
			err := f.q.admit(context.Background(), f.pr.Number)
			if successPublished(f) != tc.wantSuccess {
				t.Fatalf("success published = %v, want %v (err %v, checks %+v)", successPublished(f), tc.wantSuccess, err, f.api.checks)
			}
			switch {
			case tc.wantStale && !onlyStaleHeadWork(err):
				t.Fatalf("the superseded pass must end as stale work, got %v", err)
			case tc.unreadable && (err == nil || onlyStaleHeadWork(err)):
				t.Fatalf("an unreadable revocation time must fail the pass so it is retried, got %v", err)
			}
		})
	}
}

// The older pass listed the owned checks while the newest record was a completed
// success, so it creates a record with a higher ID than a revocation written after
// that listing. The pass must notice and put a blocking record back on top.
func TestDevRevocationSurvivesAnOlderPassThatListedACompletedRecord(t *testing.T) {
	f, start := passFixture(t)
	f.api.checks = []check{ownedRecord(f, 5, "completed", "success", "Ready for dev integration", start.Add(-time.Hour))}
	f.api.nextID = 900
	revocation := ownedRecord(f, 6, "queued", "", revocationTitle, start.Add(20*time.Second))
	listings := 0
	f.api.readHook = func(path string, _ int) {
		if strings.Contains(path, "/check-runs?") {
			listings++
			if listings == 2 { // after the pass wrote its own success record
				f.api.checks = append(f.api.checks, revocation)
			}
		}
	}
	err := f.q.admit(context.Background(), f.pr.Number)
	if !onlyStaleHeadWork(err) {
		t.Fatalf("a pass that lost the race must end as stale work, got %v", err)
	}
	newest := newestQueueCheck(f.api.checks)
	if newest == nil || newest.Status != "queued" || newest.Conclusion != "" || newest.Output.Title != revocationTitle || newest.StartedAt != revocation.StartedAt {
		t.Fatalf("the newest record must block and keep the revocation's start, got %+v", newest)
	}
	// The reconcile queued after the revocation starts later and is not refused by the re-applied record.
	f.q.queueDeadline = start.Add(2 * time.Minute).Add(admissionSeconds * time.Second)
	f.api.readHook = nil
	if err := f.q.admit(context.Background(), f.pr.Number); err != nil || !successPublished(f) {
		t.Fatalf("the later reconcile must publish: %v %+v", err, f.api.checks)
	}
}

// A pending or refusal result from an older pass would lose the revocation's title.
func TestDevRevocationIsNeverRewrittenByAnOlderPass(t *testing.T) {
	f, start := passFixture(t)
	f.api.nextID = 900
	revocation := ownedRecord(f, 800, "queued", "", revocationTitle, start.Add(20*time.Second))
	f.api.checks = []check{revocation}
	session := &queueReconciliation{q: f.q}
	b := newBinding(f.c.repo.ID, f.pr, nil)
	for name, result := range map[string]outcome{"pending": pending("older"), "refusal": refused("older"), "success": {Status: "completed", Conclusion: "success", Title: "ok", Summary: "ok"}} {
		if _, err := session.publishRecord(context.Background(), f.pr, b, &revocation, result); !onlyStaleHeadWork(err) {
			t.Fatalf("%s: an older pass rewrote the revocation (%v)", name, err)
		}
	}
	if len(f.api.writes) != 0 {
		t.Fatalf("the revocation record was written to: %v", f.api.writes)
	}
	// A pass that began after the revocation owns the head again.
	f.q.queueDeadline = start.Add(time.Minute).Add(admissionSeconds * time.Second)
	if _, err := session.publishRecord(context.Background(), f.pr, b, &revocation, pending("later")); err != nil {
		t.Fatalf("a later reconcile must be able to rewrite the revocation: %v", err)
	}
}

// failingListAPI reads normally until the first write, then fails every check listing.
type failingListAPI struct {
	inner *fakeAPI
	armed bool
}

func (a *failingListAPI) read(ctx context.Context, path string, result any) error {
	if a.armed && strings.Contains(path, "/check-runs?") {
		return errors.New("listing unavailable")
	}
	return a.inner.read(ctx, path, result)
}

func (a *failingListAPI) write(ctx context.Context, method, path string, payload, result any) error {
	a.armed = true
	return a.inner.write(ctx, method, path, payload, result)
}

// The listing after the success write can trail the write. The pass acts only on a listing
// that shows the exact success it wrote: a lagging one is waited out, and one that never
// shows it (absent, or an error) fails closed with a blocking record over the success.
func TestDevPostWriteListingFailsClosedWhenItCannotConfirmTheSuccess(t *testing.T) {
	for name, tc := range map[string]struct {
		lag         int
		listFails   bool
		wantSuccess bool
		wantWaits   int
	}{
		"visible at once":   {wantSuccess: true},
		"lags then appears": {lag: 2, wantSuccess: true, wantWaits: 2},
		"never appears":     {lag: 1 << 20, wantWaits: len(writeConfirmBackoff)},
		"listing fails":     {listFails: true},
	} {
		t.Run(name, func(t *testing.T) {
			f, _ := passFixture(t)
			waits := recordReadWaits(t)
			if tc.listFails {
				f.c.api = &failingListAPI{inner: f.api}
			} else {
				f.c.api = &laggingListAPI{inner: f.api, lag: tc.lag}
			}
			err := f.q.admit(context.Background(), f.pr.Number)
			if successPublished(f) != tc.wantSuccess {
				t.Fatalf("newest record is success = %v, want %v (err %v, checks %+v)", successPublished(f), tc.wantSuccess, err, f.api.checks)
			}
			if tc.wantSuccess && err != nil {
				t.Fatalf("a confirmed success failed the pass: %v", err)
			}
			if !tc.wantSuccess {
				newest := newestQueueCheck(f.api.checks)
				if err == nil || newest == nil || newest.Status != "queued" || !blockingQueueCheck(newest) {
					t.Fatalf("an unconfirmed success must leave a blocking record on top and fail the pass: err %v, newest %+v", err, newest)
				}
			}
			if len(*waits) != tc.wantWaits && !tc.listFails {
				t.Fatalf("waits = %v, want %d", *waits, tc.wantWaits)
			}
		})
	}
}
