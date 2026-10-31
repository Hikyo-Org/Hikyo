package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type retryProbe struct {
	waits []time.Duration
	log   bytes.Buffer
}

// stubRetry makes retries instant and records the waits and log lines.
func stubRetry(t *testing.T) *retryProbe {
	t.Helper()
	probe := &retryProbe{}
	oldWait, oldLog := retryWait, retryLog
	retryWait = func(_ context.Context, d time.Duration) error {
		probe.waits = append(probe.waits, d)
		return nil
	}
	retryLog = &probe.log
	t.Cleanup(func() { retryWait, retryLog = oldWait, oldLog })
	return probe
}

func respondStatus(code int, headers ...string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		for i := 0; i+1 < len(headers); i += 2 {
			w.Header().Set(headers[i], headers[i+1])
		}
		http.Error(w, "injected", code)
	}
}

func respondOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// respondDrop aborts the connection before any response.
func respondDrop(http.ResponseWriter) { panic(http.ErrAbortHandler) }

// stepClient serves the steps in order, repeating the last one.
func stepClient(t *testing.T, steps ...func(http.ResponseWriter)) (client, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		step := min(int(hits.Add(1))-1, len(steps)-1)
		steps[step](w)
	}))
	t.Cleanup(server.Close)
	return client{http: server.Client(), base: server.URL, token: "test", repo: "owner/repo"}, &hits
}

func TestReadRetriesTransientFailureThenSucceeds(t *testing.T) {
	for _, code := range []int{429, 500, 502, 503, 504} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			probe := stubRetry(t)
			c, hits := stepClient(t, respondStatus(code), respondOK)
			var out map[string]bool
			if err := c.read(context.Background(), "actions/runs/1/jobs", &out); err != nil {
				t.Fatal(err)
			}
			if !out["ok"] || hits.Load() != 2 {
				t.Fatalf("ok=%v hits=%d", out["ok"], hits.Load())
			}
			if len(probe.waits) != 1 || probe.waits[0] != time.Second {
				t.Fatalf("waits = %v", probe.waits)
			}
			if got := probe.log.String(); strings.Count(got, "\n") != 1 || !strings.Contains(got, "HTTP "+strconv.Itoa(code)) {
				t.Fatalf("retry log = %q", got)
			}
		})
	}
}

func TestPersistentTransientFailureStopsAfterThreeAttempts(t *testing.T) {
	probe := stubRetry(t)
	c, hits := stepClient(t, respondStatus(503))
	err := c.read(context.Background(), "actions/runs/1/jobs?per_page=100", nil)
	if err == nil || err.Error() != "GitHub GET actions/runs/1/jobs?per_page=100: HTTP 503" {
		t.Fatalf("err = %v", err)
	}
	if hits.Load() != 3 {
		t.Fatalf("hits = %d", hits.Load())
	}
	if len(probe.waits) != 2 || probe.waits[0] != time.Second || probe.waits[1] != 3*time.Second {
		t.Fatalf("waits = %v", probe.waits)
	}
	if strings.Count(probe.log.String(), "\n") != 2 {
		t.Fatalf("retry log = %q", probe.log.String())
	}
}

func TestPermanentClientErrorsAreNotRetried(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 409, 422} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			probe := stubRetry(t)
			c, hits := stepClient(t, respondStatus(code), respondOK)
			err := c.read(context.Background(), "pulls/7", nil)
			if err == nil || !strings.Contains(err.Error(), "HTTP "+strconv.Itoa(code)) {
				t.Fatalf("err = %v", err)
			}
			if hits.Load() != 1 || len(probe.waits) != 0 || probe.log.Len() != 0 {
				t.Fatalf("hits=%d waits=%v log=%q", hits.Load(), probe.waits, probe.log.String())
			}
		})
	}
}

func TestWritesAreNeverRetried(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		for _, code := range []int{429, 500, 502, 503, 504} {
			t.Run(method+"/"+strconv.Itoa(code), func(t *testing.T) {
				probe := stubRetry(t)
				c, hits := stepClient(t, respondStatus(code, "Retry-After", "1"), respondOK)
				err := c.request(context.Background(), method, "check-runs", map[string]string{"name": "x"}, nil)
				if err == nil || !strings.Contains(err.Error(), "HTTP "+strconv.Itoa(code)) {
					t.Fatalf("err = %v", err)
				}
				if hits.Load() != 1 || len(probe.waits) != 0 || probe.log.Len() != 0 {
					t.Fatalf("hits=%d waits=%v log=%q", hits.Load(), probe.waits, probe.log.String())
				}
			})
		}
	}
	t.Run("transport drop", func(t *testing.T) {
		probe := stubRetry(t)
		c, hits := stepClient(t, respondDrop, respondOK)
		if err := c.request(context.Background(), http.MethodPatch, "check-runs/1", map[string]string{}, nil); err == nil {
			t.Fatal("dropped write reported success")
		}
		if hits.Load() != 1 || len(probe.waits) != 0 {
			t.Fatalf("hits=%d waits=%v", hits.Load(), probe.waits)
		}
	})
}

