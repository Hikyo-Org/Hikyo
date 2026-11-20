package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func passing() map[string]finding {
	report := map[string]finding{}
	for _, name := range requiredChecks {
		report[name] = finding{OK: true}
	}
	return report
}

func TestVerdict(t *testing.T) {
	bearer := "Bearer " + fakeToken
	if got := verdict(passing(), bearer); len(got) != 0 {
		t.Fatalf("a passing report failed: %v", got)
	}
	missing := passing()
	delete(missing, requiredChecks[0])
	failed := passing()
	failed[requiredChecks[1]] = finding{OK: false, Detail: "boom"}
	extra := passing()
	extra["unlisted check"] = finding{OK: false, Detail: "also boom"}
	for name, tc := range map[string]struct {
		report map[string]finding
		auth   string
		want   string
	}{
		"missing check":    {missing, bearer, "not reported"},
		"failed check":     {failed, bearer, "boom"},
		"unlisted failure": {extra, bearer, "also boom"},
		"no bearer":        {passing(), "", "session bearer"},
		"wrong bearer":     {passing(), "Bearer other", "session bearer"},
		"empty report":     {nil, bearer, "not reported"},
	} {
		got := strings.Join(verdict(tc.report, tc.auth), "\n")
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s: failures %q lack %q", name, got, tc.want)
		}
	}
}

func TestInstanceKeepsTheFirstReportAndTheBearer(t *testing.T) {
	in := &instance{done: make(chan struct{})}
	srv := httptest.NewServer(in.handler())
	defer srv.Close()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/__probe/ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+fakeToken)
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("ping: %v %v", res, err)
	}
	for _, body := range []string{`{"app mode scheme":{"ok":true}}`, `{"app mode scheme":{"ok":false}}`} {
		res, err := post(t, srv.URL, body)
		if err != nil || res.StatusCode != http.StatusNoContent {
			t.Fatalf("report: %v %v", res, err)
		}
	}
	<-in.done
	if !in.report["app mode scheme"].OK || in.pingAuth != "Bearer "+fakeToken {
		t.Fatalf("report %+v, ping auth %q", in.report, in.pingAuth)
	}
	if res, err := post(t, srv.URL, "{"); err != nil || res.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed report: %v %v", res, err)
	}
}

func post(t *testing.T, base, body string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/__probe/report", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return http.DefaultClient.Do(req)
}

// An app that exits before the page reports must return its error, not block on
// the deferred wait.
func TestRunReturnsWhenTheAppExitsEarly(t *testing.T) {
	done := make(chan error, 1)
	go func() { done <- run("/usr/bin/false", time.Minute) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "exited before the page reported") {
			t.Fatalf("run = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("run blocked after the app exited")
	}
}
