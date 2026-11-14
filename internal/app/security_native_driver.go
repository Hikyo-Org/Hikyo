package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/hostupgrade"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
	"github.com/Hikyo-Org/hikyo/internal/selfupdate"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradebundle"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"github.com/gofrs/flock"
)

type nativeSecurityHost interface {
	Config() hostupgrade.Config
	Environment() map[string]string
	CheckRunning(context.Context, string) error
}

// nativeSourceEvidenceCommand seeds the release archive of the installed version.
// The route is empty, so it verifies and caches without touching the service.
func nativeSourceEvidenceCommand(identity releaseidentity.Identity) string {
	raw, err := json.Marshal(identity, jsonwire.Stable)
	if err != nil {
		return "sudo hikyo upgrade --release-identity JSON"
	}
	// The identity comes from local state, so quote it for any shell it is pasted into.
	return "sudo hikyo upgrade --release-identity '" + strings.ReplaceAll(string(raw), "'", `'"'"'`) + "'"
}

// checkNativeSecurityCustody refuses before enrollment what the coordinator could
// otherwise discover only after stopping writers: its root-wrapped vault key must
// be the operator key this installation has pinned.
func checkNativeSecurityCustody(c hostupgrade.Config, environment map[string]string) error {
	const refusal = "native security maintenance signs recovery proof with its local root-wrapped vault, but "
	const remedy = "; keep the manual upgrade procedure, or see Operator-key rotation in docs/operations/manual-upgrades.md"
	if _, err := os.Lstat(filepath.Join(c.CustodyDirectory, "operator.age")); errors.Is(err, os.ErrNotExist) {
		return errors.New(refusal + "this installation's operator key is held outside it and no vault exists in " + c.CustodyDirectory + remedy)
	} else if err != nil {
		return err
	}
	// The coordinator publishes this file whenever it creates or opens the vault.
	published := filepath.Join(c.PublicDirectory, "operator.pub")
	vault, err := os.ReadFile(published)
	if err != nil {
		return err
	}
	configured, err := nativeConfiguredOperatorPin(c, environment)
	if err != nil {
		return err
	}
	if configured == published {
		return nil
	}
	pinned, err := os.ReadFile(configured)
	if err != nil {
		return err
	}
	if !bytes.Equal(pinned, vault) {
		return errors.New(refusal + "the operator key pinned by " + configured + " differs from the vault key " + published + remedy)
	}
	return nil
}

// nativeConfiguredOperatorPin is the pin the service boots with: the later runtime
// environment a coordinator writes wins over the operator's own file.
func nativeConfiguredOperatorPin(c hostupgrade.Config, environment map[string]string) (string, error) {
	configured := environment["HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY"]
	raw, err := os.ReadFile(filepath.Join(c.StateDirectory, "runtime.env"))
	if errors.Is(err, os.ErrNotExist) {
		return configured, nil
	}
	if err != nil {
		return "", err
	}
	values, err := hostupgrade.ParseEnvironmentFile(raw)
	if err != nil {
		return "", err
	}
	if value, present := values["HIKYO_UPGRADE_OPERATOR_PUBLIC_KEY"]; present {
		configured = value
	}
	return configured, nil
}

// nativeDatabase resolves the service's SQLite file from its environment.
func nativeDatabase(c hostupgrade.Config, environment map[string]string) (upgrade.Config, error) {
	cfg, _, err := config.Load("upgrade", nil, func(key string) string { return environment[key] }, nil)
	if err != nil {
		return upgrade.Config{}, err
	}
	if cfg.Store.Engine != config.EngineSQLite {
		return upgrade.Config{}, errors.New("native security maintenance requires the local SQLite deployment contract")
	}
	path := cfg.Store.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(c.WorkingDirectory, path)
	}
	return upgrade.Config{Engine: releaseidentity.SQLite, Path: path}, nil
}

