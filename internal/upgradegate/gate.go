// Package upgradegate composes release verification, durable migration state,
// public backup evidence and bounded candidate health before runtime admission.
// It never imports the runtime datastore or application wiring.
package upgradegate

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/releasetrust"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
)

type Mode string

const (
	Boot    Mode = "boot"
	Migrate Mode = "migrate"
)

var (
	ErrRestoreRequired = errors.New("upgrade requires an explicit verified restore")
	ErrNextBinary      = errors.New("upgrade route requires its next exact binary; maintenance retained")
)

type Request struct {
	afterBoundary func(durableBoundary)
	// CheckConfiguration performs only bounded local configuration validation.
	// It must start no listeners, workers or provider requests and receives no
	// datastore capability. Application wiring supplies its candidate checks.
	CheckConfiguration ConfigurationCheck
	AllowMigrations    bool
	Store              upgrade.Config
	BundleDirectory    string
	Pinned             releasetrust.PinnedTrust
	Migrations         embed.FS
	MigrationDirectory string
	Mode               Mode
	// Target optionally names the final route target. This process may execute
	// only the hop whose authenticated target matches its embedded build.
	Target                   releaseidentity.Identity
	Operator                 backupreceipt.PinnedOperator
	Ciphertext               *backupreceipt.Ciphertext
	Evidence                 backupreceipt.EvidenceMaterial
	LegacyWritersStopped     bool
	RootKey                  []byte
	StateDirectory           string
	InitialOperatorPublicKey []byte
}

// Result is operational evidence, not permission to open a datastore. F3's
// opaque admission is minted under the same Session after successful health.
type Result struct {
	Admission  upgrade.Admission
	State      upgrade.State
	SchemaOnly bool
}

// Run binds production execution to source-owned immutable build claims.
func Run(ctx context.Context, request Request) (Result, error) {
	if request.StateDirectory == "" || len(request.InitialOperatorPublicKey) == 0 {
		return Result{}, errors.New("production requires durable installation state and operator public key")
	}
	raw, _, err := buildcompat.Current()
	if err != nil {
		return Result{}, err
	}
	return run(ctx, request, raw, upgrade.Production, buildcompat.Verify)
}

// RunDevelopment uses the same gate with a distinct durable trust domain.
// A development root or signed bundle cannot adopt a production datastore.
func RunDevelopment(ctx context.Context, request Request) (Result, error) {
	raw, _, err := buildcompat.Development()
	if err != nil {
		return Result{}, err
	}
	return run(ctx, request, raw, upgrade.LocalDevelopment, buildcompat.VerifyDevelopment)
}

