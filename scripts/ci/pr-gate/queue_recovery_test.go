package main

import (
	"context"
	"strings"
	"testing"
)

// Publication, group refresh and retirement all trust this identity, so a foreign
// or differently bound record must never count as the gate's own check.
func TestAuthenticatedQueueCheckRefusesForeignIdentity(t *testing.T) {
	f := newQueueFixture(t)
	head := f.pr.Head.SHA
	b := newBinding(f.pr.Base.Repo.ID, f.pr, nil)
	owned := func() *check {
		v := &check{ID: 99, Name: queueCheckName, HeadSHA: head, ExternalID: b.externalID(), Status: "queued"}
		v.App.ID, v.App.Slug = actionsAppID, "github-actions"
		return v
	}
	if !f.c.authenticatedQueueCheck(owned(), head) {
		t.Fatal("owned check was not recognized")
	}
	otherHead := f.pr
	otherHead.Head.SHA = strings.Repeat("e", 40)
	for name, mutate := range map[string]func(*check){
		"missing ID":   func(v *check) { v.ID = 0 },
		"other name":   func(v *check) { v.Name = devCheckName },
		"other head":   func(v *check) { v.HeadSHA = strings.Repeat("e", 40) },
		"foreign app":  func(v *check) { v.App.ID++ },
		"foreign slug": func(v *check) { v.App.Slug = "foreign" },
		"bad binding":  func(v *check) { v.ExternalID = "not-a-binding" },
		"other repo":   func(v *check) { v.ExternalID = newBinding(f.pr.Base.Repo.ID+1, f.pr, nil).externalID() },
		"bound elsewhere": func(v *check) {
			v.ExternalID = newBinding(f.pr.Base.Repo.ID, otherHead, nil).externalID()
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := owned()
			mutate(v)
			if f.c.authenticatedQueueCheck(v, head) {
				t.Fatal("foreign identity was accepted")
			}
		})
	}
	if f.c.authenticatedQueueCheck(nil, head) {
		t.Fatal("missing check was accepted")
	}
}

// Recovery after a PR moves must leave a blocking barrier and never authorize a
// merge. It may stop only once the PR is closed or confirmed unchanged.
func TestBlockMovementConfirmsCurrentIdentity(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate  func(f *queueFixture)
		wantErr bool
		writes  int
	}{
		"unchanged":    {writes: 1},
		"keeps moving": {wantErr: true, writes: 1, mutate: func(f *queueFixture) { f.pr.Head.SHA = strings.Repeat("f", 40) }},
		"base moves":   {wantErr: true, writes: 1, mutate: func(f *queueFixture) { f.pr.Base.SHA = strings.Repeat("f", 40) }},
		"closes":       {writes: 1, mutate: func(f *queueFixture) { f.pr.State = "closed" }},
	} {
		t.Run(name, func(t *testing.T) {
			f := newQueueFixture(t)
			path := f.c.endpoint("pulls/7")
			read := 0
			f.api.readHook = func(got string, _ int) {
				if got != path {
					return
				}
				read++
				if read == 2 && tc.mutate != nil {
					tc.mutate(f)
					f.refresh()
				}
			}
			err := f.c.blockMovement(context.Background(), 7, "fixture")
			if (err != nil) != tc.wantErr {
				t.Fatalf("recovery error %v, want error=%t", err, tc.wantErr)
			}
			if len(f.api.writes) != tc.writes {
				t.Fatalf("recovery wrote %d checks, want %d: %+v", len(f.api.writes), tc.writes, f.api.writes)
			}
			for _, w := range f.api.writes {
				if w["conclusion"] == "success" {
					t.Fatal("recovery authorized a merge")
				}
			}
			latest := f.api.checks[len(f.api.checks)-1]
			if latest.Status != "queued" || !blockingQueueCheck(&latest) {
				t.Fatalf("recovery left no blocking barrier: %+v", latest)
			}
		})
	}
}
