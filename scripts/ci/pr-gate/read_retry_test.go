package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Existing fake-gh tests exercise persistent transient failures; keep them instant.
func TestMain(m *testing.M) {
	readRetryWait = func(context.Context, time.Duration) error { return nil }
	// A fake clock keeps group re-reads from ever sleeping through their window.
	(&groupClock{now: time.Unix(1_800_000_000, 0)}).install()
	os.Exit(m.Run())
}

func recordReadWaits(t *testing.T, backoff ...time.Duration) *[]time.Duration {
	t.Helper()
	oldWait, oldBackoff := readRetryWait, readRetryBackoff
	waits := &[]time.Duration{}
	readRetryWait = func(_ context.Context, delay time.Duration) error {
		*waits = append(*waits, delay)
		return nil
	}
	if len(backoff) != 0 {
		readRetryBackoff = backoff
	}
	t.Cleanup(func() { readRetryWait, readRetryBackoff = oldWait, oldBackoff })
	return waits
}

// fakeGH runs script after setting $n to the 1-based invocation count and
// returns a function reporting how many times gh ran.
func fakeGH(t *testing.T, script string) func() int {
	t.Helper()
	tools := t.TempDir()
	counter := filepath.Join(tools, "count")
	body := "#!/bin/sh\necho x >> '" + counter + "'\nn=$(wc -l < '" + counter + "' | tr -d ' ')\n" + script + "\n"
	if err := os.WriteFile(filepath.Join(tools, "gh"), []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+os.Getenv("PATH"))
	return func() int {
		data, err := os.ReadFile(counter)
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "\n")
	}
}

// The queue publisher's reader: retries are opt-in, never part of ghAPI itself.
var retryingGH = retryingAPI{api: ghAPI{}}

func TestGitHubReadRetriesTransientFailureThenSucceeds(t *testing.T) {
	for _, path := range []string{"repos/Hikyo-Org/Hikyo/actions/runs/1/attempts/1/jobs?per_page=100&page=1", "graphql:query{repository{name}}"} {
		t.Run(path, func(t *testing.T) {
			waits := recordReadWaits(t)
			attempts := fakeGH(t, `if [ "$n" = 1 ]; then echo '<html>bad gateway</html>'; echo 'gh: HTTP 502' >&2; exit 1; fi
echo '{"ok":true}'`)
			var result struct {
				OK bool `json:"ok"`
			}
			if err := retryingGH.read(context.Background(), path, &result); err != nil || !result.OK {
				t.Fatalf("transient read was not retried to success: %v %+v", err, result)
			}
			if attempts() != 2 || len(*waits) != 1 || (*waits)[0] != time.Second {
				t.Fatalf("attempts=%d waits=%v, want 2 attempts after one 1s backoff", attempts(), *waits)
			}
		})
	}
}

func TestGitHubReadGivesUpAfterThreeAttempts(t *testing.T) {
	waits := recordReadWaits(t)
	attempts := fakeGH(t, `echo 'gh: Service Unavailable (HTTP 503)' >&2; exit 1`)
	var result map[string]any
	err := retryingGH.read(context.Background(), "repos/Hikyo-Org/Hikyo", &result)
	var failure *githubReadError
	if !errors.As(err, &failure) || failure.status != 503 || !strings.Contains(err.Error(), "HTTP 503") {
		t.Fatalf("persistent transient failure lost its status: %v", err)
	}
	if strings.Contains(err.Error(), "Service Unavailable") {
		t.Fatalf("diagnostic exposed gh stderr text: %v", err)
	}
	if attempts() != 3 || len(*waits) != 2 || (*waits)[0] != time.Second || (*waits)[1] != 3*time.Second {
		t.Fatalf("attempts=%d waits=%v, want 3 attempts with 1s then 3s backoff", attempts(), *waits)
	}
}

