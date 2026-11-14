package app

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
	"github.com/Hikyo-Org/hikyo/internal/jsonwire"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/securitydeployment"
	"github.com/Hikyo-Org/hikyo/internal/securityfeed"
	"github.com/Hikyo-Org/hikyo/internal/securitymaintenance"
)

const SecurityMaintenanceConfigPath = "/etc/hikyo/security-maintenance.json"
const SecurityMaintenanceConfigFormat = "hikyo.security-maintenance-config/v1"

type SecurityFeedConfiguration struct {
	MetadataURL      string `json:"metadata_url"`
	TargetsURL       string `json:"targets_url"`
	RootFile         string `json:"root_file"`
	OfflineDirectory string `json:"offline_directory,omitempty"`
}

// SecurityMaintenanceConfiguration is installation-owned authority, never
// downloaded metadata. The serving application does not load it or its tokens.
type SecurityMaintenanceConfiguration struct {
	Format         string                     `json:"format"`
	StateDirectory string                     `json:"state_directory"`
	Feed           SecurityFeedConfiguration  `json:"feed"`
	Policy         securitymaintenance.Policy `json:"policy"`
	Kind           string                     `json:"kind"`
	NativeConfig   string                     `json:"native_config,omitempty"`
	// TimeoutSeconds is how long a native replacement may stay unfinished before a later
	// run stops and rolls it back (default 30 minutes, at most 24 hours). It must fit the
	// installation's largest migration. The other kinds set theirs under deployment.
	TimeoutSeconds int                        `json:"timeout_seconds,omitzero"`
	Deployment     *securitydeployment.Config `json:"deployment,omitzero"`
	// StatusFile optionally publishes a public status subset for the serving
	// application to read. It must not be inside the private state directory.
	StatusFile string `json:"status_file,omitempty"`
}

// volumeRoot is the mounted volume holding the state when a Kubernetes coordinator
// runs in the cluster; its mode belongs to the platform. Empty everywhere else.
func (c SecurityMaintenanceConfiguration) volumeRoot() string {
	if c.Kind == "kubernetes" && c.Deployment != nil && c.Deployment.Kubernetes != nil {
		return c.Deployment.Kubernetes.VolumeRoot
	}
	return ""
}

// openStore opens the coordinator state, accepting a world-writable volume root only when configured to.
func (c SecurityMaintenanceConfiguration) openStore() (*securitymaintenance.Store, error) {
	if c.acceptWorldWritableVolumeRoot() {
		return securitymaintenance.NewStoreOnWorldWritableVolume(c.StateDirectory, c.volumeRoot())
	}
	return securitymaintenance.NewStoreOnVolume(c.StateDirectory, c.volumeRoot())
}

// acceptWorldWritableVolumeRoot is the operator's explicit acceptance of a volume root any local user can write.
func (c SecurityMaintenanceConfiguration) acceptWorldWritableVolumeRoot() bool {
	return c.volumeRoot() != "" && c.Deployment.Kubernetes.AcceptWorldWritableVolumeRoot
}

// inCluster means the pod's service account is the authority: there is no host
// root to require, and the unprivileged chart user owns the coordinator's state.
func (c SecurityMaintenanceConfiguration) inCluster() bool {
	return c.Kind == "kubernetes" && c.Deployment != nil && c.Deployment.Kubernetes != nil && c.Deployment.Kubernetes.Kubeconfig == ""
}

func (c SecurityMaintenanceConfiguration) downloadCache() string {
	return filepath.Join(c.StateDirectory, "downloads")
}

