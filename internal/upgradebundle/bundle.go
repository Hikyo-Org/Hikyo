// Package upgradebundle loads bounded offline release evidence through the
// signature-backed trust factories. Bundle paths and inventory indexes are
// transport hints, never previous-version or trust-root authority.
package upgradebundle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

const IndexFormat = "hikyo.dev/offline-upgrade-bundle/v1"
const maxBundleDocuments = 64 << 20

var keyIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type ReleaseEntry struct {
	Profile        releaseidentity.Profile `json:"profile"`
	ManifestSHA256 releaseidentity.Digest  `json:"manifest_sha256"`
}

type Index struct {
	Format        string                   `json:"format"`
	PrimaryKeyIDs []string                 `json:"primary_key_ids"`
	Releases      []ReleaseEntry           `json:"releases"`
	Bridges       []releaseidentity.Digest `json:"bridges"`
}

// Bundle holds immutable authenticated documents, not mutable downloaded
// artifact paths. The executor must separately pin and verify each payload.
type Bundle struct {
	snapshot releasetrust.Snapshot
	nodes    []upgradecompat.VerifiedNode
	bridges  []releasetrust.VerifiedBridge
	releases map[releaseidentity.Identity]releasetrust.VerifiedRelease
}

func (b Bundle) Valid() bool                     { return b.snapshot.Valid() }
func (b Bundle) Snapshot() releasetrust.Snapshot { return b.snapshot }
func (b Bundle) Plan(source upgradecompat.InstalledSource, target releaseidentity.Identity) (upgradecompat.Plan, error) {
	return upgradecompat.PlanRoute(b.snapshot, source, target, b.nodes, b.bridges)
}
func (b Bundle) Release(identity releaseidentity.Identity) (releasetrust.VerifiedRelease, error) {
	release, ok := b.releases[identity]
	if !ok {
		return releasetrust.VerifiedRelease{}, errors.New("release absent from authenticated bundle")
	}
	return release, nil
}
func (b Bundle) Manifest(identity releaseidentity.Identity, engine releaseidentity.Engine) (releaseidentity.MigrationManifest, error) {
	for _, node := range b.nodes {
		if node.Identity() == identity {
			return node.Manifest(engine)
		}
	}
	return releaseidentity.MigrationManifest{}, errors.New("source release absent from authenticated bundle")
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
	reader := documentReader{ctx: ctx, root: root}
	raw, err := reader.read("index.json")
	if err != nil {
		return Bundle{}, err
	}
	var index Index
	if definitions.DecodeStrict(raw, &index) != nil || index.Format != IndexFormat || index.PrimaryKeyIDs == nil || len(index.PrimaryKeyIDs) > 256 || index.Releases == nil || len(index.Releases) == 0 || len(index.Releases) > upgradecompat.MaxReleases || index.Bridges == nil || len(index.Bridges) > upgradecompat.MaxEdges {
		return Bundle{}, errors.New("invalid bounded offline bundle index")
	}
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
	return bundle, nil
}

type documentReader struct {
	ctx          context.Context
	root         *os.Root
	bytes        int
	payloadBytes int64
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
	return candidates
}
