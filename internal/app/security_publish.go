package app

import (
	"context"
	"crypto"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/securityfeed"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

// SecurityFeedUsage describes local signing operations. Neither command uploads
// a feed, enrolls an installation or activates any production signing root.
func SecurityFeedUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: hikyo security-maintenance feed init-root --config FILE --output FILE")
	fmt.Fprintln(out, "       hikyo security-maintenance feed publish --catalog FILE --root FILE --output DIRECTORY --cache DIRECTORY --version N")
	fmt.Fprintln(out, "         --targets-expires RFC3339 --snapshot-expires RFC3339 --timestamp-expires RFC3339")
	fmt.Fprintln(out, "         --targets-key FILE --snapshot-key FILE --timestamp-key FILE [--previous-root FILE]")
	fmt.Fprintln(out, "Role key flags are repeatable for threshold signing. Inputs use absolute canonical paths and protected operator-owned files.")
	fmt.Fprintln(out, "Root config: {version, expires, roles: {ROLE: {public_key_files: [], threshold}}, signer_files: [], previous_root_file?}.")
	fmt.Fprintln(out, "Publishing independently verifies each active exact release and image before writing consistent-snapshot files; no network upload is performed.")
}

// RunSecurityFeed performs explicit local feed-authoring operations. Release
// verification is independently anchored in the current binary's build stamp;
// feed signing keys cannot substitute release execution authority.
func RunSecurityFeed(ctx context.Context, args []string, out io.Writer) error {
	return runSecurityFeed(ctx, args, out, verifySecurityPublicationCatalog)
}

type publicationCatalogVerifier func(context.Context, securitymaintenance.Catalog, string) error

func runSecurityFeed(ctx context.Context, args []string, out io.Writer, verify publicationCatalogVerifier) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(args) == 0 || slices.Contains([]string{"help", "--help", "-h"}, args[0]) {
		SecurityFeedUsage(out)
		if len(args) == 0 {
			return errors.New("feed requires init-root or publish")
		}
		return nil
	}
	var err error
	switch args[0] {
	case "init-root":
		err = runSecurityFeedInitRoot(ctx, args[1:], out)
	case "publish":
		err = runSecurityFeedPublish(ctx, args[1:], out, verify)
	default:
		return errors.New("unknown security feed command; use init-root or publish")
	}
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

type publicationRootRole struct {
	PublicKeyFiles []string `json:"public_key_files"`
	Threshold      int      `json:"threshold"`
}

type publicationRootConfig struct {
	Version          uint64                         `json:"version"`
	Expires          time.Time                      `json:"expires"`
	Roles            map[string]publicationRootRole `json:"roles"`
	SignerFiles      []string                       `json:"signer_files"`
	PreviousRootFile string                         `json:"previous_root_file,omitempty"`
}

