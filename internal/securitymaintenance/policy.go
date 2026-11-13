package securitymaintenance

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"slices"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

type Mode string

const (
	Automatic Mode = "automatic"
	Manual    Mode = "manual"
	Offline   Mode = "offline"
)

type Policy struct {
	Mode    Mode                `json:"mode"`
	Channel updatecheck.Channel `json:"channel"`
	Pin     string              `json:"pin,omitempty"`
	// UpgradeClass is patch, minor, or compatible. Compatible still requires an
	// independently authenticated exact route; it does not infer SemVer safety.
	UpgradeClass     string   `json:"upgrade_class"`
	AllowMaintenance bool     `json:"allow_maintenance"`
	MinimumSeverity  Severity `json:"minimum_severity"`
	// A zero-length window permits any time. Nonzero windows use UTC minutes.
	WindowStartMinute     int `json:"window_start_minute"`
	WindowDurationMinutes int `json:"window_duration_minutes"`
	// A verified fix at or above UrgentSeverity need not wait for the window.
	// Empty means windows always apply. Pins, upgrade classes, maintenance
	// consent, signatures and recovery fences bind regardless of severity.
	UrgentSeverity Severity `json:"urgent_severity,omitempty"`
}

func DefaultPolicy() Policy {
	return Policy{Mode: Automatic, Channel: updatecheck.ChannelStable, UpgradeClass: "compatible", AllowMaintenance: true, MinimumSeverity: Low, UrgentSeverity: Critical}
}

func (p Policy) Validate() error {
	if !slices.Contains([]Mode{Automatic, Manual, Offline}, p.Mode) ||
		!slices.Contains([]updatecheck.Channel{updatecheck.ChannelStable, updatecheck.ChannelNightly}, p.Channel) ||
		!slices.Contains([]string{"patch", "minor", "compatible"}, p.UpgradeClass) || p.MinimumSeverity.rank() == 0 ||
		p.WindowStartMinute < 0 || p.WindowStartMinute >= 1440 || p.WindowDurationMinutes < 0 || p.WindowDurationMinutes > 1440 ||
		p.UrgentSeverity != "" && p.UrgentSeverity.rank() == 0 {
		return errors.New("security maintenance policy is invalid")
	}
	if p.Pin != "" {
		if _, err := canonicalVersion(p.Pin); err != nil {
			return errors.New("security maintenance application pin is invalid")
		}
	}
	return nil
}

func (p Policy) permits(source releaseidentity.Identity, target Target, now time.Time, urgent bool) string {
	if p.Mode == Manual {
		return "manual_mode"
	}
	expected := releaseidentity.StableV1
	if p.Channel == updatecheck.ChannelNightly {
		expected = releaseidentity.NightlyV1
	}
	if target.Identity.Profile != expected || source.Profile != expected {
		return "channel_change"
	}
	if p.Pin != "" && target.Identity.Version != p.Pin {
		return "application_pin"
	}
	installed, _ := canonicalVersion(source.Version)
	candidate, err := canonicalVersion(target.Identity.Version)
	if err != nil || installed == nil || target.Identity.Sequence <= source.Sequence || !candidate.GreaterThan(installed) {
		return "not_a_forward_upgrade"
	}
	if p.UpgradeClass == "patch" && (candidate.Major() != installed.Major() || candidate.Minor() != installed.Minor() ||
		target.Impact.Features || target.Impact.API || target.Impact.Configuration) {
		return "feature_upgrade_required"
	}
	if p.UpgradeClass == "minor" && candidate.Major() != installed.Major() {
		return "major_upgrade_required"
	}
	if !p.AllowMaintenance && (target.Impact.Maintenance || target.Impact.Schema) {
		return "maintenance_required"
	}
	if p.WindowDurationMinutes != 0 && !urgent {
		minute := now.UTC().Hour()*60 + now.UTC().Minute()
		if (minute-p.WindowStartMinute+1440)%1440 >= p.WindowDurationMinutes {
			return "maintenance_window"
		}
	}
	return ""
}

type Decision struct {
	Applicable []string `json:"applicable"`
	Target     *Target  `json:"target,omitzero"`
	Blocked    string   `json:"blocked,omitempty"`
	// Urgent records that the selected target fixes an urgent applicable advisory.
	Urgent bool `json:"urgent,omitzero"`
}

// urgentFix reports whether target fixes an applicable advisory at or above
// UrgentSeverity. Only the fix being installed can justify skipping the window.
func (p Policy) urgentFix(c Catalog, target Target, applicable []string) bool {
	if p.UrgentSeverity == "" {
		return false
	}
	for _, advisory := range c.Advisories {
		if advisory.Severity.rank() >= p.UrgentSeverity.rank() && slices.Contains(applicable, advisory.ID) && slices.Contains(target.Fixes, advisory.ID) {
			return true
		}
	}
	return false
}

