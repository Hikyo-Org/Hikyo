package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func releasePageResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func releasePageBounds(t *testing.T, request *http.Request) (int, int) {
	t.Helper()
	if request.URL.Scheme != "https" || request.URL.Host != "api.github.com" || request.URL.Path != "/repos/Hikyo-Org/Hikyo/releases" {
		t.Fatalf("untrusted release request: %s", request.URL)
	}
	size, err := strconv.Atoi(request.URL.Query().Get("per_page"))
	if err != nil || size < 1 || size > 20 {
		t.Fatalf("unexpected page size: %s", request.URL)
	}
	page, err := strconv.Atoi(request.URL.Query().Get("page"))
	if err != nil || page < 1 {
		t.Fatalf("unexpected page number: %s", request.URL)
	}
	return size, (page - 1) * size
}

func releasePageBody(t *testing.T, start, count int) string {
	t.Helper()
	records := make([]githubRelease, count)
	for i := range records {
		records[i] = githubRelease{
			TagName:    fmt.Sprintf("v1.1.0-nightly.20261030.%d.gbbbbbbbb", start+i+1),
			Prerelease: true, PublishedAt: "2026-10-30T02:00:00Z",
		}
	}
	body, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestGitHubSourcePaginatesSameHundredReleaseWindow(t *testing.T) {
	var starts []int
	stableCalls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() == githubLatestReleaseURL {
			stableCalls++
			return releasePageResponse(`{"tag_name":"v1.0.1","published_at":"2026-01-01T00:00:00Z"}`), nil
		}
		size, start := releasePageBounds(t, request)
		if start >= 100 {
			t.Fatalf("read beyond the existing release window: %s", request.URL)
		}
		starts = append(starts, start)
		response := releasePageResponse(releasePageBody(t, start, size))
		// Never follow a response-supplied URL, including a purported next page.
		response.Header.Set("Link", `<https://attacker.invalid/releases?page=2>; rel="next"`)
		return response, nil
	})}
	releases, err := NewGitHubSource(client).Releases(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 101 || stableCalls != 1 || fmt.Sprint(starts) != "[0 20 40 60 80]" {
		t.Fatalf("releases=%d stable=%d starts=%v", len(releases), stableCalls, starts)
	}
	for i, release := range releases[:100] {
		want := fmt.Sprintf("1.1.0-nightly.20261030.%d.gbbbbbbbb", i+1)
		if release.Version != want {
			t.Fatalf("release %d = %s, want %s", i, release.Version, want)
		}
	}
	for _, tc := range []struct {
		channel Channel
		latest  string
	}{
		{ChannelNightly, "1.1.0-nightly.20261030.100.gbbbbbbbb"},
		{ChannelStable, "1.0.1"},
	} {
		status, err := Select("1.0.0", tc.channel, releases)
		if err != nil || !status.Available || status.LatestVersion != tc.latest {
			t.Fatalf("%s status = %+v, error = %v", tc.channel, status, err)
		}
	}
}

func TestGitHubSourceShrinksOversizedPagesWithoutSkipping(t *testing.T) {
	for _, shrinkAt := range []int{0, 40} {
		t.Run(fmt.Sprint(shrinkAt), func(t *testing.T) {
			calls, next, oversized := 0, 0, 0
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if request.URL.String() == githubLatestReleaseURL {
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
				}
				size, start := releasePageBounds(t, request)
				if start != next {
					t.Fatalf("next offset = %d, want %d: %s", start, next, request.URL)
				}
				if start == shrinkAt && size > 1 {
					oversized++
					return releasePageResponse(strings.Repeat(" ", (2<<20)+1)), nil
				}
				next += size
				return releasePageResponse(releasePageBody(t, start, size)), nil
			})}
			releases, err := NewGitHubSource(client).Releases(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(releases) != 100 || next != 100 || oversized != 3 || calls > 104 {
				t.Fatalf("releases=%d next=%d oversized=%d calls=%d", len(releases), next, oversized, calls)
			}
			if shrinkAt == 0 && calls != 104 {
				t.Fatalf("worst-case request count = %d, want 104", calls)
			}
			for i, release := range releases {
				if release.Version != fmt.Sprintf("1.1.0-nightly.20261030.%d.gbbbbbbbb", i+1) {
					t.Fatalf("release %d = %s", i, release.Version)
				}
			}
		})
	}
}

func TestGitHubSourceStopsOnShortOrEmptyPage(t *testing.T) {
	for _, count := range []int{0, 7, 20, 47} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			listCalls := 0
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() == githubLatestReleaseURL {
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
				}
				listCalls++
				size, start := releasePageBounds(t, request)
				if start > count {
					t.Fatalf("read beyond end: %s", request.URL)
				}
				return releasePageResponse(releasePageBody(t, start, min(size, count-start))), nil
			})}
			releases, err := NewGitHubSource(client).Releases(t.Context())
			if err != nil || len(releases) != count || listCalls != count/20+1 {
				t.Fatalf("releases=%d listCalls=%d error=%v", len(releases), listCalls, err)
			}
		})
	}
}

func TestGitHubSourceDoesNotRetryOtherPageFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
	}{
		{"rate limit", strings.Repeat("x", (2<<20)+1), "application/json", http.StatusTooManyRequests},
		{"invalid JSON", "{", "application/json", http.StatusOK},
		{"invalid content type", "[]", "text/html", http.StatusOK},
		{"too many records", releasePageBody(t, 0, 21), "application/json", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				response := releasePageResponse(tc.body)
				response.StatusCode = tc.status
				response.Header.Set("Content-Type", tc.contentType)
				return response, nil
			})}
			releases, err := NewGitHubSource(client).Releases(t.Context())
			if err == nil || releases != nil || calls != 1 {
				t.Fatalf("releases=%d calls=%d error=%v", len(releases), calls, err)
			}
		})
	}
}

