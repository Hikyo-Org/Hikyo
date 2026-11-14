package app

import (
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/diagnostics"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/filedurability"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securefile"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/storagehealth"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
	"github.com/gofrs/flock"
)

// AutomaticHandoff asks the entrypoint to run the authenticated target's own
// coordinator. The current service executable has not been changed at this point.
type AutomaticHandoff struct {
	Executable string
	Arguments  []string
}

func (e *AutomaticHandoff) Error() string {
	return "continue upgrade with the verified target coordinator"
}

type automaticJournal struct {
	Format   string                        `json:"format"`
	Phase    string                        `json:"phase"`
	Target   releaseidentity.Identity      `json:"target"`
	Source   upgradecompat.InstalledSource `json:"source"`
	Instance string                        `json:"instance"`
	Route    releaseidentity.Digest        `json:"route"`
	Hop      int                           `json:"hop"`
	Runtime  hostupgrade.RuntimeEvidence   `json:"runtime"`
	// Aborted records why preparation ended before the live database was written.
	// It is evidence for the coordinator and never permission: every reader
	// rechecks the control row and the "preparing" phase.
	Aborted string `json:"aborted,omitempty"`
	// RollbackDatabase is the database identity recorded on entering "rolling-back".
	// Only that phase carries it; the final rewrite to "preparing" drops it so the
	// source image, which does not know the field, can still read the journal.
	RollbackDatabase string `json:"rollback_database,omitempty"`
}

