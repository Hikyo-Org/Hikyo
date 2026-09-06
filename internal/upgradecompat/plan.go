package upgradecompat

import (
	"encoding/json"
	"errors"
	"slices"

	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
)

// InstalledSource is inspected by F2 under the database gate. It is not an
// executable verified release. SchemaSHA256 binds the inspected domain catalog.
type InstalledSource struct {
	Identity     releaseidentity.Source            `json:"identity"`
	Migrations   releaseidentity.MigrationManifest `json:"migrations"`
	SchemaSHA256 releaseidentity.Digest            `json:"schema_sha256"`
}

type Step struct {
	Source             releaseidentity.Source            `json:"source"`
	Target             releaseidentity.Identity          `json:"target"`
	Mode               Mode                              `json:"mode"`
	SourceMigrations   releaseidentity.MigrationManifest `json:"source_migrations"`
	TargetMigrations   releaseidentity.MigrationManifest `json:"target_migrations"`
	SourceSchemaSHA256 releaseidentity.Digest            `json:"source_schema_sha256"`
	TargetSchemaSHA256 releaseidentity.Digest            `json:"target_schema_sha256"`
	SourcePolicySHA256 releaseidentity.Digest            `json:"source_policy_sha256,omitempty"`
	TargetPolicySHA256 releaseidentity.Digest            `json:"target_policy_sha256"`
	BridgeSHA256       releaseidentity.Digest            `json:"bridge_sha256,omitempty"`
	Artifacts          []releasetrust.Artifact           `json:"artifacts"`
}

type planState struct {
	source   InstalledSource
	target   releaseidentity.Identity
	steps    []Step
	digest   releaseidentity.Digest
	snapshot releaseidentity.Digest
}

// Plan's private state is built only from current verified nodes and bridges.
// It pins inventory digests, not an assertion that mutable local paths exist.
type Plan struct{ state *planState }

func (p Plan) Valid() bool { return p.state != nil }
func (p Plan) Digest() releaseidentity.Digest {
	if p.state == nil {
		return ""
	}
	return p.state.digest
}
func (p Plan) SnapshotDigest() releaseidentity.Digest {
	if p.state == nil {
		return ""
	}
	return p.state.snapshot
}
func (p Plan) Source() releaseidentity.Source {
	if p.state == nil {
		return releaseidentity.Source{}
	}
	return p.state.source.Identity
}
func (p Plan) Target() releaseidentity.Identity {
	if p.state == nil {
		return releaseidentity.Identity{}
	}
	return p.state.target
}
func (p Plan) SourceSchemaDigest() releaseidentity.Digest {
	if p.state == nil {
		return ""
	}
	return p.state.source.SchemaSHA256
}
func (p Plan) SourceManifest(engine releaseidentity.Engine) (releaseidentity.MigrationManifest, error) {
	if p.state == nil || p.state.source.Migrations.Engine != engine {
		return releaseidentity.MigrationManifest{}, errors.New("unverified plan or wrong source engine")
	}
	return p.state.source.Migrations.Clone(), nil
}
func (p Plan) Steps() []Step {
	if p.state == nil {
		return nil
	}
	steps := slices.Clone(p.state.steps)
	for i := range steps {
		steps[i].SourceMigrations = steps[i].SourceMigrations.Clone()
		steps[i].TargetMigrations = steps[i].TargetMigrations.Clone()
		steps[i].Artifacts = slices.Clone(steps[i].Artifacts)
	}
	return steps
}
func (p Plan) BridgeDigests() []releaseidentity.Digest {
	if p.state == nil {
		return nil
	}
	digests := []releaseidentity.Digest{}
	for _, step := range p.state.steps {
		if step.BridgeSHA256 != "" {
			digests = append(digests, step.BridgeSHA256)
		}
	}
	return digests
}

// RequiresOperatorAttestation exposes the exceptional-edge second-proof
// obligation. Ordinary maintenance still requires F4 backup evidence in F5.
func (p Plan) RequiresOperatorAttestation() bool { return len(p.BridgeDigests()) > 0 }