func (c SecurityMaintenanceConfiguration) Validate() error {
	if c.Format != SecurityMaintenanceConfigFormat || !filepath.IsAbs(c.StateDirectory) || filepath.Clean(c.StateDirectory) != c.StateDirectory || c.StateDirectory == "/" ||
		!filepath.IsAbs(c.Feed.RootFile) || filepath.Clean(c.Feed.RootFile) != c.Feed.RootFile {
		return errors.New("invalid security maintenance configuration identity or authority paths")
	}
	if c.StatusFile != "" && (!filepath.IsAbs(c.StatusFile) || filepath.Clean(c.StatusFile) != c.StatusFile || pathWithin(c.StatusFile, c.StateDirectory) || pathWithin(c.StateDirectory, c.StatusFile)) {
		return errors.New("security maintenance status file must be a clean absolute path outside the private state directory")
	}
	if err := c.Policy.Validate(); err != nil {
		return err
	}
	if (c.Policy.Mode == securitymaintenance.Offline) != (c.Feed.OfflineDirectory != "") {
		return errors.New("offline policy requires a verified local repository and online policy cannot silently use one")
	}
	switch c.Kind {
	case "native":
		if c.Deployment != nil || !filepath.IsAbs(c.NativeConfig) || filepath.Clean(c.NativeConfig) != c.NativeConfig {
			return errors.New("native maintenance requires one fixed host configuration")
		}
		if c.TimeoutSeconds < 0 || c.TimeoutSeconds > 86400 {
			return errors.New("native timeout_seconds must be between 0 and 86400")
		}
	case "compose", "kubernetes", "ecs":
		if c.TimeoutSeconds != 0 {
			return errors.New("timeout_seconds belongs under deployment for this kind")
		}
		if c.NativeConfig != "" || c.Deployment == nil || string(c.Deployment.Kind) != c.Kind {
			return errors.New("deployment maintenance requires its exact local executor contract")
		}
		if err := securitydeployment.Validate(*c.Deployment); err != nil {
			return err
		}
		if c.Deployment.StateDirectory != filepath.Join(c.StateDirectory, "deployment") {
			return errors.New("deployment journal must use the coordinator's private installation directory")
		}
		if c.Kind == "kubernetes" && c.Deployment.Kubernetes.StatusConfigMap != "" && c.StatusFile == "" {
			return errors.New("a status ConfigMap needs status_file, the public status it copies")
		}
	default:
		return errors.New("unsupported security maintenance deployment; a qualified owner-specific executor is required")
	}
	return nil
}

