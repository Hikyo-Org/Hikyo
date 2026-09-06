// nightly prepares and verifies public evidence. It never holds signing keys.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "nightly:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("require preflight, verify, sources, or legacy-bridges")
	}
	fs := flag.NewFlagSet("nightly", flag.ContinueOnError)
	trust := fs.String("trust", "release/trust", "independently pinned public trust directory")
	directory := fs.String("directory", "", "complete signed nightly download")
	sources := fs.String("sources", "release/compatibility/sources.json", "reviewed genesis/source edges")
	out := fs.String("out", "", "new source edges file")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	snapshot, policy, err := loadTrust(*trust)
	if err != nil {
		return fmt.Errorf("signed nightly bootstrap required: %w", err)
	}
	switch args[0] {
	case "preflight":
		_, err = fmt.Fprintf(output, "Authenticated nightly policy %s\n", releaseidentity.Hash(policy))
		return err
	case "verify":
		release, err := upgradebundle.VerifyNightlyDirectory(ctx, *directory, snapshot)
		if err != nil {
			return err
		}
		return json.NewEncoder(output).Encode(release.Identity())
	case "legacy-bridges":
		release, err := upgradebundle.VerifyNightlyDirectory(ctx, *directory, snapshot)
		if err != nil {
			return err
		}
		raw, err := read(filepath.Join(*directory, releasetrust.CompatibilityArtifact))
		if err != nil {
			return err
		}
		node, err := upgradecompat.Bind(release, raw)
		if err != nil {
			return err
		}
		if err := os.Mkdir(*out, 0700); err != nil {
			return err
		}
		count := 0
		for _, engine := range []releaseidentity.Engine{releaseidentity.SQLite, releaseidentity.Postgres} {
			for _, source := range node.GenesisSources(engine) {
				if source.Identity.Genesis != releaseidentity.LegacyGenesisV1 {
					continue
				}
				manifest, err := node.Manifest(engine)
				if err != nil {
					return err
				}
				schema, err := node.SchemaDigest(engine)
				if err != nil {
					return err
				}
				statement := releasetrust.BridgeStatement{Schema: "hikyo.dev/legacy-nightly-bridge/v1", SourceGenesis: releaseidentity.LegacyGenesisV1, Target: node.Identity(), TargetPolicySHA256: release.PolicyDigest(), SourceMigrations: source.Migrations, TargetMigrations: manifest, SourceSchemaSHA256: source.SchemaSHA256, TargetSchemaSHA256: schema, Mode: "maintenance"}
				statementRaw, err := json.MarshalIndent(statement, "", "  ")
				if err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(*out, string(releaseidentity.Hash(statementRaw))+".json"), statementRaw, 0600); err != nil {
					return err
				}
				count++
			}
		}
		if count == 0 {
			return errors.New("target has no reviewed legacy schema candidates")
		}
		_, err = fmt.Fprintf(output, "%d unsigned legacy bridge proposals at %s; recovery signatures and catalog authorization required\n", count, *out)
		return err
	case "sources":
		var document struct {
			Schema  string                                                `json:"schema"`
			Engines map[releaseidentity.Engine][]upgradecompat.SourceEdge `json:"engines"`
		}
		raw, err := read(*sources)
		if err != nil {
			return err
		}
		if err := definitions.DecodeStrict(raw, &document); err != nil {
			return err
		}
		if document.Schema != "hikyo.dev/upgrade-sources/v1" || len(document.Engines) != 2 {
			return errors.New("invalid reviewed source inventory")
		}
		if *directory != "" {
			release, err := upgradebundle.VerifyNightlyDirectory(ctx, *directory, snapshot)
			if err != nil {
				return err
			}
			raw, err := read(filepath.Join(*directory, releasetrust.CompatibilityArtifact))
			if err != nil {
				return err
			}
			node, err := upgradecompat.Bind(release, raw)
			if err != nil {
				return err
			}
			for engine, edges := range document.Engines {
				manifest, err := node.Manifest(engine)
				if err != nil {
					return err
				}
				schema, err := node.SchemaDigest(engine)
				if err != nil {
					return err
				}
				document.Engines[engine] = append(edges, upgradecompat.SourceEdge{Source: releaseidentity.Source{Release: node.Identity()}, Migrations: manifest, SchemaSHA256: schema, Mode: upgradecompat.Maintenance})
			}
		}
		file, err := os.OpenFile(*out, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		return errors.Join(json.NewEncoder(file).Encode(document), file.Close())
	default:
		return errors.New("unknown nightly operation")
	}
}