// rolloutStart staggers one installation inside the published rollout window
// using only local identity, so staged rollout needs no project-side cohort list.
func (t Target) rolloutStart(instance string) time.Time {
	if t.RolloutMinutes == 0 {
		return t.AvailableAt
	}
	sum := sha256.Sum256([]byte(instance + "/" + string(t.Identity.ManifestSHA256)))
	return t.AvailableAt.Add(time.Duration(binary.BigEndian.Uint64(sum[:8])%uint64(t.RolloutMinutes)) * time.Minute)
}

// windowEnd bounds admission of the first mutation, not completion of an
// already admitted migration. Zero means there is no window deadline.
func (p Policy) windowEnd(now time.Time) time.Time {
	if p.WindowDurationMinutes == 0 || p.WindowDurationMinutes == 1440 {
		return time.Time{}
	}
	utc := now.UTC()
	start := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC).Add(time.Duration(p.WindowStartMinute) * time.Minute)
	if utc.Before(start) {
		start = start.Add(-24 * time.Hour)
	}
	return start.Add(time.Duration(p.WindowDurationMinutes) * time.Minute)
}

// Select chooses the newest nonwithdrawn target covering every applicable
// advisory at the configured severity, or failing that the one fixing the most,
// because a remaining unfixed advisory must not hold back a critical fix.
// Artifact and route verification remain mandatory in the driver.
func Select(c Catalog, installed releaseidentity.Identity, platform string, p Policy, now time.Time) (Decision, error) {
	return selectForDelivery(c, installed, "", platform, p, now, false)
}

func selectForDelivery(c Catalog, installed releaseidentity.Identity, instance, platform string, p Policy, now time.Time, image bool) (Decision, error) {
	var decision Decision
	if err := c.Validate(now); err != nil {
		return decision, err
	}
	if err := p.Validate(); err != nil {
		return decision, err
	}
	if installed.Validate() != nil || !platformName.MatchString(platform) {
		return decision, errors.New("exact installed release and supported platform required")
	}
	decision.Applicable = applicableAdvisories(c, installed, platform, p.MinimumSeverity, image)
	if len(decision.Applicable) == 0 {
		return decision, nil
	}
	decision.Blocked = "no_complete_security_target"
	best := 0
	for _, target := range c.Targets {
		if (target.Image != "") != image || target.Withdrawn || !slices.Contains(target.Platforms, platform) || target.Identity.Profile != installed.Profile {
			continue
		}
		covered := 0
		for _, id := range decision.Applicable {
			if slices.Contains(target.Fixes, id) {
				covered++
			}
		}
		if covered == 0 || c.addsExposure(target, decision.Applicable, platform, p.MinimumSeverity, image) {
			continue
		}
		urgent := p.urgentFix(c, target, decision.Applicable)
		reason := p.permits(installed, target, now, urgent)
		if reason == "" && now.Before(target.rolloutStart(instance)) {
			reason = "staged_rollout"
		}
		if reason != "" {
			if decision.Target == nil {
				decision.Blocked = reason
			}
			continue
		}
		if decision.Target == nil || covered > best || covered == best && target.Identity.Sequence > decision.Target.Identity.Sequence {
			copy := target
			copy.Platforms = slices.Clone(target.Platforms)
			copy.Fixes = slices.Clone(target.Fixes)
			decision.Target, decision.Blocked, decision.Urgent, best = &copy, "", urgent, covered
		}
	}
	return decision, nil
}

// addsExposure refuses a move onto a release carrying an advisory that the
// installed release does not have, even when that move fixes something else.
func (c Catalog) addsExposure(target Target, applicable []string, platform string, minimum Severity, image bool) bool {
	return slices.ContainsFunc(c.Advisories, func(a Advisory) bool {
		return a.Severity.rank() >= minimum.rank() && !slices.Contains(applicable, a.ID) && a.affects(target.Identity, platform, image)
	})
}

// Applicability depends on authenticated catalog scope and observed release,
// independently of target selection, readiness, maintenance windows or pins.
func applicableAdvisories(c Catalog, installed releaseidentity.Identity, platform string, minimum Severity, image bool) []string {
	applicable := []string{}
	for _, advisory := range c.Advisories {
		if advisory.Severity.rank() >= minimum.rank() && advisory.affects(installed, platform, image) {
			applicable = append(applicable, advisory.ID)
		}
	}
	slices.Sort(applicable)
	return applicable
}