type nativeSecurityInstallation struct {
	Host         nativeSecurityHost
	Database     upgrade.Config
	Inspection   automaticInspection
	SourceBundle string
	SourceCache  string
}

type nativeSecurityObservation struct {
	Observation  securitymaintenance.Observation
	Installation nativeSecurityInstallation
	State        upgrade.State
	BinarySHA256 releaseidentity.Digest
}

type nativeSecurityPreparation struct {
	Proof       securitymaintenance.Preparation
	Route       automaticRoute
	SourceGuard nativeSecuritySourceGuard
}

type nativeSecurityDriver struct {
	configuration  string
	out            io.Writer
	load           func(context.Context) (nativeSecurityInstallation, error)
	prepareRelease func(context.Context, releaseidentity.Identity, string) (preparedSecurityRelease, error)
	verifySource   func(context.Context, nativeSecurityInstallation, upgrade.State) (preparedSecurityRelease, error)
	runUpgrade     func(context.Context, releaseidentity.Identity, string, nativeSecurityAuthorization, io.Writer) error
	executeHandoff func(context.Context, *AutomaticHandoff, io.Writer) error
	now            func() time.Time
	prepared       *nativeSecurityPreparation
	// timeout bounds one rollback from the coordinator's side; the engine's deadline decides when to try.
	timeout time.Duration
}

func newNativeSecurityDriver(configuration string, out io.Writer) (securitymaintenance.Driver, error) {
	if configuration == "" {
		configuration = hostupgrade.ConfigPath
	}
	// Creates the standard host configuration once, like `hikyo upgrade`.
	c, err := hostupgrade.InitializeConfig(configuration)
	if err != nil {
		return nil, err
	}
	if _, err := hostupgrade.New(c); err != nil {
		return nil, err
	}
	if out == nil {
		out = io.Discard
	}
	driver := &nativeSecurityDriver{configuration: configuration, out: out,
		prepareRelease: prepareSecurityRelease, verifySource: verifyNativeSecuritySource, runUpgrade: runNativeSecurityUpgrade, executeHandoff: executeNativeSecurityHandoff, now: time.Now, timeout: defaultNativeRollbackTimeout}
	driver.load = func(ctx context.Context) (nativeSecurityInstallation, error) {
		current, err := hostupgrade.LoadConfig(configuration)
		if err != nil {
			return nativeSecurityInstallation{}, err
		}
		if current != c {
			return nativeSecurityInstallation{}, errors.New("native deployment configuration changed; restart the security coordinator")
		}
		host, err := hostupgrade.New(current)
		if err != nil {
			return nativeSecurityInstallation{}, err
		}
		if err := preflightNativeHost(ctx, host, current.StateDirectory); err != nil {
			return nativeSecurityInstallation{}, err
		}
		if err := host.PrepareCacheDirectory(filepath.Join(current.StateDirectory, "downloads")); err != nil {
			return nativeSecurityInstallation{}, err
		}
		bundle, err := host.LocalBundleDirectory()
		if err != nil {
			return nativeSecurityInstallation{}, fmt.Errorf("%w: set HIKYO_UPGRADE_BUNDLE in %s to the signed bundle under public_directory in %s", err, current.EnvironmentFile, configuration)
		}
		database, err := nativeDatabase(current, host.Environment())
		if err != nil {
			return nativeSecurityInstallation{}, err
		}
		return nativeSecurityInstallation{Host: host, Database: database, Inspection: automaticStore{database}, SourceBundle: bundle, SourceCache: filepath.Join(current.StateDirectory, "downloads")}, nil
	}
	return driver, nil
}

// nativePreflighter is the part of the host the preflight choice needs.
type nativePreflighter interface {
	Preflight(context.Context) error
	PreflightNewInstallation(context.Context) error
}

