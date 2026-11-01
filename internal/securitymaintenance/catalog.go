// Package securitymaintenance evaluates public security advisories locally and
// coordinates installation-owned update drivers. Advisory metadata never grants
// release, migration, shell or deployment authority.
package securitymaintenance

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Masterminds/semver/v3"
)

const CatalogFormat = "hikyo.security-catalog/v1"
const MaxCatalogBytes = 4 << 20

type Severity string

const (
	Low      Severity = "low"
	Moderate Severity = "moderate"
	High     Severity = "high"
	Critical Severity = "critical"
)

func (s Severity) rank() int {
	switch s {
	case Low:
		return 1
	case Moderate:
		return 2
	case High:
		return 3
	case Critical:
		return 4
	default:
		return 0
	}
}

// Affected is a half-open SemVer interval within a release trust profile.
// Empty Introduced means the beginning of that profile; Fixed is exclusive.
type Affected struct {
	Profile    releaseidentity.Profile `json:"profile"`
	Introduced string                  `json:"introduced,omitempty"`
	Fixed      string                  `json:"fixed"`
	Platforms  []string                `json:"platforms"`
	// Empty covers both delivery forms; image-only component advisories must
	// not apply to a native installation of the same application release.
	Delivery string `json:"delivery,omitempty"`
}

type Advisory struct {
	ID         string     `json:"id"`
	Summary    string     `json:"summary"`
	Severity   Severity   `json:"severity"`
	Components []string   `json:"components"`
	Affected   []Affected `json:"affected"`
	Withdrawn  bool       `json:"withdrawn"`
}

// Impact describes the reviewed change, independently of advisory severity.
// Drivers additionally measure schema/maintenance impact from signed routes.
type Impact struct {
	Restart       bool `json:"restart"`
	Features      bool `json:"features"`
	Schema        bool `json:"schema"`
	API           bool `json:"api"`
	Configuration bool `json:"configuration"`
	Maintenance   bool `json:"maintenance"`
}

type Target struct {
	BeforeMutation func() error           `json:"-"`
	PolicyFile     string                 `json:"-"`
	PolicySHA256   releaseidentity.Digest `json:"-"`
	// EligibilityUntil is local execution context derived from verified feed
	// expiry. It is never accepted from or serialized into advisory metadata.
	EligibilityUntil   time.Time                `json:"-"`
	Identity           releaseidentity.Identity `json:"identity"`
	Platforms          []string                 `json:"platforms"`
	Image              string                   `json:"image,omitempty"`
	RuntimeImageSHA256 releaseidentity.Digest   `json:"runtime_image_sha256,omitempty"`
	Fixes              []string                 `json:"fixes"`
	CoverageThrough    string                   `json:"coverage_through,omitempty"`
	Impact             Impact                   `json:"impact"`
	Withdrawn          bool                     `json:"withdrawn"`
}

