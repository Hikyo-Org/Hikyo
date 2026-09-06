package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type automaticReleaseSource interface {
	Releases(context.Context) ([]updatecheck.Release, error)
	ReleaseByVersion(context.Context, string) (updatecheck.Release, error)
}

func nightlyStatus(release updatecheck.Release) (updatecheck.Status, error) {
	if !release.Prerelease || !release.Immutable || !slices.ContainsFunc(release.Assets, func(asset updatecheck.Asset) bool { return asset.Name == "release-manifest.sigstore.json" }) {
		return updatecheck.Status{}, errors.New("automatic upgrade requires an immutable signed nightly")
	}
	return updatecheck.Status{Available: true, Channel: updatecheck.ChannelNightly, LatestVersion: release.Version, Prerelease: true, Immutable: true, Assets: release.Assets, URL: release.URL}, nil
}

func selectAutomaticRelease(ctx context.Context, source automaticReleaseSource, version string) (updatecheck.Status, error) {
	if version != "" {
		release, err := source.ReleaseByVersion(ctx, version)
		if err != nil {
			return updatecheck.Status{}, err
		}
		return nightlyStatus(release)
	}
	releases, err := source.Releases(ctx)
	if err != nil {
		return updatecheck.Status{}, err
	}
	releases = slices.DeleteFunc(releases, func(release updatecheck.Release) bool { _, err := nightlyStatus(release); return err != nil })
	status, err := updatecheck.Select("0.0.0", updatecheck.ChannelNightly, releases)
	if err != nil {
		return status, err
	}
	if !status.Available {
		return status, errors.New("no signed nightly release is available")
	}
	return status, nil
}

type automaticRoute struct {
	Bundle      upgradebundle.Bundle
	Directory   string
	Plan        upgradecompat.Plan
	Instance    string
	Executables map[releaseidentity.Identity]selfupdate.PreparedNightly
}

type automaticReleasePreparer interface {
	PrepareNightlySource(context.Context, updatecheck.Status, releaseidentity.Identity) (selfupdate.PreparedNightly, error)
	AssembleNightlyRoute(context.Context, selfupdate.PreparedNightly, []selfupdate.PreparedNightly) (string, error)
}

// prepareAutomaticRoute follows only authenticated identity references. It
// stops as soon as the actual source has a complete authenticated route, so a
// recent installation does not download unrelated predecessor history.
func prepareAutomaticRoute(ctx context.Context, installer automaticReleasePreparer, source automaticReleaseSource, target selfupdate.PreparedNightly, pinned releasetrust.PinnedTrust, database upgrade.Config, previous *automaticJournal) (automaticRoute, error) {
	return discoverAutomaticRoute(ctx, installer, source, target, pinned, automaticStore{database}, database.Engine, previous)
}