// preflightNativeHost is Preflight, except that a host journal at "rolling-back" lets the SQLite file be
// absent: a rollback killed after it removed the database resumes from that journal, and every later step
// (observe, the gate, the stop, the restore) would otherwise stop at the missing file before reaching the
// restore that creates it. A journal that is missing, malformed or unreadable allows nothing.
func preflightNativeHost(ctx context.Context, host nativePreflighter, stateDirectory string) error {
	if journal, err := readAutomaticJournal(filepath.Join(stateDirectory, "operation.json")); err == nil && journal != nil && journal.Phase == "rolling-back" {
		return host.PreflightNewInstallation(ctx)
	}
	return host.Preflight(ctx)
}

func (d *nativeSecurityDriver) Observe(ctx context.Context) (securitymaintenance.Observation, error) {
	observed, err := d.observe(ctx)
	return observed.Observation, err
}

func (d *nativeSecurityDriver) observe(ctx context.Context) (nativeSecurityObservation, error) {
	var result nativeSecurityObservation
	installation, err := d.load(ctx)
	if err != nil {
		return result, err
	}
	state, err := installation.Inspection.Control(ctx)
	if err != nil {
		return result, err
	}
	observed, err := securityControlObservation(state)
	if err != nil {
		return result, err
	}
	observed.Engine = installation.Database.Engine
	// Authenticate local source evidence independently of the checker build's
	// application version and migrations. No installed-version request is sent.
	source, err := d.verifySource(ctx, installation, state)
	if err != nil {
		return result, err
	}
	if source.Target.Identity != observed.Identity || source.Target.BinarySHA256.Validate() != nil ||
		state.ReleaseRootDigest != releaseidentity.Hash(source.Pinned.Root) {
		return result, errors.New("native source identity or installed release trust differs from independently verified evidence")
	}
	bundle, err := upgradebundle.Load(ctx, source.Target.BundleDirectory, source.Pinned, state.Floor)
	if err != nil {
		return result, err
	}
	if _, err := bundle.Release(observed.Identity); err != nil {
		return result, err
	}
	var claim upgradecompat.InstalledSource
	for _, candidate := range bundle.Sources(installation.Database.Engine) {
		if candidate.Identity.IsRelease() && candidate.Identity.Release == observed.Identity {
			if claim.Identity.IsRelease() {
				return result, errors.New("native source has ambiguous authenticated schema evidence")
			}
			claim = candidate
		}
	}
	if !claim.Identity.IsRelease() {
		return result, errors.New("installed native source lacks an authenticated engine schema")
	}
	actual, err := installation.Inspection.Installed(ctx, claim.Migrations)
	if err != nil {
		return result, err
	}
	digest, err := claim.Migrations.Digest()
	if err != nil || actual.Source != state.Applied || actual.SchemaDigest != claim.SchemaSHA256 || actual.MigrationDigest != digest ||
		actual.InstanceID != state.InstanceID || actual.RestoreEpoch != state.RestoreEpoch || actual.Ledger == nil || !reflect.DeepEqual(*actual.Ledger, state) {
		return result, errors.New("actual native source schema, migration inventory or recovery incarnation differs from verified release state")
	}
	if observed.Ready {
		if err := installation.Host.CheckRunning(ctx, string(source.Target.BinarySHA256)); err != nil {
			return result, fmt.Errorf("prove running native source: %w", err)
		}
	}
	current, err := installation.Inspection.Control(ctx)
	if err != nil || !reflect.DeepEqual(current, state) {
		return result, errors.Join(err, errors.New("native source changed during identity and readiness observation"))
	}
	return nativeSecurityObservation{Observation: observed, Installation: installation, State: state, BinarySHA256: source.Target.BinarySHA256}, nil
}

