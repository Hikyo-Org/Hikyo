package main

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"
)

// GitHub answers an exhausted GraphQL limit in two shapes (docs: primary is HTTP 200;
// secondary is 200 or 403): an errors entry in a 200 body, which gh reports as a
// nonzero exit with the body on stdout, or a 403 whose stderr names the rate limit.
func TestGitHubGraphQLRateLimitShapesWaitForTheGraphQLReset(t *testing.T) {
	for name, failure := range map[string]string{
		"200 with errors type RATE_LIMITED": `echo '{"data":null,"errors":[{"type":"RATE_LIMITED","message":"API rate limit already exceeded for installation ID 1."}]}'; exit 1`,
		"200 with a rate limit message":     `echo '{"data":null,"errors":[{"message":"You have exceeded a secondary rate limit."}]}'; exit 1`,
		"403 with the rate limit text":      `echo '{"message":"API rate limit exceeded","status":"403"}'; echo 'gh: API rate limit exceeded for installation. (HTTP 403)' >&2; exit 1`,
	} {
		t.Run(name, func(t *testing.T) {
			now := time.Unix(1_800_000_000, 0)
			old := rateLimitNow
			rateLimitNow = func() time.Time { return now }
			t.Cleanup(func() { rateLimitNow = old })
			t.Setenv("PR_GATE_DEADLINE", strconv.FormatInt(now.Add(10*time.Minute).Unix(), 10))
			waits := recordReadWaits(t)
			attempts := fakeGH(t, fmt.Sprintf(`if [ "$2" = rate_limit ]; then echo '{"resources":{"core":{"remaining":4000,"reset":%d},"graphql":{"remaining":0,"reset":%d}}}'; exit 0; fi
if [ "$n" -le 1 ]; then %s; fi
echo '{"data":{"ok":true}}'`, now.Unix()+60, now.Unix()+120, failure))
			var result map[string]any
			if err := retryingGH.read(context.Background(), "graphql:query{repository{name}}", &result); err != nil {
				t.Fatalf("graphql read did not recover after the reset: %v", err)
			}
			if want := 120*time.Second + rateLimitSkew; len(*waits) != 1 || (*waits)[0] != want || attempts() != 3 {
				t.Fatalf("waits=%v attempts=%d, want one wait of %v for the graphql bucket then the retried read", *waits, attempts(), want)
			}
		})
	}
}

// A GraphQL error that is not about the rate limit stays terminal and is never waited on.
func TestGitHubGraphQLOtherErrorsDoNotWait(t *testing.T) {
	waits := recordReadWaits(t)
	attempts := fakeGH(t, `echo '{"data":null,"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository"}]}'; exit 1`)
	var result map[string]any
	if err := retryingGH.read(context.Background(), "graphql:query{repository{name}}", &result); err == nil || len(*waits) != 0 || attempts() != 1 {
		t.Fatalf("err=%v waits=%v attempts=%d", err, *waits, attempts())
	}
}