type Catalog struct {
	Format     string     `json:"format"`
	Sequence   uint64     `json:"sequence"`
	IssuedAt   time.Time  `json:"issued_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Advisories []Advisory `json:"advisories"`
	Targets    []Target   `json:"targets"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,159}$`)
var platformName = regexp.MustCompile(`^(linux|darwin|windows)/(amd64|arm64)$`)
var imageName = regexp.MustCompile(`^[a-z0-9][a-z0-9./:_-]{0,254}@sha256:[0-9a-f]{64}$`)

func canonicalVersion(raw string) (*semver.Version, error) {
	parsed, err := semver.StrictNewVersion(raw)
	if err != nil || len(raw) > 256 || parsed.String() != raw {
		return nil, errors.New("canonical semantic version required")
	}
	return parsed, nil
}

func validPlatforms(platforms []string) bool {
	if len(platforms) == 0 || len(platforms) > 6 {
		return false
	}
	seen := map[string]bool{}
	for _, platform := range platforms {
		if !platformName.MatchString(platform) || seen[platform] {
			return false
		}
		seen[platform] = true
	}
	return true
}

// DecodeCatalog is structural validation, not authentication. Production calls
// it only on a fixed target authenticated by the maintained TUF client.
func DecodeCatalog(raw []byte, now time.Time) (Catalog, error) {
	var catalog Catalog
	if len(raw) == 0 || len(raw) > MaxCatalogBytes {
		return catalog, errors.New("security catalog is empty or exceeds 4 MiB")
	}
	if err := definitions.DecodeStrict(raw, &catalog); err != nil {
		return catalog, fmt.Errorf("security catalog: %w", err)
	}
	return catalog, catalog.Validate(now)
}

func (c Catalog) Validate(now time.Time) error {
	if c.Format != CatalogFormat || c.Sequence == 0 || c.IssuedAt.IsZero() || c.ExpiresAt.IsZero() ||
		!c.ExpiresAt.After(c.IssuedAt) || c.ExpiresAt.Sub(c.IssuedAt) > 7*24*time.Hour ||
		c.IssuedAt.After(now.Add(5*time.Minute)) || !c.ExpiresAt.After(now) {
		return errors.New("security catalog format, sequence or freshness is invalid")
	}
	if len(c.Advisories) > 2048 || len(c.Targets) > 512 {
		return errors.New("security catalog inventory exceeds bounds")
	}
	advisories := make(map[string]Advisory, len(c.Advisories))
	for _, advisory := range c.Advisories {
		if _, exists := advisories[advisory.ID]; exists || !identifier.MatchString(advisory.ID) ||
			advisory.Severity.rank() == 0 || strings.TrimSpace(advisory.Summary) == "" || len(advisory.Summary) > 1024 ||
			len(advisory.Components) == 0 || len(advisory.Components) > 64 || len(advisory.Affected) == 0 || len(advisory.Affected) > 64 {
			return errors.New("security advisory identity, severity or scope is invalid")
		}
		components := map[string]bool{}
		for _, component := range advisory.Components {
			if !identifier.MatchString(component) || components[component] {
				return errors.New("security advisory component inventory is invalid")
			}
			components[component] = true
		}
		for _, affected := range advisory.Affected {
			upper, err := canonicalVersion(affected.Fixed)
			if affected.Profile.Validate() != nil || !validPlatforms(affected.Platforms) || err != nil || affected.Delivery != "" && affected.Delivery != "binary" && affected.Delivery != "image" {
				return errors.New("security advisory affected range is invalid")
			}
			if affected.Introduced != "" {
				lower, err := canonicalVersion(affected.Introduced)
				if err != nil || !lower.LessThan(upper) {
					return errors.New("security advisory affected range is empty or reversed")
				}
			}
		}
		advisories[advisory.ID] = advisory
	}
	targets := map[string]bool{}
	sequences := map[uint64]releaseidentity.Identity{}
	for _, target := range c.Targets {
		version, versionErr := canonicalVersion(target.Identity.Version)
		if versionErr != nil || target.Identity.Profile == releaseidentity.StableV1 && version.Prerelease() != "" {
			return errors.New("security stable targets cannot name unsupported prereleases")
		}
		if target.Identity.Validate() != nil || !validPlatforms(target.Platforms) || !target.Impact.Restart ||
			len(target.Fixes) == 0 || len(target.Fixes) > 2048 || target.Image != "" && !imageName.MatchString(target.Image) ||
			target.RuntimeImageSHA256 != "" && (target.Image == "" || target.RuntimeImageSHA256.Validate() != nil) {
			return errors.New("security target identity, platform, image or impact is invalid")
		}
		if target.CoverageThrough != "" {
			date, err := time.Parse(time.DateOnly, target.CoverageThrough)
			if err != nil || date.Format(time.DateOnly) != target.CoverageThrough || date.After(c.IssuedAt) {
				return errors.New("security coverage date is invalid")
			}
		}
		if prior, exists := sequences[target.Identity.Sequence]; exists && prior != target.Identity {
			return errors.New("security targets equivocate at one release sequence")
		}
		sequences[target.Identity.Sequence] = target.Identity
		for _, platform := range target.Platforms {
			delivery := "binary"
			if target.Image != "" {
				delivery = "image"
			}
			key := string(target.Identity.ManifestSHA256) + "/" + platform + "/" + delivery
			if targets[key] {
				return errors.New("duplicate security target for platform")
			}
			targets[key] = true
		}
		fixes := map[string]bool{}
		for _, id := range target.Fixes {
			advisory, exists := advisories[id]
			if !exists || fixes[id] {
				return errors.New("security target references an unknown or duplicate advisory")
			}
			fixes[id] = true
			for _, platform := range target.Platforms {
				if advisory.affects(target.Identity, platform, target.Image != "") {
					return errors.New("security target claims a fix within the affected range")
				}
			}
		}
	}
	return nil
}

func (a Advisory) affects(identity releaseidentity.Identity, platform string, image bool) bool {
	version, err := canonicalVersion(identity.Version)
	if err != nil || a.Withdrawn {
		return false
	}
	for _, affected := range a.Affected {
		if affected.Profile != identity.Profile || !slices.Contains(affected.Platforms, platform) || affected.Delivery == "image" && !image || affected.Delivery == "binary" && image {
			continue
		}
		upper, err := canonicalVersion(affected.Fixed)
		if err != nil || !version.LessThan(upper) {
			continue
		}
		if affected.Introduced == "" {
			return true
		}
		lower, err := canonicalVersion(affected.Introduced)
		if err == nil && !version.LessThan(lower) {
			return true
		}
	}
	return false
}