func verifyNativeSecuritySource(ctx context.Context, installation nativeSecurityInstallation, state upgrade.State) (preparedSecurityRelease, error) {
	var result preparedSecurityRelease
	if !state.Applied.IsRelease() || installation.SourceBundle == "" || installation.SourceCache == "" {
		if state.Applied.IsRelease() {
			return result, errors.New("native source proof unavailable: configure the installed release's local signed bundle, then run " + nativeSourceEvidenceCommand(state.Applied.Release))
		}
		return result, errors.New("native source proof unavailable: the database has no installed signed release; complete a signed upgrade first")
	}
	// Before the archive check: no seed can make this installation eligible.
	if err := checkNativeSecurityCustody(installation.Host.Config(), installation.Host.Environment()); err != nil {
		return result, err
	}
	pinned, err := buildcompat.ProductionTrust()
	if err != nil {
		return result, err
	}
	if state.TrustDomain != upgrade.Production || state.ReleaseRootDigest != releaseidentity.Hash(pinned.Root) {
		return result, errors.New("native installed release belongs to another production trust root")
	}
	installer, err := selfupdate.NewInstaller(selfupdate.Config{StateDir: installation.SourceCache,
		TrustRootBase64: base64.StdEncoding.EncodeToString(pinned.Root), RecoveryKeyBase64: base64.StdEncoding.EncodeToString(pinned.RecoveryPublicKey)})
	if err != nil {
		return result, err
	}
	identity := state.Applied.Release
	directory := selfupdate.CachedReleaseDirectory(installation.SourceCache, identity)
	prepared, err := installer.VerifyCachedRelease(ctx, selfupdate.PreparedNightly{Identity: identity, Directory: directory, BundleDirectory: installation.SourceBundle}, state.Floor)
	if err != nil {
		return result, fmt.Errorf("native source proof unavailable: local signed bundle/archive verification failed; run %s: %w", nativeSourceEvidenceCommand(identity), err)
	}
	bundle, err := upgradebundle.Load(ctx, installation.SourceBundle, pinned, state.Floor)
	if err != nil {
		return result, err
	}
	release, err := bundle.Release(identity)
	if err != nil {
		return result, err
	}
	return preparedSecurityRelease{Installer: installer, Pinned: pinned, Target: prepared, Bundle: bundle, Release: release}, nil
}

// Reconcile refuses while the host upgrade journal is unfinished, so neither the
// coordinator's automatic release nor an operator can drop a pin that Prepare would
// then refuse forever under a less useful reason. The one journal it clears is a guarded
// security update's "preparing" journal while the service runs outside maintenance: the
// source is serving, so the journal and its source record describe an abandoned attempt.
// That is the coordinator dying before the stop, or a rollback that restarted the source
// and lifted the fence and died before the journal cleared. A "rolling-back" journal under
// a serving, unfenced source was put back by hand and clears the same way. Every release
// that follows a rollback reports ErrRolledBack, also when the journal is already gone.
func (d *nativeSecurityDriver) Reconcile(ctx context.Context, _ securitymaintenance.Observation) error {
	installation, err := d.load(ctx)
	if err != nil {
		return err
	}
	stateDirectory := installation.Host.Config().StateDirectory
	journalPath := filepath.Join(stateDirectory, "operation.json")
	previous, err := readAutomaticJournal(journalPath)
	if err != nil {
		return err
	}
	if previous == nil || previous.Phase == "complete" {
		record, err := readNativeSourceRecord(stateDirectory)
		if err != nil {
			return err
		}
		if record != nil && record.RolledBack {
			return securitymaintenance.ErrRolledBack
		}
		return nil
	}
	unfinished := errors.New("the native upgrade journal is unfinished; finish it with sudo hikyo upgrade first")
	if previous.Phase == "rolling-back" {
		unfinished = errors.New("the native rollback is unfinished; the next coordinator run finishes it")
	}
	host, ok := installation.Host.(nativeRollbackHost)
	// A serving source under "rolling-back" was put back by hand: the journal has nothing left to do.
	if !ok || previous.Phase != "preparing" && previous.Phase != "rolling-back" {
		return unfinished
	}
	release, err := holdNativeUpgradeLock(stateDirectory)
	if err != nil {
		return errors.Join(err, unfinished)
	}
	defer release()
	record, err := readNativeSourceRecord(stateDirectory)
	if err != nil || record == nil || record.bind(previous) != nil {
		return errors.Join(err, unfinished)
	}
	if maintenance, err := host.Maintenance(); err != nil || maintenance {
		return errors.Join(err, unfinished)
	}
	// The engine observed the source before this lock; a stop since then must keep the attempt.
	// A "rolling-back" journal was put back by hand, possibly to another release than the record's.
	if previous.Phase == "preparing" {
		if err := host.CheckRunning(ctx, string(record.BinarySHA256)); err != nil {
			return errors.Join(err, unfinished)
		}
	}
	rolledBack := previous.Aborted == rolledBackAborted || previous.Phase == "rolling-back" || record.RolledBack
	if rolledBack && !record.RolledBack {
		record.RolledBack = true
		if err := writeNativeSourceRecord(stateDirectory, *record); err != nil {
			return err
		}
	}
	if err := clearNativeAttempt(journalPath, nil, stateDirectory); err != nil {
		return err
	}
	if rolledBack {
		return securitymaintenance.ErrRolledBack
	}
	return nil
}

