package app

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/securitydeployment"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type coordinatorProbe struct {
	follower                *coordinatorFollower
	checks, prepares, swaps int
	prepared                []releaseidentity.Identity
	bound                   []securitymaintenance.Target
	replacedImage           string
}

const probeImage = "registry.example/hikyo@sha256:0000000000000000000000000000000000000000000000000000000000000001"

func newCoordinatorProbe(ownVersion string) *coordinatorProbe {
	p := &coordinatorProbe{}
	p.follower = &coordinatorFollower{
		own:   func() (upgradecompat.Declaration, error) { return upgradecompat.Declaration{Version: ownVersion}, nil },
		check: func() error { p.checks++; return nil },
		prepare: func(_ context.Context, id releaseidentity.Identity, _ string) (preparedSecurityRelease, error) {
			p.prepares++
			p.prepared = append(p.prepared, id)
			return preparedSecurityRelease{}, nil
		},
		bind: func(_ releasetrust.VerifiedRelease, target securitymaintenance.Target) error {
			p.bound = append(p.bound, target)
			return nil
		},
		replace: func(_ context.Context, _ preparedSecurityRelease, image string) error {
			p.swaps++
			p.replacedImage = image
			return nil
		},
	}
	return p
}

func runningState(profile releaseidentity.Profile) securitymaintenance.Status {
	identity := nativeSecurityIdentity(profile)
	running := securitymaintenance.Observation{Engine: releaseidentity.SQLite, Identity: identity, Image: probeImage, Instance: "installation", Generation: 3,
		Incarnation: string(releaseidentity.Hash([]byte("incarnation"))), Phase: "healthy", Ready: true}
	return securitymaintenance.Status{Phase: "current", Running: &running}
}

func TestCoordinatorFollowsOnlyAnOlderRelease(t *testing.T) {
	for _, tc := range []struct {
		name    string
		own     string
		profile releaseidentity.Profile
		swaps   int
	}{
		{"older coordinator", "1.3.0", releaseidentity.StableV1, 1},
		{"equal release costs nothing", "1.3.1", releaseidentity.StableV1, 0},
		{"newer coordinator is never downgraded", "1.4.0", releaseidentity.StableV1, 0},
		{"newer coordinator under a nightly image", "1.4.0", releaseidentity.NightlyV1, 0},
		{"nightly coordinator under a stable image", "1.3.1-nightly.9", releaseidentity.StableV1, 0},
		{"stable coordinator under its nightly image", "1.3.1", releaseidentity.NightlyV1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := newCoordinatorProbe(tc.own)
			state := runningState(tc.profile)
			if err := probe.follower.follow(t.Context(), "cache", state); err != nil {
				t.Fatal(err)
			}
			if probe.swaps != tc.swaps || probe.prepares != tc.swaps || probe.checks != tc.swaps {
				t.Fatalf("checks=%d prepares=%d swaps=%d, want %d of each", probe.checks, probe.prepares, probe.swaps, tc.swaps)
			}
			if tc.swaps == 1 && probe.prepared[0] != state.Running.Identity {
				t.Fatalf("prepared %+v, want the running release", probe.prepared[0])
			}
		})
	}
}

func TestCoordinatorWaitsForAQuietHealthyJournal(t *testing.T) {
	pending := runningState(releaseidentity.StableV1)
	pending.Operation = &securitymaintenance.Operation{}
	unhealthy := runningState(releaseidentity.StableV1)
	unhealthy.Running.Ready = false
	unobserved := runningState(releaseidentity.StableV1)
	unobserved.Running = nil
	cases := map[string]securitymaintenance.Status{"operation pending": pending, "running not healthy": unhealthy, "running unknown": unobserved}
	for _, phase := range []string{"applying", "failed", "blocked", "stale", "staged", "applicable", "enrolled"} {
		state := runningState(releaseidentity.StableV1)
		state.Phase = phase
		cases["phase "+phase] = state
	}
	for name, state := range cases {
		t.Run(name, func(t *testing.T) {
			probe := newCoordinatorProbe("1.3.0")
			if err := probe.follower.follow(t.Context(), "cache", state); err != nil || probe.checks+probe.prepares+probe.swaps != 0 {
				t.Fatalf("err=%v checks=%d prepares=%d swaps=%d, want no action", err, probe.checks, probe.prepares, probe.swaps)
			}
		})
	}
	healthy := runningState(releaseidentity.StableV1)
	healthy.Phase = "healthy"
	probe := newCoordinatorProbe("1.3.0")
	if err := probe.follower.follow(t.Context(), "cache", healthy); err != nil || probe.swaps != 1 {
		t.Fatalf("a freshly proven update must follow: err=%v swaps=%d", err, probe.swaps)
	}
}