func run(ctx context.Context, request Request, build []byte, domain upgrade.TrustDomain, verifyBuild func(upgradecompat.VerifiedNode) error) (Result, error) {
	if request.Mode != Boot && request.Mode != Migrate {
		return Result{}, errors.New("unknown upgrade gate operation")
	}
	if request.Store.Engine.Validate() != nil || domain.Validate() != nil {
		return Result{}, errors.New("invalid upgrade datastore or trust domain")
	}
	if request.Mode == Boot && len(request.RootKey) != crypto.KeySize {
		return Result{}, crypto.ErrRootKeyFormat
	}
	root := bytes.Clone(request.RootKey)
	defer crypto.Zero(root)
	control, err := upgrade.InspectControl(ctx, request.Store)
	absent := errors.Is(err, upgrade.ErrAbsent)
	if err != nil && !absent {
		return Result{}, err
	}
	rootDigest := releaseidentity.Hash(request.Pinned.Root)
	floor := releaseidentity.SnapshotFloor{}
	if !absent {
		if control.TrustDomain != domain || control.ReleaseRootDigest != rootDigest {
			return Result{}, errors.New("installation trust domain or release root differs")
		}
		floor = control.Floor
	}
	bundle, err := upgradebundle.Load(ctx, request.BundleDirectory, request.Pinned, floor)
	if err != nil {
		return Result{}, err
	}
	node, err := bundle.MatchBuild(build)
	if err != nil {
		return Result{}, err
	}
	if err := verifyBuild(node); err != nil {
		return Result{}, err
	}
	manifest, err := releaseidentity.BuildMigrationManifest(request.Migrations, request.MigrationDirectory, request.Store.Engine)
	if err != nil {
		return Result{}, err
	}
	embeddedDigest, _ := manifest.Digest()
	declared, err := node.Manifest(request.Store.Engine)
	if err != nil {
		return Result{}, err
	}
	declaredDigest, _ := declared.Digest()
	if embeddedDigest != declaredDigest {
		return Result{}, errors.New("embedded migration bytes differ from verified build")
	}
	// Authenticity and exact embedded SQL are established before WithLock can
	// create a missing SQLite file or perform any control/migration write.
	var custody *operatorFile
	if request.StateDirectory != "" {
		custody, err = openOperatorFile(ctx, request.StateDirectory, request.InitialOperatorPublicKey, absent)
		if err != nil {
			return Result{}, err
		}
		defer custody.close()
		if custody.value.Journal != nil {
			return Result{}, errors.New("operator rotation incomplete; resume the exact local operator command")
		}
	}
	var result Result
	err = upgrade.WithLock(ctx, request.Store, func(session *upgrade.Session) error {
		current, readErr := session.Read(ctx)
		if absent {
			if request.Mode == Boot && !request.AllowMigrations {
				return errors.New("migration required; auto-migrate disabled")
			}
			if readErr == nil {
				return upgrade.ErrConflict
			}
			// Bootstrap repeats complete genesis inspection in its transaction.
			observed, source, err := inspectSource(ctx, request.Store, bundle)
			if err != nil {
				return err
			}
			plan, err := bundle.Plan(source, desiredTarget(request, node))
			if err != nil {
				return err
			}
			if len(plan.Steps()) == 0 || plan.Steps()[0].Target != node.Identity() {
				return ErrNextBinary
			}
			if custody != nil && custody.value.InstanceID != "" {
				if observed.InstanceID != custody.value.InstanceID || observed.RestoreEpoch < custody.value.EpochFloor {
					return errors.New("source predates or differs from durable operator installation")
				}
			}
			if custody != nil && observed.InstanceID != "" {
				request.Operator, err = custody.value.pin(observed.InstanceID)
				if err != nil {
					return err
				}
			}
			acceptance, incarnation, backup, err := verifyNewEvidence(ctx, request, observed, plan, bundle.Snapshot().Floor(), rootDigest)
			if err != nil {
				return err
			}
			operation := operationFor(plan, 0, 1, incarnation, backup, acceptance)
			current, err = session.Bootstrap(ctx, source.Migrations, operation, domain)
			if err != nil {
				return err
			}
			if custody != nil {
				custody.value.InstanceID = current.InstanceID
				custody.value.EpochFloor = current.RestoreEpoch
				if err := custody.save(); err != nil {
					return err
				}
			}
			return execute(ctx, session, request, node, plan, current, root, &result)
		}
		if readErr != nil {
			return readErr
		}
		if custody != nil {
			if custody.value.InstanceID == "" {
				if current.Generation != 1 || current.Pending.Phase != upgrade.Prepared {
					return errors.New("unbound operator installation pin cannot adopt an existing database")
				}
				custody.value.InstanceID = current.InstanceID
				custody.value.EpochFloor = current.RestoreEpoch
				if err := custody.save(); err != nil {
					return err
				}
			}
			if err := custody.value.check(current); err != nil {
				return err
			}
			request.Operator, err = custody.value.pin(current.InstanceID)
			if err != nil {
				return err
			}
		}
		if current.TrustDomain != domain || current.ReleaseRootDigest != rootDigest || current.Floor != control.Floor {
			return upgrade.ErrConflict
		}
		if current.Pending == nil {
			return ErrRestoreRequired
		}
		if current.Pending.Invalidated {
			observed, source, err := inspectSource(ctx, request.Store, bundle)
			if err != nil {
				return err
			}
			plan, err := bundle.Plan(source, desiredTarget(request, node))
			if err != nil {
				return err
			}
			acceptance, incarnation, backup, err := verifyNewEvidence(ctx, request, observed, plan, bundle.Snapshot().Floor(), rootDigest)
			if err != nil {
				return err
			}
			if len(plan.Steps()) == 0 {
				if current.Applied != (releaseidentity.Source{Release: node.Identity()}) {
					return upgrade.ErrConflict
				}
				operation := recoveryOperation(plan, current, incarnation, backup, acceptance)
				current, err = session.PrepareRecovery(ctx, current, operation)
				if err != nil {
					return err
				}
				return executeRecovery(ctx, session, request, node, current, root, &result)
			}
			if plan.Steps()[0].Target != node.Identity() {
				return ErrNextBinary
			}
			current, err = session.PrepareAfterRestore(ctx, current, operationFor(plan, 0, current.Generation+1, incarnation, backup, acceptance))
			if err != nil {
				return err
			}
			return execute(ctx, session, request, node, plan, current, root, &result)
		}
		if current.Pending.Phase == upgrade.RestoreRequired {
			return ErrRestoreRequired
		}
		if current.Pending.Kind == upgrade.RecoveryOperation && current.Pending.Phase != upgrade.Healthy {
			source, err := routeSource(bundle, current.Pending.RouteSource, request.Store.Engine)
			if err != nil {
				return err
			}
			plan, err := bundle.Plan(source, desiredTarget(request, node))
			if err != nil {
				return err
			}
			if len(plan.Steps()) != 0 || plan.Digest() != current.Pending.RouteDigest {
				return upgrade.ErrConflict
			}
			return executeRecovery(ctx, session, request, node, current, root, &result)
		}
		if current.Applied == (releaseidentity.Source{Release: node.Identity()}) && !current.Maintenance {
			if err := verifyCatalog(ctx, session, node, request.Store.Engine); err != nil {
				return err
			}
			if err := checkRestartHealth(ctx, session, current, root, request.Mode, request.CheckConfiguration); err != nil {
				return err
			}
			current, err = session.RefreshTrust(ctx, current, bundle.Snapshot().Floor(), rootDigest)
			if err != nil {
				return err
			}
			result = Result{State: current, SchemaOnly: request.Mode == Migrate}
			if request.Mode == Boot {
				result.Admission, err = session.Admit(ctx, current, node)
			}
			return err
		}
		if current.Pending.Phase == upgrade.Healthy && !current.Maintenance {
			if request.Mode == Boot && !request.AllowMigrations {
				return errors.New("migration required; auto-migrate disabled")
			}
			observed, source, err := inspectSource(ctx, request.Store, bundle)
			if err != nil {
				return err
			}
			plan, err := bundle.Plan(source, desiredTarget(request, node))
			if err != nil {
				return err
			}
			if len(plan.Steps()) == 0 || plan.Steps()[0].Target != node.Identity() {
				return ErrNextBinary
			}
			acceptance, incarnation, backup, err := verifyNewEvidence(ctx, request, observed, plan, bundle.Snapshot().Floor(), rootDigest)
			if err != nil {
				return err
			}
			current, err = session.Prepare(ctx, current, operationFor(plan, 0, current.Generation+1, incarnation, backup, acceptance))
			if err != nil {
				return err
			}
			return execute(ctx, session, request, node, plan, current, root, &result)
		}
		// Resume derives the original complete route from authenticated source
		// declarations; current post-write schema is checked against this hop.
		source, err := routeSource(bundle, current.Pending.RouteSource, request.Store.Engine)
		if err != nil {
			return err
		}
		plan, err := bundle.Plan(source, desiredTarget(request, node))
		if err != nil {
			return err
		}
		if plan.Digest() != current.Pending.RouteDigest || int64(len(plan.Steps())) != current.Pending.RouteLength {
			return upgrade.ErrConflict
		}
		if current.Pending.Phase == upgrade.Healthy {
			hop := current.Pending.Hop + 1
			if hop >= int64(len(plan.Steps())) || plan.Steps()[hop].Target != node.Identity() {
				return ErrNextBinary
			}
			current, err = session.Prepare(ctx, current, operationFor(plan, hop, current.Generation, current.RecoveryIncarnation, current.Pending.BackupID, current.Pending.Acceptance))
			if err != nil {
				return err
			}
		}
		return execute(ctx, session, request, node, plan, current, root, &result)
	})
	if err != nil {
		return Result{}, err
	}
	return result, nil
}

