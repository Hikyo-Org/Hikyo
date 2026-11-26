package main

import (
	"context"
	"strings"
	"testing"
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
