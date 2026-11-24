package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api/apigen"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
)

func TestReclassifyTightenPrintsAdvisoryOnStderrOnly(t *testing.T) {
	for _, tc := range []struct {
		classification string
		advisory       bool
	}{
		{"secret", true},
		{"config", false},
	} {
		t.Run(tc.classification, func(t *testing.T) {
			key := apigen.Key{Id: "key_1", Name: "API_TOKEN", Classification: apigen.KeyClassification(tc.classification)}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut || r.URL.Path != "/key/classification" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = jsonwire.Encode(w, key)
			}))
			defer srv.Close()
			client := &Client{Entry: TrustEntry{Origin: srv.URL}, HTTP: srv.Client()}
			var stdout, stderr bytes.Buffer
			if err := reclassifyKey(t.Context(), IO{Stdout: &stdout, Stderr: &stderr}, FormatJSON, client, "/key", tc.classification); err != nil {
				t.Fatal(err)
			}
			if got := strings.Contains(stderr.String(), tightenAdvisory); got != tc.advisory {
				t.Fatalf("stderr advisory = %v, want %v; stderr %q", got, tc.advisory, stderr.String())
			}
			var plain bytes.Buffer
			if err := renderKey(IO{Stdout: &plain}, FormatJSON, key); err != nil {
				t.Fatal(err)
			}
			if stdout.String() != plain.String() {
				t.Fatalf("stdout changed:\n got %q\nwant %q", stdout.String(), plain.String())
			}
		})
	}
}

// The CLI and the web UI state the same advisory.
func TestTightenAdvisoryMatchesWeb(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "api", "matrix.ts"))
	if err != nil {
		t.Fatal(err)
	}
	decl := regexp.MustCompile(`(?s)export const TIGHTEN_ADVISORY =(.*?)';\n`).FindSubmatch(src)
	if decl == nil {
		t.Fatal("TIGHTEN_ADVISORY not found in web/src/api/matrix.ts")
	}
	var web strings.Builder
	for _, part := range regexp.MustCompile(`'([^']*)'`).FindAllSubmatch(append(decl[1], '\''), -1) {
		web.Write(part[1])
	}
	if web.String() != tightenAdvisory {
		t.Fatalf("CLI advisory drifted from web TIGHTEN_ADVISORY:\n cli %q\n web %q", tightenAdvisory, web.String())
	}
}