// holdNativeUpgradeLock excludes a target still running under a killed coordinator, which holds
// this lock from before it writes "preparing" until it exits.
func holdNativeUpgradeLock(stateDirectory string) (func(), error) {
	lock := flock.New(filepath.Join(stateDirectory, "upgrade.lock"))
	held, err := lock.TryLock()
	if err != nil || !held {
		return nil, errors.Join(err, errors.New("another upgrade process holds the host upgrade lock"))
	}
	return func() { _ = lock.Unlock() }, nil
}

func (d *nativeSecurityDriver) Prepare(ctx context.Context, target securitymaintenance.Target) (securitymaintenance.Preparation, error) {
	d.prepared = nil
	var result securitymaintenance.Preparation
	if target.Identity.Validate() != nil || target.Image != "" || target.RuntimeImageSHA256 != "" {
		return result, errors.New("native security preparation requires an exact application release without an OCI image")
	}
	before, err := d.observe(ctx)
	if err != nil {
		return result, err
	}
	if !before.Observation.Healthy() {
		return result, errors.New("native security preparation requires an admitted healthy source")
	}
	previous, err := readAutomaticJournal(filepath.Join(before.Installation.Host.Config().StateDirectory, "operation.json"))
	if err != nil {
		return result, err
	}
	if previous != nil && previous.Phase != "complete" {
		return result, errors.New("unfinished native upgrade requires exact-target reconciliation before new security staging")
	}
	// A rollback's record was kept for a coordinator that died before it recorded the rollback. No
	// operation is pinned here, so it describes nothing a later Reconcile may report.
	if err := removeNativeSourceRecord(before.Installation.Host.Config().StateDirectory); err != nil {
		return result, err
	}
	// Share the privileged host coordinator's verified cache. Staging is real
	// before the stop, without downloading a second copy during target handoff.
	prepared, err := d.prepareRelease(ctx, target.Identity, filepath.Join(before.Installation.Host.Config().StateDirectory, "downloads"))
	if err != nil {
		return result, err
	}
	if prepared.Target.Identity != target.Identity || before.State.ReleaseRootDigest != releaseidentity.Hash(prepared.Pinned.Root) {
		return result, errors.New("security target differs from independent native release authority")
	}
	route, err := stageNativeSecurityRoute(ctx, prepared, before.Installation)
	if err != nil {
		return result, err
	}
	manifest, err := route.Plan.SourceManifest(before.Installation.Database.Engine)
	if err != nil {
		return result, err
	}
	sourceDigest, err := manifest.Digest()
	if err != nil || route.Plan.Source() != before.State.Applied || route.Instance != before.State.InstanceID ||
		sourceDigest != before.State.MigrationDigest || route.Plan.SourceSchemaDigest() != before.State.SchemaDigest {
		return result, errors.New("prepared native route belongs to a different installed source")
	}
	impact, err := securityRouteImpact(route.Plan)
	if err != nil {
		return result, err
	}
	after, err := d.observe(ctx)
	if err != nil || !sameNativeSecurityObservation(before.Observation, after.Observation) {
		return result, errors.Join(err, errors.New("native source changed during security staging"))
	}
	result = securitymaintenance.Preparation{Target: target.Identity, Source: before.Observation, RouteSHA256: route.Plan.Digest(), Impact: impact}
	hostDigest, err := nativeSecurityHostFingerprint(d.configuration, before.Installation.Host.Config())
	if err != nil {
		return result, err
	}
	d.prepared = &nativeSecurityPreparation{Proof: result, Route: route, SourceGuard: nativeSecuritySourceGuard{Source: before.Observation, HostSHA256: hostDigest, BinarySHA256: before.BinarySHA256}}
	return result, nil
}

