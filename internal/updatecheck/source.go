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
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/netpolicy"
)

const (
	githubReleasesURL       = "https://api.github.com/repos/Hikyo-Org/Hikyo/releases?per_page=100"
	githubLatestReleaseURL  = "https://api.github.com/repos/Hikyo-Org/Hikyo/releases/latest"
	githubReleasePageBase   = "https://github.com/Hikyo-Org/Hikyo/releases/tag/"
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
	return newHTTPClient(timeout, func(*http.Request, []*http.Request) error {
		return errors.New("updatecheck: release source redirected")
	})
}

// NewDownloadHTTPClient creates a public-address-only client that follows only
// GitHub's fixed release-asset redirect hosts.
func NewDownloadHTTPClient(timeout time.Duration) (*http.Client, error) {
	return newHTTPClient(timeout, func(request *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("updatecheck: too many release download redirects")
		}
		if request.URL.Scheme != "https" || !releaseDownloadHost(request.URL.Hostname()) {
			return fmt.Errorf("updatecheck: release download redirected to untrusted host %q", request.URL.Hostname())
		}
		return nil
	})
}

func newHTTPClient(timeout time.Duration, checkRedirect func(*http.Request, []*http.Request) error) (*http.Client, error) {
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
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: checkRedirect,
	}, nil
}

func releaseDownloadHost(host string) bool {
	switch strings.ToLower(host) {
	case "github.com", "raw.githubusercontent.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	default:
		return false
	}
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Prerelease  bool          `json:"prerelease"`
	Draft       bool          `json:"draft"`
	Immutable   bool          `json:"immutable"`
	PublishedAt string        `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
	Size               int64  `json:"size"`
}

var sha256DigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

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
		assets, err := releaseAssets(tag, record.Assets)
		if err != nil {
			return nil, fmt.Errorf("updatecheck: release %q: %w", tag, err)
		}
		releases = append(releases, Release{
			Version: version, URL: githubReleasePageBase + url.PathEscape(tag),
			Prerelease: record.Prerelease, Immutable: record.Immutable,
			PublishedAt: publishedAt, Assets: assets,
		})
	}
	return releases, nil
}

func releaseAssets(tag string, records []githubAsset) ([]Asset, error) {
	assets := make([]Asset, 0, len(records))
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		if record.Name == "" || record.Name == "." || record.Name == ".." ||
			path.Base(record.Name) != record.Name || strings.Contains(record.Name, "..") ||
			strings.ContainsAny(record.Name, `/\\`) {
			return nil, fmt.Errorf("unsafe asset name %q", record.Name)
		}
		if seen[record.Name] {
			return nil, fmt.Errorf("duplicate asset name %q", record.Name)
		}
		seen[record.Name] = true
		expectedURL := "https://github.com/Hikyo-Org/Hikyo/releases/download/" +
			url.PathEscape(tag) + "/" + url.PathEscape(record.Name)
		if record.BrowserDownloadURL != expectedURL {
			return nil, fmt.Errorf("asset %q has unexpected download URL", record.Name)
		}
		if record.Size <= 0 {
			return nil, fmt.Errorf("asset %q has invalid size %d", record.Name, record.Size)
		}
		if record.Digest != "" && !sha256DigestPattern.MatchString(record.Digest) {
			return nil, fmt.Errorf("asset %q has invalid digest", record.Name)
		}
		assets = append(assets, Asset{
			Name: record.Name, URL: record.BrowserDownloadURL,
			Digest: record.Digest, Size: record.Size,
		})
	}
	return assets, nil
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
