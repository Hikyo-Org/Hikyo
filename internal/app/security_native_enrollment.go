package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecustody"
	"github.com/gofrs/flock"
)

// nativeEnrollmentBootTimeout bounds a first boot, which migrates a new database.
const nativeEnrollmentBootTimeout = 5 * time.Minute

type nativeEnrollmentHost interface {
	Config() hostupgrade.Config
	Environment() map[string]string
	PreflightNewInstallation(context.Context) error
	PrepareDirectories() error
	PublishPublicEvidence(string, []byte) (string, error)
	StagePublicBundle(string) (string, error)
	ConfigureRuntime(context.Context, hostupgrade.RuntimeEvidence) error
	PrunePublic(hostupgrade.RuntimeEvidence) error
	BootNewInstallation(context.Context, string, bool, time.Duration, io.Writer, io.Writer) error
}

// nativeInstalledRelease is the signed release the installed executable proves to
// be, with its verified bundle still in the private cache.
type nativeInstalledRelease struct {
	Identity        releaseidentity.Identity
	BundleDirectory string
	BinarySHA256    string
	// Retire drops cache entries other than this release once the bundle is staged.
	Retire func() error
}

// nativeEnrollment prepares custody for a native host whose operator key this
// installation itself holds. A host whose key lives elsewhere is left alone so the
// ordinary custody check refuses it with its remedy.
type nativeEnrollment struct {
	Host    nativeEnrollmentHost
	Out     io.Writer
	Inspect func(context.Context, upgrade.Config) (upgrade.State, error)
	// Fresh fails unless the database is empty: a datastore without control objects may still be a legacy installation.
	Fresh   func(context.Context, upgrade.Config) error
	RootKey func(string) ([]byte, error)
	Release func(context.Context, hostupgrade.Config) (nativeInstalledRelease, error)
}

func newNativeEnrollment(host *hostupgrade.Host, out io.Writer) nativeEnrollment {
	return nativeEnrollment{Host: host, Out: out, Inspect: upgrade.InspectControl, Fresh: requireFreshNativeDatabase,
		RootKey: func(path string) ([]byte, error) { return crypto.ReadRootKey(path, "") }, Release: installedNativeRelease}
}

// installedNativeRelease downloads and verifies the release the installed executable
// claims to be, as the container recipe does for its image, so a source build or a
// replaced binary cannot enroll.
func installedNativeRelease(ctx context.Context, c hostupgrade.Config) (nativeInstalledRelease, error) {
	var result nativeInstalledRelease
	claim, declaration, err := buildcompat.Current()
	if err != nil {
		return result, err
	}
	prepared, err := prepareSecurityReleaseVersion(ctx, declaration.Version, nil, filepath.Join(c.StateDirectory, "downloads"))
	if err != nil {
		return result, err
	}
	node, err := prepared.Bundle.MatchBuild(claim)
	if err != nil || node.Identity() != prepared.Target.Identity {
		return result, errors.Join(err, errors.New("downloaded release differs from the installed executable's compatibility declaration"))
	}
	if err := buildcompat.Verify(node); err != nil {
		return result, err
	}
	digest, err := fileDigest(c.Binary)
	if err != nil || strings.TrimPrefix(digest, "sha256:") != string(prepared.Target.BinarySHA256) {
		return result, errors.Join(err, errors.New("the installed executable "+c.Binary+" differs from its authenticated release archive"))
	}
	return nativeInstalledRelease{Identity: prepared.Target.Identity, BundleDirectory: prepared.Target.BundleDirectory, BinarySHA256: string(prepared.Target.BinarySHA256),
		Retire: func() error { return prepared.Installer.PruneNightlyCache(ctx, prepared.Target.Identity) }}, nil
}

func requireFreshNativeDatabase(ctx context.Context, database upgrade.Config) error {
	inspection, err := upgrade.Inspect(ctx, database, releaseidentity.MigrationManifest{Engine: database.Engine, Entries: []releaseidentity.Migration{}})
	if err != nil || inspection.Genesis != releaseidentity.FreshGenesisV1 {
		return errors.Join(err, errors.New("native enrollment requires a fresh database; legacy adoption is unsupported"))
	}
	return nil
}

