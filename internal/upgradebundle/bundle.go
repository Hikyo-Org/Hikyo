// Package upgradebundle loads bounded offline release evidence through the
// signature-backed trust factories. Bundle paths and inventory indexes are
// transport hints, never previous-version or trust-root authority.
package upgradebundle

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

const IndexFormat = "hikyo.dev/offline-upgrade-bundle/v1"
const PlatformIndexFormat = "hikyo.dev/offline-upgrade-bundle/v2"

const maxBundleDocuments = 64 << 20

var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type ReleaseEntry struct {
	Profile        releaseidentity.Profile `json:"profile"`
	ManifestSHA256 releaseidentity.Digest  `json:"manifest_sha256"`
}

type Index struct {
	Format          string                   `json:"format"`
	NightlyPlatform string                   `json:"nightly_platform,omitempty"`
	PrimaryKeyIDs   []string                 `json:"primary_key_ids"`
	Releases        []ReleaseEntry           `json:"releases"`
	Bridges         []releaseidentity.Digest `json:"bridges"`
}

// Bundle holds immutable authenticated documents, not mutable downloaded
// artifact paths. The executor must separately pin and verify each payload.
type Bundle struct {
	snapshot       releasetrust.Snapshot
	materialDigest releaseidentity.Digest
	nodes          []upgradecompat.VerifiedNode
	bridges        []releasetrust.VerifiedBridge
	releases       map[releaseidentity.Identity]releasetrust.VerifiedRelease
	revocations    *revocationLookup
	clock          func() time.Time // nil is the real clock; tests move it
}

// MaterialDigest binds every loaded public document, including index and
// signatures. Authenticated manifests independently bind loaded payload bytes.
func (b Bundle) MaterialDigest() releaseidentity.Digest { return b.materialDigest }

func (b Bundle) Valid() bool                     { return b.snapshot.Valid() }
func (b Bundle) Snapshot() releasetrust.Snapshot { return b.snapshot }

// errMissingRevocationLookup refuses a bridge route on a bundle that was never given
// a revocation lookup: an unconfigured hook must not mean "nothing is revoked".
// ErrLegacyBridgeCannotResume refuses a resume through an expiry-free legacy v1 bridge. It is
// not the retired-signer refusal: no rotation makes such a route resumable.
var ErrLegacyBridgeCannotResume = errors.New("a journaled route through an expiry-free legacy v1 recovery bridge can never resume; only a security rollback may use it")

var errMissingRevocationLookup = errors.New("recovery bridge revocation check: no revocation lookup is configured, so no bridge route is usable")

// Plan is the route for a new admission: expired and revoked bridges are not used.
func (b Bundle) Plan(source upgradecompat.InstalledSource, target releaseidentity.Identity) (upgradecompat.Plan, error) {
	now := time.Now
	if b.clock != nil {
		now = b.clock
	}
	return b.PlanAt(source, target, now())
}

// PlanAt is Plan as of the instant now: the route an admission at that instant
// would have journaled.
func (b Bundle) PlanAt(source upgradecompat.InstalledSource, target releaseidentity.Identity, now time.Time) (upgradecompat.Plan, error) {
	plan, err := upgradecompat.PlanRouteAt(b.snapshot, source, target, b.nodes, b.bridges, now, nil)
	if errors.Is(err, upgradecompat.ErrDuplicateBridgePair) && b.revocations != nil {
		// A revoked statement and its re-signed replacement share a pair; the list
		// decides which one is live.
		revoked, lookupErr := b.revocations.lookup()
		if lookupErr != nil {
			return upgradecompat.Plan{}, fmt.Errorf("recovery bridge revocation check: %w", lookupErr)
		}
		plan, err = upgradecompat.PlanRouteAt(b.snapshot, source, target, b.nodes, b.bridges, now, revoked)
	}
	if err != nil || len(plan.BridgeDigests()) == 0 {
		return plan, err
	}
	if b.revocations == nil {
		return upgradecompat.Plan{}, errMissingRevocationLookup
	}
	// Only a route that actually uses a recovery bridge consults the revocation
	// list, so ordinary upgrades never touch the endpoint.
	revoked, err := b.revocations.lookup()
	if err != nil {
		return upgradecompat.Plan{}, fmt.Errorf("recovery bridge revocation check: %w", err)
	}
	if !slices.ContainsFunc(plan.BridgeDigests(), func(d releaseidentity.Digest) bool { return slices.Contains(revoked, d) }) {
		return plan, nil
	}
	// Route around the revoked bridges; with no other route the plan is refused.
	plan, err = upgradecompat.PlanRouteAt(b.snapshot, source, target, b.nodes, b.bridges, now, revoked)
	if err != nil {
		return upgradecompat.Plan{}, fmt.Errorf("recovery bridge revoked: %w", err)
	}
	return plan, nil
}