func TestCoordinatorSkipsAnExecutableItCannotOwnWithoutDownloading(t *testing.T) {
	probe := newCoordinatorProbe("1.3.0")
	probe.follower.check = func() error { return errors.New("not root installed") }
	if err := probe.follower.follow(t.Context(), "cache", runningState(releaseidentity.StableV1)); err != nil || probe.prepares != 0 || probe.swaps != 0 {
		t.Fatalf("err=%v prepares=%d swaps=%d, want a quiet skip before any download", err, probe.prepares, probe.swaps)
	}
	probe = newCoordinatorProbe("1.3.0")
	probe.follower.own = func() (upgradecompat.Declaration, error) {
		return upgradecompat.Declaration{}, errors.New("development build")
	}
	if err := probe.follower.follow(t.Context(), "cache", runningState(releaseidentity.StableV1)); err != nil || probe.prepares != 0 {
		t.Fatalf("err=%v prepares=%d, want a skip for a build with no release identity", err, probe.prepares)
	}
}

func TestCoordinatorFailureIsRetriedAndNeverChangesTheEngineResult(t *testing.T) {
	probe := newCoordinatorProbe("1.3.0")
	state := runningState(releaseidentity.StableV1)
	before, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	probe.follower.prepare = func(context.Context, releaseidentity.Identity, string) (preparedSecurityRelease, error) {
		probe.prepares++
		return preparedSecurityRelease{}, errors.New("release artifact missing")
	}
	if err := probe.follower.follow(t.Context(), "cache", state); err == nil || !strings.Contains(err.Error(), "release artifact missing") || probe.swaps != 0 {
		t.Fatalf("prepare failure: err=%v swaps=%d", err, probe.swaps)
	}
	probe.follower.prepare = func(context.Context, releaseidentity.Identity, string) (preparedSecurityRelease, error) {
		probe.prepares++
		return preparedSecurityRelease{}, nil
	}
	probe.follower.replace = func(context.Context, preparedSecurityRelease, string) error {
		return errors.New("read-only file system")
	}
	if err := probe.follower.follow(t.Context(), "cache", state); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("replace failure: err=%v", err)
	}
	// Neither failure marks the release followed, so the next run tries again.
	probe.follower.replace = func(context.Context, preparedSecurityRelease, string) error { probe.swaps++; return nil }
	if err := probe.follower.follow(t.Context(), "cache", state); err != nil || probe.swaps != 1 {
		t.Fatalf("retry: err=%v swaps=%d", err, probe.swaps)
	}
	if after, _ := json.Marshal(state); string(after) != string(before) {
		t.Fatal("following the coordinator changed the engine's status")
	}
	// A long-lived process still embeds the old release; it must not fetch again.
	if err := probe.follower.follow(t.Context(), "cache", state); err != nil || probe.prepares != 3 {
		t.Fatalf("repeat: err=%v prepares=%d, want no further download", err, probe.prepares)
	}
}

// The serving container reports the running release, so a compromised one could
// name a newer signed release that the feed withdrew.
func TestCoordinatorRefusesAReleaseTheCatalogWithdrew(t *testing.T) {
	probe := newCoordinatorProbe("1.3.0")
	state := runningState(releaseidentity.StableV1)
	state.RunningWithdrawn = true
	if err := probe.follower.follow(t.Context(), "cache", state); err == nil || !strings.Contains(err.Error(), "withdrawn") || probe.prepares+probe.swaps != 0 {
		t.Fatalf("err=%v prepares=%d swaps=%d, want a refusal before any download", err, probe.prepares, probe.swaps)
	}
	state.RunningWithdrawn = false
	if err := probe.follower.follow(t.Context(), "cache", state); err != nil || probe.swaps != 1 {
		t.Fatalf("a release that is no longer withdrawn must follow: err=%v swaps=%d", err, probe.swaps)
	}
}