func TestGitHubReadDoesNotRetryTerminalFailures(t *testing.T) {
	for _, tc := range []struct{ name, script, path string }{
		{"404 from stderr", `echo 'gh: Not Found (HTTP 404)' >&2; exit 1`, "repos/Hikyo-Org/Hikyo/git/ref/heads/group"},
		{"404 from body", `echo '{"status":"404"}'; echo 'gh: HTTP 502' >&2; exit 1`, "repos/Hikyo-Org/Hikyo/git/ref/heads/group"},
		{"403", `echo 'gh: Forbidden (HTTP 403)' >&2; exit 1`, "repos/Hikyo-Org/Hikyo"},
		{"GraphQL error response", `echo '{"errors":[{"message":"boom"}]}'; exit 1`, "graphql:query{repository{name}}"},
		{"malformed success", `echo '{"ok":'`, "repos/Hikyo-Org/Hikyo"},
		{"gh authentication required (exit 4)", `echo 'To get started with GitHub CLI, please run:  gh auth login' >&2; exit 4`, "repos/Hikyo-Org/Hikyo"},
		{"gh cancelled (exit 2)", `exit 2`, "repos/Hikyo-Org/Hikyo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			waits := recordReadWaits(t)
			attempts := fakeGH(t, tc.script)
			var result map[string]any
			if err := retryingGH.read(context.Background(), tc.path, &result); err == nil {
				t.Fatal("terminal failure reported success")
			}
			if attempts() != 1 || len(*waits) != 0 {
				t.Fatalf("attempts=%d waits=%v, want a single attempt", attempts(), *waits)
			}
		})
	}
}

func TestPlainGHAPIReadIsSingleAttempt(t *testing.T) {
	waits := recordReadWaits(t)
	attempts := fakeGH(t, `if [ "$n" = 1 ]; then echo 'gh: HTTP 502' >&2; exit 1; fi
echo '{"ok":true}'`)
	var result map[string]any
	err := (ghAPI{}).read(context.Background(), "repos/Hikyo-Org/Hikyo", &result)
	var failure *githubReadError
	if !errors.As(err, &failure) || failure.status != 502 || attempts() != 1 || len(*waits) != 0 {
		t.Fatalf("plain reader retried (attempts=%d waits=%v): %v", attempts(), *waits, err)
	}
}

// retirement_discovery_scan.go promises one attempt per request, no retries or
// backoff, and counts requests on that basis. A retrying API must not leak in.
func TestRetirementDiscoveryReadsStaySingleAttempt(t *testing.T) {
	inventory := "repos/Hikyo-Org/Hikyo/commits/" + strings.Repeat("a", 40) + "/check-runs?check_name=ci-required&filter=all&per_page=100&page=1"
	for name, wired := range map[string]api{"plain": ghAPI{}, "retrying": retryingGH} {
		for _, path := range []string{inventory, "repos/Hikyo-Org/Hikyo/git/ref/heads/main"} {
			t.Run(name+" "+path[strings.LastIndex(path, "/")+1:], func(t *testing.T) {
				waits := recordReadWaits(t)
				attempts := fakeGH(t, `if [ "$n" = 1 ]; then echo 'gh: Bad Gateway (HTTP 502)' >&2; exit 1; fi
echo '{"ok":true}'`)
				scan := newRetirementDiscoveryScan()
				var result map[string]any
				err := retirementDiscoveryReadAPI{api: wired, scan: scan}.read(context.Background(), path, &result)
				var failure *githubReadError
				if !errors.As(err, &failure) || failure.status != 502 {
					t.Fatalf("failed discovery read was not reported as is: %v", err)
				}
				wantRequests := int64(0)
				if path == inventory {
					wantRequests = 1
				}
				if attempts() != 1 || len(*waits) != 0 || scan.restRequests.Load() != wantRequests || scan.restPages.Load() != 0 {
					t.Fatalf("attempts=%d waits=%v counted requests=%d pages=%d, want one attempt and %d counted", attempts(), *waits, scan.restRequests.Load(), scan.restPages.Load(), wantRequests)
				}
			})
		}
	}
}

func TestRetirementDiscoveryCountsEveryGHInvocation(t *testing.T) {
	recordReadWaits(t)
	attempts := fakeGH(t, `echo '{"ok":true}'`)
	scan := newRetirementDiscoveryScan()
	reader := retirementDiscoveryReadAPI{api: retryingGH, scan: scan}
	for page := 1; page <= 3; page++ {
		var result map[string]any
		path := fmt.Sprintf("repos/Hikyo-Org/Hikyo/commits/%s/check-runs?check_name=ci&page=%d", strings.Repeat("b", 40), page)
		if err := reader.read(context.Background(), path, &result); err != nil {
			t.Fatal(err)
		}
	}
	if attempts() != 3 || scan.restRequests.Load() != 3 || scan.restPages.Load() != 3 {
		t.Fatalf("gh ran %d times; counted %d requests and %d pages", attempts(), scan.restRequests.Load(), scan.restPages.Load())
	}
}