// PlanAdmitted validates the route an earlier admission journaled by digest, for
// resuming that upgrade. A bridge's not_after limits new admissions, so expiry is
// not consulted; a revoked bridge, or one under a root that still pins recovery-1, refuses it. The route need
// not be the one a fresh search would pick today: it must be a valid path in the
// authenticated graph with exactly the journaled digest.
func (b Bundle) PlanAdmitted(source upgradecompat.InstalledSource, target releaseidentity.Identity, journaled releaseidentity.Digest) (upgradecompat.Plan, error) {
	plan, err := upgradecompat.PlanRouteMatching(b.snapshot, source, target, b.nodes, b.bridges, nil, journaled)
	if err != nil || len(plan.BridgeDigests()) == 0 {
		return plan, err
	}
	// R2: a bridge under a root that still pins recovery-1 never authorizes a resume (the
	// journal is an unauthenticated file); only a security rollback ignores that.
	onRoute := func(bridge releasetrust.VerifiedBridge) bool {
		return slices.Contains(plan.BridgeDigests(), bridge.Digest())
	}
	if slices.ContainsFunc(b.bridges, func(bridge releasetrust.VerifiedBridge) bool { return bridge.RetiredSigner() && onRoute(bridge) }) {
		return upgradecompat.Plan{}, fmt.Errorf("recovery bridge on the journaled route: %w", releasetrust.ErrRetiredBridgeSigner)
	}
	// An expiry-free legacy v1 statement is never a route today, but an older build did route
	// it and the journal is an unauthenticated file, so it cannot be resumed either.
	if slices.ContainsFunc(b.bridges, func(bridge releasetrust.VerifiedBridge) bool {
		return bridge.Statement().NotAfter == "" && onRoute(bridge)
	}) {
		return upgradecompat.Plan{}, ErrLegacyBridgeCannotResume
	}
	if b.revocations == nil {
		return upgradecompat.Plan{}, errMissingRevocationLookup
	}
	revoked, err := b.revocations.lookup()
	if err != nil {
		return upgradecompat.Plan{}, fmt.Errorf("recovery bridge revocation check: %w", err)
	}
	if slices.ContainsFunc(plan.BridgeDigests(), func(d releaseidentity.Digest) bool { return slices.Contains(revoked, d) }) {
		return upgradecompat.Plan{}, errors.New("recovery bridge revoked: the journaled route uses a withdrawn bridge")
	}
	return plan, nil
}

// PlanRollback validates the journaled route for a security rollback. Neither
// expiry nor revocation blocks it: returning to the journaled source is never refused.
func (b Bundle) PlanRollback(source upgradecompat.InstalledSource, target releaseidentity.Identity, journaled releaseidentity.Digest) (upgradecompat.Plan, error) {
	return upgradecompat.PlanRouteMatching(b.snapshot, source, target, b.nodes, b.bridges, nil, journaled)
}

// revocationLookup runs the lookup once and reuses its outcome.
type revocationLookup struct {
	once    sync.Once
	fetch   func() ([]releaseidentity.Digest, error)
	revoked []releaseidentity.Digest
	err     error
}

func (l *revocationLookup) lookup() ([]releaseidentity.Digest, error) {
	l.once.Do(func() { l.revoked, l.err = l.fetch() })
	return l.revoked, l.err
}

// WithBridgeRevocations returns a copy whose plans consult the verified
// revocation list (fetched at most once) when the route uses a recovery bridge:
// a lookup error refuses the plan and a revoked bridge is routed around.
func (b Bundle) WithBridgeRevocations(fetch func() ([]releaseidentity.Digest, error)) Bundle {
	b.revocations = &revocationLookup{fetch: fetch}
	return b
}

// WithClock returns a copy whose new admissions are judged at the given clock
// instead of the wall clock, so a bridge's not_after can be crossed without waiting.
func (b Bundle) WithClock(now func() time.Time) Bundle {
	b.clock = now
	return b
}

func (b Bundle) Release(identity releaseidentity.Identity) (releasetrust.VerifiedRelease, error) {
	release, ok := b.releases[identity]
	if !ok {
		return releasetrust.VerifiedRelease{}, errors.New("release absent from authenticated bundle")
	}
	return release, nil
}

