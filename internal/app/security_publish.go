package app

import (
	"bytes"
	"context"
	"crypto"
	"encoding/json"
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
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/sigstore/sigstore/pkg/signature"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

// SecurityFeedUsage describes local authoring and signing operations. None of
// them uploads a feed, enrolls an installation or activates a production root.
func SecurityFeedUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: hikyo security-maintenance feed init-root --config FILE --output FILE")
	fmt.Fprintln(out, "       hikyo security-maintenance feed advisory --output FILE [--previous-catalog FILE] [--cache DIRECTORY]")
	fmt.Fprintln(out, "         [--id ID --summary TEXT --severity low|moderate|high|critical --component NAME --fixed-version VERSION --platform OS/ARCH [--introduced VERSION]]")
	fmt.Fprintln(out, "         [--features] [--schema] [--api] [--configuration] [--maintenance] [--rollout-minutes N]")
	fmt.Fprintln(out, "         [--withdraw-target VERSION] [--withdraw-advisory ID]")
	fmt.Fprintln(out, "       hikyo security-maintenance feed publish --catalog FILE --root FILE --output DIRECTORY --cache DIRECTORY --version N")
	fmt.Fprintln(out, "         --targets-expires RFC3339 --snapshot-expires RFC3339 --timestamp-expires RFC3339")
	fmt.Fprintln(out, "         --targets-key FILE --snapshot-key FILE --timestamp-key FILE [--previous-root FILE]")
	fmt.Fprintln(out, "       hikyo security-maintenance feed refresh --root FILE --output DIRECTORY")
	fmt.Fprintln(out, "         --snapshot-expires RFC3339 --timestamp-expires RFC3339 --snapshot-key FILE --timestamp-key FILE [--previous-root FILE]")
	fmt.Fprintln(out, "Role key flags and --component/--platform are repeatable. Inputs use absolute canonical paths and protected operator-owned files.")
	fmt.Fprintln(out, "Root config: {version, expires, roles: {ROLE: {public_key_files: [], threshold}}, signer_files: [], previous_root_file?}.")
	fmt.Fprintln(out, "Advisory derives exact targets from the independently verified release and writes the next complete catalog; with no changes it renews the previous one.")
	fmt.Fprintln(out, "Publishing independently verifies each new or changed active target, then checks the output with the real client. Refresh re-signs liveness metadata around the existing targets.")
	fmt.Fprintln(out, "No network upload is performed. Upload only metadata/ and targets/; publish.lock is a local lock file in the output directory.")
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
			return errors.New("feed requires init-root, advisory, publish or refresh")
		}
		return nil
	}
	var err error
	switch args[0] {
	case "init-root":
		err = runSecurityFeedInitRoot(ctx, args[1:], out)
	case "advisory":
		err = runSecurityFeedAdvisory(ctx, args[1:], out, resolveSecurityPublicationRelease)
	case "publish":
		err = runSecurityFeedPublish(ctx, args[1:], out, verify)
	case "refresh":
		err = runSecurityFeedRefresh(ctx, args[1:], out)
	default:
		return errors.New("unknown security feed command; use init-root, advisory, publish or refresh")
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
	if err := writeNewPublicationFile(*outputPath, root); err != nil {
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
	published, exists, err := securityfeed.ReadPublished(*output, root, request.PreviousRoot)
	if err != nil {
		return err
	}
	changed := catalog
	if exists {
		changed, err = changedPublicationCatalog(raw, catalog, published.Catalog)
		if err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := verify(ctx, changed, *cache); err != nil {
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
	if err := verifyPublicationWithClient(ctx, *output, root, raw); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Prepared local feed metadata version %d for catalog sequence %d. Nothing was uploaded or activated.\n", *version, catalog.Sequence)
	return err
}

// changedPublicationCatalog applies the client's rollback rule at the publisher
// and keeps only targets the previous authenticated catalog did not already
// carry, so a withdrawal cannot be blocked by an aged-out release or an outage.
func changedPublicationCatalog(raw []byte, next securitymaintenance.Catalog, previousRaw []byte) (securitymaintenance.Catalog, error) {
	var previous securitymaintenance.Catalog
	if err := definitions.DecodeStrict(previousRaw, &previous); err != nil {
		return next, fmt.Errorf("previous published catalog: %w", err)
	}
	if next.Sequence <= previous.Sequence && !bytes.Equal(raw, previousRaw) {
		return next, fmt.Errorf("catalog sequence %d does not advance past published sequence %d and its content differs; clients would refuse it", next.Sequence, previous.Sequence)
	}
	known := map[string]bool{}
	for _, target := range previous.Targets {
		record, err := json.Marshal(target)
		if err != nil {
			return next, err
		}
		known[string(record)] = true
	}
	targets := []securitymaintenance.Target{}
	for _, target := range next.Targets {
		record, err := json.Marshal(target)
		if err != nil {
			return next, err
		}
		if !known[string(record)] {
			targets = append(targets, target)
		}
	}
	next.Targets = targets
	return next, nil
}

// verifyPublicationWithClient runs the real client against fresh state, so a
// repository a client would reject fails here and never gets uploaded.
func verifyPublicationWithClient(ctx context.Context, output string, root, catalog []byte) error {
	temporary, err := os.MkdirTemp("", "hikyo-feed-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	state, err := filepath.EvalSymlinks(temporary)
	if err != nil {
		return err
	}
	client, err := securityfeed.New(securityfeed.Config{StateDirectory: state, TrustedRoot: root, OfflineDirectory: output})
	if err != nil {
		return fmt.Errorf("verify written feed with the client: %w", err)
	}
	document, err := client.Refresh(ctx)
	if err != nil {
		return fmt.Errorf("verify written feed with the client: %w", err)
	}
	if !bytes.Equal(document.Data, catalog) {
		return errors.New("verify written feed with the client: authenticated catalog differs from the published catalog")
	}
	if _, err := securitymaintenance.DecodeCatalog(document.Data, time.Now().UTC()); err != nil {
		return fmt.Errorf("verify written feed with the client: %w", err)
	}
	return nil
}

// runSecurityFeedRefresh keeps a feed alive with online keys only: it has no
// targets key input and reuses the existing targets metadata unchanged.
func runSecurityFeedRefresh(ctx context.Context, args []string, out io.Writer) error {
	if err := uniqueSecurityFeedFlags(args, map[string]bool{"snapshot-key": true, "timestamp-key": true}); err != nil {
		return err
	}
	fs := flag.NewFlagSet("security-maintenance feed refresh", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() { SecurityFeedUsage(out) }
	rootPath := fs.String("root", "", "explicitly authorized public feed root")
	previousRootPath := fs.String("previous-root", "", "previous feed root when rotating online role keys")
	output := fs.String("output", "", "existing local repository directory")
	snapshotExpires := fs.String("snapshot-expires", "", "snapshot metadata expiration in RFC3339")
	timestampExpires := fs.String("timestamp-expires", "", "timestamp metadata expiration in RFC3339")
	var snapshotKeys, timestampKeys publicationKeyPaths
	fs.Var(&snapshotKeys, "snapshot-key", "protected snapshot role key file; repeat for threshold")
	fs.Var(&timestampKeys, "timestamp-key", "protected timestamp role key file; repeat for threshold")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !canonicalPublicationPath(*rootPath) || !canonicalPublicationPath(*output) || len(snapshotKeys) == 0 || len(timestampKeys) == 0 {
		return errors.New("feed refresh requires root/output, snapshot and timestamp key sets and no positional arguments")
	}
	request := securityfeed.RefreshConfig{Signers: map[string][]signature.Signer{}}
	var err error
	if request.SnapshotExpires, err = time.Parse(time.RFC3339, *snapshotExpires); err != nil {
		return errors.New("feed refresh --snapshot-expires requires an RFC3339 timestamp")
	}
	if request.TimestampExpires, err = time.Parse(time.RFC3339, *timestampExpires); err != nil {
		return errors.New("feed refresh --timestamp-expires requires an RFC3339 timestamp")
	}
	if request.Root, err = securityfeed.ReadPublicFile(*rootPath, securityfeed.MaxDocumentBytes); err != nil {
		return fmt.Errorf("read protected public feed root: %w", err)
	}
	var previousRoot []byte
	if *previousRootPath != "" {
		if previousRoot, err = securityfeed.ReadPublicFile(*previousRootPath, securityfeed.MaxDocumentBytes); err != nil {
			return fmt.Errorf("read previous public feed root: %w", err)
		}
	}
	published, exists, err := securityfeed.ReadPublished(*output, request.Root, previousRoot)
	if err != nil {
		return err
	}
	if !exists {
		return errors.New("feed refresh requires an existing published repository; run feed publish first")
	}
	request.Published = published
	for role, paths := range map[string][]string{metadata.SNAPSHOT: snapshotKeys, metadata.TIMESTAMP: timestampKeys} {
		if request.Signers[role], err = loadPublicationSigners(paths); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	publication, err := securityfeed.BuildRefresh(request)
	if err != nil {
		return err
	}
	if err := publication.Write(*output); err != nil {
		return err
	}
	if err := verifyPublicationWithClient(ctx, *output, request.Root, published.Catalog); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Refreshed local feed metadata to version %d; targets expire %s. Catalog and targets are unchanged. Nothing was uploaded or activated.\n", published.Version+1, published.TargetsExpires.UTC().Format(time.RFC3339))
	return err
}

type publicationReleaseResolver func(context.Context, string, string) (publicationVerifiedRelease, error)

func resolveSecurityPublicationRelease(ctx context.Context, version, cache string) (publicationVerifiedRelease, error) {
	prepared, err := prepareSecurityReleaseVersion(ctx, version, nil, cache)
	if err != nil {
		return publicationVerifiedRelease{}, err
	}
	return publicationReleaseEvidence(prepared)
}

func publicationReleaseEvidence(prepared preparedSecurityRelease) (publicationVerifiedRelease, error) {
	if !prepared.Release.Valid() {
		return publicationVerifiedRelease{}, errors.New("release verifier returned no authenticated artifact inventory")
	}
	return publicationVerifiedRelease{Identity: prepared.Release.Identity(), Artifacts: prepared.Release.Artifacts()}, nil
}

type publicationValues []string

func (v *publicationValues) String() string { return strings.Join(*v, ",") }
func (v *publicationValues) Set(value string) error {
	*v = append(*v, value)
	return nil
}

// runSecurityFeedAdvisory authors the next complete catalog. It signs nothing;
// feed publish remains the step that authenticates and releases it.
func runSecurityFeedAdvisory(ctx context.Context, args []string, out io.Writer, resolve publicationReleaseResolver) error {
	impactFlags := []string{"features", "schema", "api", "configuration", "maintenance"}
	values := slices.DeleteFunc(slices.Clone(args), func(argument string) bool {
		name, _, _ := strings.Cut(strings.TrimLeft(argument, "-"), "=")
		return strings.HasPrefix(argument, "-") && slices.Contains(impactFlags, name)
	})
	if err := uniqueSecurityFeedFlags(values, map[string]bool{"component": true, "platform": true}); err != nil {
		return err
	}
	fs := flag.NewFlagSet("security-maintenance feed advisory", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() { SecurityFeedUsage(out) }
	output := fs.String("output", "", "new catalog file; existing files are never replaced")
	previousPath := fs.String("previous-catalog", "", "current published catalog whose entries carry over")
	cache := fs.String("cache", "", "private independent release-verification cache")
	id := fs.String("id", "", "advisory identifier; an existing identifier is replaced")
	summary := fs.String("summary", "", "advisory summary")
	severity := fs.String("severity", "", "low, moderate, high or critical")
	introduced := fs.String("introduced", "", "first affected version; omit for the start of the profile")
	fixedVersion := fs.String("fixed-version", "", "signed release that fixes the advisory")
	withdrawTarget := fs.String("withdraw-target", "", "release version to withdraw from eligibility")
	withdrawAdvisory := fs.String("withdraw-advisory", "", "advisory identifier to withdraw")
	rollout := fs.Int("rollout-minutes", 0, "staged rollout duration for the new target, 0 to 10080")
	features := fs.Bool("features", false, "the release changes features")
	schema := fs.Bool("schema", false, "the release changes the schema")
	api := fs.Bool("api", false, "the release changes the API")
	configuration := fs.Bool("configuration", false, "the release changes configuration")
	maintenance := fs.Bool("maintenance", false, "the release requires maintenance")
	var components, platforms publicationValues
	fs.Var(&components, "component", "affected component; repeatable")
	fs.Var(&platforms, "platform", "affected platform as OS/ARCH; repeatable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !canonicalPublicationPath(*output) {
		return errors.New("feed advisory requires an absolute --output path and no positional arguments")
	}
	adding := *id != ""
	if !adding && (*summary != "" || *severity != "" || *introduced != "" || *fixedVersion != "" || len(components) != 0 || len(platforms) != 0 || *features || *schema || *api || *configuration || *maintenance || *rollout != 0) {
		return errors.New("feed advisory: advisory and target flags require --id")
	}
	if adding {
		for flagName, missing := range map[string]bool{"summary": *summary == "", "severity": *severity == "", "fixed-version": *fixedVersion == "", "component": len(components) == 0, "platform": len(platforms) == 0, "cache": !canonicalPublicationPath(*cache)} {
			if missing {
				return fmt.Errorf("feed advisory --id %s requires --%s", *id, flagName)
			}
		}
		if !slices.Contains([]securitymaintenance.Severity{securitymaintenance.Low, securitymaintenance.Moderate, securitymaintenance.High, securitymaintenance.Critical}, securitymaintenance.Severity(*severity)) {
			return fmt.Errorf("feed advisory --severity %q must be low, moderate, high or critical", *severity)
		}
		if *withdrawTarget == *fixedVersion {
			return fmt.Errorf("feed advisory cannot both withdraw and target release %s", *fixedVersion)
		}
	}
	now := time.Now().UTC().Truncate(time.Second)
	next := securitymaintenance.Catalog{Format: securitymaintenance.CatalogFormat, Advisories: []securitymaintenance.Advisory{}, Targets: []securitymaintenance.Target{}}
	if *previousPath != "" {
		if !canonicalPublicationPath(*previousPath) {
			return errors.New("feed advisory --previous-catalog requires an absolute canonical path")
		}
		raw, err := securityfeed.ReadPublicFile(*previousPath, securitymaintenance.MaxCatalogBytes)
		if err != nil {
			return fmt.Errorf("read previous security catalog: %w", err)
		}
		// An expired previous catalog must still carry over during an emergency.
		if err := definitions.DecodeStrict(raw, &next); err != nil {
			return fmt.Errorf("previous security catalog: %w", err)
		}
	} else if *withdrawTarget != "" || *withdrawAdvisory != "" {
		return errors.New("feed advisory withdrawal requires --previous-catalog")
	}
	next.Sequence++
	next.IssuedAt, next.ExpiresAt = now, now.Add(securitymaintenance.MaxCatalogLifetime)
	if *withdrawTarget != "" {
		found := false
		for i := range next.Targets {
			if next.Targets[i].Identity.Version == *withdrawTarget {
				next.Targets[i].Withdrawn, found = true, true
			}
		}
		if !found {
			return fmt.Errorf("feed advisory --withdraw-target %s is not in the previous catalog", *withdrawTarget)
		}
	}
	if *withdrawAdvisory != "" {
		index := slices.IndexFunc(next.Advisories, func(advisory securitymaintenance.Advisory) bool { return advisory.ID == *withdrawAdvisory })
		if index < 0 {
			return fmt.Errorf("feed advisory --withdraw-advisory %s is not in the previous catalog", *withdrawAdvisory)
		}
		next.Advisories[index].Withdrawn = true
	}
	if adding {
		release, err := resolve(ctx, *fixedVersion, *cache)
		if err != nil {
			return fmt.Errorf("independent security release verification: %w", err)
		}
		if release.Identity.Version != *fixedVersion {
			return fmt.Errorf("independently verified release %s differs from --fixed-version %s", release.Identity.Version, *fixedVersion)
		}
		advisory := securitymaintenance.Advisory{ID: *id, Summary: *summary, Severity: securitymaintenance.Severity(*severity), Components: components, Affected: []securitymaintenance.Affected{{Profile: release.Identity.Profile, Introduced: *introduced, Fixed: *fixedVersion, Platforms: platforms}}}
		if index := slices.IndexFunc(next.Advisories, func(existing securitymaintenance.Advisory) bool { return existing.ID == *id }); index >= 0 {
			// Replacing the record would silently clear a withdrawal, including one this run requested.
			if next.Advisories[index].Withdrawn {
				return fmt.Errorf("feed advisory %s is withdrawn and cannot be extended; publish a new ID", *id)
			}
			// Rerunning for the nightly profile must not erase the stable range.
			for _, previous := range next.Advisories[index].Affected {
				if previous.Profile != release.Identity.Profile {
					advisory.Affected = append(advisory.Affected, previous)
				}
			}
			next.Advisories[index] = advisory
		} else {
			next.Advisories = append(next.Advisories, advisory)
		}
		template := securitymaintenance.Target{Identity: release.Identity, Impact: securitymaintenance.Impact{Restart: true, Features: *features, Schema: *schema, API: *api, Configuration: *configuration, Maintenance: *maintenance}, RolloutMinutes: *rollout}
		if *rollout != 0 {
			template.AvailableAt = now
		}
		records, err := publicationReleaseTargets(release, platforms, template)
		if err != nil {
			return err
		}
		if err := mergePublicationTargets(&next, records); err != nil {
			return err
		}
	}
	// Every active target gains each advisory it fixes, including new targets and targets
	// already listed. Fixes are only added: an earlier fix stays listed until its target
	// is withdrawn.
	for i := range next.Targets {
		if !next.Targets[i].Withdrawn {
			for _, fix := range next.FixesFor(next.Targets[i]) {
				if !slices.Contains(next.Targets[i].Fixes, fix) {
					next.Targets[i].Fixes = append(next.Targets[i].Fixes, fix)
				}
			}
		}
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if _, err := securitymaintenance.DecodeCatalog(raw, now); err != nil {
		return fmt.Errorf("next security catalog: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeNewPublicationFile(*output, raw); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Wrote catalog sequence %d with %d advisories and %d targets. Review it, then run feed publish.\n", next.Sequence, len(next.Advisories), len(next.Targets))
	return err
}

// publicationSignedBinary is the advisory's signed-binary rule, shared so a hand-made
// catalog cannot advertise a platform the release has no binary for.
func publicationSignedBinary(release publicationVerifiedRelease, platform string) error {
	goos, goarch, _ := strings.Cut(platform, "/")
	name, err := selfupdate.ArchiveName(release.Identity.Version, goos, goarch)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(release.Artifacts, func(artifact releasetrust.Artifact) bool {
		return artifact.Kind == "binary" && (artifact.Platform == platform || artifact.Platform == "" && artifact.Name == name)
	}) {
		return fmt.Errorf("release %s has no signed binary for %s", release.Identity.Version, platform)
	}
	return nil
}

// publicationReleaseTargets derives binary and image records only from the
// signed inventory. Images are Linux; stable binaries are bound by signed name.
func publicationReleaseTargets(release publicationVerifiedRelease, platforms []string, template securitymaintenance.Target) ([]securitymaintenance.Target, error) {
	version := release.Identity.Version
	linux := []string{}
	for _, platform := range platforms {
		if err := publicationSignedBinary(release, platform); err != nil {
			return nil, fmt.Errorf("feed advisory --platform %s: %w", platform, err)
		}
		if goos, _, _ := strings.Cut(platform, "/"); goos == "linux" {
			linux = append(linux, platform)
		}
	}
	binary := template
	binary.Platforms = slices.Clone(platforms)
	records := []securitymaintenance.Target{binary}
	if len(linux) != 0 {
		images := slices.DeleteFunc(slices.Clone(release.Artifacts), func(artifact releasetrust.Artifact) bool { return artifact.Kind != "image" })
		if len(images) != 1 {
			return nil, fmt.Errorf("release %s must have exactly one signed image, found %d", version, len(images))
		}
		image := template
		image.Platforms, image.Image = linux, images[0].Image+"@"+images[0].Digest
		records = append(records, image)
	}
	return records, nil
}

// mergePublicationTargets adds the records or extends an existing record of the
// same release. Fixes are computed afterwards for every active target, so a
// hurried maintainer cannot omit one.
func mergePublicationTargets(next *securitymaintenance.Catalog, records []securitymaintenance.Target) error {
	for _, record := range records {
		index := -1
		for i, existing := range next.Targets {
			if existing.Identity.Version != record.Identity.Version {
				continue
			}
			if existing.Identity != record.Identity {
				return fmt.Errorf("catalog target %s differs from the independently verified release identity", record.Identity.Version)
			}
			if existing.Withdrawn {
				return fmt.Errorf("release %s is withdrawn and cannot be a fix", record.Identity.Version)
			}
			if (existing.Image != "") == (record.Image != "") {
				index = i
			}
		}
		if index < 0 {
			next.Targets = append(next.Targets, record)
			index = len(next.Targets) - 1
		} else {
			existing := &next.Targets[index]
			if existing.Image != record.Image {
				return fmt.Errorf("catalog target %s image differs from the independently signed image", record.Identity.Version)
			}
			for _, platform := range record.Platforms {
				if !slices.Contains(existing.Platforms, platform) {
					existing.Platforms = append(existing.Platforms, platform)
				}
			}
			existing.Impact.Features = existing.Impact.Features || record.Impact.Features
			existing.Impact.Schema = existing.Impact.Schema || record.Impact.Schema
			existing.Impact.API = existing.Impact.API || record.Impact.API
			existing.Impact.Configuration = existing.Impact.Configuration || record.Impact.Configuration
			existing.Impact.Maintenance = existing.Impact.Maintenance || record.Impact.Maintenance
			if record.RolloutMinutes != 0 {
				existing.AvailableAt, existing.RolloutMinutes = record.AvailableAt, record.RolloutMinutes
			}
		}
	}
	return nil
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

func writeNewPublicationFile(path string, raw []byte) (returnErr error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("create new local feed file without replacement: %w", err)
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
		return publicationReleaseEvidence(prepared)
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
		if target.Image == "" {
			for _, platform := range target.Platforms {
				if err := publicationSignedBinary(release, platform); err != nil {
					return fmt.Errorf("security catalog binary target: %w", err)
				}
			}
		}
	}
	return nil
}
