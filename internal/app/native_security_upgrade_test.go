package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
)

func nativeSecurityIdentity(profile releaseidentity.Profile) releaseidentity.Identity {
	version := "1.3.1"
	if profile == releaseidentity.NightlyV1 {
		version = "1.3.1-nightly.8"
	}
	return releaseidentity.Identity{Profile: profile, Version: version, Sequence: 8, Commit: strings.Repeat("a", 40), CompatibilitySHA256: releaseidentity.Hash([]byte("compatibility")), ManifestSHA256: releaseidentity.Hash([]byte("manifest"))}
}

func nativeSecurityIdentityJSON(t *testing.T, identity releaseidentity.Identity) string {
	t.Helper()
	raw, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestNativeSecurityIdentityFlagsRefuseAmbiguousInputBeforeHostAccess(t *testing.T) {
	raw := nativeSecurityIdentityJSON(t, nativeSecurityIdentity(releaseidentity.StableV1))
	for name, args := range map[string][]string{
		"conflict":              {"--target", "1.3.1", "--release-identity", raw},
		"empty target conflict": {"--target=", "--release-identity", raw},
		"empty identity":        {"--release-identity="},
		"null identity":         {"--release-identity", "null"},
		"missing fields":        {"--release-identity", `{}`},
		"unknown field":         {"--release-identity", strings.TrimSuffix(raw, "}") + `,"command":"sh"}`},
		"trailing identity":     {"--release-identity", raw + raw},
		"duplicate member":      {"--release-identity", strings.TrimSuffix(raw, "}") + `,"version":"1.3.2"}`},
		"unbounded identity":    {"--release-identity", strings.Repeat(" ", 4097)},
	} {
		t.Run(name, func(t *testing.T) {
			err := RunAutomaticUpgrade(t.Context(), args, io.Discard, nil)
			if err == nil || !strings.Contains(err.Error(), "--release-identity") {
				t.Fatalf("did not refuse the flag input before reaching host operations: %v", err)
			}
		})
	}
}

func TestNativeSecurityIdentityParsingKeepsDefaultAndExactProfiles(t *testing.T) {
	identity, err := parseAutomaticReleaseIdentity("", false, false)
	if err != nil || identity != nil {
		t.Fatal("changed the default nightly selection", identity, err)
	}
	for _, profile := range []releaseidentity.Profile{releaseidentity.StableV1, releaseidentity.NightlyV1} {
		expected := nativeSecurityIdentity(profile)
		identity, err := parseAutomaticReleaseIdentity(nativeSecurityIdentityJSON(t, expected), true, false)
		if err != nil || identity == nil || *identity != expected {
			t.Fatal("lost exact release identity", identity, err)
		}
	}
	if err := RunSecurityUpgrade(t.Context(), releaseidentity.Identity{}, "invalid configuration", io.Discard); err == nil || !strings.Contains(err.Error(), "security upgrade target") {
		t.Fatal("invalid security target reached host operations", err)
	}
}

type nativeSecurityReleaseSource struct {
	release   updatecheck.Release
	requested []string
	listed    bool
}

func (s *nativeSecurityReleaseSource) Releases(context.Context) ([]updatecheck.Release, error) {
	s.listed = true
	return nil, errors.New("exact target must never list releases")
}

func (s *nativeSecurityReleaseSource) ReleaseByVersion(_ context.Context, version string) (updatecheck.Release, error) {
	s.requested = append(s.requested, version)
	return s.release, nil
}

type nativeSecurityReleasePreparer struct {
	prepared selfupdate.PreparedNightly
	expected releaseidentity.Identity
	status   updatecheck.Status
	called   bool
}

func (p *nativeSecurityReleasePreparer) PrepareReleaseSource(_ context.Context, status updatecheck.Status, expected releaseidentity.Identity) (selfupdate.PreparedNightly, error) {
	p.called, p.status, p.expected = true, status, expected
	return p.prepared, nil
}

func TestNativeSecurityExactSelectionDoesNotFollowLatest(t *testing.T) {
	for _, profile := range []releaseidentity.Profile{releaseidentity.StableV1, releaseidentity.NightlyV1} {
		t.Run(string(profile), func(t *testing.T) {
			expected := nativeSecurityIdentity(profile)
			source := &nativeSecurityReleaseSource{release: updatecheck.Release{Version: expected.Version, Prerelease: profile == releaseidentity.NightlyV1, Immutable: true, Assets: []updatecheck.Asset{{Name: "release-manifest.sigstore.json"}}}}
			installer := &nativeSecurityReleasePreparer{prepared: selfupdate.PreparedNightly{Identity: expected, BinaryPath: "verified-executable"}}
			prepared, err := prepareExactAutomaticRelease(t.Context(), source, installer, expected)
			if err != nil || prepared.Identity != expected || !installer.called || installer.expected != expected {
				t.Fatal("exact prepared selection changed", prepared.Identity, err)
			}
			channel := updatecheck.ChannelStable
			if profile == releaseidentity.NightlyV1 {
				channel = updatecheck.ChannelNightly
			}
			if source.listed || !slices.Equal(source.requested, []string{expected.Version}) || installer.status.Channel != channel || installer.status.LatestVersion != expected.Version {
				t.Fatal("selected another release or channel", source, installer.status)
			}
		})
	}
}

func TestNativeSecurityExactSelectionRejectsDiscoverySubstitution(t *testing.T) {
	expected := nativeSecurityIdentity(releaseidentity.StableV1)
	for name, release := range map[string]updatecheck.Release{
		"newer version":  {Version: "1.4.0", Immutable: true},
		"mutable target": {Version: expected.Version},
		"wrong channel":  {Version: expected.Version, Immutable: true, Prerelease: true},
	} {
		t.Run(name, func(t *testing.T) {
			source := &nativeSecurityReleaseSource{release: release}
			installer := &nativeSecurityReleasePreparer{prepared: selfupdate.PreparedNightly{Identity: expected}}
			if _, err := prepareExactAutomaticRelease(t.Context(), source, installer, expected); err == nil || installer.called {
				t.Fatal("passed substituted discovery to the preparer", err)
			}
		})
	}
}

func TestNativeSecurityExactSelectionRejectsPreparedIdentitySubstitution(t *testing.T) {
	expected := nativeSecurityIdentity(releaseidentity.StableV1)
	for name, change := range map[string]func(*releaseidentity.Identity){
		"profile":  func(i *releaseidentity.Identity) { i.Profile = releaseidentity.NightlyV1 },
		"version":  func(i *releaseidentity.Identity) { i.Version = "1.3.2" },
		"sequence": func(i *releaseidentity.Identity) { i.Sequence++ },
		"commit":   func(i *releaseidentity.Identity) { i.Commit = strings.Repeat("b", 40) },
		"compatibility": func(i *releaseidentity.Identity) {
			i.CompatibilitySHA256 = releaseidentity.Hash([]byte("other compatibility"))
		},
		"manifest": func(i *releaseidentity.Identity) { i.ManifestSHA256 = releaseidentity.Hash([]byte("other manifest")) },
	} {
		t.Run(name, func(t *testing.T) {
			substituted := expected
			change(&substituted)
			source := &nativeSecurityReleaseSource{release: updatecheck.Release{Version: expected.Version, Immutable: true}}
			installer := &nativeSecurityReleasePreparer{prepared: selfupdate.PreparedNightly{Identity: substituted}}
			if _, err := prepareExactAutomaticRelease(t.Context(), source, installer, expected); err == nil {
				t.Fatal("accepted a different prepared identity")
			}
		})
	}
}

func TestNativeSecurityHandoffPreservesCompleteIdentity(t *testing.T) {
	expected := nativeSecurityIdentity(releaseidentity.StableV1)
	prepared := selfupdate.PreparedNightly{Identity: expected, BinaryPath: "verified-executable"}
	handoff, err := automaticReleaseHandoff(prepared, "/etc/hikyo/upgrade.json", &expected)
	if err != nil {
		t.Fatal(err)
	}
	if handoff.Executable != prepared.BinaryPath || len(handoff.Arguments) != 6 || handoff.Arguments[0] != "upgrade" || handoff.Arguments[3] != "--release-identity" || handoff.Arguments[5] != "--handoff" {
		t.Fatal("lost trusted handoff arguments", handoff)
	}
	var actual releaseidentity.Identity
	if err := definitions.DecodeStrict([]byte(handoff.Arguments[4]), &actual); err != nil || actual != expected {
		t.Fatal("handoff lost exact selected identity", actual, err)
	}
	other := expected
	other.ManifestSHA256 = releaseidentity.Hash([]byte("other manifest"))
	if _, err := automaticReleaseHandoff(prepared, "/etc/hikyo/upgrade.json", &other); err == nil {
		t.Fatal("handoff accepted a changed manifest")
	}
	nightly := nativeSecurityIdentity(releaseidentity.NightlyV1)
	prepared.Identity = nightly
	handoff, err = automaticReleaseHandoff(prepared, "/etc/hikyo/upgrade.json", nil)
	if err != nil || !slices.Equal(handoff.Arguments, []string{"upgrade", "--config", "/etc/hikyo/upgrade.json", "--target", nightly.Version, "--handoff"}) {
		t.Fatal("changed the legacy nightly handoff", handoff, err)
	}
}

func TestNativeSecurityPendingOperationKeepsExactIdentity(t *testing.T) {
	target := nativeSecurityIdentity(releaseidentity.StableV1)
	previous := &automaticJournal{Phase: "schema-applied", Target: target}
	version, expected, err := resolveAutomaticReleaseIdentity("", nil, previous)
	if err != nil || version != target.Version || expected == nil || *expected != target {
		t.Fatal("plain rerun lost the pending stable identity", version, expected, err)
	}
	other := target
	other.ManifestSHA256 = releaseidentity.Hash([]byte("replacement manifest"))
	if _, _, err := resolveAutomaticReleaseIdentity("", &other, previous); err == nil {
		t.Fatal("replaced a pending operation with the same version and another manifest")
	}
	if _, _, err := resolveAutomaticReleaseIdentity("1.4.0", nil, previous); err == nil {
		t.Fatal("changed a pending operation's version")
	}
	previous.Phase = "complete"
	if _, expected, err := resolveAutomaticReleaseIdentity("", nil, previous); err != nil || expected != nil {
		t.Fatal("completed stable operation changed default nightly selection", expected, err)
	}
}

func TestNativeSecurityPolicyFlagsRequireExactRelease(t *testing.T) {
	raw := nativeSecurityIdentityJSON(t, nativeSecurityIdentity(releaseidentity.StableV1))
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"route without identity":    {[]string{"--route-sha256", strings.Repeat("a", 64)}, "--route-sha256 requires"},
		"malformed route":           {[]string{"--release-identity", raw, "--route-sha256", "bad"}, "--route-sha256"},
		"deadline without identity": {[]string{"--eligibility-until", "2099-01-01T00:00:00Z"}, "--eligibility-until requires"},
		"malformed deadline":        {[]string{"--release-identity", raw, "--eligibility-until", "tomorrow"}, "--eligibility-until"},
		"expired deadline":          {[]string{"--release-identity", raw, "--eligibility-until", "2000-01-01T00:00:00Z"}, "eligibility expired"},
		"policy without identity":   {[]string{"--maintenance-policy-file", "/etc/hikyo/security-maintenance.json", "--maintenance-policy-sha256", strings.Repeat("a", 64)}, "maintenance policy guard requires"},
		"policy path only":          {[]string{"--release-identity", raw, "--maintenance-policy-file", "/etc/hikyo/security-maintenance.json"}, "maintenance policy guard requires"},
		"policy hash only":          {[]string{"--release-identity", raw, "--maintenance-policy-sha256", strings.Repeat("a", 64)}, "maintenance policy guard requires"},
		"relative policy path":      {[]string{"--release-identity", raw, "--maintenance-policy-file", "policy.json", "--maintenance-policy-sha256", strings.Repeat("a", 64)}, "maintenance policy guard requires"},
		"source without identity":   {[]string{"--maintenance-source", `{}`}, "--maintenance-source requires"},
		"incomplete source":         {[]string{"--release-identity", raw, "--maintenance-source", `{}`}, "--maintenance-source requires"},
		"null source":               {[]string{"--release-identity", raw, "--maintenance-source", `null`}, "--maintenance-source requires"},
	} {
		t.Run(name, func(t *testing.T) {
			err := RunAutomaticUpgrade(t.Context(), tc.args, io.Discard, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatal("policy flag reached host operations without valid exact authorization", err)
			}
		})
	}
}