func stageNativeSecurityRoute(ctx context.Context, prepared preparedSecurityRelease, installation nativeSecurityInstallation) (automaticRoute, error) {
	if prepared.Installer == nil || prepared.Source == nil {
		return automaticRoute{}, errors.New("native security route requires independent release preparation")
	}
	route, err := discoverReleaseRoute(ctx, unattendedRouteInstaller{prepared.Installer}, prepared.Source, prepared.Target, prepared.Pinned,
		installation.Inspection, installation.Database.Engine, nil, true)
	if err != nil {
		return route, err
	}
	if !route.Plan.Valid() || route.Plan.Target() != prepared.Target.Identity || len(route.Plan.Steps()) == 0 {
		return route, errors.New("native security target has no applicable authenticated upgrade route")
	}
	return route, preflightUnattendedRoute(ctx, route)
}

func sameNativeSecurityObservation(a, b securitymaintenance.Observation) bool {
	return a.Healthy() && b.Healthy() && a.Platform == b.Platform && a.Engine == b.Engine && a.Identity == b.Identity && a.Instance == b.Instance &&
		a.Generation == b.Generation && a.RestoreEpoch == b.RestoreEpoch && a.Incarnation == b.Incarnation
}

func (d *nativeSecurityDriver) Apply(ctx context.Context, target securitymaintenance.Target) error {
	if d.prepared == nil || target.Identity != d.prepared.Proof.Target || target.Image != "" || target.RuntimeImageSHA256 != "" {
		return errors.Join(securitymaintenance.ErrNotStarted, errors.New("native security application requires the exact independently staged target"))
	}
	if target.EligibilityUntil.IsZero() {
		return errors.Join(securitymaintenance.ErrNotStarted, errors.New("native security application requires verified feed eligibility expiry"))
	}
	before, err := d.Observe(ctx)
	if err != nil || !sameNativeSecurityObservation(d.prepared.Proof.Source, before) {
		return errors.Join(securitymaintenance.ErrNotStarted, err, errors.New("native source changed before security application"))
	}
	if err := preflightUnattendedRoute(ctx, d.prepared.Route); err != nil {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	if err := authorizeAutomaticMutation(target.EligibilityUntil, d.now, target.BeforeMutation); err != nil {
		return errors.Join(securitymaintenance.ErrNotStarted, err)
	}
	err = d.runTarget(ctx, target.Identity, nativeSecurityAuthorization{Route: d.prepared.Proof.RouteSHA256, Until: target.EligibilityUntil, PolicyFile: target.PolicyFile, PolicySHA256: target.PolicySHA256, Source: &d.prepared.SourceGuard})
	if errors.Is(err, securitymaintenance.ErrSourceRestored) {
		// The child reports the rollback; the coordinator still proves the source serving.
		return d.provenSourceServing(ctx, err, d.prepared.Proof.Source)
	}
	return err
}

// runTarget runs the target's upgrade and follows its handoff to the verified executable.
func (d *nativeSecurityDriver) runTarget(ctx context.Context, identity releaseidentity.Identity, authorization nativeSecurityAuthorization) error {
	err := d.runUpgrade(ctx, identity, d.configuration, authorization, d.out)
	var handoff *AutomaticHandoff
	if errors.As(err, &handoff) {
		err = d.executeHandoff(ctx, handoff, d.out)
	}
	return err
}

// provenSourceServing keeps the reported outcome (a rollback, a restored source, or a refusal)
// only once the exact source, at the generation observed before the replacement, is healthy and
// running. Otherwise the operation stays pinned, and the text carries no classification.
func (d *nativeSecurityDriver) provenSourceServing(ctx context.Context, reported error, source securitymaintenance.Observation) error {
	observed, err := d.Observe(ctx)
	if err != nil || !sameNativeSecurityObservation(source, observed) {
		return errors.Join(fmt.Errorf("the source is not proven serving after the target reported: %v", reported), err)
	}
	return reported
}

// RestoreAborted is the coordinator's answer, on any later run, to a pinned update whose
// deadline passed: also under manual policy or an untrusted feed, because putting the
// source back is not an update. The service is proven stopped before anything is done.
// Only an error carrying ErrNotStarted releases the pin, and only after the exact source
// is observed healthy and running at the generation it had before.
func (d *nativeSecurityDriver) RestoreAborted(ctx context.Context, _ securitymaintenance.Observation, operation securitymaintenance.Operation) error {
	source := operation.Source
	timeout := d.timeout
	if timeout <= 0 {
		timeout = defaultNativeRollbackTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	installation, err := d.load(ctx)
	if err != nil {
		return err
	}
	host, ok := installation.Host.(nativeRollbackHost)
	if !ok {
		return errors.New("the native host cannot roll back an unfinished update")
	}
	stateDirectory := installation.Host.Config().StateDirectory
	journalPath := filepath.Join(stateDirectory, "operation.json")
	journal, err := readAutomaticJournal(journalPath)
	if err != nil {
		return err
	}
	record, err := readNativeSourceRecord(stateDirectory)
	if err != nil {
		return err
	}
	stopped := false
	outcome := errNativeSourceRestored
	if journal != nil && journal.Phase != "complete" {
		if record == nil || record.bind(journal) != nil {
			return errors.New("the unfinished host operation is not a guarded security update; finish it with sudo hikyo upgrade")
		}
		if !journal.describes(source) || journal.Target != operation.Target.Identity || journal.Route != operation.RouteSHA256 {
			return errors.New("the unfinished host operation belongs to another operation than the pinned one")
		}
		maintenance, err := host.Maintenance()
		if err != nil {
			return err
		}
		if journal.Phase == "preparing" {
			release, err := holdNativeUpgradeLock(stateDirectory)
			if err != nil {
				return err
			}
			defer release()
		}
		switch {
		case journal.Phase == "preparing" && !maintenance:
			// Killed after the journal and before the stop, nothing changed; or just after a rollback
			// restarted the source and before the journal cleared, which still counts as one.
			if journal.Aborted == rolledBackAborted {
				stopped, outcome = true, errNativeRolledBack
			}
		case journal.Phase == "preparing":
			// Fenced and stopped before any write, or just after a rollback that wrote the snapshot back
			// and was killed before the source started: only the journal says which.
			stopped = true
			if journal.Aborted == rolledBackAborted {
				outcome = errNativeRolledBack
			}
			if err := host.FenceAndStop(ctx); err != nil {
				return err
			}
			if restored, err := restoreNativeSource(ctx, host, installation.Inspection, journal, stateDirectory, record.BinarySHA256, nil); !restored || err != nil {
				return errors.Join(err, errors.New("the source could not be restarted"))
			}
		case maintenance:
			stopped = true
			err := d.runTarget(ctx, journal.Target, nativeSecurityAuthorization{Rollback: true})
			if !errors.Is(err, securitymaintenance.ErrSourceRestored) {
				return errors.Join(errors.New("native rollback did not finish"), err)
			}
			// The target's own report: it restored the snapshot, or found the source untouched.
			outcome = err
		case journal.Phase == "rolling-back":
			return errors.New("the rollback's service is not fenced, so it was put back by hand; once it serves, reconcile releases it")
		default:
			return errors.New("the host journal is past preparation but the service is not fenced; recover with sudo hikyo upgrade")
		}
	} else if record != nil && record.RolledBack {
		// The rollback finished and the coordinator died before it recorded that.
		stopped, outcome = true, errNativeRolledBack
	}
	if err := d.provenSourceServing(ctx, outcome, source); !errors.Is(err, securitymaintenance.ErrSourceRestored) {
		return err
	}
	// A serving source can still carry the fence, and a reboot would then not start it.
	if maintenance, err := host.Maintenance(); err != nil || maintenance {
		return errors.Join(err, errors.New("the source serves but the host maintenance fence is still set"))
	}
	// The source serves: the record and a journal that described the abandoned attempt are spent.
	if journal != nil && journal.Phase != "complete" {
		if err := clearNativeAttempt(journalPath, nil, stateDirectory); err != nil {
			return err
		}
	} else if err := settleNativeSourceRecord(stateDirectory); err != nil {
		return err
	}
	if !stopped {
		// Nothing was stopped, so this releases the pin without counting an outage.
		return errors.Join(securitymaintenance.ErrNotStarted, errors.New("the source was never stopped and is serving untouched"))
	}
	return outcome
}

// NoFinalGate reads the control row, which the stopped or running host can always open,
// and falls back to the journal: before "schema-applied" the target has written nothing, and
// "rolling-back" says the gate decision was already taken, with the database possibly absent.
func (d *nativeSecurityDriver) NoFinalGate(ctx context.Context, operation securitymaintenance.Operation) (bool, error) {
	installation, err := d.load(ctx)
	if err != nil {
		return false, err
	}
	state, err := installation.Inspection.Control(ctx)
	if err == nil {
		if state.InstanceID != operation.Source.Instance || state.RestoreEpoch != operation.Source.RestoreEpoch || fmt.Sprintf("%x", state.RecoveryIncarnation[:]) != operation.Source.Incarnation {
			return false, errors.New("the control row belongs to another installation")
		}
		past := state.Pending != nil && state.Pending.Phase == upgrade.Healthy && !state.Maintenance && state.Applied != (releaseidentity.Source{Release: operation.Source.Identity})
		return !past, nil
	}
	journal, jerr := readAutomaticJournal(filepath.Join(installation.Host.Config().StateDirectory, "operation.json"))
	if jerr == nil && journal != nil && (journal.Phase == "preparing" || journal.Phase == "proved" || journal.Phase == "write-intent" || journal.Phase == "rolling-back") {
		if !journal.describes(operation.Source) || journal.Target != operation.Target.Identity || journal.Route != operation.RouteSHA256 {
			return false, errors.Join(err, errors.New("the host journal belongs to another operation than the pinned one"))
		}
		return true, nil
	}
	return false, errors.Join(err, jerr)
}

// describes ties a journal to the installation and source release an operation pinned.
func (j *automaticJournal) describes(source securitymaintenance.Observation) bool {
	return j.Instance == source.Instance && j.Source.Identity == (releaseidentity.Source{Release: source.Identity})
}

func executeNativeSecurityHandoff(ctx context.Context, handoff *AutomaticHandoff, out io.Writer) error {
	command := exec.CommandContext(ctx, handoff.Executable, handoff.Arguments...)
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	command.Stdout, command.Stderr = out, out
	return AutomaticUpgradeHandoffError(command.Run())
}
