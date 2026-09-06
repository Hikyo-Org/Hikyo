package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

// Only process/service operations are abstracted here. Production inspection
// always measures the real SQLite catalog, migration history and runtime ledger.
type automaticApplyHost interface {
	FenceAndStop(context.Context) error
	Migrate(context.Context, string, hostupgrade.RuntimeEvidence) ([]byte, error)
	InstallBinary(context.Context, string, string) error
	ConfigureRuntime(context.Context, hostupgrade.RuntimeEvidence) error
	StartCandidate(context.Context, string, bool, time.Duration) error
	Complete(context.Context) error
	PrunePublic(hostupgrade.RuntimeEvidence) error
}

type automaticInspection interface {
	Control(context.Context) (upgrade.State, error)
	Installed(context.Context, releaseidentity.MigrationManifest) (upgrade.InstalledSource, error)
}

type automaticStore struct{ config upgrade.Config }

func (s automaticStore) Control(ctx context.Context) (upgrade.State, error) {
	return upgrade.InspectControl(ctx, s.config)
}
func (s automaticStore) Installed(ctx context.Context, manifest releaseidentity.MigrationManifest) (upgrade.InstalledSource, error) {
	return upgrade.InspectInstalled(ctx, s.config, manifest)
}

func applyAutomaticRoute(ctx context.Context, host automaticApplyHost, database automaticInspection, route automaticRoute, staged map[releaseidentity.Identity]string, journal *automaticJournal, journalPath string, out io.Writer) (err error) {
	// Register the failure fence before any reconciliation or process operation.
	defer func() {
		if err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			err = errors.Join(err, host.FenceAndStop(cleanup))
		}
	}()
	if err := validateAutomaticPosition(route.Plan, journal); err != nil {
		return err
	}
	steps := route.Plan.Steps()
	start := journal.Hop
	finalAlreadyHealthy := journal.Phase == "hop-healthy" && start == len(steps)
	if finalAlreadyHealthy {
		// Entry fenced the service during reconciliation. Repeat exact binary
		// installation and readiness proof before removing that fence or completing.
		start--
	}
	if journal.Phase == "hop-healthy" && journal.Hop > 0 {
		state, err := database.Control(ctx)
		if err != nil {
			return errors.New("completed hop has uncertain database state; operator recovery is required")
		}
		if err := validateAutomaticHealthy(state, journal, route.Plan, journal.Hop-1); err != nil {
			return err
		}
	}
	fmt.Fprintln(out, "4/5 Applying the authenticated migration route.")
	for index := start; index < len(steps); index++ {
		step := steps[index]
		candidate, ok := staged[step.Target]
		prepared, have := route.Executables[step.Target]
		if !ok || !have || candidate == "" || prepared.Identity != step.Target || prepared.BinarySHA256.Validate() != nil {
			return errors.New("authenticated route executable is missing or mismatched")
		}
		shouldMigrate := false
		if !finalAlreadyHealthy {
			shouldMigrate, err = automaticMigrationDisposition(ctx, database, journal, route.Plan, index)
			if err != nil {
				return err
			}
		}
		if shouldMigrate {
			journal.Hop, journal.Phase = index, "write-intent"
			if err := writeAutomaticJournal(journalPath, journal); err != nil {
				return err
			}
			if _, err := host.Migrate(ctx, candidate, journal.Runtime); err != nil {
				return fmt.Errorf("migration failed; service remains stopped: %w", err)
			}
			// A successful child exit alone is insufficient. Confirm the exact durable
			// target/hop before publishing schema-applied or installing its executable.
			state, err := database.Control(ctx)
			if err != nil {
				return errors.New("migration returned without readable durable admission state")
			}
			if err := validateAutomaticPending(state, journal, route.Plan, index); err != nil {
				return err
			}
			if state.Pending.Phase != upgrade.SchemaApplied {
				return errors.New("migration did not establish exact schema-applied state")
			}
			journal.Phase = "schema-applied"
			if err := writeAutomaticJournal(journalPath, journal); err != nil {
				return err
			}
		}
		if err := host.InstallBinary(ctx, candidate, string(prepared.BinarySHA256)); err != nil {
			return err
		}
		startEvidence := journal.Runtime
		if finalAlreadyHealthy {
			// A completed final DB admission needs only restart evidence, not
			// the old encrypted backup or one-use attestation files.
			startEvidence.EvidenceDirectory, startEvidence.CiphertextPath = "", ""
			startEvidence.LegacyWritersStopped = false
		}
		if err := host.ConfigureRuntime(ctx, startEvidence); err != nil {
			return err
		}
		final := index == len(steps)-1
		fmt.Fprintf(out, "5/5 Checking Hikyo %s (%d/%d).\n", step.Target.Version, index+1, len(steps))
		if err := host.StartCandidate(ctx, string(prepared.BinarySHA256), final, 45*time.Second); err != nil {
			return recordAutomaticHealthFailure(host, database, route.Plan, journal, journalPath, index, err)
		}
		state, err := database.Control(ctx)
		if err != nil {
			return recordAutomaticHealthFailure(host, database, route.Plan, journal, journalPath, index, err)
		}
		if err := validateAutomaticHealthy(state, journal, route.Plan, index); err != nil {
			return recordAutomaticHealthFailure(host, database, route.Plan, journal, journalPath, index, err)
		}
		journal.Phase, journal.Hop = "hop-healthy", index+1
		if err := writeAutomaticJournal(journalPath, journal); err != nil {
			return err
		}
		if !final {
			if err := host.FenceAndStop(ctx); err != nil {
				return err
			}
		}
	}
	restartEvidence := journal.Runtime
	restartEvidence.EvidenceDirectory, restartEvidence.CiphertextPath = "", ""
	restartEvidence.LegacyWritersStopped = false
	if err := host.ConfigureRuntime(ctx, restartEvidence); err != nil {
		return err
	}
	if err := host.Complete(ctx); err != nil {
		return err
	}
	journal.Phase = "complete"
	if err := writeAutomaticJournal(journalPath, journal); err != nil {
		return err
	}
	// Earlier runs' public bundles, one-use attestations and backups are no
	// longer referenced; only this run's evidence and encrypted backup remain.
	return host.PrunePublic(journal.Runtime)
}

