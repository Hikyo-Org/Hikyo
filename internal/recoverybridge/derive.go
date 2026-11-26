// Package recoverybridge derives the exact, unsigned recovery-bridge statements
// that authorize one stable release to upgrade into a later one. The fields come
// only from authenticated release evidence, never from caller-typed values, so
// the signer's job reduces to comparing and signing bytes.
package recoverybridge

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// Schema is the statement schema Derive writes: v2, with a signed expiry.
const Schema = releasetrust.BridgeSchemaV2

// MaxWindow is the longest a new bridge may stay usable after it is derived.
// A later upgrade needs a re-sign ceremony, which is the point of the bound.
const MaxWindow = releasetrust.MaxBridgeWindow

// Evidence is one authenticated stable release and its parsed declaration.
type Evidence struct {
	Release     releasetrust.VerifiedRelease
	Declaration upgradecompat.Declaration
}

// Statement is one unsigned per-engine statement and the digest the catalog and
// stable policy must list for it.
type Statement struct {
	Engine releaseidentity.Engine
	Bytes  []byte
	Digest releaseidentity.Digest
}

// Derive returns one statement per engine that both declarations name, ordered
// by engine, each expiring at notAfter. It refuses a pair whose engines differ, a declaration that does not
// describe its authenticated release, a target that is not newer, and a source
// migration inventory that is not a strict prefix of the target's (a bridge may
// only extend the schema, never rewrite history).
func Derive(source, target Evidence, now, notAfter time.Time) ([]Statement, error) {
	if !notAfter.After(now) || notAfter.Sub(now) > MaxWindow || notAfter.Nanosecond() != 0 {
		return nil, fmt.Errorf("not_after must be a whole second after now and at most %s later", MaxWindow)
	}
	if !source.Release.Valid() || !target.Release.Valid() || source.Release.SnapshotDigest() != target.Release.SnapshotDigest() {
		return nil, errors.New("bridge evidence must be two verified releases from one trust snapshot")
	}
	from, to := source.Release.Identity(), target.Release.Identity()
	if from.Profile != releaseidentity.StableV1 || to.Profile != releaseidentity.StableV1 || to.Sequence <= from.Sequence {
		return nil, errors.New("bridge target must be a newer stable release than its source")
	}
	for _, e := range []Evidence{source, target} {
		id := e.Release.Identity()
		if e.Declaration.Profile != id.Profile || e.Declaration.Version != id.Version || e.Declaration.Sequence != id.Sequence || e.Declaration.Commit != id.Commit {
			return nil, errors.New("compatibility declaration does not describe its authenticated release")
		}
	}
	if len(source.Declaration.Engines) != len(target.Declaration.Engines) {
		return nil, errors.New("source and target declare different engines")
	}
	var out []Statement
	for _, t := range target.Declaration.Engines {
		engine := t.Migrations.Engine
		i := slices.IndexFunc(source.Declaration.Engines, func(e upgradecompat.EngineDeclaration) bool { return e.Migrations.Engine == engine })
		if i < 0 {
			return nil, fmt.Errorf("source does not declare engine %s", engine)
		}
		s := source.Declaration.Engines[i]
		if len(s.Migrations.Entries) >= len(t.Migrations.Entries) || !slices.Equal(s.Migrations.Entries, t.Migrations.Entries[:len(s.Migrations.Entries)]) {
			return nil, fmt.Errorf("%s: source migrations are not a strict prefix of the target's", engine)
		}
		statement := releasetrust.BridgeStatement{
			Schema: Schema, Source: from, Target: to,
			SourcePolicySHA256: source.Release.PolicyDigest(), TargetPolicySHA256: target.Release.PolicyDigest(),
			SourceMigrations: s.Migrations.Clone(), TargetMigrations: t.Migrations.Clone(),
			SourceSchemaSHA256: s.SchemaSHA256, TargetSchemaSHA256: t.SchemaSHA256, Mode: "maintenance",
			NotAfter: notAfter.UTC().Format(time.RFC3339),
		}
		raw, err := json.Marshal(statement, jsonwire.Stable, jsontext.WithIndent("  "))
		if err != nil {
			return nil, err
		}
		raw = append(raw, '\n')
		out = append(out, Statement{Engine: engine, Bytes: raw, Digest: releaseidentity.Hash(raw)})
	}
	slices.SortFunc(out, func(a, b Statement) int { return strings.Compare(string(a.Engine), string(b.Engine)) })
	return out, nil
}