func TestGitHubSourceSharesOneDiscoveryDeadline(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		clientTimeout, budget time.Duration
	}{
		{"unlimited client", 0, 30 * time.Second},
		{"long client", time.Minute, 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var deadline time.Time
			client := &http.Client{Timeout: tc.clientTimeout, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				got, ok := request.Context().Deadline()
				if !ok || time.Until(got) > tc.budget {
					t.Fatal("missing bounded discovery deadline")
				}
				if deadline.IsZero() {
					deadline = got
				}
				if got.After(deadline) {
					t.Fatal("pagination reset the discovery deadline")
				}
				if request.URL.String() == githubLatestReleaseURL {
					return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
				}
				size, start := releasePageBounds(t, request)
				return releasePageResponse(releasePageBody(t, start, min(size, 21-start))), nil
			})}
			if _, err := NewGitHubSource(client).Releases(t.Context()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGitHubSourcePaginationKeepsOriginalTimeoutAllowance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		client := &http.Client{Timeout: time.Second, Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			calls++
			select {
			case <-time.After(750 * time.Millisecond):
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
			if request.URL.String() == githubLatestReleaseURL {
				return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}
			size, start := releasePageBounds(t, request)
			return releasePageResponse(releasePageBody(t, start, min(size, 21-start))), nil
		})}
		started := time.Now()
		releases, err := NewGitHubSource(client).Releases(t.Context())
		if !errors.Is(err, context.DeadlineExceeded) || releases != nil || calls != 3 || time.Since(started) != 2*time.Second {
			t.Fatalf("releases=%d calls=%d elapsed=%s error=%v", len(releases), calls, time.Since(started), err)
		}
	})
}

type trackedReleaseBody struct {
	io.Reader
	bytes  *int
	closed bool
}

func (b *trackedReleaseBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	*b.bytes += n
	return n, err
}

func (b *trackedReleaseBody) Close() error {
	b.closed = true
	return nil
}

func TestGitHubSourceBoundsAggregateReadsAndClosesEveryBody(t *testing.T) {
	bytes, calls := 0, 0
	var bodies []*trackedReleaseBody
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.URL.String() == githubLatestReleaseURL {
			t.Fatal("partial discovery must not proceed to stable lookup")
		}
		size, start := releasePageBounds(t, request)
		body := strings.Repeat(" ", (2<<20)+1)
		if size <= 5 {
			body = releasePageBody(t, start, size)
			body += strings.Repeat(" ", (2<<20)-1-len(body))
		}
		tracked := &trackedReleaseBody{Reader: strings.NewReader(body), bytes: &bytes}
		bodies = append(bodies, tracked)
		response := releasePageResponse("")
		response.Body = tracked
		return response, nil
	})}
	releases, err := NewGitHubSource(client).Releases(t.Context())
	if err == nil || !strings.Contains(err.Error(), "byte budget") || releases != nil {
		t.Fatalf("releases=%d bytes=%d calls=%d error=%v", len(releases), bytes, calls, err)
	}
	if bytes > (16<<20)+1 || calls > 10 {
		t.Fatalf("discovery exceeded aggregate bounds: bytes=%d calls=%d", bytes, calls)
	}
	for i, body := range bodies {
		if !body.closed {
			t.Errorf("response body %d left open", i)
		}
	}
}

func TestGitHubSourceCancellationStopsAfterEarlierPage(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		size, start := releasePageBounds(t, request)
		cancel()
		return releasePageResponse(releasePageBody(t, start, size)), nil
	})}
	releases, err := NewGitHubSource(client).Releases(ctx)
	if !errors.Is(err, context.Canceled) || releases != nil || calls != 1 {
		t.Fatalf("releases=%d calls=%d error=%v", len(releases), calls, err)
	}
}

func TestGitHubSourceLaterPageFailureReturnsNoPartialResult(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls++
		size, start := releasePageBounds(t, request)
		if start == 0 {
			return releasePageResponse(releasePageBody(t, start, size)), nil
		}
		response := releasePageResponse(`{}`)
		response.StatusCode = http.StatusServiceUnavailable
		return response, nil
	})}
	releases, err := NewGitHubSource(client).Releases(t.Context())
	if err == nil || releases != nil || calls != 2 {
		t.Fatalf("releases=%d calls=%d error=%v", len(releases), calls, err)
	}
}

func TestGitHubSourceDoesNotRetryOversizedSingleRecords(t *testing.T) {
	for _, kind := range []string{"page", "stable", "exact"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				calls++
				if kind == "stable" && request.URL.String() != githubLatestReleaseURL {
					return releasePageResponse(`[]`), nil
				}
				return releasePageResponse(strings.Repeat(" ", (2<<20)+1)), nil
			})}
			source := NewGitHubSource(client)
			var err error
			if kind == "exact" {
				_, err = source.ReleaseByVersion(t.Context(), "1.0.0")
			} else {
				_, err = source.Releases(t.Context())
			}
			wantCalls := map[string]int{"page": 4, "stable": 2, "exact": 1}[kind]
			if !errors.Is(err, errReleaseResponseTooLarge) || calls != wantCalls {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
		})
	}
}