func validateAutomaticPosition(plan upgradecompat.Plan, journal *automaticJournal) error {
	if journal == nil || !plan.Valid() || len(plan.Steps()) == 0 || journal.Target != plan.Target() || journal.Route != plan.Digest() || journal.Source.Identity != plan.Source() || journal.Source.SchemaSHA256 != plan.SourceSchemaDigest() || journal.Instance == "" {
		return errors.New("host journal differs from the authenticated route")
	}
	expected, err := plan.SourceManifest(journal.Source.Migrations.Engine)
	actualDigest, actualErr := journal.Source.Migrations.Digest()
	expectedDigest, expectedErr := expected.Digest()
	if err != nil || actualErr != nil || expectedErr != nil || actualDigest != expectedDigest {
		return errors.New("host journal source migrations differ from the authenticated route")
	}
	length := len(plan.Steps())
	if journal.Hop < 0 || journal.Hop > length {
		return errors.New("host journal hop lies outside the authenticated route")
	}
	switch journal.Phase {
	case "proved":
		if journal.Hop != 0 {
			return errors.New("backup proof journal must start at the first hop")
		}
	case "hop-healthy":
		if journal.Hop == 0 {
			return errors.New("healthy hop journal has no completed hop")
		}
	case "write-intent", "schema-applied":
		if journal.Hop == length {
			return errors.New("unfinished hop journal lies beyond the authenticated route")
		}
	default:
		return errors.New("host journal is not ready to apply an authenticated route")
	}
	return nil
}

func validateAutomaticPending(state upgrade.State, journal *automaticJournal, plan upgradecompat.Plan, index int) error {
	steps := plan.Steps()
	if index < 0 || index >= len(steps) {
		return errors.New("pending hop lies outside the authenticated route")
	}
	step := steps[index]
	sourceDigest, sourceErr := step.SourceMigrations.Digest()
	targetDigest, targetErr := step.TargetMigrations.Digest()
	p := state.Pending
	if sourceErr != nil || targetErr != nil || state.InstanceID != journal.Instance || p == nil || p.Kind != upgrade.UpgradeOperation || p.Invalidated || p.RouteDigest != journal.Route || p.RouteSource != journal.Source.Identity || p.Source != step.Source || p.Target != step.Target || p.Hop != int64(index) || p.RouteLength != int64(len(steps)) || p.SourceMigrationDigest != sourceDigest || p.TargetMigrationDigest != targetDigest || p.SourceSchemaDigest != step.SourceSchemaSHA256 || p.TargetSchemaDigest != step.TargetSchemaSHA256 {
		return errors.New("database pending operation differs from the exact authenticated host route")
	}
	return nil
}