func runSecurityFeedInitRoot(ctx context.Context, args []string, out io.Writer) error {
	if err := uniqueSecurityFeedFlags(args, nil); err != nil {
		return err
	}
	fs := flag.NewFlagSet("security-maintenance feed init-root", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() { SecurityFeedUsage(out) }
	configPath := fs.String("config", "", "closed local role and signer configuration")
	outputPath := fs.String("output", "", "new local root file; existing files are never replaced")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !canonicalPublicationPath(*configPath) || !canonicalPublicationPath(*outputPath) {
		return errors.New("feed init-root requires absolute --config and --output paths without positional arguments")
	}
	raw, err := securityfeed.ReadPublicFile(*configPath, 65536)
	if err != nil {
		return fmt.Errorf("read protected feed root configuration: %w", err)
	}
	var config publicationRootConfig
	if err := definitions.DecodeStrict(raw, &config); err != nil {
		return fmt.Errorf("feed root configuration: %w", err)
	}
	if len(config.Roles) != 4 || len(config.SignerFiles) == 0 || len(config.SignerFiles) > 32 {
		return errors.New("feed root configuration requires four roles and explicit bounded signer files")
	}
	request := securityfeed.RootConfig{Version: config.Version, Expires: config.Expires, Roles: map[string]securityfeed.RoleKeys{}}
	for _, role := range metadata.TOP_LEVEL_ROLE_NAMES {
		input, ok := config.Roles[role]
		if !ok || len(input.PublicKeyFiles) == 0 || len(input.PublicKeyFiles) > 16 || input.Threshold < 1 || input.Threshold > len(input.PublicKeyFiles) {
			return errors.New("feed root role has an absent or unsatisfiable key threshold")
		}
		keys := securityfeed.RoleKeys{Threshold: input.Threshold, PublicKeys: []crypto.PublicKey{}}
		seen := map[string]bool{}
		for _, path := range input.PublicKeyFiles {
			if !canonicalPublicationPath(path) || seen[path] {
				return errors.New("feed role public-key paths must be absolute, canonical and unique")
			}
			seen[path] = true
			public, err := securityfeed.LoadPublicKey(path)
			if err != nil {
				return fmt.Errorf("read protected feed role public key: %w", err)
			}
			keys.PublicKeys = append(keys.PublicKeys, public)
		}
		request.Roles[role] = keys
	}
	request.Signers, err = loadPublicationSigners(config.SignerFiles)
	if err != nil {
		return err
	}
	if config.PreviousRootFile != "" {
		request.PreviousRoot, err = securityfeed.ReadPublicFile(config.PreviousRootFile, securityfeed.MaxDocumentBytes)
		if err != nil {
			return fmt.Errorf("read prior feed root: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root, err := securityfeed.CreateRoot(request)
	if err != nil {
		return err
	}
	if err := writeNewPublicationRoot(*outputPath, root); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Created local feed root version %d. Production trust is unchanged.\n", config.Version)
	return err
}

type publicationKeyPaths []string

func (p *publicationKeyPaths) String() string { return "protected local key files" }
func (p *publicationKeyPaths) Set(path string) error {
	if !canonicalPublicationPath(path) || len(*p) >= 16 || slices.Contains(*p, path) {
		return errors.New("feed key paths must be absolute, canonical, bounded and unique")
	}
	*p = append(*p, path)
	return nil
}

func runSecurityFeedPublish(ctx context.Context, args []string, out io.Writer, verify publicationCatalogVerifier) error {
	if err := uniqueSecurityFeedFlags(args, map[string]bool{"targets-key": true, "snapshot-key": true, "timestamp-key": true}); err != nil {
		return err
	}
	fs := flag.NewFlagSet("security-maintenance feed publish", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() { SecurityFeedUsage(out) }
	catalogPath := fs.String("catalog", "", "structurally validated local security catalog")
	rootPath := fs.String("root", "", "explicitly authorized public feed root")
	previousRootPath := fs.String("previous-root", "", "previous feed root when rotating online role keys")
	output := fs.String("output", "", "private local repository staging directory")
	cache := fs.String("cache", "", "private independent release-verification cache")
	version := fs.Uint64("version", 0, "strictly increasing TUF metadata version")
	targetsExpires := fs.String("targets-expires", "", "targets metadata expiration in RFC3339")
	snapshotExpires := fs.String("snapshot-expires", "", "snapshot metadata expiration in RFC3339")
	timestampExpires := fs.String("timestamp-expires", "", "timestamp metadata expiration in RFC3339")
	var targetsKeys, snapshotKeys, timestampKeys publicationKeyPaths
	fs.Var(&targetsKeys, "targets-key", "protected targets role key file; repeat for threshold")
	fs.Var(&snapshotKeys, "snapshot-key", "protected snapshot role key file; repeat for threshold")
	fs.Var(&timestampKeys, "timestamp-key", "protected timestamp role key file; repeat for threshold")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *version == 0 || !canonicalPublicationPath(*catalogPath) || !canonicalPublicationPath(*rootPath) || !canonicalPublicationPath(*output) || !canonicalPublicationPath(*cache) || *output == *cache || len(targetsKeys) == 0 || len(snapshotKeys) == 0 || len(timestampKeys) == 0 || verify == nil {
		return errors.New("feed publish requires catalog/root/output/cache, positive version, three explicit role key sets and no positional arguments")
	}
	raw, err := securityfeed.ReadPublicFile(*catalogPath, securitymaintenance.MaxCatalogBytes)
	if err != nil {
		return fmt.Errorf("read protected security catalog: %w", err)
	}
	catalog, err := securitymaintenance.DecodeCatalog(raw, time.Now().UTC())
	if err != nil {
		return err
	}
	expires := securityfeed.Expirations{}
	for _, value := range []struct {
		input       string
		destination *time.Time
	}{{*targetsExpires, &expires.Targets}, {*snapshotExpires, &expires.Snapshot}, {*timestampExpires, &expires.Timestamp}} {
		parsed, err := time.Parse(time.RFC3339, value.input)
		if err != nil || parsed.After(catalog.ExpiresAt) {
			return errors.New("feed metadata expiries require RFC3339 timestamps no later than catalog expiration")
		}
		*value.destination = parsed
	}
	root, err := securityfeed.ReadPublicFile(*rootPath, securityfeed.MaxDocumentBytes)
	if err != nil {
		return fmt.Errorf("read protected public feed root: %w", err)
	}
	request := securityfeed.PublishConfig{Root: root, Catalog: raw, Version: *version, Expires: expires, Signers: map[string][]signature.Signer{}}
	for role, paths := range map[string][]string{metadata.TARGETS: targetsKeys, metadata.SNAPSHOT: snapshotKeys, metadata.TIMESTAMP: timestampKeys} {
		request.Signers[role], err = loadPublicationSigners(paths)
		if err != nil {
			return err
		}
	}
	if *previousRootPath != "" {
		request.PreviousRoot, err = securityfeed.ReadPublicFile(*previousRootPath, securityfeed.MaxDocumentBytes)
		if err != nil {
			return fmt.Errorf("read previous public feed root: %w", err)
		}
	}
	previousPath := filepath.Join(*output, "metadata/timestamp.json")
	request.PreviousTimestamp, err = securityfeed.ReadPublicFile(previousPath, securityfeed.MaxDocumentBytes)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read previous feed timestamp: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verify(ctx, catalog, *cache); err != nil {
		return fmt.Errorf("independent security release verification: %w", err)
	}
	publication, err := securityfeed.BuildPublication(request)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := publication.Write(*output); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Prepared local feed metadata version %d for catalog sequence %d. Nothing was uploaded or activated.\n", *version, catalog.Sequence)
	return err
}

func loadPublicationSigners(paths []string) ([]signature.Signer, error) {
	if len(paths) == 0 || len(paths) > 32 {
		return nil, errors.New("feed signing requires a bounded explicit local key list")
	}
	seen := map[string]bool{}
	signers := make([]signature.Signer, 0, len(paths))
	for _, path := range paths {
		if !canonicalPublicationPath(path) || seen[path] {
			return nil, errors.New("feed signer paths must be absolute, canonical and unique")
		}
		seen[path] = true
		signer, err := securityfeed.LoadSigner(path)
		if err != nil {
			return nil, fmt.Errorf("load protected feed signer: %w", err)
		}
		signers = append(signers, signer)
	}
	return signers, nil
}

func canonicalPublicationPath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && path != string(filepath.Separator)
}

func writeNewPublicationRoot(path string, raw []byte) (returnErr error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("create new local feed root without replacement: %w", err)
	}
	defer func() {
		if returnErr != nil {
			returnErr = errors.Join(returnErr, os.Remove(path))
		}
	}()
	_, writeErr := file.Write(raw)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}
	return filedurability.SyncDirectory(filepath.Dir(path))
}

// uniqueSecurityFeedFlags prevents last-one-wins ambiguity on singleton
// authority inputs while allowing only the explicitly repeatable key flags.
func uniqueSecurityFeedFlags(args []string, repeatable map[string]bool) error {
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		argument := args[i]
		if !strings.HasPrefix(argument, "-") || argument == "--" {
			continue
		}
		name, _, inline := strings.Cut(strings.TrimLeft(argument, "-"), "=")
		if seen[name] && !repeatable[name] {
			return fmt.Errorf("duplicate security feed flag --%s", name)
		}
		seen[name] = true
		if !inline && name != "help" && name != "h" {
			i++
		}
	}
	return nil
}

type publicationVerifiedRelease struct {
	Identity  releaseidentity.Identity
	Artifacts []releasetrust.Artifact
}

func verifySecurityPublicationCatalog(ctx context.Context, catalog securitymaintenance.Catalog, cache string) error {
	if _, err := buildcompat.ProductionTrust(); err != nil {
		return err
	}
	return verifySecurityPublicationTargets(ctx, catalog, cache, func(ctx context.Context, identity releaseidentity.Identity, directory string) (publicationVerifiedRelease, error) {
		prepared, err := prepareSecurityRelease(ctx, identity, directory)
		if err != nil {
			return publicationVerifiedRelease{}, err
		}
		if !prepared.Release.Valid() {
			return publicationVerifiedRelease{}, errors.New("release verifier returned no authenticated artifact inventory")
		}
		return publicationVerifiedRelease{Identity: prepared.Release.Identity(), Artifacts: prepared.Release.Artifacts()}, nil
	})
}

func verifySecurityPublicationTargets(ctx context.Context, catalog securitymaintenance.Catalog, cache string, prepare func(context.Context, releaseidentity.Identity, string) (publicationVerifiedRelease, error)) error {
	verified := map[releaseidentity.Identity]publicationVerifiedRelease{}
	for _, target := range catalog.Targets {
		if target.Withdrawn {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		release, ok := verified[target.Identity]
		if !ok {
			var err error
			release, err = prepare(ctx, target.Identity, cache)
			if err != nil {
				return err
			}
			if release.Identity != target.Identity {
				return errors.New("security catalog target differs from independently authenticated release")
			}
			verified[target.Identity] = release
		}
		if target.Image != "" && !slices.ContainsFunc(release.Artifacts, func(artifact releasetrust.Artifact) bool {
			return artifact.Kind == "image" && artifact.Image+"@"+artifact.Digest == target.Image
		}) {
			return errors.New("security catalog image is absent from the independently signed release inventory")
		}
	}
	return nil
}