func TestNativeSecurityHandoffPreservesStagedRouteAndEligibility(t *testing.T) {
	identity := nativeSecurityIdentity(releaseidentity.StableV1)
	route := releaseidentity.Hash([]byte("staged route"))
	until := time.Date(2099, 1, 1, 0, 0, 0, 1234, time.UTC)
	guard := nativeSecurityAuthorization{Route: route, Until: until, PolicyFile: "/etc/hikyo/security-maintenance.json", PolicySHA256: releaseidentity.Hash([]byte("local policy"))}
	handoff, err := automaticReleaseHandoffWithAuthorization(selfupdate.PreparedNightly{Identity: identity, BinaryPath: "verified-executable"}, "/etc/hikyo/upgrade.json", &identity, guard)
	if err != nil {
		t.Fatal(err)
	}
	for flag, value := range map[string]string{"--route-sha256": string(route), "--eligibility-until": until.Format(time.RFC3339Nano), "--maintenance-policy-file": guard.PolicyFile, "--maintenance-policy-sha256": string(guard.PolicySHA256)} {
		index := slices.Index(handoff.Arguments, flag)
		if index < 0 || index+1 >= len(handoff.Arguments) || handoff.Arguments[index+1] != value {
			t.Fatal("handoff lost policy context", handoff.Arguments)
		}
	}
	if _, err := automaticReleaseHandoffWithAuthorization(selfupdate.PreparedNightly{Identity: identity, BinaryPath: "verified-executable"}, "/etc/hikyo/upgrade.json", nil, guard); err == nil {
		t.Fatal("attached security policy to legacy nightly selection")
	}
}