func desiredTarget(request Request, node upgradecompat.VerifiedNode) releaseidentity.Identity {
	if request.Target == (releaseidentity.Identity{}) {
		return node.Identity()
	}
	return request.Target
}

func inspectSource(ctx context.Context, cfg upgrade.Config, bundle upgradebundle.Bundle) (upgrade.InstalledSource, upgradecompat.InstalledSource, error) {
	var observed upgrade.InstalledSource
	var matched upgradecompat.InstalledSource
	found := false
	for _, candidate := range bundle.Sources(cfg.Engine) {
		actual, err := upgrade.InspectInstalled(ctx, cfg, candidate.Migrations)
		if err != nil {
			continue
		}
		if actual.Source != candidate.Identity || actual.SchemaDigest != candidate.SchemaSHA256 {
			continue
		}
		if found && matched.Identity != candidate.Identity {
			return observed, matched, errors.New("ambiguous authenticated source")
		}
		observed, matched, found = actual, candidate, true
	}
	if !found {
		return observed, matched, errors.New("datastore does not match any authenticated source")
	}
	return observed, matched, nil
}

func routeSource(bundle upgradebundle.Bundle, identity releaseidentity.Source, engine releaseidentity.Engine) (upgradecompat.InstalledSource, error) {
	for _, candidate := range bundle.Sources(engine) {
		if candidate.Identity == identity {
			return candidate, nil
		}
	}
	return upgradecompat.InstalledSource{}, errors.New("original route source absent from authenticated bundle")
}