// RunAutomaticUpgrade is the root operator entrypoint. It is deliberately
// separate from the retired remotely callable updater and server boot path.
func RunAutomaticUpgrade(ctx context.Context, args []string, out io.Writer, readPassword func(string) (string, error)) (err error) {
	fs := flag.NewFlagSet("hikyo upgrade", flag.ContinueOnError)
	fs.SetOutput(out)
	configuration := fs.String("config", hostupgrade.ConfigPath, "root-owned deployment configuration")
	version := fs.String("target", "", "exact nightly version; default is latest signed nightly")
	releaseIdentity := fs.String("release-identity", "", "independently selected exact stable or nightly release identity JSON")
	routeSHA256 := fs.String("route-sha256", "", "exact independently staged route digest; requires --release-identity")
	eligibilityUntil := fs.String("eligibility-until", "", "verified security selection expiry (RFC3339); requires --release-identity")
	maintenancePolicyFile := fs.String("maintenance-policy-file", "", "local maintenance policy guarded through target handoff")
	maintenancePolicySHA256 := fs.String("maintenance-policy-sha256", "", "SHA-256 of the guarded local policy file bytes")
	maintenanceSource := fs.String("maintenance-source", "", "original native source and local authority guard through target handoff")
	handoff := fs.Bool("handoff", false, "continue in the independently verified target executable")
	securityRollback := fs.Bool("security-rollback", false, "roll an unfinished guarded security update back to its fenced snapshot; requires --release-identity")
	fs.Usage = func() {
		fmt.Fprintln(out, "Usage: sudo hikyo [-v|-vv|-vvv] upgrade [--target VERSION | --release-identity JSON] [--config FILE]")
		fmt.Fprintln(out, "Verbosity: -v phases, -vv artifacts and requests, -vvv timings. Diagnostics go to stderr.")
		fs.PrintDefaults()
	}
	// Armed before any parsing: the security coordinator treats an unclassified
	// exit as a possible mutation and pins the operation. Scanned from args
	// because a parse failure can precede the flag.
	sourceSupplied := slices.ContainsFunc(args, func(arg string) bool {
		name := strings.TrimLeft(arg, "-")
		return name != arg && (name == "maintenance-source" || strings.HasPrefix(name, "maintenance-source="))
	})
	maintenanceStarted := false
	defer func() {
		var handoff *AutomaticHandoff
		// maintenanceStarted is cleared again only after the source service is
		// proven serving, so an unfinished stop is never classified as a refusal.
		if sourceSupplied && err != nil && !maintenanceStarted && !errors.As(err, &handoff) {
			err = errors.Join(securitymaintenance.ErrNotStarted, err)
		}
	}()
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: sudo hikyo upgrade [--target VERSION | --release-identity JSON] [--config FILE]")
	}
	var identitySupplied, targetSupplied, routeSupplied, eligibilitySupplied, policySupplied bool
	fs.Visit(func(f *flag.Flag) {
		identitySupplied = identitySupplied || f.Name == "release-identity"
		targetSupplied = targetSupplied || f.Name == "target"
		routeSupplied = routeSupplied || f.Name == "route-sha256"
		eligibilitySupplied = eligibilitySupplied || f.Name == "eligibility-until"
		policySupplied = policySupplied || f.Name == "maintenance-policy-file" || f.Name == "maintenance-policy-sha256"
	})
	if *securityRollback && (!identitySupplied || sourceSupplied) {
		return errors.New("--security-rollback requires --release-identity and refuses --maintenance-source")
	}
	expected, err := parseAutomaticReleaseIdentity(*releaseIdentity, identitySupplied, targetSupplied)
	if err != nil {
		return err
	}
	expectedRoute, err := parseAutomaticRouteDigest(*routeSHA256, routeSupplied, expected)
	if err != nil {
		return err
	}
	eligibleUntil, err := parseAutomaticEligibility(*eligibilityUntil, eligibilitySupplied, expected)
	if err != nil {
		return err
	}
	policyFile, policyDigest, err := parseAutomaticSecurityPolicy(*maintenancePolicyFile, *maintenancePolicySHA256, policySupplied, expected)
	if err != nil {
		return err
	}
	sourceGuard, err := parseAutomaticSecuritySource(*maintenanceSource, sourceSupplied, expected)
	if err != nil {
		return err
	}
	var authorizeSource func() error
	authorizeMutation := func() error {
		return authorizeAutomaticMutation(eligibleUntil, time.Now, func() error {
			if authorizeSource != nil {
				if err := authorizeSource(); err != nil {
					return err
				}
			}
			if policyFile != "" {
				return verifySecurityPolicyFingerprint(policyFile, policyDigest)
			}
			return nil
		})
	}
	if err := requireAutomaticEligibility(eligibleUntil, time.Now()); err != nil {
		return err
	}
	defer diagnostics.Time(ctx, "automatic upgrade")()
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return errors.New("automatic upgrades require sudo hikyo upgrade on the Linux systemd server")
	}
	if err := authorizeMutation(); err != nil {
		return err
	}
	pinned, err := buildcompat.ProductionTrust()
	if err != nil {
		return err
	}
	c, err := hostupgrade.InitializeConfig(*configuration)
	if err != nil {
		return err
	}
	diagnostics.Printf(ctx, 1, "Checking deployment configuration and systemd prerequisites")
	host, err := hostupgrade.New(c)
	if err != nil {
		return err
	}
	if err := preflightNativeHost(ctx, host, c.StateDirectory); err != nil {
		return err
	}
	if err := host.PrepareDirectories(); err != nil {
		return err
	}
	lock := flock.New(filepath.Join(c.StateDirectory, "upgrade.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return err
	}
	if !locked {
		return errors.New("another operator is already upgrading this installation")
	}
	defer func() {
		if unlockErr := lock.Unlock(); unlockErr != nil {
			unlockErr = fmt.Errorf("release operator upgrade lock: %w", unlockErr)
			// A handoff is control flow: the entrypoint must not execute it
			// after a failed unlock. Any other outcome stays visible.
			var handoff *AutomaticHandoff
			if errors.As(err, &handoff) {
				err = unlockErr
			} else {
				err = errors.Join(err, unlockErr)
			}
		}
	}()
	journalPath := filepath.Join(c.StateDirectory, "operation.json")
	previous, err := readAutomaticJournal(journalPath)
	if err != nil {
		return err
	}
	if sourceGuard != nil && previous != nil && previous.Phase != "complete" {
		return errors.Join(securitymaintenance.ErrNotStarted, errors.New("guarded native security application cannot adopt an unfinished host operation"))
	}
	if *securityRollback {
		return runNativeSecurityRollback(ctx, host, c, *configuration, journalPath, previous, expected, *handoff)
	}
	if previous != nil && previous.Phase == "rolling-back" {
		return refetchNativeRollbackSource(ctx, c.StateDirectory, expected, func(ctx context.Context, identity releaseidentity.Identity) error {
			installer, err := newSecurityImageInstaller(filepath.Join(c.StateDirectory, "downloads"), pinned)
			if err != nil {
				return err
			}
			source, err := newAutomaticReleaseSource()
			if err != nil {
				return err
			}
			_, err = prepareExactAutomaticRelease(ctx, source, installer, identity)
			return err
		}, out)
	}
	if previous != nil && previous.Phase == "restore-required" {
		if err := authorizeMutation(); err != nil {
			return err
		}
		maintenanceStarted = true
		return errors.Join(errors.New("previous candidate failed post-write health; explicit operator recovery is required"), host.FenceAndStop(ctx))
	}
	*version, expected, err = resolveAutomaticReleaseIdentity(*version, expected, previous)
	if err != nil {
		return err
	}
	if expectedRoute != "" && previous != nil && previous.Phase != "complete" && previous.Route != expectedRoute {
		return errors.New("unfinished upgrade pins a different exact route")
	}
	environment := host.Environment()
	getenv := func(key string) string { return environment[key] }
	cfg, _, err := config.Load("upgrade", nil, getenv, nil)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(cfg.Store.Path) {
		cfg.Store.Path = filepath.Join(c.WorkingDirectory, cfg.Store.Path)
	}
	database := upgrade.Config{Engine: releaseidentity.SQLite, Path: cfg.Store.Path}
	if sourceGuard != nil {
		authorizeSource = func() error { return verifyNativeSecurityMutation(ctx, *configuration, host, database, *sourceGuard) }
		if err := authorizeMutation(); err != nil {
			return err
		}
	}
	cache := filepath.Join(c.StateDirectory, "downloads")
	installer, err := newSecurityImageInstaller(cache, pinned)
	if err != nil {
		return err
	}
	// Reclaim abandoned work before the first download, while the operator
	// lock is held. Recovery artifacts referenced by the journal stay public.
	keep := automaticRetainedReleases(previous, sourceGuard)
	diagnostics.Printf(ctx, 1, "Cleaning obsolete private download artifacts in %s", cache)
	if previous != nil && previous.Phase != "complete" {
		if err := installer.PruneNightlyScratch(ctx); err != nil {
			return err
		}
	} else if err := installer.PruneNightlyCache(ctx, keep...); err != nil {
		return err
	}
	capacity, err := storagehealth.Read(c.StateDirectory)
	if err != nil {
		return fmt.Errorf("inspect upgrade storage: %w", err)
	}
	diagnostics.Printf(ctx, 1, "Upgrade filesystem has %.0f MiB available after cleanup", float64(capacity.AvailableBytes)/(1<<20))
	if previous == nil || previous.Phase == "complete" {
		if err := host.PruneCandidates(); err != nil {
			return err
		}
		if previous != nil {
			if err := host.PrunePublic(previous.Runtime); err != nil {
				return err
			}
		}
	}
	// Failures can happen before a journal exists. Retire reproducible private
	// cache work on ordinary errors too; preserve all public recovery material.
	defer func() {
		var next *AutomaticHandoff
		if err != nil && !errors.As(err, &next) {
			currentJournal, readErr := readAutomaticJournal(journalPath)
			if readErr != nil || (currentJournal != nil && currentJournal.Phase != "complete") {
				err = errors.Join(err, readErr, installer.PruneNightlyScratch(ctx))
			} else {
				err = errors.Join(err, installer.PruneNightlyCache(ctx, keep...))
			}
		}
	}()
	source, err := newAutomaticReleaseSource()
	if err != nil {
		return err
	}
	var target selfupdate.PreparedNightly
	if expected != nil {
		fmt.Fprintln(out, "1/5 Verifying the selected signed release and migration route.")
		target, err = prepareExactAutomaticRelease(ctx, source, installer, *expected)
	} else {
		fmt.Fprintln(out, "1/5 Verifying the signed nightly and migration route.")
		var status updatecheck.Status
		status, err = selectAutomaticRelease(ctx, source, *version)
		if err != nil {
			return err
		}
		if previous != nil && previous.Phase != "complete" {
			// The unfinished operation pins its exact authenticated target.
			target, err = installer.PrepareNightlySource(ctx, status, previous.Target)
		} else {
			target, err = installer.PrepareNightly(ctx, status)
		}
	}
	if err != nil {
		return err
	}
	diagnostics.Printf(ctx, 1, "  Selected release %s.", target.Identity.Version)
	current, _, err := buildcompat.Current()
	if err != nil {
		return err
	}
	targetBundle, err := upgradebundle.Load(ctx, target.BundleDirectory, pinned, releaseidentity.SnapshotFloor{})
	if err != nil {
		return err
	}
	if node, matchErr := targetBundle.MatchBuild(current); matchErr != nil || node.Identity() != target.Identity {
		if *handoff {
			return errors.New("verified target binary does not match its signed compatibility declaration")
		}
		diagnostics.Printf(ctx, 1, "  Handing off to the verified %s executable; it restarts the five steps.", target.Identity.Version)
		next, err := automaticReleaseHandoffWithAuthorization(target, *configuration, expected, nativeSecurityAuthorization{Route: expectedRoute, Until: eligibleUntil, PolicyFile: policyFile, PolicySHA256: policyDigest, Source: sourceGuard})
		if err != nil {
			return err
		}
		return next
	}
	diagnostics.Printf(ctx, 1, "  Resolving the migration route from the installed release.")
	var route automaticRoute
	if expected != nil {
		route, err = discoverReleaseRoute(ctx, unattendedRouteInstaller{installer}, source, target, pinned, automaticStore{database}, database.Engine, previous, true)
	} else {
		route, err = prepareAutomaticRoute(ctx, installer, source, target, pinned, database, previous)
	}
	if err != nil {
		return err
	}
	if err := requireAutomaticRouteDigest(route, expectedRoute); err != nil {
		return err
	}
	if len(route.Plan.Steps()) == 0 {
		if err := errors.Join(installer.PruneNightlyCache(ctx, target.Identity), host.PruneCandidates()); err != nil {
			return fmt.Errorf("Hikyo %s is already installed; temporary artifact cleanup failed: %w", target.Identity.Version, err)
		}
		fmt.Fprintf(out, "Hikyo %s is already installed.\n", target.Identity.Version)
		return nil
	}
	if previous != nil && previous.Phase != "complete" && previous.Route != route.Plan.Digest() {
		return errors.New("unfinished upgrade route differs from current authenticated evidence")
	}
	diagnostics.Printf(ctx, 1, "  Route has %d step(s). Staging the route bundle for the service.", len(route.Plan.Steps()))
	publicBundle, err := host.StagePublicBundle(route.Directory)
	if err != nil {
		return err
	}
	retainPublicBundle := false
	defer func() {
		// Journal publication can fail before rename (for example ENOSPC)
		// or after rename during directory sync. Measure its actual reference.
		if !retainPublicBundle {
			err = errors.Join(err, cleanupAutomaticUnpublishedBundle(journalPath, publicBundle))
		}
	}()
	if _, err := upgradebundle.Load(ctx, publicBundle, pinned, route.Bundle.Snapshot().Floor()); err != nil {
		return err
	}
	staged := make(map[releaseidentity.Identity]string)
	for _, step := range route.Plan.Steps() {
		prepared, ok := route.Executables[step.Target]
		if !ok {
			return errors.New("authenticated route executable is missing")
		}
		diagnostics.Printf(ctx, 1, "  Staging candidate executable %s.", step.Target.Version)
		staged[step.Target], err = host.StageCandidate(prepared.BinaryPath, string(prepared.BinarySHA256))
		if err != nil {
			return err
		}
		if err := checkAutomaticBundleFormat(ctx, staged[step.Target]); err != nil {
			return fmt.Errorf("release %s cannot use the platform bundle: %w", step.Target.Version, err)
		}
	}
	resume := previous != nil && previous.Phase != "complete" && previous.Phase != "preparing"
	journal := previous
	if !resume {
		// Assigned, not declared: the unstarted-journal defer below reads the named result.
		var vault *upgradecustody.Vault
		vault, err = openAutomaticCustody(c, route.Instance, readPassword)
		if err != nil {
			return err
		}
		defer vault.Close()
		// Before the journal, the public evidence and the service stop.
		var root []byte
		root, err = crypto.ReadRootKey(c.RootKeyFile, "")
		if err != nil {
			return err
		}
		err = requireAutomaticCustodyMatchesPolicy(ctx, cfg, database, root, vault.Recipient())
		crypto.Zero(root)
		if err != nil {
			return err
		}
		var publicKey string
		publicKey, err = host.PublishPublicEvidence("operator.pub", vault.PublicKey())
		if err != nil {
			return err
		}
		var manifest releaseidentity.MigrationManifest
		manifest, err = route.Plan.SourceManifest(database.Engine)
		if err != nil {
			return err
		}
		journal = &automaticJournal{Format: hostJournalFormat, Phase: "preparing", Target: target.Identity, Source: upgradecompat.InstalledSource{Identity: route.Plan.Source(), Migrations: manifest, SchemaSHA256: route.Plan.SourceSchemaDigest()}, Instance: route.Instance, Route: route.Plan.Digest(), Runtime: hostupgrade.RuntimeEvidence{BundleDirectory: publicBundle, OperatorPublicKey: publicKey, TargetManifest: string(target.Identity.ManifestSHA256), LegacyWritersStopped: route.Plan.Source().Genesis == releaseidentity.LegacyGenesisV1}}
		var sourceEvidence hostupgrade.RuntimeEvidence
		if sourceGuard != nil {
			sourceEvidence, err = nativeSourceEvidence(host, previous)
			if err != nil {
				return err
			}
			defer func() {
				if err != nil && !maintenanceStarted {
					err = errors.Join(err, clearNativeAttempt(journalPath, previous, c.StateDirectory))
					retainPublicBundle = false
				}
			}()
			// Durable before the journal: a journal past "preparing" without it can never be rolled back.
			if err = recordNativeSource(c.StateDirectory, journal, *sourceGuard, sourceEvidence); err != nil {
				return err
			}
		}
		if err := writeAutomaticJournal(journalPath, journal); err != nil {
			return err
		}
		if sourceGuard == nil {
			// A manual upgrade must not bind to an earlier guarded attempt's kept record. Only now:
			// until its own journal replaced it, that record was still the earlier journal's.
			if err := removeNativeSourceRecord(c.StateDirectory); err != nil {
				return err
			}
		}
		retainPublicBundle = true
		fmt.Fprintln(out, "2/5 Stopping writers and creating the encrypted backup.")
		if err := authorizeMutation(); err != nil {
			return err
		}
		maintenanceStarted = true
		if err := host.FenceAndStop(ctx); err != nil {
			return err
		}
		// Every error after the full stop leaves the durable startup fence. A
		// later command reconciles it; no old executable is silently restarted.
		// The security coordinator is the exception before any database write:
		// its refusal repeats on every run, so a stopped service would stay down.
		if err := prepareAutomaticEvidence(ctx, host, database, route, staged[target.Identity], vault, journal, out); err != nil {
			if sourceGuard == nil {
				return err
			}
			restored, restoreErr := restoreNativeSource(ctx, host, automaticStore{database}, journal, c.StateDirectory, sourceGuard.BinarySHA256, err)
			if restored {
				// Classified as a refusal: the source is serving and no database write happened.
				maintenanceStarted, retainPublicBundle = false, false
				restoreErr = errors.Join(securitymaintenance.ErrSourceRestored, restoreErr, host.PrunePublic(sourceEvidence))
			}
			return restoreErr
		}
		journal.Phase = "proved"
		if err := writeAutomaticJournal(journalPath, journal); err != nil {
			return err
		}
	} else {
		journal.Runtime.BundleDirectory = publicBundle
		if err := authorizeMutation(); err != nil {
			return err
		}
		maintenanceStarted = true
		if err := host.FenceAndStop(ctx); err != nil {
			return err
		}
	}
	retainPublicBundle = true
	if err := applyAutomaticRoute(ctx, host, automaticStore{database}, route, staged, journal, journalPath, out); err != nil {
		if sourceGuard == nil {
			return err
		}
		// This process is the target's own executable, the one that wrote the archive.
		fmt.Fprintln(out, "The update did not complete; rolling the database back to the fenced snapshot.")
		served, snapshotRestored, rollbackErr := newNativeRollback(host, c, database, journalPath).run(ctx)
		if !served {
			return errors.Join(err, rollbackErr)
		}
		// The deferred journal restore and bundle cleanup above run now: the source is serving.
		maintenanceStarted, retainPublicBundle = false, false
		return errors.Join(nativeRestoredOutcome(snapshotRestored), rollbackErr, err)
	}
	if sourceGuard != nil {
		if err := removeNativeSourceRecord(c.StateDirectory); err != nil {
			return err
		}
	}
	// The installed binary and public bundle are copies; the download cache
	// and staged candidates behind them are now dead weight. Keep the target
	// nightly so an immediate rerun reports "already installed" without a
	// fresh multi-hundred-MiB download.
	if err := errors.Join(installer.PruneNightlyCache(ctx, target.Identity), host.PruneCandidates()); err != nil {
		return fmt.Errorf("Hikyo %s is upgraded and ready; temporary artifact cleanup failed: %w", target.Identity.Version, err)
	}
	fmt.Fprintf(out, "Hikyo %s is upgraded and ready. Encrypted backup retained at %s.\n", target.Identity.Version, journal.Runtime.CiphertextPath)
	return nil
}

