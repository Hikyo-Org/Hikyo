package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/scripts/ci/checksuites"
)

func (f *fixture) requested(prefix string) int {
	n := 0
	for _, path := range f.requests {
		if strings.HasPrefix(path, prefix) {
			n++
		}
	}
	return n
}

// Past REST's cap of 1000 check suites a listing by commit shows no report, so a
// head's existing report used to look absent and a second one was created.
func TestExistingReportOnHeadBeyondRESTSuiteCapIsUpdatedNotDuplicated(t *testing.T) {
	f := newFixture(t)
	f.existing("in_progress")
	f.suites, f.holding = 2005, []int64{1856}
	if err := f.c.publish(context.Background(), f.e); err != nil {
		t.Fatal(err)
	}
	if len(f.writeMethods) != 1 || f.writeMethods[0] != "PATCH" || f.writePaths[0] != "check-runs/777" {
		t.Fatalf("writes %v %v, want one PATCH of the existing report", f.writeMethods, f.writePaths)
	}
	if f.requested("check-suites/1856/check-runs") == 0 || f.requested("check-suites/") != f.requested("check-suites/1856/") || f.requested("commits/") != 1 {
		t.Fatalf("requests %v, want the holding suite and one listing by commit", f.requests)
	}
}

// With no suite holding the report, the first publication still creates it.
func TestFirstReportOnHeadBeyondRESTSuiteCapIsCreated(t *testing.T) {
	f := newFixture(t)
	f.suites = 1147
	if err := f.c.publish(context.Background(), f.e); err != nil {
		t.Fatal(err)
	}
	if len(f.writeMethods) != 1 || f.writeMethods[0] != "POST" || f.requested("check-suites/") != 0 {
		t.Fatalf("writes %v requests %v, want one POST and no suite listing", f.writeMethods, f.requests)
	}
}

// A head within the margin keeps its single listing by commit.
func TestReportOnHeadWithinSuiteMarginStaysOnCommitListing(t *testing.T) {
	for _, suites := range []int{0, checksuites.Margin} {
		f := newFixture(t)
		f.existing("in_progress")
		f.suites = suites
		if err := f.c.publish(context.Background(), f.e); err != nil {
			t.Fatal(err)
		}
		if f.requested("commits/") == 0 || f.requested("check-suites/") != 0 || f.writeMethods[0] != "PATCH" {
			t.Fatalf("%d suites: requests %v writes %v", suites, f.requests, f.writeMethods)
		}
	}
}

// An unreadable suite inventory must not fall back to the partial listing.
func TestReportOnHeadBeyondRESTSuiteCapFailsClosedWithoutSuiteInventory(t *testing.T) {
	f := newFixture(t)
	f.existing("in_progress")
	f.suites, f.holding, f.graphqlFault = 2005, []int64{1856}, "incomplete server result"
	if err := f.c.publish(context.Background(), f.e); err == nil || !strings.Contains(err.Error(), "incomplete server result") {
		t.Fatalf("error: %v", err)
	}
	if len(f.writes) != 0 {
		t.Fatal("published without a complete suite inventory")
	}
}

// The same report in two suites is ambiguous, never silently the first.
func TestReportInTwoSuitesBeyondRESTSuiteCapIsRefused(t *testing.T) {
	f := newFixture(t)
	f.existing("in_progress")
	f.suites, f.holding = 2005, []int64{7, 1856}
	if err := f.c.publish(context.Background(), f.e); err == nil || len(f.writes) != 0 {
		t.Fatalf("error %v with %d writes, want a refusal", err, len(f.writes))
	}
}

// Another app's record under the report's identity is invisible to the suite
// reads. The listing by commit still shows it and refuses, as it always did.
func TestForeignAppReportBeyondRESTSuiteCapIsRefused(t *testing.T) {
	f := newFixture(t)
	f.existing("in_progress")
	f.suites = 2005
	f.checks[0].App.ID++
	if err := f.c.publish(context.Background(), f.e); err == nil || len(f.writes) != 0 {
		t.Fatalf("error %v with %d writes, want a refusal", err, len(f.writes))
	}
}

// A report the listing by commit shows but the suite scan did not find was
// written between the two reads; creating another would duplicate it.
func TestReportSeenOnlyByTheCommitListingBeyondRESTSuiteCapIsRefused(t *testing.T) {
	f := newFixture(t)
	f.existing("in_progress")
	f.suites, f.recent = 2005, true
	if err := f.c.publish(context.Background(), f.e); err == nil || len(f.writes) != 0 {
		t.Fatalf("error %v with %d writes, want a refusal", err, len(f.writes))
	}
}

// Suites added while the commit listing is read can push an older suite out of
// REST's newest 1000, so the reporter counts again after the read and refuses.
func TestReportRefusesACommitListingWhenSuitesGrowDuringTheRead(t *testing.T) {
	f := newFixture(t)
	f.existing("in_progress")
	f.suites, f.grown = checksuites.Margin, checksuites.Margin+1
	if err := f.c.publish(context.Background(), f.e); err == nil || !strings.Contains(err.Error(), "grew past") || len(f.writes) != 0 {
		t.Fatalf("error %v with %d writes, want a refusal after the recount", err, len(f.writes))
	}
}