func operationFor(plan upgradecompat.Plan, hop, generation int64, incarnation upgrade.Incarnation, backup string, acceptance upgrade.Acceptance) upgrade.Operation {
	step := plan.Steps()[hop]
	sourceDigest, _ := step.SourceMigrations.Digest()
	targetDigest, _ := step.TargetMigrations.Digest()
	return upgrade.Operation{Kind: upgrade.UpgradeOperation, Source: step.Source, RouteSource: plan.Source(), Target: step.Target, SourceSchemaDigest: step.SourceSchemaSHA256, TargetSchemaDigest: step.TargetSchemaSHA256, SourceMigrationDigest: sourceDigest, TargetMigrationDigest: targetDigest, RouteDigest: plan.Digest(), Hop: hop, RouteLength: int64(len(plan.Steps())), Generation: generation, RecoveryIncarnation: incarnation, BackupID: backup, Acceptance: acceptance, Phase: upgrade.Prepared}
}

func execute(ctx context.Context, session *upgrade.Session, request Request, node upgradecompat.VerifiedNode, plan upgradecompat.Plan, state upgrade.State, root []byte, result *Result) error {
	pending := state.Pending
	if pending == nil || pending.Invalidated || pending.Hop >= int64(len(plan.Steps())) || pending.Target != node.Identity() || pending.RouteDigest != plan.Digest() {
		return upgrade.ErrConflict
	}
	step := plan.Steps()[pending.Hop]
	if pending.Source != step.Source || pending.TargetSchemaDigest != step.TargetSchemaSHA256 {
		return upgrade.ErrConflict
	}
	var err error
	if (pending.Phase == upgrade.Prepared || pending.Phase == upgrade.SchemaWriteStarted) && request.Mode == Boot && !request.AllowMigrations {
		return errors.New("migration required; auto-migrate disabled")
	}
	if pending.Phase == upgrade.Prepared {
		request.observe(boundaryPrepared)
		catalog, err := session.DomainCatalog(ctx)
		if err != nil {
			return err
		}
		if catalog.Digest() != pending.SourceSchemaDigest {
			return upgrade.ErrConflict
		}
		state, err = session.Advance(ctx, state, upgrade.SchemaWriteStarted)
		if err != nil {
			return err
		}
		request.observe(boundaryWriteStarted)
	}
	if state.Pending.Phase == upgrade.SchemaWriteStarted {
		if err := session.ApplyMigrations(ctx, state, request.Migrations, request.MigrationDirectory); err != nil {
			return fmt.Errorf("schema write did not complete; maintenance retained: %w", err)
		}
		request.observe(boundarySQLComplete)
		if err := verifyCatalog(ctx, session, node, request.Store.Engine); err != nil {
			return err
		}
		state, err = session.Advance(ctx, state, upgrade.SchemaApplied)
		if err != nil {
			return err
		}
		request.observe(boundarySchemaApplied)
	}
	if state.Pending.Phase != upgrade.SchemaApplied {
		return upgrade.ErrConflict
	}
	if err := verifyCatalog(ctx, session, node, request.Store.Engine); err != nil {
		return err
	}
	if state.Pending.Source.Genesis == releaseidentity.FreshGenesisV1 {
		if err := session.ReconcileFreshInstance(ctx, state); err != nil {
			return err
		}
	}
	if request.Mode == Migrate {
		*result = Result{State: state, SchemaOnly: true}
		return nil
	}
	if err := candidateHealth(ctx, session, state, root, request.CheckConfiguration); err != nil {
		_, markErr := session.Advance(ctx, state, upgrade.RestoreRequired)
		if markErr == nil {
			request.observe(boundaryHealthFailed)
		}
		return errors.Join(fmt.Errorf("candidate health refused: %w", err), markErr)
	}
	state, err = session.Advance(ctx, state, upgrade.Healthy)
	if err != nil {
		return err
	}
	request.observe(boundaryHealthy)
	if state.Maintenance {
		return ErrNextBinary
	}
	*result = Result{State: state}
	result.Admission, err = session.Admit(ctx, state, node)
	return err
}