func TestPublishRecoversFromTransientReadButNotWriteFailure(t *testing.T) {
	jobs := "actions/runs/50/attempts/1/jobs"
	t.Run("read 502 then success", func(t *testing.T) {
		probe := stubRetry(t)
		f := newFixture(t)
		f.existing("in_progress")
		failed := false
		f.inject = func(method, path string) int {
			if method == http.MethodGet && path == jobs && !failed {
				failed = true
				return http.StatusBadGateway
			}
			return 0
		}
		if err := f.c.publish(context.Background(), f.e); err != nil {
			t.Fatal(err)
		}
		reads := 0
		for _, path := range f.requests {
			if path == jobs {
				reads++
			}
		}
		if reads != 2 || len(f.writes) != 1 || len(probe.waits) != 1 {
			t.Fatalf("job reads=%d writes=%d waits=%v", reads, len(f.writes), probe.waits)
		}
	})
	for _, existing := range []bool{true, false} {
		name := "create"
		if existing {
			name = "update"
		}
		t.Run("write 503 "+name, func(t *testing.T) {
			probe := stubRetry(t)
			f := newFixture(t)
			if existing {
				f.existing("in_progress")
			}
			f.inject = func(method, _ string) int {
				if method != http.MethodGet {
					return http.StatusServiceUnavailable
				}
				return 0
			}
			err := f.c.publish(context.Background(), f.e)
			if err == nil || !strings.Contains(err.Error(), "HTTP 503") {
				t.Fatalf("err = %v", err)
			}
			writes := 0
			for _, path := range f.requests {
				if strings.HasPrefix(path, "check-runs") {
					writes++
				}
			}
			if writes != 1 || len(probe.waits) != 0 {
				t.Fatalf("write requests=%d waits=%v", writes, probe.waits)
			}
		})
	}
}

func TestRetryNeverWaitsPastContextDeadline(t *testing.T) {
	t.Run("deadline shorter than first backoff", func(t *testing.T) {
		probe := stubRetry(t)
		c, hits := stepClient(t, respondStatus(503), respondOK)
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		err := c.read(ctx, "pulls/7", nil)
		if err == nil || !strings.Contains(err.Error(), "HTTP 503") {
			t.Fatalf("err = %v", err)
		}
		if hits.Load() != 1 || len(probe.waits) != 0 || probe.log.Len() != 0 {
			t.Fatalf("hits=%d waits=%v log=%q", hits.Load(), probe.waits, probe.log.String())
		}
	})
	t.Run("second backoff does not fit", func(t *testing.T) {
		probe := stubRetry(t)
		c, hits := stepClient(t, respondStatus(502))
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := c.read(ctx, "pulls/7", nil)
		if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
			t.Fatalf("err = %v", err)
		}
		if hits.Load() != 2 || len(probe.waits) != 1 || probe.waits[0] != time.Second {
			t.Fatalf("hits=%d waits=%v", hits.Load(), probe.waits)
		}
	})
	t.Run("wait interrupted by cancellation returns the request error", func(t *testing.T) {
		stubRetry(t)
		retryWait = func(context.Context, time.Duration) error { return context.Canceled }
		c, hits := stepClient(t, respondStatus(502), respondOK)
		err := c.read(context.Background(), "pulls/7", nil)
		if err == nil || !strings.Contains(err.Error(), "HTTP 502") || hits.Load() != 1 {
			t.Fatalf("err=%v hits=%d", err, hits.Load())
		}
	})
}

func TestRetryAfterIsHonouredWhenItFits(t *testing.T) {
	cases := []struct {
		name   string
		code   int
		header string
		want   time.Duration
	}{
		{"429 shorter than backoff cap", 429, "2", 2 * time.Second},
		{"503 at cap", 503, "10", 10 * time.Second},
		{"429 not a number", 429, "soon", time.Second},
		{"429 zero", 429, "0", time.Second},
		{"429 negative", 429, "-3", time.Second},
		{"500 ignored", 500, "2", time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := stubRetry(t)
			c, _ := stepClient(t, respondStatus(tc.code, "Retry-After", tc.header), respondOK)
			if err := c.read(context.Background(), "pulls/7", nil); err != nil {
				t.Fatal(err)
			}
			if len(probe.waits) != 1 || probe.waits[0] != tc.want {
				t.Fatalf("waits = %v, want [%s]", probe.waits, tc.want)
			}
		})
	}
}

