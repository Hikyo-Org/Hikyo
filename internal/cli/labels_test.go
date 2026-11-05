package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/api/apigen"
)

func TestIDWithName(t *testing.T) {
	empty, name := "", "Dana"
	for _, tc := range []struct {
		name string
		in   *string
		want string
	}{
		{"named", &name, "usr_1 (Dana)"},
		{"nil name", nil, "usr_1"},
		{"empty name", &empty, "usr_1"},
		{"name equals id", func() *string { s := "usr_1"; return &s }(), "usr_1"},
		{"newline cannot forge a row", func() *string { s := "Dana\nusr_2\tadmin"; return &s }(), "usr_1 (Danausr_2admin)"},
		{"escape sequence", func() *string { s := "\x1b[2JDana\u009b"; return &s }(), "usr_1 ([2JDana)"},
		{"bidi override", func() *string { s := "Da\u202ena"; return &s }(), "usr_1 (Dana)"},
		{"only control characters", func() *string { s := "\n\x1b"; return &s }(), "usr_1"},
	} {
		if got := idWithName("usr_1", tc.in); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func labelerServer(t *testing.T, status int, requests *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == api.PathPrefix+"/meta" {
			_ = json.NewEncoder(w).Encode(apigen.Meta{ApiRevision: api.Revision})
			return
		}
		*requests++
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(apigen.EnvironmentList{Items: []apigen.Environment{{Id: "env_1", Name: "production"}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestEnvironmentLabelerNamesOnceAndFallsBackToTheID(t *testing.T) {
	requests := 0
	srv := labelerServer(t, http.StatusOK, &requests)
	client, err := NewClient(pinnedTestEntry("test", srv), "bearer")
	if err != nil {
		t.Fatal(err)
	}
	label := environmentLabeler(t.Context(), client, "/project", FormatTable)
	if got := label("env_1"); got != "env_1 (production)" {
		t.Fatalf("named = %q", got)
	}
	if got := label("env_gone"); got != "env_gone" {
		t.Fatalf("unknown id = %q", got)
	}
	if requests != 1 {
		t.Fatalf("lookups = %d, want 1 per command", requests)
	}
}

func TestEnvironmentLabelerNeverFailsOrLooksUpForJSON(t *testing.T) {
	requests := 0
	srv := labelerServer(t, http.StatusForbidden, &requests)
	client, err := NewClient(pinnedTestEntry("test", srv), "bearer")
	if err != nil {
		t.Fatal(err)
	}
	if got := environmentLabeler(t.Context(), client, "/project", FormatTable)("env_1"); got != "env_1" {
		t.Fatalf("failed lookup = %q, want the bare ID", got)
	}
	requests = 0
	if got := environmentLabeler(t.Context(), client, "/project", FormatJSON)("env_1"); got != "env_1" || requests != 0 {
		t.Fatalf("json label = %q after %d lookups", got, requests)
	}
}