// Only the queue publisher's proof paths retry. Broad inventories (dispatch and
// retirement discovery) and the legacy gate keep single attempts.
func TestControllerAPIRetriesOnlyQueuePublisher(t *testing.T) {
	for _, tc := range []struct {
		name            string
		queue, dispatch bool
		retries         bool
	}{
		{"queue publisher", true, false, true},
		{"queue worker discovery", true, true, false},
		{"retirement, lifecycle and legacy gate", false, false, false},
	} {
		_, retries := controllerAPI(tc.queue, tc.dispatch).(retryingAPI)
		if retries != tc.retries {
			t.Errorf("%s: retrying=%t, want %t", tc.name, retries, tc.retries)
		}
	}
}

func TestGitHubReadKeeps404Recognizable(t *testing.T) {
	recordReadWaits(t)
	fakeGH(t, `echo 'gh: Not Found (HTTP 404)' >&2; exit 1`)
	var result map[string]any
	err := retryingGH.read(context.Background(), "repos/Hikyo-Org/Hikyo/git/ref/heads/group", &result)
	var failure *githubReadError
	if !errors.As(err, &failure) || failure.status != 404 || transientGitHubRead(err) {
		t.Fatalf("404 lost its non-transient status: %v", err)
	}
}

func TestGitHubReadNeverSleepsPastContextDeadline(t *testing.T) {
	waits := recordReadWaits(t, time.Hour)
	attempts := fakeGH(t, `echo 'gh: HTTP 502' >&2; exit 1`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var result map[string]any
	err := retryingGH.read(ctx, "repos/Hikyo-Org/Hikyo", &result)
	var failure *githubReadError
	if !errors.As(err, &failure) || failure.status != 502 {
		t.Fatalf("last transient error was not returned: %v", err)
	}
	if attempts() != 1 || len(*waits) != 0 {
		t.Fatalf("attempts=%d waits=%v, want immediate return when backoff exceeds the deadline", attempts(), *waits)
	}
}

func TestGitHubReadChecksDeadlineBeforeEachBackoff(t *testing.T) {
	waits := recordReadWaits(t, 10*time.Millisecond, time.Hour)
	attempts := fakeGH(t, `echo 'gh: HTTP 504' >&2; exit 1`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var result map[string]any
	if err := retryingGH.read(ctx, "repos/Hikyo-Org/Hikyo", &result); err == nil {
		t.Fatal("persistent failure reported success")
	}
	if attempts() != 2 || len(*waits) != 1 || (*waits)[0] != 10*time.Millisecond {
		t.Fatalf("attempts=%d waits=%v, want one short backoff then stop before the oversized one", attempts(), *waits)
	}
}

func TestGitHubReadStopsWhenContextEndsDuringBackoff(t *testing.T) {
	recordReadWaits(t)
	readRetryWait = func(context.Context, time.Duration) error { return context.Canceled }
	attempts := fakeGH(t, `echo 'gh: HTTP 502' >&2; exit 1`)
	var result map[string]any
	err := retryingGH.read(context.Background(), "repos/Hikyo-Org/Hikyo", &result)
	if !errors.Is(err, context.Canceled) || transientGitHubRead(err) || attempts() != 1 {
		t.Fatalf("cancellation during backoff was concealed or retried (attempts=%d): %v", attempts(), err)
	}
}

func TestGitHubReadWaitHonorsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitForReadRetry(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("backoff ignored cancellation: %v", err)
	}
	if err := waitForReadRetry(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("elapsed backoff failed: %v", err)
	}
}

func TestGitHubWriteNeverRetries(t *testing.T) {
	waits := recordReadWaits(t)
	attempts := fakeGH(t, `echo 'gh: HTTP 502' >&2; exit 1`)
	var receipt map[string]any
	err := retryingGH.write(context.Background(), "POST", "repos/Hikyo-Org/Hikyo/issues/1/comments", map[string]string{"body": "x"}, &receipt)
	if err == nil || !strings.Contains(err.Error(), "no mutation retry attempted") {
		t.Fatalf("write failure was not terminal: %v", err)
	}
	if attempts() != 1 || len(*waits) != 0 {
		t.Fatalf("attempts=%d waits=%v, write must never retry", attempts(), *waits)
	}
}

func TestStderrHTTPStatus(t *testing.T) {
	for _, tc := range []struct {
		name, stderr string
		want         int
	}{
		{"non-JSON body", "gh: HTTP 502\n", 502},
		{"JSON error message", "gh: Server Error (HTTP 502)\n", 502},
		{"not found", "gh: Not Found (HTTP 404)\n", 404},
		{"no trailing newline", "gh: Bad Gateway (HTTP 502)", 502},
		{"hint lines after status", "gh: Forbidden (HTTP 403)\nTry authenticating with: gh auth login\n", 403},
		{"last gh line wins", "gh: first (HTTP 404)\ngh: Bad Gateway (HTTP 502)\n", 502},
		{"last gh line wins over a later line without the prefix", "gh: says (HTTP 404)\nBad Gateway (HTTP 502)\n", 404},
		{"unprefixed parenthesised status is ignored", "upstream said Bad Gateway (HTTP 502)\n", 0},
		{"quoted body line before gh status", "<html>(HTTP 404)</html>\ngh: HTTP 502\n", 502},
		{"mid-line text is ignored", "gh: refused (HTTP 404) by upstream\n", 0},
		{"success status is ignored", "gh: HTTP 200\n", 0},
		{"transport error", "error connecting to api.github.com\n", 0},
		{"empty", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := stderrHTTPStatus([]byte(tc.stderr)); got != tc.want {
				t.Fatalf("stderrHTTPStatus(%q) = %d, want %d", tc.stderr, got, tc.want)
			}
		})
	}
}