func TestCoordinatorBindsTheImageTheOwnerObservedToTheSignedRelease(t *testing.T) {
	probe := newCoordinatorProbe("1.3.0")
	state := runningState(releaseidentity.StableV1)
	probe.follower.bind = func(_ releasetrust.VerifiedRelease, target securitymaintenance.Target) error {
		probe.bound = append(probe.bound, target)
		return errors.New("security image digest is absent from independently signed release inventory")
	}
	if err := probe.follower.follow(t.Context(), "cache", state); err == nil || !strings.Contains(err.Error(), "absent from") || probe.swaps != 0 {
		t.Fatalf("an image outside the signed inventory was adopted: err=%v swaps=%d", err, probe.swaps)
	}
	if len(probe.bound) != 1 || probe.bound[0].Identity != state.Running.Identity || probe.bound[0].Image != probeImage {
		t.Fatalf("bound %+v, want the running identity and the owner's image", probe.bound)
	}
	probe.follower.bind = func(releasetrust.VerifiedRelease, securitymaintenance.Target) error { return nil }
	if err := probe.follower.follow(t.Context(), "cache", state); err != nil || probe.swaps != 1 || probe.replacedImage != probeImage {
		t.Fatalf("refusal was remembered as followed, or the wrong image was moved: err=%v swaps=%d image=%q", err, probe.swaps, probe.replacedImage)
	}
}

// Production wires signedSecurityImage; an unauthenticated release never passes it.
func TestCoordinatorProductionBindingRefusesAnUnauthenticatedRelease(t *testing.T) {
	probe := newCoordinatorProbe("1.3.0")
	f := newCoordinatorFollower(SecurityMaintenanceConfiguration{Kind: "compose"}, nil)
	f.own, f.check, f.prepare, f.replace = probe.follower.own, probe.follower.check, probe.follower.prepare, probe.follower.replace
	if err := f.follow(t.Context(), "cache", runningState(releaseidentity.StableV1)); err == nil || probe.swaps != 0 {
		t.Fatalf("err=%v swaps=%d, want a refusal", err, probe.swaps)
	}
}

func TestCoordinatorAfterRunFollowsHostAndClusterButNotNative(t *testing.T) {
	compose := SecurityMaintenanceConfiguration{Kind: "compose", StateDirectory: "/var/lib/hikyo-security-maintenance"}
	native := SecurityMaintenanceConfiguration{Kind: "native"}
	inCluster := SecurityMaintenanceConfiguration{Kind: "kubernetes", Deployment: &securitydeployment.Config{Kubernetes: &securitydeployment.KubernetesConfig{CronJob: "coordinator"}}}
	noCronJob := SecurityMaintenanceConfiguration{Kind: "kubernetes", Deployment: &securitydeployment.Config{Kubernetes: &securitydeployment.KubernetesConfig{}}}
	for name, tc := range map[string]struct {
		c    SecurityMaintenanceConfiguration
		want int
	}{"compose": {compose, 1}, "native": {native, 0}, "in-cluster": {inCluster, 1}, "in-cluster without a CronJob": {noCronJob, 0}} {
		t.Run(name, func(t *testing.T) {
			probe := newCoordinatorProbe("1.3.0")
			probe.follower.afterRun(t.Context(), tc.c, runningState(releaseidentity.StableV1))
			want := tc.want
			if runtime.GOOS != "linux" {
				want = 0
			}
			if probe.swaps != want {
				t.Fatalf("swaps=%d, want %d", probe.swaps, want)
			}
			if want == 1 && probe.replacedImage != probeImage {
				t.Fatalf("moved %q, want the image the deployment owner observed", probe.replacedImage)
			}
		})
	}
}