type nativeSourceHost interface {
	FenceAndStop(context.Context) error
	StartCandidate(context.Context, string, bool, time.Duration) error
	Complete(context.Context) error
}

// restoreNativeSource is the native counterpart of abandonUnstartedPreparation. The
// preparation steps cannot write the live database: export only reads and the proof
// runs on scratch. Even so, it restarts the source only while the journal is still
// "preparing" and the control row is the untouched source. Any doubt keeps the
// service stopped, and the deferred journal restore then runs only on success.
func restoreNativeSource(ctx context.Context, host nativeSourceHost, database automaticInspection, journal *automaticJournal, stateDirectory string, binary releaseidentity.Digest, cause error) (bool, error) {
	state, err := database.Control(ctx)
	if !journal.preparationNeverWrote(state, err) {
		return false, errors.Join(cause, err, errors.New("the service stays stopped: the database is not the untouched source"))
	}
	// StartCandidate permits this boot only and fences the service again if the source is not healthy.
	if err := host.StartCandidate(ctx, string(binary), true, 45*time.Second); err != nil {
		return false, errors.Join(cause, err)
	}
	after, err := database.Control(ctx)
	// StartCandidate succeeded, so the source is writing: only a stop keeps the promise below.
	fence := func() error {
		cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		return host.FenceAndStop(cleanup)
	}
	if err != nil || after.Applied != state.Applied || after.InstanceID != state.InstanceID || after.RestoreEpoch != state.RestoreEpoch || after.Generation != state.Generation || after.Maintenance || after.Pending == nil || after.Pending.Phase != upgrade.Healthy {
		return false, errors.Join(cause, err, errors.New("the restarted source is not the untouched source; the service stays under maintenance"), fence())
	}
	if err := host.Complete(ctx); err != nil {
		return false, errors.Join(cause, err, fence())
	}
	// The source is serving, so only the reason is left to record; the caller still
	// reports the failure as a refusal.
	return true, errors.Join(cause, recordAbandonedPreparation(stateDirectory, "the source service was restarted after its preparation failed before the live database was written", journal))
}

