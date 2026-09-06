package updatecheck

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestExactReleaseLookupDoesNotDependOnDiscoveryPage(t *testing.T) {
	for _, tc := range []struct {
		name, tag, draft, published string
		valid                       bool
	}{
		{"published", "v1.1.0-nightly.1", "false", "2026-09-06T00:00:00Z", true},
		{"wrong tag", "v1.1.0-nightly.2", "false", "2026-09-06T00:00:00Z", false},
		{"draft", "v1.1.0-nightly.1", "true", "2026-09-06T00:00:00Z", false},
		{"invalid timestamp", "v1.1.0-nightly.1", "false", "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != "https://api.github.com/repos/Hikyo-Org/Hikyo/releases/tags/v1.1.0-nightly.1" {
					t.Fatalf("unexpected request: %s", request.URL)
				}
				raw := `{"tag_name":"` + tc.tag + `","draft":` + tc.draft + `,"prerelease":true,"immutable":true,"published_at":"` + tc.published + `","assets":[]}`
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(raw))}, nil
			})}
			release, err := NewGitHubSource(client).ReleaseByVersion(context.Background(), "1.1.0-nightly.1")
			if (err == nil) != tc.valid {
				t.Fatalf("lookup: %v", err)
			}
			if tc.valid && (release.Version != "1.1.0-nightly.1" || !release.Immutable) {
				t.Fatalf("wrong release: %+v", release)
			}
		})
	}
}

func TestExactReleaseLookupRejectsInvalidVersionBeforeNetwork(t *testing.T) {
	source := NewGitHubSource(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid version reached network")
		return nil, nil
	})})
	for _, version := range []string{"", "../../main", "v1.0.0", "1.0.0?draft=true"} {
		if _, err := source.ReleaseByVersion(t.Context(), version); err == nil {
			t.Fatalf("accepted %q", version)
		}
	}
}