// read bounds public documents and refuses symlinks and special files. These
// inputs are reviewed repository files or owned immutable download staging.
func read(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > releasetrust.MaxDocumentBytes {
		return nil, errors.New("expected bounded regular public document")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("public document changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(file, releasetrust.MaxDocumentBytes+1))
	if len(raw) > releasetrust.MaxDocumentBytes {
		return nil, errors.New("public document exceeds bound")
	}
	return raw, err
}

func loadTrust(directory string) (releasetrust.Snapshot, []byte, error) {
	var snapshot releasetrust.Snapshot
	rootRaw, err := read(filepath.Join(directory, "root.json"))
	if err != nil {
		return snapshot, nil, err
	}
	var root releasetrust.Root
	if err := definitions.DecodeStrict(rootRaw, &root); err != nil {
		return snapshot, nil, err
	}
	member := func(name string) ([]byte, error) {
		if name == "" || name == "." || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
			return nil, errors.New("unsafe public key name")
		}
		return read(filepath.Join(directory, name))
	}
	recovery, err := member(root.Recovery.PublicKey)
	if err != nil {
		return snapshot, nil, err
	}
	material := releasetrust.SnapshotMaterial{PrimaryKeys: map[string][]byte{}}
	for name, destination := range map[string]*[]byte{"metadata.json": &material.Metadata, "metadata.sigstore.json": &material.MetadataSignature, "catalog.json": &material.Catalog, "catalog.sigstore.json": &material.CatalogSignature} {
		*destination, err = member(name)
		if err != nil {
			return snapshot, nil, err
		}
	}
	var metadata releasetrust.Metadata
	if err := definitions.DecodeStrict(material.Metadata, &metadata); err != nil {
		return snapshot, nil, err
	}
	if len(metadata.PrimaryKeys) > 256 {
		return snapshot, nil, errors.New("too many primary keys")
	}
	for _, key := range metadata.PrimaryKeys {
		material.PrimaryKeys[key.ID], err = member(key.PublicKey)
		if err != nil {
			return snapshot, nil, err
		}
	}
	snapshot, err = releasetrust.VerifySnapshot(releasetrust.PinnedTrust{Root: rootRaw, RecoveryPublicKey: recovery}, material, releaseidentity.SnapshotFloor{})
	if err != nil {
		return snapshot, nil, err
	}
	policyRaw, err := read(filepath.Join(directory, "nightly", "policy.json"))
	if err != nil {
		return snapshot, nil, err
	}
	var policy releasetrust.NightlyPolicy
	if err := definitions.DecodeStrict(policyRaw, &policy); err != nil {
		return snapshot, nil, err
	}
	if err := policy.Validate(); err != nil {
		return snapshot, nil, err
	}
	trustedRoot, err := read(filepath.Join(directory, "nightly", "trusted-root.json"))
	if err != nil {
		return snapshot, nil, err
	}
	var catalog releasetrust.Catalog
	if err := definitions.DecodeStrict(material.Catalog, &catalog); err != nil {
		return snapshot, nil, err
	}
	if !slices.Contains(catalog.NightlyPolicies, releaseidentity.Hash(policyRaw)) || releaseidentity.Hash(trustedRoot) != policy.TrustedRootSHA256 {
		return snapshot, nil, errors.New("nightly policy or Sigstore roots lack recovery authorization")
	}
	return snapshot, policyRaw, nil
}