// GenesisManifests exposes signed candidate declarations for inspection. The
// caller must compare every candidate against the actual database catalog and
// goose history; this list itself does not identify the installed source.
func (b Bundle) GenesisManifests(engine releaseidentity.Engine) []upgradecompat.InstalledSource {
	candidates := []upgradecompat.InstalledSource{}
	for _, node := range b.nodes {
		for _, candidate := range node.GenesisSources(engine) {
			duplicate := false
			for _, existing := range candidates {
				a, _ := existing.Migrations.Digest()
				c, _ := candidate.Migrations.Digest()
				if existing.Identity == candidate.Identity && existing.SchemaSHA256 == candidate.SchemaSHA256 && a == c {
					duplicate = true
				}
			}
			if !duplicate {
				candidates = append(candidates, candidate)
			}
		}
	}
	return candidates
}

// Load uses caller-pinned installation/build trust and persisted rollback
// floors. A root or public key packaged as an archive member cannot override it.
// Only fixed filenames below digest-named release/bridge directories are read.
func Load(ctx context.Context, directory string, pinned releasetrust.PinnedTrust, floor releaseidentity.SnapshotFloor) (Bundle, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return Bundle{}, errors.New("open offline upgrade bundle")
	}
	defer root.Close()
	reader := documentReader{ctx: ctx, root: root, documents: map[string]releaseidentity.Digest{}}
	raw, err := reader.read("index.json")
	if err != nil {
		return Bundle{}, err
	}
	var index Index
	if definitions.DecodeStrict(raw, &index) != nil || !validIndexPlatform(index) || index.PrimaryKeyIDs == nil || len(index.PrimaryKeyIDs) > 256 || index.Releases == nil || len(index.Releases) == 0 || len(index.Releases) > upgradecompat.MaxReleases || index.Bridges == nil || len(index.Bridges) > upgradecompat.MaxEdges {
		return Bundle{}, errors.New("invalid bounded offline bundle index")
	}
	reader.nightlyPlatform = index.NightlyPlatform
	keys := map[string][]byte{}
	for _, id := range index.PrimaryKeyIDs {
		if !keyIDPattern.MatchString(id) || id == "." || id == ".." || keys[id] != nil {
			return Bundle{}, errors.New("invalid or duplicate offline key identifier")
		}
		keys[id], err = reader.read("keys/" + id + ".pub")
		if err != nil {
			return Bundle{}, err
		}
	}
	material := releasetrust.SnapshotMaterial{PrimaryKeys: keys}
	for _, file := range []struct {
		name   string
		target *[]byte
	}{{"metadata.json", &material.Metadata}, {"metadata.sigstore.json", &material.MetadataSignature}, {"catalog.json", &material.Catalog}, {"catalog.sigstore.json", &material.CatalogSignature}} {
		*file.target, err = reader.read(file.name)
		if err != nil {
			return Bundle{}, err
		}
	}
	var metadata releasetrust.Metadata
	if err := definitions.DecodeStrict(material.Metadata, &metadata); err != nil {
		return Bundle{}, err
	}
	if _, statErr := root.Lstat("stable-policy.json"); statErr == nil || metadata.Event.SignedBy == releasetrust.StableWorkflowSigner {
		for name, target := range map[string]*[]byte{"stable-policy.json": &material.StablePolicy, "stable-policy.sigstore.json": &material.StablePolicySignature, "stable-trusted-root.json": &material.StableTrustedRoot} {
			*target, err = reader.read(name)
			if err != nil {
				return Bundle{}, err
			}
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Bundle{}, statErr
	}
	snapshot, err := releasetrust.VerifySnapshot(pinned, material, floor)
	if err != nil {
		return Bundle{}, fmt.Errorf("authenticate offline trust snapshot: %w", err)
	}
	indexedBridges := slices.Clone(index.Bridges)
	authorizedBridges := snapshot.BridgeDigests()
	slices.Sort(indexedBridges)
	slices.Sort(authorizedBridges)
	if !slices.Equal(indexedBridges, authorizedBridges) {
		return Bundle{}, errors.New("offline index omits or substitutes current authorized bridge proofs")
	}
	bundle := Bundle{snapshot: snapshot, nodes: []upgradecompat.VerifiedNode{}, bridges: []releasetrust.VerifiedBridge{}, releases: map[releaseidentity.Identity]releasetrust.VerifiedRelease{}}
	seen := map[releaseidentity.Digest]bool{}
	for _, entry := range index.Releases {
		if entry.ManifestSHA256.Validate() != nil || seen[entry.ManifestSHA256] {
			return Bundle{}, errors.New("invalid or duplicate offline manifest identity")
		}
		seen[entry.ManifestSHA256] = true
		release, compatibility, err := reader.release(snapshot, entry)
		if err != nil {
			return Bundle{}, err
		}
		node, err := upgradecompat.Bind(release, compatibility)
		if err != nil {
			return Bundle{}, err
		}
		bundle.nodes = append(bundle.nodes, node)
		bundle.releases[release.Identity()] = release
	}
	seen = map[releaseidentity.Digest]bool{}
	for _, digest := range index.Bridges {
		if digest.Validate() != nil || seen[digest] {
			return Bundle{}, errors.New("invalid or duplicate offline bridge identity")
		}
		seen[digest] = true
		dir := "bridges/" + string(digest) + "/"
		raw, err := reader.read(dir + "statement.json")
		if err != nil {
			return Bundle{}, err
		}
		signature, err := reader.read(dir + "statement.sigstore.json")
		if err != nil {
			return Bundle{}, err
		}
		if releaseidentity.Hash(raw) != digest {
			return Bundle{}, errors.New("offline bridge directory digest mismatch")
		}
		bridge, err := releasetrust.VerifyBridge(snapshot, releasetrust.BridgeMaterial{Statement: raw, Signature: signature})
		if err != nil {
			return Bundle{}, err
		}
		bundle.bridges = append(bundle.bridges, bridge)
	}
	encodedMaterial, err := json.Marshal(reader.documents, jsonwire.Stable)
	if err != nil {
		return Bundle{}, err
	}
	bundle.materialDigest = releaseidentity.Hash(encodedMaterial)
	return bundle, nil
}

