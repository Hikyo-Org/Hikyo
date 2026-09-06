package upgradebundle

import (
	"errors"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// ReferencedReleases returns a bounded, deterministic set of exact release
// identities named by authenticated source edges and recovery bridges for this
// engine. References are discovery hints, not authenticated release envelopes or
// an execution route. Fetch exact matching proofs and call Plan after inspection.
func (b Bundle) ReferencedReleases(engine releaseidentity.Engine) ([]releaseidentity.Identity, error) {
	if !b.Valid() || engine.Validate() != nil {
		return nil, errors.New("release discovery requires an authenticated bundle and supported engine")
	}
	seen := map[releaseidentity.Identity]bool{}
	manifests := map[releaseidentity.Digest]releaseidentity.Identity{}
	add := func(identity releaseidentity.Identity) error {
		if previous, ok := manifests[identity.ManifestSHA256]; ok && previous != identity {
			return errors.New("conflicting identities for referenced release manifest")
		}
		manifests[identity.ManifestSHA256] = identity
		seen[identity] = true
		if len(seen) > upgradecompat.MaxReleases {
			return errors.New("referenced release inventory exceeds bound")
		}
		return nil
	}
	for _, node := range b.nodes {
		for _, identity := range node.ReferencedReleases(engine) {
			if err := add(identity); err != nil {
				return nil, err
			}
		}
	}
	for _, bridge := range b.bridges {
		statement := bridge.Statement()
		if statement.TargetMigrations.Engine != engine {
			continue
		}
		if err := add(statement.Target); err != nil {
			return nil, err
		}
		if statement.SourceIdentity().IsRelease() {
			if err := add(statement.Source); err != nil {
				return nil, err
			}
		}
	}
	result := make([]releaseidentity.Identity, 0, len(seen))
	for identity := range seen {
		result = append(result, identity)
	}
	slices.SortFunc(result, func(a, b releaseidentity.Identity) int {
		return strings.Compare(string(a.ManifestSHA256), string(b.ManifestSHA256))
	})
	return result, nil
}