// lock excludes a concurrent operator upgrade, which holds the same lock file.
func (e nativeEnrollment) lock() (func() error, error) {
	lock := flock.New(filepath.Join(e.Host.Config().StateDirectory, "upgrade.lock"))
	locked, err := lock.TryLock()
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("another operator is already upgrading this installation")
	}
	return lock.Unlock, nil
}

func (e nativeEnrollment) say(format string, args ...any) {
	if e.Out != nil {
		fmt.Fprintf(e.Out, format+"\n", args...)
	}
}

// Prepare ends with a vault bound to the booted installation, the pin published and
// configured, and the installed release's signed bundle and archive cached. Every
// step is idempotent. The pin is written after everything it names exists, so a
// failure before it leaves no pin and one after it resumes from the booted service.
func (e nativeEnrollment) Prepare(ctx context.Context) (result error) {
	c := e.Host.Config()
	if err := e.Host.PreflightNewInstallation(ctx); err != nil {
		return err
	}
	environment := e.Host.Environment()
	pin, err := nativeConfiguredOperatorPin(c, environment)
	if err != nil {
		return err
	}
	published := filepath.Join(c.PublicDirectory, "operator.pub")
	if pin != "" && pin != published {
		return nil
	}
	_, vaultErr := os.Lstat(filepath.Join(c.CustodyDirectory, "operator.age"))
	if vaultErr != nil && !errors.Is(vaultErr, os.ErrNotExist) {
		return vaultErr
	}
	hasVault := vaultErr == nil
	database, err := nativeDatabase(c, environment)
	if err != nil {
		return err
	}
	state, err := e.Inspect(ctx, database)
	booted := err == nil
	if err != nil && !errors.Is(err, upgrade.ErrAbsent) {
		return err
	}
	if booted {
		// A booted service with no vault of ours holds its key elsewhere.
		if !hasVault || pin != published {
			return nil
		}
		unlock, err := e.lock()
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, unlock()) }()
		if err := e.bind(state, published); err != nil {
			return err
		}
		if state.Maintenance || state.Pending == nil || state.Pending.Phase != upgrade.Healthy {
			// An interrupted migrate leaves its control objects at these phases, and only migrate advances them.
			migrate := state.Pending != nil && (state.Pending.Phase == upgrade.Prepared || state.Pending.Phase == upgrade.SchemaWriteStarted)
			if err := e.requireNoUnfinishedUpgrade(); err != nil {
				return err
			}
			return e.boot(ctx, c, migrate)
		}
		return nil
	}
	// Refuse before the first change; the recheck under the lock covers a concurrent writer.
	if err := errors.Join(e.Fresh(ctx, database), e.requireNoUnfinishedUpgrade()); err != nil {
		return err
	}
	e.say("Preparing operator custody and the signed release evidence for this new installation.")
	if err := e.Host.PrepareDirectories(); err != nil {
		return err
	}
	unlock, err := e.lock()
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, unlock()) }()
	if err := errors.Join(e.Fresh(ctx, database), e.requireNoUnfinishedUpgrade()); err != nil {
		return err
	}
	root, err := e.RootKey(c.RootKeyFile)
	if err != nil {
		return err
	}
	defer crypto.Zero(root)
	public, err := upgradecustody.PrepareLocalEnrollment(c.CustodyDirectory, root)
	if err != nil {
		return err
	}
	if _, err := e.Host.PublishPublicEvidence("operator.pub", public); err != nil {
		return err
	}
	release, err := e.Release(ctx, c)
	if err != nil {
		return err
	}
	bundle, err := e.Host.StagePublicBundle(release.BundleDirectory)
	if err != nil {
		return err
	}
	evidence := hostupgrade.RuntimeEvidence{BundleDirectory: bundle, OperatorPublicKey: published, TargetManifest: string(release.Identity.ManifestSHA256)}
	if err := e.Host.ConfigureRuntime(ctx, evidence); err != nil {
		return err
	}
	if release.Retire != nil {
		if err := release.Retire(); err != nil {
			return err
		}
	}
	if err := e.Host.PrunePublic(evidence); err != nil {
		return err
	}
	e.say("Starting the new installation and binding its operator key.")
	if err := e.Host.BootNewInstallation(ctx, release.BinarySHA256, true, nativeEnrollmentBootTimeout, e.Out, e.Out); err != nil {
		return err
	}
	state, err = e.Inspect(ctx, database)
	if err != nil {
		return err
	}
	return e.bind(state, published)
}

