package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// rateLimitFixture fakes gh: reads of repos/... answer with a rate limit failure
// until the Nth call, and rate_limit reports the given core budget.
func rateLimitFixture(t *testing.T, remaining int, resetIn, budget time.Duration, failures int) (attempts func() int, waits *[]time.Duration, now time.Time) {
	t.Helper()
	now = time.Unix(1_800_000_000, 0)
	old := rateLimitNow
	rateLimitNow = func() time.Time { return now }
	t.Cleanup(func() { rateLimitNow = old })
	t.Setenv("PR_GATE_DEADLINE", strconv.FormatInt(now.Add(budget).Unix(), 10))
	waits = recordReadWaits(t)
	attempts = fakeGH(t, fmt.Sprintf(`if [ "$2" = rate_limit ]; then echo '{"resources":{"core":{"remaining":%d,"reset":%d}}}'; exit 0; fi
if [ "$n" -le %d ]; then echo 'gh: API rate limit exceeded for installation. (HTTP 403)' >&2; exit 1; fi
echo '{"ok":true}'`, remaining, now.Add(resetIn).Unix(), failures+1))
	return attempts, waits, now
}

func TestGitHubReadWaitsForRateLimitResetWhenItFitsTheBudget(t *testing.T) {
	// n counts every gh call, so the rate_limit read is call 2 and the retry is call 3.
	attempts, waits, _ := rateLimitFixture(t, 0, 120*time.Second, 10*time.Minute, 1)
	var result struct {
		OK bool `json:"ok"`
	}
	if err := retryingGH.read(context.Background(), "repos/Hikyo-Org/Hikyo", &result); err != nil || !result.OK {
		t.Fatalf("read did not recover after the reset: %v %+v", err, result)
	}
	if want := []time.Duration{120*time.Second + rateLimitSkew}; len(*waits) != 1 || (*waits)[0] != want[0] || attempts() != 3 {
		t.Fatalf("waits=%v attempts=%d, want one wait of %v then the retried read", *waits, attempts(), want)
	}
}

func TestGitHubReadFailsVisiblyWhenRateLimitResetExceedsTheBudget(t *testing.T) {
	attempts, waits, now := rateLimitFixture(t, 0, 20*time.Minute, 2*time.Minute, 5)
	var result map[string]any
	err := retryingGH.read(context.Background(), "repos/Hikyo-Org/Hikyo", &result)
	reset := now.Add(20 * time.Minute).UTC().Format(time.RFC3339)
	var failure *githubReadError
	if err == nil || !strings.Contains(err.Error(), "rate limit exhausted until "+reset) || !errors.As(err, &failure) || failure.status != 403 {
		t.Fatalf("failure must name the rate limit and its reset time and keep the status: %v", err)
	}
	if strings.Contains(err.Error(), "installation") {
		t.Fatalf("diagnostic exposed gh stderr text: %v", err)
	}
	if len(*waits) != 0 || attempts() != 2 {
		t.Fatalf("waits=%v attempts=%d, want no wait and no retry", *waits, attempts())
	}
}

func TestGitHubReadDoesNotWaitWithoutAKnownBudget(t *testing.T) {
	_, waits, _ := rateLimitFixture(t, 0, time.Second, time.Hour, 5)
	t.Setenv("PR_GATE_DEADLINE", "")
	var result map[string]any
	if err := retryingGH.read(context.Background(), "repos/Hikyo-Org/Hikyo", &result); err == nil || !strings.Contains(err.Error(), "rate limit exhausted until") || len(*waits) != 0 {
		t.Fatalf("an unbounded job must not sleep on a reset: err=%v waits=%v", err, *waits)
	}
}

func TestGitHubReadDoesNotWaitOnASecondaryLimit(t *testing.T) {
	// The core budget has requests left, so a reset wait would not help.
	attempts, waits, _ := rateLimitFixture(t, 4000, time.Minute, time.Hour, 5)
	var result map[string]any
	err := retryingGH.read(context.Background(), "repos/Hikyo-Org/Hikyo", &result)
	if err == nil || strings.Contains(err.Error(), "exhausted") || len(*waits) != 0 || attempts() != 2 {
		t.Fatalf("a 403 with core budget left must stay terminal: err=%v waits=%v attempts=%d", err, *waits, attempts())
	}
}

func TestGitHubReadWaitsForTheResetOnlyOnce(t *testing.T) {
	// Still limited after the wait: report it instead of sleeping again.
	attempts, waits, _ := rateLimitFixture(t, 0, 30*time.Second, 10*time.Minute, 5)
	var result map[string]any
	if err := retryingGH.read(context.Background(), "repos/Hikyo-Org/Hikyo", &result); err == nil || len(*waits) != 1 || attempts() != 3 {
		t.Fatalf("err=%v waits=%v attempts=%d, want one wait then a reported failure", err, *waits, attempts())
	}
}