func discoverAutomaticRoute(ctx context.Context, installer automaticReleasePreparer, source automaticReleaseSource, target selfupdate.PreparedNightly, pinned releasetrust.PinnedTrust, database automaticInspection, engine releaseidentity.Engine, previous *automaticJournal) (automaticRoute, error) {
	result := automaticRoute{Directory: target.BundleDirectory, Executables: map[releaseidentity.Identity]selfupdate.PreparedNightly{target.Identity: target}}
	floor := releaseidentity.SnapshotFloor{}
	control, err := database.Control(ctx)
	if err == nil {
		floor = control.Floor
	} else if !errors.Is(err, upgrade.ErrAbsent) {
		return result, err
	}
	resume := previous != nil && previous.Phase != "complete"
	preferred := control.Applied
	if resume {
		if previous.Target != target.Identity {
			return result, errors.New("unfinished upgrade pins a different exact target")
		}
		preferred = previous.Source.Identity
	}
	var lastPlanError error
	depths := map[releaseidentity.Identity]int{target.Identity: 0}
	expandedDepth := 0
	for {
		result.Bundle, err = upgradebundle.Load(ctx, result.Directory, pinned, floor)
		if err != nil {
			return result, err
		}
		floor = result.Bundle.Snapshot().Floor()
		var candidatePlan upgradecompat.Plan
		var candidateInstance string
		if resume {
			// Physical schema may already belong to an intermediate hop. Retain
			// the original source and route; apply reconciliation measures its
			// exact durable position before any process can migrate or serve.
			plan, planErr := result.Bundle.Plan(previous.Source, target.Identity)
			if planErr == nil && plan.Digest() == previous.Route {
				result.Plan, result.Instance = plan, previous.Instance
				return result, nil
			}
			lastPlanError = errors.New("unfinished upgrade no longer has its exact authenticated original route")
		} else {
			for _, candidate := range result.Bundle.Sources(engine) {
				actual, inspectErr := database.Installed(ctx, candidate.Migrations)
				digest, digestErr := candidate.Migrations.Digest()
				if inspectErr != nil || digestErr != nil || actual.Source != candidate.Identity || actual.MigrationDigest != digest || actual.SchemaDigest != candidate.SchemaSHA256 || actual.InstanceID == "" {
					continue
				}
				plan, planErr := result.Bundle.Plan(candidate, target.Identity)
				if planErr == nil {
					if len(plan.Steps()) == 0 {
						result.Plan, result.Instance = plan, actual.InstanceID
						return result, nil
					}
					candidatePlan, candidateInstance = plan, actual.InstanceID
					break
				}
				lastPlanError = fmt.Errorf("installed source has no authenticated route: %w", planErr)
			}
		}
		references, err := result.Bundle.ReferencedReleases(engine)
		if err != nil {
			return result, err
		}
		references = slices.DeleteFunc(references, func(identity releaseidentity.Identity) bool {
			_, present := result.Executables[identity]
			// Declaration and bridge verification require strictly increasing
			// sequence edges. Nodes beyond the final target or before an exact
			// installed release cannot participate in its route. This only prunes
			// discovery; signatures, actual inspection and Plan grant authority.
			return present || identity.Sequence > target.Identity.Sequence || (preferred.IsRelease() && identity.Sequence < preferred.Release.Sequence)
		})
		for _, identity := range references {
			if depth, exists := depths[identity]; !exists || expandedDepth+1 < depth {
				depths[identity] = expandedDepth + 1
			}
		}
		slices.SortFunc(references, func(a, b releaseidentity.Identity) int {
			if depths[a] < depths[b] {
				return -1
			}
			if depths[a] > depths[b] {
				return 1
			}
			if preferred.IsRelease() {
				if a == preferred.Release && b != preferred.Release {
					return -1
				}
				if b == preferred.Release && a != preferred.Release {
					return 1
				}
			}
			if a.Sequence > b.Sequence {
				return -1
			}
			if a.Sequence < b.Sequence {
				return 1
			}
			return strings.Compare(string(a.ManifestSHA256), string(b.ManifestSHA256))
		})
		// Unknown nodes at a shallower depth could supply a shorter route or
		// change its deterministic tie break. Resolve that frontier first;
		// older nodes beyond the found route cannot improve it.
		if candidatePlan.Valid() && (len(references) == 0 || depths[references[0]] >= len(candidatePlan.Steps())) {
			result.Plan, result.Instance = candidatePlan, candidateInstance
			return result, nil
		}
		if len(references) == 0 {
			if lastPlanError != nil {
				return result, lastPlanError
			}
			return result, errors.New("installed database does not match an authenticated release or legacy bridge")
		}
		if len(result.Executables) >= upgradecompat.MaxReleases {
			return result, errors.New("automatic upgrade graph exceeds release bound")
		}
		identity := references[0]
		if identity.Profile != releaseidentity.NightlyV1 {
			return result, errors.New("automatic nightly upgrade cannot cross into a stable release")
		}
		release, err := source.ReleaseByVersion(ctx, identity.Version)
		if err != nil {
			return result, err
		}
		status, err := nightlyStatus(release)
		if err != nil {
			return result, err
		}
		prepared, err := installer.PrepareNightlySource(ctx, status, identity)
		if err != nil {
			return result, err
		}
		if prepared.Identity != identity {
			return result, errors.New("prepared source differs from authenticated reference")
		}
		result.Executables[identity] = prepared
		expandedDepth = depths[identity]
		evidence := make([]selfupdate.PreparedNightly, 0, len(result.Executables)-1)
		for identity, prepared := range result.Executables {
			if identity != target.Identity {
				evidence = append(evidence, prepared)
			}
		}
		slices.SortFunc(evidence, func(a, b selfupdate.PreparedNightly) int {
			return strings.Compare(string(a.Identity.ManifestSHA256), string(b.Identity.ManifestSHA256))
		})
		result.Directory, err = installer.AssembleNightlyRoute(ctx, target, evidence)
		if err != nil {
			return result, err
		}
	}
}
