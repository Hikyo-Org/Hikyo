package updatecheck

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/netpolicy"
)

const (
	githubReleasesURL       = "https://api.github.com/repos/Hikyo-Org/hikyo/releases?per_page=100"
	githubLatestReleaseURL  = "https://api.github.com/repos/Hikyo-Org/hikyo/releases/latest"
	githubReleasePageBase   = "https://github.com/Hikyo-Org/hikyo/releases/tag/"
	maxReleaseResponseBytes = 2 << 20
)

// Source lists published releases from one fixed release authority.
type Source interface {
	Releases(context.Context) ([]Release, error)
}

// GitHubSource reads Hikyo's public GitHub Releases feed.
type GitHubSource struct {
	client *http.Client
}

// NewGitHubSource binds the pinned Hikyo release URL to an HTTP client.
func NewGitHubSource(client *http.Client) *GitHubSource {
	return &GitHubSource{client: client}
}

// NewHTTPClient creates a no-proxy, public-address-only release client.
func NewHTTPClient(timeout time.Duration) (*http.Client, error) {
	dialer, err := netpolicy.NewPublicDialer(nil, net.DefaultResolver, &net.Dialer{
		Timeout: timeout, KeepAlive: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		Proxy: nil, DialContext: dialer.DialContext, ForceAttemptHTTP2: true,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout: timeout, ResponseHeaderTimeout: timeout,
		IdleConnTimeout: 30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("updatecheck: release source redirected")
		},
	}, nil
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Prerelease  bool   `json:"prerelease"`
	Draft       bool   `json:"draft"`
	PublishedAt string `json:"published_at"`
}

// Releases returns published records only; drafts never enter a channel.
func (s *GitHubSource) Releases(ctx context.Context) ([]Release, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("updatecheck: HTTP client is required")
	}
	var records []githubRelease
	if _, err := s.getJSON(ctx, githubReleasesURL, &records, false); err != nil {
		return nil, err
	}
	var latestStable githubRelease
	foundStable, err := s.getJSON(ctx, githubLatestReleaseURL, &latestStable, true)
	if err != nil {
		return nil, err
	}
	if foundStable {
		records = append(records, latestStable)
	}

	releases := make([]Release, 0, len(records))
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		if record.Draft {
			continue
		}
		publishedAt, err := time.Parse(time.RFC3339, record.PublishedAt)
		if err != nil {
			return nil, fmt.Errorf("updatecheck: release %q has invalid publication time: %w", record.TagName, err)
		}
		tag := record.TagName
		version := tag
		if len(version) > 0 && version[0] == 'v' {
			version = version[1:]
		}
		key := version + fmt.Sprintf("/%t", record.Prerelease)
		if seen[key] {
			continue
		}
		seen[key] = true
		releases = append(releases, Release{
			Version: version, URL: githubReleasePageBase + url.PathEscape(tag),
			Prerelease: record.Prerelease, PublishedAt: publishedAt,
		})
	}
	return releases, nil
}

func (s *GitHubSource) getJSON(ctx context.Context, endpoint string, target any, allowNotFound bool) (bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "hikyo-update-check")
	response, err := s.client.Do(request)
	if err != nil {
		return false, fmt.Errorf("updatecheck: fetch releases: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseResponseBytes+1))
	if err != nil {
		return false, fmt.Errorf("updatecheck: read releases: %w", err)
	}
	if len(raw) > maxReleaseResponseBytes {
		return false, errors.New("updatecheck: release response exceeds size limit")
	}
	if allowNotFound && response.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("updatecheck: release source returned HTTP %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return false, fmt.Errorf("updatecheck: release source returned invalid content type %q", response.Header.Get("Content-Type"))
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return false, fmt.Errorf("updatecheck: decode releases: %w", err)
	}
	return true, nil
}
