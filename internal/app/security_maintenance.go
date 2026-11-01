package app

import (
	"context"
	"encoding/json"
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
	"time"

	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/definitions"
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
	Deployment     *securitydeployment.Config `json:"deployment,omitempty"`
}

func (c SecurityMaintenanceConfiguration) Validate() error {
	if c.Format != SecurityMaintenanceConfigFormat || !filepath.IsAbs(c.StateDirectory) || filepath.Clean(c.StateDirectory) != c.StateDirectory || c.StateDirectory == "/" ||
		!filepath.IsAbs(c.Feed.RootFile) || filepath.Clean(c.Feed.RootFile) != c.Feed.RootFile {
		return errors.New("invalid security maintenance configuration identity or authority paths")
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
	case "compose", "kubernetes", "ecs":
		if c.NativeConfig != "" || c.Deployment == nil || string(c.Deployment.Kind) != c.Kind {
			return errors.New("deployment maintenance requires its exact local executor contract")
		}
		if err := securitydeployment.Validate(*c.Deployment); err != nil {
			return err
		}
		if c.Deployment.StateDirectory != filepath.Join(c.StateDirectory, "deployment") {
			return errors.New("deployment journal must use the coordinator's private installation directory")
		}
	default:
		return errors.New("unsupported security maintenance deployment; a qualified owner-specific executor is required")
	}
	return nil
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
  stage       Prepare an enrolled image's authenticated route before replacement.
  inspect     Print this running binary's actual applied database identity.
  health      Probe the configured local operational readiness endpoint.
  feed        Initialize feed trust or publish an authenticated catalog.

check/enroll/run/status: --config FILE (default /etc/hikyo/security-maintenance.json)
run: --once performs one bounded check/application attempt.
stage: --expected-manifest SHA256 is mandatory.

Application updates can include features and maintenance under disclosed local
policy. Explicit pins, manual/offline mode, trust and recovery fences are honored.
No installation registration or project-held deployment credentials are used.`)
}

func configuredSecurityEngine(c SecurityMaintenanceConfiguration, out io.Writer) (*securitymaintenance.Engine, error) {
	state, err := securitymaintenance.NewStore(c.StateDirectory)
	if err != nil {
		return nil, err
	}
	root, err := securityfeed.ReadPublicFile(c.Feed.RootFile, securityfeed.MaxDocumentBytes)
	if err != nil {
		return nil, err
	}
	feed, err := securityfeed.New(securityfeed.Config{MetadataURL: c.Feed.MetadataURL, TargetsURL: c.Feed.TargetsURL, StateDirectory: filepath.Join(c.StateDirectory, "feed"), TrustedRoot: root, OfflineDirectory: c.Feed.OfflineDirectory})
	if err != nil {
		return nil, err
	}
	var driver securitymaintenance.Driver
	cache := filepath.Join(c.StateDirectory, "downloads")
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
	return &securitymaintenance.Engine{Store: state, Feed: feed, Driver: driver, Platform: runtime.GOOS + "/" + runtime.GOARCH, ImageDelivery: c.Kind != "native", RequireRuntimeDigest: c.Kind == "kubernetes" || c.Kind == "ecs", Policy: c.Policy}, nil
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

func (d policyBoundSecurityDriver) authorize() error {
	return verifySecurityPolicyFingerprint(d.path, d.expected)
}

func bindSecurityPolicy(engine *securitymaintenance.Engine, path string, c SecurityMaintenanceConfiguration) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
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
	if args[0] == "stage" || args[0] == "inspect" || args[0] == "health" {
		return runSecurityImageCommand(ctx, args, out)
	}
	action := args[0]
	if action != "check" && action != "enroll" && action != "run" && action != "status" {
		return errors.New("unknown security maintenance command")
	}
	fs := flag.NewFlagSet("hikyo security-maintenance "+action, flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("config", SecurityMaintenanceConfigPath, "installation-owned maintenance configuration")
	once := false
	if action == "run" {
		fs.BoolVar(&once, "once", false, "perform one check and application attempt")
	}
	fs.Usage = func() { SecurityMaintenanceUsage(out); fs.PrintDefaults() }
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected security maintenance arguments")
	}
	c, err := loadSecurityMaintenanceConfiguration(*path)
	if err != nil {
		return err
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return errors.New("maintenance coordination requires the installation's Linux root operator; use the dedicated local image commands for observation")
	}
	if action == "status" {
		store, err := securitymaintenance.NewStore(c.StateDirectory)
		if err != nil {
			return err
		}
		state, err := store.Load()
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(struct {
			securitymaintenance.Status
			Stale bool `json:"stale"`
		}{state, state.LastVerified.IsZero() || !state.KnownUntil.After(time.Now())})
	}
	engine, err := configuredSecurityEngine(c, out)
	if err != nil {
		return err
	}
	if err := bindSecurityPolicy(engine, *path, c); err != nil {
		return err
	}
	if action == "enroll" {
		state, err := engine.Enroll(ctx)
		return errors.Join(err, json.NewEncoder(out).Encode(state))
	}
	for {
		state, runErr := engine.RunOnce(ctx, action == "run")
		if err := json.NewEncoder(out).Encode(state); err != nil {
			return errors.Join(runErr, err)
		}
		if action != "run" || once {
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
		c, err = loadSecurityMaintenanceConfiguration(*path)
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
	}
}

func runSecurityImageCommand(ctx context.Context, args []string, out io.Writer) error {
	action := args[0]
	fs := flag.NewFlagSet("hikyo security-maintenance "+action, flag.ContinueOnError)
	fs.SetOutput(out)
	expected := ""
	databaseOnly := false
	liveness := false
	if action == "stage" {
		fs.StringVar(&expected, "expected-manifest", "", "independently selected exact target manifest SHA-256")
	}
	if action == "inspect" {
		fs.BoolVar(&databaseOnly, "database-only", false, "inspect an isolated task's database; never claim live readiness")
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
	if action == "stage" && releaseidentity.Digest(expected).Validate() != nil {
		return errors.New("stage requires --expected-manifest with a complete lowercase SHA-256")
	}
	cfg, _, err := config.Load("server", nil, os.Getenv, os.Environ())
	if err != nil {
		return err
	}
	if action == "health" {
		return securityOperationalProbe(ctx, cfg, liveness)
	}
	var observed securitymaintenance.Observation
	if action == "stage" {
		observed, err = stageSecurityImage(ctx, cfg, releaseidentity.Digest(expected))
	} else {
		observed, err = inspectSecurityImage(ctx, cfg)
		if err == nil {
			observed.Ready = !databaseOnly && observed.Ready && securityOperationalHealth(ctx, cfg) == nil
		}
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(observed)
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
