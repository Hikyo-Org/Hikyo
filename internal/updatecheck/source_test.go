package updatecheck

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestGitHubSourceReturnsPublishedReleaseRecords(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Accept") != "application/vnd.github+json" {
			t.Fatalf("Accept = %q", request.Header.Get("Accept"))
		}
		if request.URL.String() == githubLatestReleaseURL {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"tag_name":"v1.0.1","prerelease":false,"draft":false,"immutable":true,"published_at":"2026-08-23T02:00:00Z","assets":[]}`)),
			}, nil
		}
		if request.URL.String() != githubReleasesURL {
			t.Fatalf("request URL = %s, want pinned release source", request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
			Body: io.NopCloser(strings.NewReader(`[
				{"tag_name":"v1.1.0-nightly.20260824.42.gbbbbbbbb","prerelease":true,"draft":false,"immutable":true,"published_at":"2026-08-24T02:00:00Z","assets":[
					{"name":"hikyo_1.1.0-nightly.20260824.42.gbbbbbbbb_Darwin_arm64.tar.gz","browser_download_url":"https://github.com/Hikyo-Org/Hikyo/releases/download/v1.1.0-nightly.20260824.42.gbbbbbbbb/hikyo_1.1.0-nightly.20260824.42.gbbbbbbbb_Darwin_arm64.tar.gz","size":1234,"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
					{"name":"checksums.txt","browser_download_url":"https://github.com/Hikyo-Org/Hikyo/releases/download/v1.1.0-nightly.20260824.42.gbbbbbbbb/checksums.txt","size":456,"digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
				]},
				{"tag_name":"v1.0.1","prerelease":false,"draft":false,"published_at":"2026-08-23T02:00:00Z"},
				{"tag_name":"v9.9.9","prerelease":false,"draft":true,"published_at":"2026-08-22T02:00:00Z"}
			]`)),
		}, nil
	})
	source := NewGitHubSource(&http.Client{Transport: transport})

	releases, err := source.Releases(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 2 {
		t.Fatalf("releases = %+v, want two published records", releases)
	}
	if releases[0].Version != "1.1.0-nightly.20260824.42.gbbbbbbbb" ||
		releases[0].URL != "https://github.com/Hikyo-Org/Hikyo/releases/tag/v1.1.0-nightly.20260824.42.gbbbbbbbb" ||
		!releases[0].Prerelease || !releases[0].Immutable ||
		!releases[0].PublishedAt.Equal(time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("nightly release = %+v", releases[0])
	}
	if len(releases[0].Assets) != 2 ||
		releases[0].Assets[0].Name != "hikyo_1.1.0-nightly.20260824.42.gbbbbbbbb_Darwin_arm64.tar.gz" ||
		releases[0].Assets[0].Size != 1234 ||
		releases[0].Assets[0].Digest != "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("nightly assets = %+v", releases[0].Assets)
	}
}

func TestGitHubSourceKeepsLatestStableAfterNightliesFillFirstPage(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `[{"tag_name":"v1.1.0-nightly.20260824.42.gbbbbbbbb","prerelease":true,"draft":false,"published_at":"2026-08-24T02:00:00Z"}]`
		if request.URL.String() == githubLatestReleaseURL {
			body = `{"tag_name":"v1.0.1","prerelease":false,"draft":false,"published_at":"2026-01-01T02:00:00Z"}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})
	releases, err := NewGitHubSource(&http.Client{Transport: transport}).Releases(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	status, err := Select("1.0.0", ChannelStable, releases)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available || status.LatestVersion != "1.0.1" {
		t.Fatalf("stable status = %+v, want latest stable outside first page", status)
	}
}

func TestGitHubSourceRefusesNonSuccessAndOversizedResponses(t *testing.T) {
	for _, test := range []struct {
		name string
		code int
		body string
	}{
		{name: "non-success", code: http.StatusForbidden, body: `{}`},
		{name: "oversized", code: http.StatusOK, body: strings.Repeat("x", maxReleaseResponseBytes+1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := NewGitHubSource(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: test.code, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(test.body))}, nil
			})})
			if _, err := source.Releases(context.Background()); err == nil {
				t.Fatal("Releases() accepted an invalid response")
			}
		})
	}
}

func TestDownloadClientAllowsOnlyGitHubReleaseRedirects(t *testing.T) {
	client, err := NewDownloadHTTPClient(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"https://github.com/Hikyo-Org/hikyo/releases/download/v1.0.1/hikyo.tar.gz",
		"https://raw.githubusercontent.com/Hikyo-Org/Hikyo/refs/heads/main/release/trust/metadata.json",
		"https://release-assets.githubusercontent.com/github-production-release-asset/file",
		"https://objects.githubusercontent.com/github-production-release-asset/file",
	} {
		request, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.CheckRedirect(request, []*http.Request{{}}); err != nil {
			t.Errorf("redirect to %s refused: %v", target, err)
		}
	}
	request, err := http.NewRequest(http.MethodGet, "https://downloads.example/hikyo.tar.gz", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(request, []*http.Request{{}}); err == nil {
		t.Fatal("redirect to an untrusted host was accepted")
	}
}