// pathWithin reports whether clean absolute path is dir or lies below it.
func pathWithin(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

func loadSecurityMaintenanceConfiguration(path string) (SecurityMaintenanceConfiguration, error) {
	var c SecurityMaintenanceConfiguration
	raw, err := securityfeed.ReadPublicFile(path, 65536)
	if err != nil {
		return c, err
	}
	if err := definitions.DecodeStrict(raw, &c); err != nil {
		return c, fmt.Errorf("security maintenance configuration: %w", err)
	}
	return c, c.Validate()
}

func SecurityMaintenanceUsage(out io.Writer) {
	fmt.Fprintln(out, `Usage: hikyo security-maintenance <command> [options]

  check       Verify public advisories and report local eligibility; never apply.
  enroll      Explicitly authorize maintenance for this healthy installation.
  run         Check and apply eligible updates periodically, or once with --once.
  status      Read durable local status without contacting the publisher.
  reconcile   Abandon a pinned operation, resume a target whose automatic attempts stopped, or accept a restored installation.
  stage       Prepare an enrolled image's authenticated route before replacement.
  rollback    Restore the fenced snapshot after a stopped target; run by the owner in the target image.
  inspect     Print this running binary's actual applied database identity.
  health      Probe the configured local operational readiness endpoint.
  feed        Initialize feed trust or publish an authenticated catalog.

check/enroll/run/status/reconcile: --config FILE (default /etc/hikyo/security-maintenance.json)
run: --once performs one bounded check/application attempt. It exits nonzero
while an applicable advisory remains blocked, so host monitoring sees the gap.
reconcile: --confirm VALUE names what is abandoned; status prints the command.
stage/rollback: --expected-manifest SHA256 is mandatory.

Application updates can include features and maintenance under disclosed local
policy. Explicit pins, manual/offline mode, trust and recovery fences are honored.
No installation registration or project-held deployment credentials are used.`)
}

func configuredSecurityEngine(c SecurityMaintenanceConfiguration, out io.Writer) (*securitymaintenance.Engine, error) {
	state, err := c.openStore()
	if err != nil {
		return nil, err
	}
	state.PublicStatusFile = c.StatusFile
	root, err := securityfeed.ReadPublicFile(c.Feed.RootFile, securityfeed.MaxDocumentBytes)
	if err != nil {
		return nil, err
	}
	feed, err := securityfeed.New(securityfeed.Config{MetadataURL: c.Feed.MetadataURL, TargetsURL: c.Feed.TargetsURL, StateDirectory: filepath.Join(c.StateDirectory, "feed"), TrustedRoot: root, OfflineDirectory: c.Feed.OfflineDirectory, VolumeRoot: c.volumeRoot(), AcceptWorldWritableVolumeRoot: c.acceptWorldWritableVolumeRoot()})
	if err != nil {
		return nil, err
	}
	var driver securitymaintenance.Driver
	cache := c.downloadCache()
	if c.Kind == "native" {
		driver, err = newNativeSecurityDriver(c.NativeConfig, out)
	} else {
		if err := os.MkdirAll(c.Deployment.StateDirectory, 0700); err != nil {
			return nil, err
		}
		driver, err = newSecurityDeploymentDriver(*c.Deployment, cache)
	}
	if err != nil {
		return nil, err
	}
	engine := &securitymaintenance.Engine{Store: state, Feed: feed, Driver: driver, Platform: runtime.GOOS + "/" + runtime.GOARCH, ImageDelivery: c.Kind != "native", RequireRuntimeDigest: c.Kind == "kubernetes" || c.Kind == "ecs", Policy: c.Policy}
	if owner, ok := driver.(*nativeSecurityDriver); ok {
		if c.TimeoutSeconds != 0 {
			owner.timeout = time.Duration(c.TimeoutSeconds) * time.Second
		}
		engine.OperationTimeout = owner.timeout
	}
	// Compose and Kubernetes can roll an unfinished replacement back; ECS stays pinned.
	if owner, ok := driver.(*securityDeploymentDriver); ok && (c.Kind == "compose" || c.Kind == "kubernetes") {
		engine.OperationTimeout = owner.timeout
	}
	return engine, nil
}

type policyBoundSecurityDriver struct {
	securitymaintenance.Driver
	path     string
	expected releaseidentity.Digest
}

func (d policyBoundSecurityDriver) Apply(ctx context.Context, target securitymaintenance.Target) error {
	if err := d.authorize(); err != nil {
		return err
	}
	previous := target.BeforeMutation
	target.BeforeMutation = func() error {
		if err := d.authorize(); err != nil {
			return err
		}
		if previous != nil {
			return previous()
		}
		return nil
	}
	target.PolicyFile, target.PolicySHA256 = d.path, d.expected
	return d.Driver.Apply(ctx, target)
}

func (d policyBoundSecurityDriver) Reconcile(ctx context.Context, observed securitymaintenance.Observation) error {
	if reconciler, ok := d.Driver.(securitymaintenance.Reconciler); ok {
		return reconciler.Reconcile(ctx, observed)
	}
	return nil
}

func (d policyBoundSecurityDriver) RestoreAborted(ctx context.Context, aborted securitymaintenance.Observation, operation securitymaintenance.Operation) error {
	if restorer, ok := d.Driver.(securitymaintenance.AbortRestorer); ok {
		return restorer.RestoreAborted(ctx, aborted, operation)
	}
	return securitydeployment.ErrUnsupported
}

// NoFinalGate forwards to an owner that can prove it. An owner that cannot proves nothing, and a
// target that cannot be read is never stopped without proof.
func (d policyBoundSecurityDriver) NoFinalGate(ctx context.Context, operation securitymaintenance.Operation) (bool, error) {
	if prover, ok := d.Driver.(securitymaintenance.GateProver); ok {
		return prover.NoFinalGate(ctx, operation)
	}
	return false, errors.New("the driver cannot prove that the target did not pass the final gate")
}

func (d policyBoundSecurityDriver) authorize() error {
	return verifySecurityPolicyFingerprint(d.path, d.expected)
}

func bindSecurityPolicy(engine *securitymaintenance.Engine, path string, c SecurityMaintenanceConfiguration) error {
	raw, err := securityfeed.ReadPublicFile(path, 65536)
	if err != nil {
		return err
	}
	// The engine runs c, so the guard must not bind newer bytes than c was parsed from.
	var bound SecurityMaintenanceConfiguration
	if err := definitions.DecodeStrict(raw, &bound); err != nil {
		return err
	}
	loaded, err := json.Marshal(c, jsonwire.Stable)
	if err != nil {
		return err
	}
	current, err := json.Marshal(bound, jsonwire.Stable)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, loaded) {
		return errors.New("local maintenance policy changed while the coordinator started; retry")
	}
	engine.Driver = policyBoundSecurityDriver{Driver: engine.Driver, path: path, expected: releaseidentity.Hash(raw)}
	return nil
}

