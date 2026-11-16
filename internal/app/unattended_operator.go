package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
)

// ResolveUnattendedOperator lets a host-only operator command (admin, backup,
// restore) run in an admitted unattended container.
//
// The server derives its authenticated bundle, operator public-key pin and
// target manifest in memory (RunUnattendedUpgrade), so a command started later
// with `exec` inherits none of them. This reads the installation's own record of
// those inputs back and applies them to the command's configuration. It is a
// reader: it never enrolls, downloads, selects or replaces a release, takes the
// coordinator lock, writes state or migrates (AutoMigrate is forced off), and it
// asserts no evidence, legacy-writer or recovery authority. Every existing root,
// build, migration and custody check still runs afterward in the ordinary gate.
// A manual installation (HIKYO_UPGRADE_UNATTENDED unset) is untouched.
func ResolveUnattendedOperator(ctx context.Context, cfg *config.Config) error {
	if !cfg.Upgrade.Unattended {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return resolveUnattendedOperator(ctx, cfg, executable)
}

func resolveUnattendedOperator(ctx context.Context, cfg *config.Config, executable string) error {
	if cfg.Dev || !filepath.IsAbs(cfg.Upgrade.StateDirectory) {
		return errors.New("unattended operator commands require a signed production release and an absolute HIKYO_UPGRADE_STATE_DIR")
	}
	// An unattended installation derives these from its enrollment; a second
	// source would be a second authority over which release and operator apply.
	for _, input := range []struct{ name, value string }{
		{"HIKYO_UPGRADE_BUNDLE", cfg.Upgrade.BundleDirectory},
		{"HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY", cfg.Upgrade.OperatorPublicKeyFile},
		{"HIKYO_UPGRADE_TARGET_MANIFEST", cfg.Upgrade.TargetManifestSHA256},
		{"HIKYO_UPGRADE_EVIDENCE", cfg.Upgrade.EvidenceDirectory},
		{"HIKYO_UPGRADE_BACKUP", cfg.Upgrade.CiphertextPath},
	} {
		if input.value != "" {
			return fmt.Errorf("%s belongs to manual installations; an unattended installation derives it from its enrollment", input.name)
		}
	}
	if cfg.Upgrade.LegacyWritersStopped {
		return errors.New("HIKYO_UPGRADE_LEGACY_WRITERS_STOPPED belongs to manual installations; an unattended installation asserts no legacy-writer claim")
	}
	directory := filepath.Join(cfg.Upgrade.StateDirectory, "unattended")
	info, err := os.Lstat(directory)
	if errors.Is(err, os.ErrNotExist) {
		return errUnattendedNotEnrolled
	}
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("unattended state directory must be a private real directory")
	}
	enrollment, enrolled, err := readUnattendedEnrollment(directory)
	if err != nil {
		return err
	}
	if !enrolled {
		return errUnattendedNotEnrolled
	}
	if enrollment.Instance == "" {
		return errors.New("unattended enrollment is unfinished; let the server finish its first start before running operator commands")
	}
	journal, err := readAutomaticJournal(filepath.Join(directory, "operation.json"))
	if err != nil {
		return err
	}
	if journal != nil && journal.Phase != "complete" {
		return fmt.Errorf("an unattended upgrade is unfinished (phase %q); operator commands wait until the server completes or recovers it", journal.Phase)
	}
	image, err := readUnattendedImage(directory)
	if err != nil {
		return fmt.Errorf("unattended installation has no usable image record: %w", err)
	}
	// A consistency check only: the state volume is writable by the same user, so
	// the real binding is the signed build claim matched against the bundle below.
	digest, err := fileDigest(executable)
	if err != nil || strings.TrimPrefix(digest, "sha256:") != string(image.BinarySHA256) {
		return errors.Join(err, errors.New("this executable differs from the image the installation last admitted"))
	}
	claim, _, err := buildcompat.Current()
	if err != nil {
		return err
	}
	pinned, err := buildcompat.ProductionTrust()
	if err != nil {
		return err
	}
	state, err := upgrade.InspectControl(ctx, upgrade.Config{Engine: releaseidentity.Engine(cfg.Store.Engine), Path: cfg.Store.Path, DSN: cfg.Store.DSN})
	if err != nil {
		return fmt.Errorf("unattended installation has no readable database: %w", err)
	}
	if state.InstanceID != enrollment.Instance {
		return errors.New("unattended enrollment differs from installed database")
	}
	if err := requireHealthyAdmittedState(state); err != nil {
		return err
	}
	bundle, err := upgradebundle.Load(ctx, image.BundleDirectory, pinned, state.Floor)
	if err != nil {
		return err
	}
	node, err := bundle.MatchBuild(claim)
	if err != nil || node.Identity() != image.Identity {
		return errors.Join(err, errors.New("this release differs from the one the installation last admitted"))
	}
	if err := buildcompat.Verify(node); err != nil {
		return err
	}
	publicPath := filepath.Join(directory, "operator.pub")
	public, err := backupreceipt.ReadPublicArtifact(publicPath, 1<<20)
	if err != nil || releaseidentity.Hash(public) != enrollment.Operator {
		return errors.Join(err, errors.New("unattended operator pin differs from enrolled custody"))
	}
	cfg.Upgrade.BundleDirectory, cfg.Upgrade.OperatorPublicKeyFile = image.BundleDirectory, publicPath
	cfg.Upgrade.TargetManifestSHA256 = string(image.Identity.ManifestSHA256)
	cfg.Upgrade.LegacyWritersStopped = false
	cfg.AutoMigrate = false
	return nil
}

// requireHealthyAdmittedState refuses a database that is mid-upgrade, in
// maintenance, awaiting a restore or invalidated, whatever the journal says. The
// shared gate can run recovery for some of these states; an operator command must
// never be what starts it.
func requireHealthyAdmittedState(state upgrade.State) error {
	if state.Pending == nil || state.Pending.Phase != upgrade.Healthy || state.Maintenance || state.Pending.Invalidated {
		phase := upgrade.Phase("")
		if state.Pending != nil {
			phase = state.Pending.Phase
		}
		return fmt.Errorf("the installation is not in a healthy admitted state (phase %q, maintenance %t); operator commands wait until the server completes or recovers it", phase, state.Maintenance)
	}
	return nil
}

var errUnattendedNotEnrolled = errors.New("this installation is not enrolled for unattended upgrades; operator commands never enroll, so start the server on a fresh installation to enroll it")