// requireNoUnfinishedUpgrade leaves a host upgrade's journal to that upgrade: enrollment never
// starts its candidate or builds a new installation over a rollback that removed the database.
func (e nativeEnrollment) requireNoUnfinishedUpgrade() error {
	journal, err := readAutomaticJournal(filepath.Join(e.Host.Config().StateDirectory, "operation.json"))
	if err != nil || journal != nil && journal.Phase != "complete" {
		return errors.Join(err, errors.New("an unfinished host upgrade owns this installation; finish it with sudo hikyo upgrade first"))
	}
	return nil
}

func (e nativeEnrollment) boot(ctx context.Context, c hostupgrade.Config, migrate bool) error {
	digest, err := fileDigest(c.Binary)
	if err != nil {
		return err
	}
	return e.Host.BootNewInstallation(ctx, strings.TrimPrefix(digest, "sha256:"), migrate, nativeEnrollmentBootTimeout, e.Out, e.Out)
}

// bind attaches the vault to the instance the service booted with. The pinned key
// is the published file the service read, so a vault holding another key refuses.
// An unbound vault is only ever adopted by the fresh installation it was created for.
func (e nativeEnrollment) bind(state upgrade.State, published string) error {
	c := e.Host.Config()
	root, err := e.RootKey(c.RootKeyFile)
	if err != nil {
		return err
	}
	defer crypto.Zero(root)
	if bound, err := upgradecustody.OpenLocal(c.CustodyDirectory, root, state.InstanceID); err == nil {
		bound.Close()
	} else if err := e.requireFreshInstance(state); err != nil {
		return err
	}
	instance := state.InstanceID
	public, err := os.ReadFile(published)
	if err != nil {
		return err
	}
	vault, err := upgradecustody.BindLocalEnrollment(c.CustodyDirectory, root, instance, public)
	if err != nil {
		return fmt.Errorf("the vault in %s does not open for this installation under its root key (a vault from an earlier passphrase design needs `sudo hikyo upgrade` once from a terminal; a vault sealed before a root key rotation is wrapped under the retired root): %w", c.CustodyDirectory, err)
	}
	vault.Close()
	return nil
}

// requireFreshInstance accepts only a database whose first operation is the fresh
// installation of the release this enrollment pinned.
func (e nativeEnrollment) requireFreshInstance(state upgrade.State) error {
	raw, err := os.ReadFile(filepath.Join(e.Host.Config().StateDirectory, "runtime.env"))
	if err != nil {
		return err
	}
	values, err := hostupgrade.ParseEnvironmentFile(raw)
	if err != nil {
		return err
	}
	pending := state.Pending
	if state.Generation != 1 || pending == nil || pending.Invalidated || pending.RouteSource.Genesis != releaseidentity.FreshGenesisV1 || string(pending.Target.ManifestSHA256) != values["HIKYO_UPGRADE_TARGET_MANIFEST"] {
		return errors.New("the unbound operator vault belongs to a fresh installation, and this database is not the one enrolled")
	}
	return nil
}

// nativeEnrollmentOf finds the native driver behind the policy binding.
func nativeEnrollmentOf(d securitymaintenance.Driver) *nativeSecurityDriver {
	if bound, ok := d.(policyBoundSecurityDriver); ok {
		d = bound.Driver
	}
	driver, _ := d.(*nativeSecurityDriver)
	return driver
}

// PrepareEnrollment runs before the engine's own enrollment, which observes a
// running, bound installation.
func (d *nativeSecurityDriver) PrepareEnrollment(ctx context.Context) error {
	c, err := hostupgrade.LoadConfig(d.configuration)
	if err != nil {
		return err
	}
	host, err := hostupgrade.New(c)
	if err != nil {
		return err
	}
	return newNativeEnrollment(host, d.out).Prepare(ctx)
}