// RunSecurityMaintenance is a separate local operator role. It never grants
// the serving process workload-control authority and never revives remote apply.
func RunSecurityMaintenance(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		SecurityMaintenanceUsage(out)
		return flag.ErrHelp
	}
	if args[0] == "feed" {
		return RunSecurityFeed(ctx, args[1:], out)
	}
	if args[0] == "stage" || args[0] == "rollback" || args[0] == "inspect" || args[0] == "health" {
		return runSecurityImageCommand(ctx, args, out)
	}
	action := args[0]
	if action != "check" && action != "enroll" && action != "run" && action != "status" && action != "reconcile" {
		return errors.New("unknown security maintenance command")
	}
	fs := flag.NewFlagSet("hikyo security-maintenance "+action, flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("config", SecurityMaintenanceConfigPath, "installation-owned maintenance configuration")
	once := false
	if action == "run" {
		fs.BoolVar(&once, "once", false, "perform one check and application attempt")
	}
	confirm := ""
	if action == "reconcile" {
		fs.StringVar(&confirm, "confirm", "", "exact operation target, stopped target or restored incarnation being accepted")
	}
	fs.Usage = func() { SecurityMaintenanceUsage(out); fs.PrintDefaults() }
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected security maintenance arguments")
	}
	c, err := loadCoordinatorConfiguration(*path)
	if err != nil {
		return err
	}
	if action == "status" {
		store, err := c.openStore()
		if err != nil {
			return err
		}
		state, err := store.Load()
		if err != nil {
			return err
		}
		return encodeJSON(out, struct {
			securitymaintenance.Status
			Stale bool `json:"stale"`
		}{state, state.Phase == "stale" || state.LastVerified.IsZero() || !state.KnownUntil.After(time.Now())})
	}
	engine, err := configuredSecurityEngine(c, out)
	if err != nil {
		return err
	}
	if err := bindSecurityPolicy(engine, *path, c); err != nil {
		return err
	}
	if action == "enroll" {
		return enrollSecurityMaintenance(ctx, engine, c.StatusFile, out)
	}
	if action == "reconcile" {
		state, err := engine.Reconcile(ctx, confirm)
		deploymentDriverOf(engine.Driver).PublishStatus(ctx, c.StatusFile)
		return errors.Join(err, encodeJSON(out, state))
	}
	follower := newCoordinatorFollower(c, deploymentDriverOf(engine.Driver))
	for {
		state, runErr := engine.RunOnce(ctx, action == "run")
		deploymentDriverOf(engine.Driver).PublishStatus(ctx, c.StatusFile)
		if err := encodeJSON(out, state); err != nil {
			return errors.Join(runErr, err)
		}
		if action == "run" && runErr == nil {
			follower.afterRun(ctx, c, state)
		}
		if action != "run" || once {
			if runErr == nil && action == "run" && state.ProtectionGap() {
				return fmt.Errorf("%w: %s", securitymaintenance.ErrProtectionGap, state.Reason)
			}
			return runErr
		}
		if errors.Is(runErr, securitymaintenance.ErrStateUnavailable) {
			return runErr
		}
		if ctx.Err() != nil {
			return nil
		}
		// Retry transient failures without needing an interactive browser/CLI.
		// The durable status records stale/blocked/failed outcomes separately.
		timer := time.NewTimer(time.Duration(285+rand.IntN(31)) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		// Root-owned local policy changes take effect on the next check. A
		// change during staging is refused by the bound driver's Apply check.
		previous := c
		c, err = loadCoordinatorConfiguration(*path)
		if err != nil {
			return err
		}
		engine, err = configuredSecurityEngine(c, out)
		if err != nil {
			return err
		}
		if err := bindSecurityPolicy(engine, *path, c); err != nil {
			return err
		}
		if follower, err = rebindCoordinatorFollower(follower, previous, c, deploymentDriverOf(engine.Driver)); err != nil {
			return err
		}
	}
}