func TestTransientGitHubRead(t *testing.T) {
	var exit error = exec.Command("sh", "-c", "exit 1").Run()
	if exit == nil {
		t.Fatal("expected a command failure fixture")
	}
	exitWith := func(code int) error {
		err := exec.Command("sh", "-c", fmt.Sprintf("exit %d", code)).Run()
		var failure *exec.ExitError
		if !errors.As(err, &failure) || failure.ExitCode() != code {
			t.Fatalf("expected exit status %d, got %v", code, err)
		}
		return err
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("network unavailable"), false},
		{"decode error", fmt.Errorf("invalid GitHub JSON: %w", errors.New("EOF")), false},
		{"no cause and no status", &githubReadError{path: "p"}, false},
		{"transport failure", &githubReadError{path: "p", cause: exit}, true},
		{"wrapped transport failure", fmt.Errorf("outer: %w", &githubReadError{path: "p", cause: exit}), true},
		{"deadline", &githubReadError{path: "p", cause: context.DeadlineExceeded}, false},
		{"cancelled", &githubReadError{path: "p", cause: context.Canceled}, false},
		{"GraphQL error response", fmt.Errorf("boom: %w", &githubReadError{path: "p", cause: exit, rejected: true}), false},
		// gh documents these exit codes as final; anything else without a status stays transport trouble.
		{"gh exit 2 (cancelled)", &githubReadError{path: "p", cause: exitWith(2)}, false},
		{"gh exit 4 (authentication required)", &githubReadError{path: "p", cause: exitWith(4)}, false},
		{"wrapped gh exit 4", fmt.Errorf("outer: %w", &githubReadError{path: "p", cause: exitWith(4)}), false},
		{"gh exit 3", &githubReadError{path: "p", cause: exitWith(3)}, true},
		{"gh exit 8", &githubReadError{path: "p", cause: exitWith(8)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := transientGitHubRead(tc.err); got != tc.want {
				t.Fatalf("transientGitHubRead(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
	for _, status := range []int{400, 401, 403, 404, 409, 410, 422, 501, 505} {
		if transientGitHubRead(&githubReadError{path: "p", status: status, cause: exit}) {
			t.Errorf("HTTP %d must not be transient", status)
		}
	}
	for _, status := range []int{429, 500, 502, 503, 504} {
		if !transientGitHubRead(&githubReadError{path: "p", status: status, cause: exit}) {
			t.Errorf("HTTP %s must be transient", strconv.Itoa(status))
		}
	}
}