func verifyCatalog(ctx context.Context, session *upgrade.Session, node upgradecompat.VerifiedNode, engine releaseidentity.Engine) error {
	catalog, err := session.DomainCatalog(ctx)
	if err != nil {
		return err
	}
	expected, err := node.SchemaDigest(engine)
	if err != nil {
		return err
	}
	if catalog.Digest() != expected {
		return errors.New("candidate schema differs from signed target")
	}
	return nil
}

func candidateHealth(ctx context.Context, session *upgrade.Session, state upgrade.State, root []byte, check ConfigurationCheck) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	keys, err := session.CandidateKeys(ctx, state)
	if err != nil {
		return err
	}
	if state.Pending.Source.Genesis == releaseidentity.FreshGenesisV1 {
		masters, err := keys.ActiveMasterWrappers(ctx)
		if err != nil {
			return err
		}
		tier3, err := keys.AllOpenableTier3(ctx)
		if err != nil {
			return err
		}
		if len(masters) == 0 && len(tier3) == 0 {
			if err := session.InitializeFreshHierarchy(ctx, state, bytes.Clone(root)); err != nil {
				return err
			}
		}
	}
	if err := crypto.VerifyExistingHierarchy(ctx, keys, bytes.Clone(root)); err != nil {
		return err
	}
	return checkExistingConfiguration(ctx, keys, root, check)
}

func checkRestartHealth(ctx context.Context, session *upgrade.Session, state upgrade.State, root []byte, mode Mode, check ConfigurationCheck) error {
	if mode == Migrate {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	keys, err := session.HealthyKeys(ctx, state)
	if err != nil {
		return err
	}
	if err := crypto.VerifyExistingHierarchy(ctx, keys, bytes.Clone(root)); err != nil {
		return err
	}
	return checkExistingConfiguration(ctx, keys, root, check)
}