func TestNativeSecurityEligibilityExpiredAfterStagingRefusesMaintenance(t *testing.T) {
	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	until := start.Add(time.Minute)
	if err := requireAutomaticEligibility(until, start); err != nil {
		t.Fatal("eligible target could not begin staging", err)
	}
	if err := requireAutomaticEligibility(until, until); err == nil {
		t.Fatal("staging completed at expiry but maintenance was still permitted")
	}
	if err := requireAutomaticEligibility(until, until.Add(time.Second)); err == nil {
		t.Fatal("expired selection could stop an admitted healthy source")
	}
	if err := requireAutomaticEligibility(time.Time{}, until.Add(time.Hour)); err != nil {
		t.Fatal("changed manual/nightly upgrades with no eligibility policy", err)
	}
}

func TestNativeSecurityFinalAuthorizationCannotCrossAdmissionDeadline(t *testing.T) {
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	until := now.Add(time.Minute)
	called := false
	err := authorizeAutomaticMutation(until, func() time.Time { return now }, func() error {
		called = true
		now = until
		return nil
	})
	if !called || !errors.Is(err, securitymaintenance.ErrNotStarted) {
		t.Fatal("long source/policy check admitted a stop after expiry", err)
	}
}

func TestNativeSecurityRoutePinRejectsSameTargetWithChangedRoute(t *testing.T) {
	route, _, _, _ := automaticApplyFixture(t, 2)
	if err := requireAutomaticRouteDigest(route, route.Plan.Digest()); err != nil {
		t.Fatal(err)
	}
	for _, source := range route.Bundle.Sources(releaseidentity.SQLite) {
		if !source.Identity.IsRelease() || source.Identity.Release != route.Plan.Steps()[0].Target {
			continue
		}
		changed, err := route.Bundle.Plan(source, route.Plan.Target())
		if err != nil {
			t.Fatal(err)
		}
		if changed.Target() != route.Plan.Target() || changed.Digest() == route.Plan.Digest() {
			t.Fatal("fixture did not produce a distinct authenticated route to the same target")
		}
		other := route
		other.Plan = changed
		if err := requireAutomaticRouteDigest(other, route.Plan.Digest()); err == nil {
			t.Fatal("changed authenticated route passed the pre-maintenance pin")
		}
		return
	}
	t.Fatal("fixture lacks the intermediate authenticated source")
}
