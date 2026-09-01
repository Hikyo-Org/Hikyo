package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Hikyo-Org/hikyo/api"
)

// A recovered panic must be counted as the 5xx it is: observe leads recoverPanics
// in the API stack, so the status recovery.go writes flows back through observe's
// writer. This is the interplay the ticket calls out by name.
func TestObserveCountsRecoveredPanicAs5xx(t *testing.T) {
	m := NewMetrics(nil)
	a := &API{Metrics: m}
	r := chi.NewRouter()
	r.Use(a.observe, a.recoverPanics)
	r.Get(api.PathPrefix+"/orgs/{org}/projects/{project}/values", func(http.ResponseWriter, *http.Request) {
		panic("invariant")
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + api.PathPrefix + "/orgs/o/projects/p/values")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if got := m.requests[classValues][status5xx]; got != 1 {
		t.Fatalf("values 5xx counter = %d, want 1", got)
	}
	// The +Inf-equivalent count and the histogram both saw the request.
	if got := m.counts[classValues]; got != 1 {
		t.Fatalf("values histogram count = %d, want 1", got)
	}
}

func TestObserveCountsPostCommitRecoveredPanicAs5xx(t *testing.T) {
	m := NewMetrics(nil)
	a := &API{Metrics: m}
	r := chi.NewRouter()
	r.Use(a.observe, a.recoverPanics)
	r.Get(api.PathPrefix+"/meta", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		panic("invariant after commit")
	})
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, api.PathPrefix+"/meta", nil))

	if recorder.Code != http.StatusTeapot {
		t.Fatalf("wire status = %d, want committed 418", recorder.Code)
	}
	if got := m.requests[classAuth][status5xx]; got != 1 {
		t.Fatalf("auth 5xx counter = %d, want recovered fault counted as 1", got)
	}
}

// The classifier is a closed map from templated route pattern to surface class.
// A raw path (with an ID) must land where its pattern would, never as its own
// class — the cardinality guarantee.
func TestClassifyIsAClosedSurfaceMap(t *testing.T) {
	cases := []struct {
		pattern string
		want    surfaceClass
	}{
		{api.PathPrefix + "/auth/local/login", classAuth},
		{api.PathPrefix + "/me/sessions", classAuth},
		{api.PathPrefix + "/meta", classAuth},
		{api.PathPrefix + "/accounts/{principal}/credential-reset", classAuth},
		{api.PathPrefix + "/orgs/{org}/projects/{project}/keys", classHierarchy},
		{api.PathPrefix + "/orgs/{org}/projects/{project}/values", classValues},
		{api.PathPrefix + "/orgs/{org}/projects/{project}/environments/{environment}/values", classValues},
		{api.PathPrefix + "/orgs/{org}/projects/{project}/environments/{environment}/revisions", classRevisions},
		{api.PathPrefix + "/orgs/{org}/projects/{project}/environments/{environment}/publish", classRevisions},
		{api.PathPrefix + "/orgs/{org}/projects/{project}/environments/{environment}/pins", classRevisions},
		{api.PathPrefix + "/orgs/{org}/projects/{project}/environments/{environment}/delivery", classDelivery},
		{api.PathPrefix + "/orgs/{org}/scim-bindings", classSCIM},
		{api.PathPrefix + "/orgs/{org}/scim/v2/{binding}/Users", classSCIM},
		{api.PathPrefix + "/instance/rotate-root-key", classAdmin},
		{api.PathPrefix + "/instance/retention-health", classAdmin},
		{"/not/the/api", classOther},
		{"", classOther},
	}
	for _, c := range cases {
		if got := classify(c.pattern); got != c.want {
			t.Errorf("classify(%q) = %s, want %s", c.pattern, classNames[got], classNames[c.want])
		}
	}
}

func TestBucketForStatusCoversEveryClass(t *testing.T) {
	cases := map[int]statusBucket{
		200: status2xx, 204: status2xx,
		302: status3xx,
		400: status4xx, 401: status4xx, 429: status4xx,
		500: status5xx, 503: status5xx,
		100: statusOther, 600: statusOther,
	}
	for code, want := range cases {
		if got := bucketForStatus(code); got != want {
			t.Errorf("bucketForStatus(%d) = %s, want %s", code, statusNames[got], statusNames[want])
		}
	}
}

// The access log is a Debug-level line: present under the --dev text handler
// (LevelDebug), absent under the production default. It names the class, never
// the raw path.
func TestAccessLogAppearsOnlyAtDebugLevel(t *testing.T) {
	run := func(level slog.Leveler) string {
		var buf bytes.Buffer
		a := &API{
			Metrics: NewMetrics(nil),
			Log:     slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level})),
		}
		r := chi.NewRouter()
		r.Use(a.observe)
		r.Get(api.PathPrefix+"/meta", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		srv := httptest.NewServer(r)
		defer srv.Close()
		resp, err := http.Get(srv.URL + api.PathPrefix + "/meta")
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return buf.String()
	}

	dev := run(slog.LevelDebug)
	if !strings.Contains(dev, "msg=request") || !strings.Contains(dev, "class=auth") {
		t.Fatalf("dev access log missing request line: %q", dev)
	}
	if strings.Contains(dev, "/api/") {
		t.Fatalf("access log echoed a raw path: %q", dev)
	}

	prod := run(slog.LevelInfo)
	if strings.Contains(prod, "request") {
		t.Fatalf("production log emitted the access line at Info level: %q", prod)
	}
}

// The status writer forwards Flush, so a streaming response beneath the recovery
// writer (revisions.go) still flushes.
func TestResponseWriterForwardsFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := newResponseWriter(rec)
	sw.Flush()
	if !rec.Flushed {
		t.Fatal("Flush not forwarded to the underlying writer")
	}
}

type statusSequenceWriter struct {
	header http.Header
	codes  []int
}

func (w *statusSequenceWriter) Header() http.Header            { return w.header }
func (w *statusSequenceWriter) Write(body []byte) (int, error) { return len(body), nil }
func (w *statusSequenceWriter) WriteHeader(code int)           { w.codes = append(w.codes, code) }

func TestResponseWriterKeepsFinalStatusAfterInformationalResponse(t *testing.T) {
	underlying := &statusSequenceWriter{header: make(http.Header)}
	sw := newResponseWriter(underlying)
	sw.WriteHeader(http.StatusEarlyHints)
	sw.WriteHeader(http.StatusOK)

	if sw.status != http.StatusOK || !sw.wroteHeader {
		t.Fatalf("tracked status = %d committed=%v, want final 200", sw.status, sw.wroteHeader)
	}
	if got, want := underlying.codes, []int{http.StatusEarlyHints, http.StatusOK}; !slices.Equal(got, want) {
		t.Fatalf("forwarded statuses = %v, want %v", got, want)
	}
}