func validIndexPlatform(index Index) bool {
	return (index.Format == IndexFormat && index.NightlyPlatform == "") || (index.Format == PlatformIndexFormat && releasetrust.ValidNightlyPlatform(index.NightlyPlatform))
}

type documentReader struct {
	nightlyPlatform string
	ctx             context.Context
	root            *os.Root
	bytes           int
	payloadBytes    int64
	documents       map[string]releaseidentity.Digest
}

func (r *documentReader) read(name string) ([]byte, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	file, err := openDocument(r.root, name)
	if err != nil {
		return nil, errors.New("offline bundle member unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > releasetrust.MaxDocumentBytes {
		return nil, errors.New("offline bundle member is not a bounded regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(file, releasetrust.MaxDocumentBytes+1))
	if err != nil || len(raw) > releasetrust.MaxDocumentBytes {
		return nil, errors.New("offline bundle member read exceeds bound")
	}
	r.bytes += len(raw)
	if r.bytes > maxBundleDocuments {
		return nil, errors.New("offline bundle aggregate exceeds bound")
	}
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if r.documents != nil {
		r.documents[name] = releaseidentity.Hash(raw)
	}
	return slices.Clone(raw), nil
}

// MatchBuild binds the embedded declaration's exact bytes to an authenticated
// release envelope. The manifest's own digest cannot be embedded recursively.
func (b Bundle) MatchBuild(embeddedDeclaration []byte) (upgradecompat.VerifiedNode, error) {
	declaration, err := upgradecompat.Parse(embeddedDeclaration)
	if err != nil {
		return upgradecompat.VerifiedNode{}, err
	}
	digest := releaseidentity.Hash(embeddedDeclaration)
	var matched upgradecompat.VerifiedNode
	for _, node := range b.nodes {
		identity := node.Identity()
		if identity.CompatibilitySHA256 != digest {
			continue
		}
		if identity.Profile != declaration.Profile || identity.Version != declaration.Version || identity.Sequence != declaration.Sequence || identity.Commit != declaration.Commit || matched.Valid() {
			return upgradecompat.VerifiedNode{}, errors.New("ambiguous or mismatched embedded build declaration")
		}
		matched = node
	}
	if !matched.Valid() {
		return upgradecompat.VerifiedNode{}, errors.New("embedded build absent from authenticated bundle")
	}
	return matched, nil
}

// Sources lists independently authenticated inspection candidates. The caller
// must measure the actual database, then call Plan with that observed source.
func (b Bundle) Sources(engine releaseidentity.Engine) []upgradecompat.InstalledSource {
	candidates := b.GenesisManifests(engine)
	for _, node := range b.nodes {
		manifest, err := node.Manifest(engine)
		if err != nil {
			continue
		}
		schema, err := node.SchemaDigest(engine)
		if err != nil {
			continue
		}
		candidates = append(candidates, upgradecompat.InstalledSource{Identity: releaseidentity.Source{Release: node.Identity()}, Migrations: manifest, SchemaSHA256: schema})
	}
	for _, bridge := range b.bridges {
		statement := bridge.Statement()
		if statement.SourceMigrations.Engine != engine {
			continue
		}
		candidate := upgradecompat.InstalledSource{Identity: statement.SourceIdentity(), Migrations: statement.SourceMigrations, SchemaSHA256: statement.SourceSchemaSHA256}
		duplicate := false
		for _, existing := range candidates {
			if existing.Identity == candidate.Identity && existing.SchemaSHA256 == candidate.SchemaSHA256 && slices.Equal(existing.Migrations.Entries, candidate.Migrations.Entries) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}