// loadCoordinatorConfiguration is the only way the coordinator reads its configuration,
// so a reload cannot accept authority that startup refuses.
func loadCoordinatorConfiguration(path string) (SecurityMaintenanceConfiguration, error) {
	c, err := loadSecurityMaintenanceConfiguration(path)
	if err != nil {
		return c, err
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 && !c.inCluster() {
		return c, errors.New("maintenance coordination requires the installation's Linux root operator; use the dedicated local image commands for observation")
	}
	return c, nil
}

// rebindCoordinatorFollower keeps f, and the release it has followed, only while the
// configuration it was built from is unchanged: its driver and executable belong to it.
func rebindCoordinatorFollower(f *coordinatorFollower, previous, c SecurityMaintenanceConfiguration, driver *securityDeploymentDriver) (*coordinatorFollower, error) {
	before, err := json.Marshal(previous, jsonwire.Stable)
	if err != nil {
		return nil, err
	}
	after, err := json.Marshal(c, jsonwire.Stable)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(before, after) {
		return f, nil
	}
	return newCoordinatorFollower(c, driver), nil
}

// enrollSecurityMaintenance runs in the chart's hook on every upgrade, including the one
// that recovers a failed update. An enrolled installation therefore succeeds from its
// stored status alone: the cluster and the coordinator lock may be unavailable then.
// Only the first enrollment, or a pin the operator removed, needs them.
func enrollSecurityMaintenance(ctx context.Context, engine *securitymaintenance.Engine, statusFile string, out io.Writer) error {
	if state, err := engine.Store.Load(); err == nil && state.Enrollment != nil {
		if err := deploymentDriverOf(engine.Driver).repinIfRemoved(ctx); err != nil {
			return fmt.Errorf("pin the Deployment identity: %w", err)
		}
		deploymentDriverOf(engine.Driver).PublishStatus(ctx, statusFile)
		return encodeJSON(out, state)
	}
	// Pinning first means a crash between the two steps cannot leave an enrolled
	// installation whose Deployment identity was never recorded.
	if driver := deploymentDriverOf(engine.Driver); driver != nil {
		if err := driver.PinIdentity(ctx); err != nil {
			return fmt.Errorf("pin the Deployment identity: %w", err)
		}
	}
	// A native host installed from nothing has no vault, pin or running service yet.
	if driver := nativeEnrollmentOf(engine.Driver); driver != nil {
		if err := driver.PrepareEnrollment(ctx); err != nil {
			return fmt.Errorf("prepare the native installation for enrollment: %w", err)
		}
	}
	state, err := engine.Enroll(ctx)
	deploymentDriverOf(engine.Driver).PublishStatus(ctx, statusFile)
	return errors.Join(err, encodeJSON(out, state))
}

func runSecurityImageCommand(ctx context.Context, args []string, out io.Writer) error {
	action := args[0]
	fs := flag.NewFlagSet("hikyo security-maintenance "+action, flag.ContinueOnError)
	fs.SetOutput(out)
	expected := ""
	databaseOnly, controlRow := false, false
	liveness := false
	if action == "stage" || action == "rollback" {
		fs.StringVar(&expected, "expected-manifest", "", "independently selected exact target manifest SHA-256")
	}
	if action == "inspect" {
		fs.BoolVar(&databaseOnly, "database-only", false, "inspect an isolated task's database; never claim live readiness")
		fs.BoolVar(&controlRow, "control-row", false, "print the control row alone, with no binary, schema or readiness check")
	}
	if action == "health" {
		fs.BoolVar(&liveness, "liveness", false, "probe /healthz so admitted maintenance remains live")
	}
	fs.Usage = func() { SecurityMaintenanceUsage(out); fs.PrintDefaults() }
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected image maintenance arguments")
	}
	if controlRow && databaseOnly {
		return errors.New("--control-row and --database-only are alternatives")
	}
	if (action == "stage" || action == "rollback") && releaseidentity.Digest(expected).Validate() != nil {
		return errors.New(action + " requires --expected-manifest with a complete lowercase SHA-256")
	}
	cfg, _, err := config.Load("server", nil, os.Getenv, os.Environ())
	if err != nil {
		return err
	}
	if action == "health" {
		return securityOperationalProbe(ctx, cfg, liveness)
	}
	if action == "rollback" {
		report, err := rollbackSecurityImage(ctx, cfg, releaseidentity.Digest(expected))
		if err != nil {
			return err
		}
		return encodeJSON(out, report)
	}
	var observed securitymaintenance.Observation
	if action == "stage" {
		observed, err = stageSecurityImage(ctx, cfg, releaseidentity.Digest(expected))
	} else if controlRow {
		observed, err = inspectSecurityControlRow(ctx, cfg)
	} else {
		observed, err = inspectSecurityImage(ctx, cfg)
		if err == nil {
			observed.Ready = !databaseOnly && observed.Ready && securityOperationalHealth(ctx, cfg) == nil
		}
	}
	if err != nil {
		return err
	}
	return encodeJSON(out, observed)
}

func securityOperationalHealth(ctx context.Context, cfg *config.Config) error {
	return securityOperationalProbe(ctx, cfg, false)
}

func securityOperationalProbe(ctx context.Context, cfg *config.Config, liveness bool) error {
	host, port, err := net.SplitHostPort(cfg.OperationalListen)
	if err != nil || port == "0" || port == "" {
		return errors.New("health requires a configured operational port")
	}
	ip := net.ParseIP(host)
	if host == "localhost" || host == "" {
		host = "127.0.0.1"
	} else if ip == nil {
		return errors.New("health requires a literal local operational address")
	} else if ip.IsUnspecified() {
		if ip.To4() != nil {
			host = "127.0.0.1"
		} else {
			host = "::1"
		}
	} else if !ip.IsLoopback() {
		return errors.New("health probe cannot contact a nonloopback address")
	}
	path := "/readyz"
	if liveness {
		path = "/healthz"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort(host, port)+path, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return errors.New("operational readiness is unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("operational endpoint is not ready")
	}
	return nil
}
