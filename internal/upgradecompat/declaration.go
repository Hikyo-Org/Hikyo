// Package upgradecompat binds authenticated release declarations and plans
// bounded deterministic upgrade routes. A plan never grants execution authority.
package upgradecompat

import (
	"errors"
	"fmt"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

const Schema = "hikyo.dev/upgrade-compatibility/v1"
const MaxReleases = 256
const MaxEdges = 1024
const MaxHops = 32

type Mode string

const (
	Restart     Mode = "restart"
	Maintenance Mode = "maintenance"
)

type SourceEdge struct {
	Source       releaseidentity.Source            `json:"source"`
	Migrations   releaseidentity.MigrationManifest `json:"migrations"`
	SchemaSHA256 releaseidentity.Digest            `json:"schema_sha256"`
	Mode         Mode                              `json:"mode"`
}

type EngineDeclaration struct {
	Migrations   releaseidentity.MigrationManifest `json:"migrations"`
	SchemaSHA256 releaseidentity.Digest            `json:"schema_sha256"`
	Sources      []SourceEdge                      `json:"sources"`
}

// Declaration excludes its own and its containing manifest's digest. Those
// are supplied by the separately authenticated release envelope.
type Declaration struct {
	Schema   string                  `json:"schema"`
	Profile  releaseidentity.Profile `json:"profile"`
	Version  string                  `json:"version"`
	Sequence uint64                  `json:"sequence"`
	Commit   string                  `json:"commit"`
	Engines  []EngineDeclaration     `json:"engines"`
}

func Parse(raw []byte) (Declaration, error) {
	if len(raw) == 0 || len(raw) > releasetrust.MaxDocumentBytes {
		return Declaration{}, errors.New("compatibility declaration exceeds byte bound or is empty")
	}
	var declaration Declaration
	if err := definitions.DecodeStrict(raw, &declaration); err != nil {
		return Declaration{}, err
	}
	if err := declaration.Validate(); err != nil {
		return Declaration{}, err
	}
	return declaration, nil
}

func (d Declaration) Validate() error {
	if d.Schema != Schema {
		return errors.New("unknown compatibility schema")
	}
	if err := releaseidentity.ValidateTarget(d.Profile, d.Version, d.Sequence, d.Commit); err != nil {
		return err
	}
	if len(d.Engines) == 0 || len(d.Engines) > 2 {
		return errors.New("declaration must name one or both supported engines")
	}
	seen := map[releaseidentity.Engine]bool{}
	totalEdges := 0
	for _, engine := range d.Engines {
		if engine.SchemaSHA256.Validate() != nil {
			return errors.New("target lacks exact schema digest")
		}
		if err := engine.Migrations.Validate(); err != nil {
			return err
		}
		if seen[engine.Migrations.Engine] {
			return errors.New("duplicate engine declaration")
		}
		seen[engine.Migrations.Engine] = true
		if engine.Sources == nil {
			return errors.New("source edges must be a non-null array")
		}
		totalEdges += len(engine.Sources)
		if totalEdges > MaxEdges {
			return errors.New("declaration exceeds edge bound")
		}
		sources := map[releaseidentity.Source]bool{}
		for _, edge := range engine.Sources {
			if edge.SchemaSHA256.Validate() != nil {
				return errors.New("source lacks exact schema digest")
			}
			if err := edge.Source.Validate(); err != nil {
				return err
			}
			if sources[edge.Source] {
				return errors.New("duplicate source edge")
			}
			sources[edge.Source] = true
			if edge.Migrations.Validate() != nil || edge.Migrations.Engine != engine.Migrations.Engine {
				return errors.New("source migration engine differs from target")
			}
			if edge.Mode != Restart && edge.Mode != Maintenance {
				return errors.New("unknown edge mode")
			}
			if edge.Source.IsRelease() {
				if edge.Source.Release.Sequence >= d.Sequence {
					return errors.New("descending or same-release edge")
				}
				if edge.Source.Release.Profile != d.Profile {
					return errors.New("profile transitions require a recovery bridge")
				}
			} else {
				if edge.SchemaSHA256.Validate() != nil {
					return errors.New("genesis edge lacks exact schema digest")
				}
				if edge.Source.Genesis == releaseidentity.FreshGenesisV1 && len(edge.Migrations.Entries) != 0 {
					return errors.New("fresh genesis cannot contain migrations")
				}
			}
			if edge.Mode == Restart && (!sameManifest(edge.Migrations, engine.Migrations) || edge.SchemaSHA256 != engine.SchemaSHA256) {
				return errors.New("restart edge changes migrations")
			}
		}
	}
	return nil
}

func (d Declaration) Manifest(engine releaseidentity.Engine) (releaseidentity.MigrationManifest, error) {
	for _, declaration := range d.Engines {
		if declaration.Migrations.Engine == engine {
			return declaration.Migrations.Clone(), nil
		}
	}
	return releaseidentity.MigrationManifest{}, errors.New("release does not support requested engine")
}

type nodeState struct {
	release     releasetrust.VerifiedRelease
	declaration Declaration
}
type VerifiedNode struct{ state *nodeState }

func Bind(release releasetrust.VerifiedRelease, raw []byte) (VerifiedNode, error) {
	if !release.Valid() {
		return VerifiedNode{}, errors.New("unverified release envelope")
	}
	identity := release.Identity()
	if releaseidentity.Hash(raw) != identity.CompatibilitySHA256 {
		return VerifiedNode{}, errors.New("compatibility artifact digest mismatch")
	}
	declaration, err := Parse(raw)
	if err != nil {
		return VerifiedNode{}, err
	}
	if declaration.Profile != identity.Profile || declaration.Version != identity.Version || declaration.Sequence != identity.Sequence || declaration.Commit != identity.Commit {
		return VerifiedNode{}, errors.New("declaration target differs from authenticated release")
	}
	return VerifiedNode{state: &nodeState{release: release, declaration: declaration}}, nil
}

func (n VerifiedNode) Valid() bool { return n.state != nil && n.state.release.Valid() }
func (n VerifiedNode) Identity() releaseidentity.Identity {
	if !n.Valid() {
		return releaseidentity.Identity{}
	}
	return n.state.release.Identity()
}
func (n VerifiedNode) Manifest(engine releaseidentity.Engine) (releaseidentity.MigrationManifest, error) {
	if !n.Valid() {
		return releaseidentity.MigrationManifest{}, errors.New("unverified node")
	}
	return n.state.declaration.Manifest(engine)
}

func (n VerifiedNode) SchemaDigest(engine releaseidentity.Engine) (releaseidentity.Digest, error) {
	if !n.Valid() {
		return "", errors.New("unverified node")
	}
	for _, declaration := range n.state.declaration.Engines {
		if declaration.Migrations.Engine == engine {
			return declaration.SchemaSHA256, nil
		}
	}
	return "", errors.New("release does not support requested engine")
}

// GenesisSources exposes signed inspection candidates only. A caller must
// still compare one against an actually inspected datastore before PlanRoute.
func (n VerifiedNode) GenesisSources(engine releaseidentity.Engine) []InstalledSource {
	result := []InstalledSource{}
	if !n.Valid() {
		return result
	}
	for _, declaration := range n.state.declaration.Engines {
		if declaration.Migrations.Engine != engine {
			continue
		}
		for _, edge := range declaration.Sources {
			if edge.Source.Genesis != "" {
				result = append(result, InstalledSource{Identity: edge.Source, Migrations: edge.Migrations.Clone(), SchemaSHA256: edge.SchemaSHA256})
			}
		}
	}
	return result
}

func sameManifest(a, b releaseidentity.MigrationManifest) bool {
	return a.Engine == b.Engine && slices.Equal(a.Entries, b.Entries)
}

func nodeKey(source releaseidentity.Source) string {
	if source.Genesis != "" {
		return "genesis:" + source.Genesis
	}
	i := source.Release
	return fmt.Sprintf("%s:%d:%s:%s:%s:%s", i.Profile, i.Sequence, i.Version, i.Commit, i.CompatibilitySHA256, i.ManifestSHA256)
}

// ReferencedReleases returns exact signed source references for evidence
// discovery. The referenced envelopes must still be authenticated independently.
func (n VerifiedNode) ReferencedReleases(engine releaseidentity.Engine) []releaseidentity.Identity {
	result := []releaseidentity.Identity{}
	if !n.Valid() {
		return result
	}
	for _, declaration := range n.state.declaration.Engines {
		if declaration.Migrations.Engine != engine {
			continue
		}
		for _, edge := range declaration.Sources {
			if edge.Source.IsRelease() {
				result = append(result, edge.Source.Release)
			}
		}
	}
	return result
}