// A Retry-After the reader cannot honour ends the retries: coming back sooner
// than the server asked would spend the request budget it is protecting.
func TestRetryAfterBeyondCapOrRemainingTimeStopsRetrying(t *testing.T) {
	cases := []struct {
		name    string
		code    int
		header  string
		timeout time.Duration
	}{
		{"503 over cap", 503, "11", 0},
		{"429 far over cap", 429, "3600", 0},
		{"429 beyond int32 seconds", 429, "99999999999", 0},
		{"429 longer than remaining time", 429, "5", 3 * time.Second},
		{"503 equal to remaining time", 503, "5", 5 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := stubRetry(t)
			c, hits := stepClient(t, respondStatus(tc.code, "Retry-After", tc.header), respondOK)
			ctx := context.Background()
			if tc.timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.timeout)
				defer cancel()
			}
			err := c.read(ctx, "pulls/7", nil)
			if err == nil || !strings.Contains(err.Error(), "HTTP "+strconv.Itoa(tc.code)) {
				t.Fatalf("err = %v", err)
			}
			if hits.Load() != 1 || len(probe.waits) != 0 || probe.log.Len() != 0 {
				t.Fatalf("hits=%d waits=%v log=%q, want the first error returned unretried", hits.Load(), probe.waits, probe.log.String())
			}
		})
	}
	t.Run("a later attempt can also decline", func(t *testing.T) {
		probe := stubRetry(t)
		c, hits := stepClient(t, respondStatus(503), respondStatus(429, "Retry-After", "30"), respondOK)
		err := c.read(context.Background(), "pulls/7", nil)
		if err == nil || !strings.Contains(err.Error(), "HTTP 429") || hits.Load() != 2 || len(probe.waits) != 1 || probe.waits[0] != time.Second {
			t.Fatalf("err=%v hits=%d waits=%v", err, hits.Load(), probe.waits)
		}
	})
}

func TestTransportErrorsAreRetriedOnReads(t *testing.T) {
	t.Run("connection dropped before response", func(t *testing.T) {
		probe := stubRetry(t)
		c, hits := stepClient(t, respondDrop, respondOK)
		var out map[string]bool
		if err := c.read(context.Background(), "pulls/7", &out); err != nil {
			t.Fatal(err)
		}
		if !out["ok"] || hits.Load() != 2 || len(probe.waits) != 1 {
			t.Fatalf("ok=%v hits=%d waits=%v", out["ok"], hits.Load(), probe.waits)
		}
	})
	t.Run("body cut short", func(t *testing.T) {
		probe := stubRetry(t)
		truncated := func(w http.ResponseWriter) {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte(`{"ok":`))
		}
		c, hits := stepClient(t, truncated, respondOK)
		var out map[string]bool
		if err := c.read(context.Background(), "pulls/7", &out); err != nil {
			t.Fatal(err)
		}
		if !out["ok"] || hits.Load() != 2 || len(probe.waits) != 1 {
			t.Fatalf("ok=%v hits=%d waits=%v", out["ok"], hits.Load(), probe.waits)
		}
	})
	t.Run("persistent drop gives up after three attempts", func(t *testing.T) {
		probe := stubRetry(t)
		c, hits := stepClient(t, respondDrop)
		if err := c.read(context.Background(), "pulls/7", nil); err == nil {
			t.Fatal("persistent drop reported success")
		}
		if hits.Load() != 3 || len(probe.waits) != 2 {
			t.Fatalf("hits=%d waits=%v", hits.Load(), probe.waits)
		}
	})
}

func TestNonTransientReadFailuresAreNotRetried(t *testing.T) {
	t.Run("cancelled context", func(t *testing.T) {
		probe := stubRetry(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var hits atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			cancel()
			// Let cancellation reach the transport before the handler returns.
			// Aborting here races that cancellation with a connection EOF.
			<-r.Context().Done()
		}))
		t.Cleanup(server.Close)
		c := client{http: server.Client(), base: server.URL, token: "test", repo: "owner/repo"}
		err := c.read(ctx, "pulls/7", nil)
		if !errors.Is(err, context.Canceled) || hits.Load() != 1 || len(probe.waits) != 0 {
			t.Fatalf("err=%v hits=%d waits=%v", err, hits.Load(), probe.waits)
		}
	})
	t.Run("redirect", func(t *testing.T) {
		probe := stubRetry(t)
		c, hits := stepClient(t, func(w http.ResponseWriter) {
			w.Header().Set("Location", "/elsewhere")
			w.WriteHeader(http.StatusFound)
		}, respondOK)
		c.http.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("unexpected GitHub API redirect") }
		err := c.read(context.Background(), "pulls/7", nil)
		if err == nil || !strings.Contains(err.Error(), "unexpected GitHub API redirect") || hits.Load() != 1 || len(probe.waits) != 0 {
			t.Fatalf("err=%v hits=%d waits=%v", err, hits.Load(), probe.waits)
		}
	})
	t.Run("malformed JSON", func(t *testing.T) {
		probe := stubRetry(t)
		c, hits := stepClient(t, func(w http.ResponseWriter) { _, _ = w.Write([]byte("not json")) }, respondOK)
		var out map[string]bool
		if err := c.read(context.Background(), "pulls/7", &out); err == nil || hits.Load() != 1 || len(probe.waits) != 0 {
			t.Fatalf("err=%v hits=%d waits=%v", err, hits.Load(), probe.waits)
		}
	})
}