// PlanRoute chooses the fewest edges, breaking equal-length ties by ascending
// release sequence. Current-snapshot identity is mandatory for cached nodes.
func PlanRoute(snapshot releasetrust.Snapshot, source InstalledSource, target releaseidentity.Identity, nodes []VerifiedNode, bridges []releasetrust.VerifiedBridge) (Plan, error) {
	if !snapshot.Valid() || source.Identity.Validate() != nil || source.Migrations.Validate() != nil || target.Validate() != nil {
		return Plan{}, errors.New("unverified snapshot or invalid source/target")
	}
	if source.SchemaSHA256.Validate() != nil {
		return Plan{}, errors.New("source requires an exact inspected schema digest")
	}
	if len(nodes) > MaxReleases || len(bridges) > MaxEdges {
		return Plan{}, errors.New("upgrade graph exceeds bounds")
	}
	if len(bridges) != len(snapshot.BridgeDigests()) {
		return Plan{}, errors.New("complete current recovery bridge inventory required")
	}
	engine := source.Migrations.Engine
	byIdentity := map[releaseidentity.Identity]VerifiedNode{}
	bySequence := map[uint64]releaseidentity.Identity{}
	edgeCount := len(bridges)
	for _, node := range nodes {
		if !node.Valid() || node.state.release.SnapshotDigest() != snapshot.Digest() {
			return Plan{}, errors.New("node proof is missing or belongs to a stale trust snapshot")
		}
		id := node.Identity()
		if _, duplicate := byIdentity[id]; duplicate {
			return Plan{}, errors.New("duplicate release node")
		}
		if _, duplicate := bySequence[id.Sequence]; duplicate {
			return Plan{}, errors.New("inconsistent release sequence")
		}
		byIdentity[id] = node
		bySequence[id.Sequence] = id
		for _, declaration := range node.state.declaration.Engines {
			edgeCount += len(declaration.Sources)
		}
		if edgeCount > MaxEdges {
			return Plan{}, errors.New("upgrade graph exceeds edge bound")
		}
	}
	if source.Identity.IsRelease() {
		if _, exists := byIdentity[source.Identity.Release]; !exists && len(nodes) == MaxReleases {
			return Plan{}, errors.New("installed source exceeds graph release bound")
		}
	}
	targetNode, exists := byIdentity[target]
	if !exists {
		return Plan{}, errors.New("target is not independently authenticated")
	}
	targetManifest, err := targetNode.Manifest(engine)
	if err != nil {
		return Plan{}, err
	}
	if source.Identity.IsRelease() && source.Identity.Release == target {
		targetSchema, _ := targetNode.SchemaDigest(engine)
		if !sameManifest(source.Migrations, targetManifest) || source.SchemaSHA256 != targetSchema {
			return Plan{}, errors.New("same-release restart migration identity differs")
		}
	}
	adjacency := map[string][]Step{}
	for _, node := range nodes {
		for _, declaration := range node.state.declaration.Engines {
			if declaration.Migrations.Engine != engine {
				continue
			}
			for _, edge := range declaration.Sources {
				if edge.Source.Genesis == releaseidentity.LegacyGenesisV1 && node.Identity().Profile == releaseidentity.NightlyV1 {
					continue // populated unsigned installations require a recovery-signed legacy bridge
				}
				var sourcePolicy releaseidentity.Digest
				if edge.Source.IsRelease() {
					from, authenticated := byIdentity[edge.Source.Release]
					if !authenticated {
						continue
					} // no ordinary edge from revoked/unavailable source
					manifest, err := from.Manifest(engine)
					schema, _ := from.SchemaDigest(engine)
					if err != nil || !sameManifest(manifest, edge.Migrations) || schema != edge.SchemaSHA256 {
						return Plan{}, errors.New("declared source migrations differ from authenticated source")
					}
					sourcePolicy = from.state.release.PolicyDigest()
					if sourcePolicy != node.state.release.PolicyDigest() {
						continue // trust-policy transitions require a recovery bridge
					}
				} else if edge.Source != source.Identity || edge.SchemaSHA256 != source.SchemaSHA256 {
					continue
				}
				if edge.Source == source.Identity && (!sameManifest(source.Migrations, edge.Migrations) || source.SchemaSHA256 != edge.SchemaSHA256) {
					return Plan{}, errors.New("installed source migrations differ from declared source")
				}
				step := Step{Source: edge.Source, Target: node.Identity(), Mode: edge.Mode, SourceMigrations: edge.Migrations.Clone(), TargetMigrations: declaration.Migrations.Clone(), SourceSchemaSHA256: edge.SchemaSHA256, TargetSchemaSHA256: declaration.SchemaSHA256, SourcePolicySHA256: sourcePolicy, TargetPolicySHA256: node.state.release.PolicyDigest(), Artifacts: node.state.release.Artifacts()}
				adjacency[nodeKey(edge.Source)] = append(adjacency[nodeKey(edge.Source)], step)
			}
		}
	}
	seenBridges := map[releaseidentity.Digest]bool{}
	seenBridgePairs := map[string]bool{}
	for _, bridge := range bridges {
		if !bridge.Valid() || bridge.SnapshotDigest() != snapshot.Digest() || seenBridges[bridge.Digest()] {
			return Plan{}, errors.New("missing, stale or duplicate bridge proof")
		}
		seenBridges[bridge.Digest()] = true
		statement := bridge.Statement()
		pair := nodeKey(statement.SourceIdentity()) + ">" + nodeKey(releaseidentity.Source{Release: statement.Target}) + ":" + string(statement.SourceMigrations.Engine)
		if seenBridgePairs[pair] {
			return Plan{}, errors.New("duplicate recovery bridge pair")
		}
		seenBridgePairs[pair] = true
		if statement.SourceMigrations.Engine != engine {
			continue
		}
		fromSource := statement.SourceIdentity()
		if fromSource != source.Identity {
			if _, present := byIdentity[statement.Source]; !present {
				continue
			}
		}
		if statement.Target.Sequence > target.Sequence {
			continue
		}
		to, authenticated := byIdentity[statement.Target]
		if !authenticated {
			return Plan{}, errors.New("bridge target lacks independent release authentication")
		}
		manifest, err := to.Manifest(engine)
		targetSchema, _ := to.SchemaDigest(engine)
		if err != nil || !sameManifest(manifest, statement.TargetMigrations) || to.state.release.PolicyDigest() != statement.TargetPolicySHA256 || targetSchema != statement.TargetSchemaSHA256 {
			return Plan{}, errors.New("bridge target policy or migration binding mismatch")
		}
		if fromSource == source.Identity {
			if !sameManifest(source.Migrations, statement.SourceMigrations) || source.SchemaSHA256 != statement.SourceSchemaSHA256 {
				return Plan{}, errors.New("bridge differs from inspected source migrations")
			}
			if from, authenticated := byIdentity[statement.Source]; authenticated {
				knownSchema, _ := from.SchemaDigest(engine)
				knownManifest, err := from.Manifest(engine)
				if from.state.release.PolicyDigest() != statement.SourcePolicySHA256 || knownSchema != statement.SourceSchemaSHA256 || err != nil || !sameManifest(knownManifest, statement.SourceMigrations) {
					return Plan{}, errors.New("bridge differs from authenticated source policy/schema/migrations")
				}
			}
		} else {
			from, authenticated := byIdentity[statement.Source]
			if !authenticated {
				continue
			}
			manifest, err := from.Manifest(engine)
			sourceSchema, _ := from.SchemaDigest(engine)
			if err != nil || !sameManifest(manifest, statement.SourceMigrations) || from.state.release.PolicyDigest() != statement.SourcePolicySHA256 || sourceSchema != statement.SourceSchemaSHA256 {
				return Plan{}, errors.New("bridge source policy or migrations mismatch")
			}
		}
		step := Step{Source: fromSource, Target: statement.Target, Mode: Maintenance, SourceMigrations: statement.SourceMigrations.Clone(), TargetMigrations: statement.TargetMigrations.Clone(), SourceSchemaSHA256: statement.SourceSchemaSHA256, TargetSchemaSHA256: statement.TargetSchemaSHA256, SourcePolicySHA256: statement.SourcePolicySHA256, TargetPolicySHA256: statement.TargetPolicySHA256, BridgeSHA256: bridge.Digest(), Artifacts: to.state.release.Artifacts()}
		key := nodeKey(fromSource)
		// An explicit root-authorized bridge controls this exact exceptional
		// pair even if an ordinary declaration also names it.
		adjacency[key] = slices.DeleteFunc(adjacency[key], func(existing Step) bool { return existing.Target == step.Target })
		adjacency[key] = append(adjacency[key], step)
	}
	if source.Identity.IsRelease() && source.Identity.Release == target {
		return makePlan(snapshot, source, target, []Step{})
	}
	for key := range adjacency {
		slices.SortFunc(adjacency[key], func(a, b Step) int {
			if a.Target.Sequence < b.Target.Sequence {
				return -1
			}
			if a.Target.Sequence > b.Target.Sequence {
				return 1
			}
			return 0
		})
	}
	type pathState struct {
		source releaseidentity.Source
		steps  []Step
	}
	queue := []pathState{{source: source.Identity, steps: []Step{}}}
	visited := map[string]bool{nodeKey(source.Identity): true}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, step := range adjacency[nodeKey(current.source)] {
			if len(current.steps) >= MaxHops {
				return Plan{}, errors.New("upgrade route exceeds hop bound")
			}
			steps := append(slices.Clone(current.steps), step)
			if step.Target == target {
				return makePlan(snapshot, source, target, steps)
			}
			next := releaseidentity.Source{Release: step.Target}
			key := nodeKey(next)
			if !visited[key] {
				visited[key] = true
				queue = append(queue, pathState{source: next, steps: steps})
			}
		}
	}
	return Plan{}, errors.New("no authenticated upgrade route")
}

func makePlan(snapshot releasetrust.Snapshot, source InstalledSource, target releaseidentity.Identity, steps []Step) (Plan, error) {
	source.Migrations = source.Migrations.Clone()
	// The route digest binds exact route authority, not unrelated later
	// metadata changes. Pre-apply still revalidates against the current snapshot.
	raw, err := json.Marshal(struct {
		Schema string                   `json:"schema"`
		Source InstalledSource          `json:"source"`
		Target releaseidentity.Identity `json:"target"`
		Steps  []Step                   `json:"steps"`
	}{"hikyo.dev/upgrade-route/v1", source, target, steps})
	if err != nil {
		return Plan{}, err
	}
	return Plan{state: &planState{source: source, target: target, steps: steps, digest: releaseidentity.Hash(raw), snapshot: snapshot.Digest()}}, nil
}