// nativeSourceEvidence is what the source boots from. A host enrolled from nothing
// has no completed journal, so its bundle exists only as the runtime.env entry.
func nativeSourceEvidence(host interface{ LocalBundleDirectory() (string, error) }, previous *automaticJournal) (hostupgrade.RuntimeEvidence, error) {
	if previous != nil {
		return previous.Runtime, nil
	}
	bundle, err := host.LocalBundleDirectory()
	return hostupgrade.RuntimeEvidence{BundleDirectory: bundle}, err
}

// newAutomaticReleaseSource is the signed-release source both the upgrade and the rollback refetch use.
func newAutomaticReleaseSource() (*updatecheck.GitHubSource, error) {
	client, err := updatecheck.NewHTTPClient(60 * time.Second)
	if err != nil {
		return nil, err
	}
	client.Transport = diagnostics.Transport{Base: client.Transport}
	return updatecheck.NewGitHubSource(client), nil
}

func restoreAutomaticUnstartedJournal(path string, previous *automaticJournal) error {
	if previous != nil {
		return writeAutomaticJournal(path, previous)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return filedurability.SyncDirectory(filepath.Dir(path))
}

func automaticRetainedReleases(previous *automaticJournal, sourceGuard *nativeSecuritySourceGuard) []releaseidentity.Identity {
	keep := []releaseidentity.Identity{}
	if sourceGuard != nil {
		keep = append(keep, sourceGuard.Source.Identity)
	}
	if previous != nil {
		keep = append(keep, previous.Target)
	}
	return keep
}

// errUnlockedByNeitherSecret names both causes of an ErrUnlock: the vault cannot
// tell a passphrase vault from one wrapped under a retired root key.
var errUnlockedByNeitherSecret = fmt.Errorf("%w: upgrade custody did not open under the root key in use; either it still uses a passphrase (run sudo hikyo upgrade once from a terminal to migrate it to root-key wrapping) or the root key was rotated after it was sealed (a host vault is root-only and is not re-sealed by rotate-root-key)", upgradecustody.ErrUnlock)

// openAutomaticCustody unlocks the operator vault with a secret derived from
// the installation's root key, so an upgrade needs no human at a terminal.
// A vault created under the earlier passphrase design is migrated once: the
// passphrase is asked for a last time, the same record is rewrapped, and the
// backup identity that decrypts earlier upgrade backups is preserved.
func openAutomaticCustody(c hostupgrade.Config, instance string, readPassword func(string) (string, error)) (*upgradecustody.Vault, error) {
	root, err := crypto.ReadRootKey(c.RootKeyFile, "")
	if err != nil {
		return nil, err
	}
	defer crypto.Zero(root)
	secret, err := upgradecustody.RootKeySecret(root)
	if err != nil {
		return nil, err
	}
	defer clear(secret)
	_, err = os.Lstat(filepath.Join(c.CustodyDirectory, "operator.age"))
	if errors.Is(err, os.ErrNotExist) {
		return upgradecustody.Create(c.CustodyDirectory, secret, root, instance)
	}
	if err != nil {
		return nil, err
	}
	vault, err := upgradecustody.Open(c.CustodyDirectory, secret, instance)
	if err == nil {
		return vault, nil
	}
	if !errors.Is(err, upgradecustody.ErrUnlock) {
		return nil, err
	}
	if readPassword == nil {
		return nil, errUnlockedByNeitherSecret
	}
	password, err := readPassword("Enter the upgrade recovery passphrase once more to migrate the keys to root-key wrapping: ")
	if err != nil {
		return nil, err
	}
	previous := []byte(password)
	defer clear(previous)
	if err := upgradecustody.Rewrap(c.CustodyDirectory, previous, secret, instance); err != nil {
		if errors.Is(err, upgradecustody.ErrUnlock) {
			return nil, fmt.Errorf("%w: the passphrase did not open it either", errUnlockedByNeitherSecret)
		}
		return nil, err
	}
	return upgradecustody.Open(c.CustodyDirectory, secret, instance)
}

func prepareAutomaticEvidence(ctx context.Context, host *hostupgrade.Host, database upgrade.Config, route automaticRoute, candidate string, vault *upgradecustody.Vault, journal *automaticJournal, out io.Writer) error {
	nonce, err := backupreceipt.NewNonce()
	if err != nil {
		return err
	}
	output, err := host.PreparePublicOutput("backup-" + string(nonce))
	if err != nil {
		return err
	}
	raw, err := host.Export(ctx, candidate, hostupgrade.ExportRequest{OutputDirectory: output, Recipient: vault.Recipient(), Runtime: journal.Runtime})
	if err != nil {
		return err
	}
	var exported struct {
		Ciphertext string `json:"ciphertext"`
		Receipt    string `json:"receipt"`
	}
	if definitions.DecodeStrict(raw, &exported) != nil || filepath.Dir(exported.Ciphertext) != output || filepath.Dir(exported.Receipt) != output {
		return errors.New("backup export returned invalid artifact paths")
	}
	receipt, err := backupreceipt.ReadPublicArtifact(exported.Receipt, backupreceipt.MaxArtifactBytes)
	if err != nil {
		return err
	}
	ciphertext, err := backupreceipt.PinCiphertext(ctx, exported.Ciphertext, host.Config().StateDirectory)
	if err != nil {
		return err
	}
	defer ciphertext.Close()
	work, err := os.MkdirTemp(host.Config().StateDirectory, ".restore-drill-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	fmt.Fprintln(out, "3/5 Restoring the backup to scratch and proving recovery.")
	scope := domain.Scope{}
	if host.Config().Project != "" {
		scope, err = parseDrillProject(host.Config().Project)
		if err != nil {
			return err
		}
	}
	result, err := DrillUpgrade(ctx, UpgradeDrillRequest{Scratch: store.Config{Engine: store.EngineSQLite, Path: filepath.Join(work, "scratch.db")}, Ciphertext: ciphertext, Receipt: receipt, Plan: route.Plan, Operator: vault.Pin(), Unlock: vault.BackupUnlock(), RootKey: vault.RootKey(), Principal: domain.PrincipalID(host.Config().Principal), Scope: scope, AutoCredentialProof: host.Config().Principal == "", Now: time.Now(), Lifetime: backupreceipt.MaxAttestationLifetime})
	if err != nil {
		return err
	}
	signature, err := vault.SignAttestation(result.Attestation, time.Now())
	if err != nil {
		return err
	}
	directory := filepath.Join(host.Config().PublicDirectory, "evidence-"+string(nonce))
	if err := os.Mkdir(directory, 0755); err != nil {
		return err
	}
	for name, contents := range map[string][]byte{"receipt.json": receipt, "attestation.json": result.Attestation, "attestation.sigstore.json": signature} {
		if err := writeAutomaticFile(filepath.Join(directory, name), contents, 0644); err != nil {
			return err
		}
	}
	if err := filedurability.SyncDirectory(host.Config().PublicDirectory); err != nil {
		return err
	}
	journal.Runtime.EvidenceDirectory, journal.Runtime.CiphertextPath = directory, exported.Ciphertext
	return nil
}

// writeAutomaticFile is securefile.WriteAtomic: the mode is applied before any
// byte lands, and the rename plus directory sync make the publication durable.
func writeAutomaticFile(path string, raw []byte, mode os.FileMode) error {
	return securefile.WriteAtomic(path, raw, mode)
}

// cleanupAutomaticUnpublishedBundle receives only this invocation's newly
// staged path. An unreadable journal leaves ownership uncertain, so preserve
// the bundle and report the error rather than deleting potential recovery data.
func cleanupAutomaticUnpublishedBundle(journalPath, bundlePath string) error {
	journal, err := readAutomaticJournal(journalPath)
	if err != nil {
		return fmt.Errorf("check staged bundle journal before cleanup: %w", err)
	}
	if journal != nil && journal.Runtime.BundleDirectory == bundlePath {
		return nil
	}
	return os.RemoveAll(bundlePath)
}

func writeAutomaticJournal(path string, journal *automaticJournal) error {
	raw, err := json.Marshal(journal, jsonwire.Stable)
	if err != nil {
		return err
	}
	return writeAutomaticFile(path, raw, 0600)
}

func readAutomaticJournal(path string) (*automaticJournal, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 1<<20 {
		return nil, errors.New("invalid private upgrade journal")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var journal automaticJournal
	if definitions.DecodeStrict(raw, &journal) != nil || (journal.Format != hostJournalFormat && journal.Format != containerJournalFormat) || journal.Target.Validate() != nil || journal.Source.Identity.Validate() != nil || journal.Source.Migrations.Validate() != nil || journal.Source.SchemaSHA256.Validate() != nil || journal.Route.Validate() != nil || journal.Hop < 0 || journal.Hop > upgradecompat.MaxHops {
		return nil, errors.New("invalid private upgrade journal")
	}
	switch journal.Phase {
	case "preparing", "proved", "write-intent", "schema-applied", "hop-healthy", "restore-required", "rolling-back", "complete":
	default:
		return nil, errors.New("unknown upgrade journal phase")
	}
	return &journal, nil
}