func validateAutomaticHealthy(state upgrade.State, journal *automaticJournal, plan upgradecompat.Plan, index int) error {
	if err := validateAutomaticPending(state, journal, plan, index); err != nil {
		return err
	}
	step := plan.Steps()[index]
	digest, _ := step.TargetMigrations.Digest()
	final := index == len(plan.Steps())-1
	if state.Pending.Phase != upgrade.Healthy || state.Applied != (releaseidentity.Source{Release: step.Target}) || state.MigrationDigest != digest || state.SchemaDigest != step.TargetSchemaSHA256 || state.Maintenance == final {
		return errors.New("candidate health did not establish the exact applied release and maintenance state")
	}
	return nil
}

func automaticMigrationDisposition(ctx context.Context, database automaticInspection, journal *automaticJournal, plan upgradecompat.Plan, index int) (bool, error) {
	state, err := database.Control(ctx)
	if err != nil && !errors.Is(err, upgrade.ErrAbsent) {
		return false, errors.New("interrupted migration has uncertain database state; operator recovery is required")
	}
	if err == nil && validateAutomaticPending(state, journal, plan, index) == nil {
		switch state.Pending.Phase {
		case upgrade.SchemaApplied, upgrade.Healthy:
			if journal.Phase != "write-intent" && journal.Phase != "schema-applied" {
				return false, errors.New("database advanced beyond the recorded host migration intent")
			}
			return false, nil
		case upgrade.Prepared:
			if journal.Phase != "write-intent" {
				return false, errors.New("database admission differs from recorded host migration intent")
			}
		default:
			return false, errors.New("migration outcome requires operator recovery; the old binary will remain stopped")
		}
	} else {
		if journal.Phase == "schema-applied" {
			return false, errors.New("schema-applied host intent lacks its exact database operation")
		}
		if errors.Is(err, upgrade.ErrAbsent) {
			if index != 0 || journal.Source.Identity.Genesis != releaseidentity.LegacyGenesisV1 {
				return false, errors.New("runtime ledger disappeared after upgrade admission")
			}
		} else if state.Pending == nil || state.Pending.Invalidated || state.Pending.Phase != upgrade.Healthy {
			return false, errors.New("database contains a different unfinished operation; operator recovery is required")
		} else if index > 0 {
			if err := validateAutomaticHealthy(state, journal, plan, index-1); err != nil {
				return false, err
			}
		}
	}
	step := plan.Steps()[index]
	actual, err := database.Installed(ctx, step.SourceMigrations)
	digest, digestErr := step.SourceMigrations.Digest()
	if err != nil || digestErr != nil || actual.InstanceID != journal.Instance || actual.Source != step.Source || actual.MigrationDigest != digest || actual.SchemaDigest != step.SourceSchemaSHA256 {
		return false, errors.New("pre-write retry requires the exact unchanged inspected source")
	}
	return true, nil
}

// RequireRestore uses the existing exact-state CAS transition. Applied identity
// is never reversed when the runtime already completed its own health checks.
func (s automaticStore) RequireRestore(ctx context.Context, expected upgrade.State) error {
	return upgrade.WithLock(ctx, s.config, func(session *upgrade.Session) error {
		_, err := session.Advance(ctx, expected, upgrade.RestoreRequired)
		return err
	})
}

func recordAutomaticHealthFailure(host automaticApplyHost, database automaticInspection, plan upgradecompat.Plan, journal *automaticJournal, journalPath string, index int, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	stopErr := host.FenceAndStop(ctx)
	journal.Phase, journal.Hop = "restore-required", index
	journalErr := writeAutomaticJournal(journalPath, journal)
	state, stateErr := database.Control(ctx)
	if stateErr == nil {
		stateErr = validateAutomaticPending(state, journal, plan, index)
	}
	if stateErr == nil {
		switch state.Pending.Phase {
		case upgrade.Healthy:
			// External service/readiness proof can fail after runtime health. The
			// terminal host fence blocks retries without falsifying DB history.
			stateErr = validateAutomaticHealthy(state, journal, plan, index)
		case upgrade.RestoreRequired:
			// The candidate's runtime gate already recorded its own refusal.
		case upgrade.SchemaApplied:
			recovery, ok := database.(interface {
				RequireRestore(context.Context, upgrade.State) error
			})
			if !ok {
				stateErr = errors.New("database inspection cannot record exact restore-required state")
			} else {
				stateErr = recovery.RequireRestore(ctx, state)
			}
		default:
			stateErr = errors.New("failed candidate has uncertain runtime phase; recovery fence retained")
		}
	}
	return errors.Join(errors.New("candidate health failed; explicit operator recovery is required"), cause, stopErr, journalErr, stateErr)
}
