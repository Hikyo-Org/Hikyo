// Package app wires config, store, migrations, service, and the HTTP layer
// into the runnable subcommands.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/remotefetch"
	"github.com/Hikyo-Org/hikyo/internal/runtimeconfig"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/updater"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// Version is the build's version string, set from main's linker-stamped
// value. It is what /api/v1/meta advertises, so a client that refuses an
// operation above the server's API revision can name the version it refused.
var Version = "dev"

// Logger builds the process logger: text in dev, JSON in production.
func Logger(dev bool) *slog.Logger {
	if dev {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, nil))
}

func storeConfig(cfg *config.Config) store.Config {
	return store.Config{
		Engine:          store.Engine(cfg.Store.Engine),
		Path:            cfg.Store.Path,
		DSN:             cfg.Store.DSN,
		PostgresPoolMax: cfg.Store.PostgresPoolMax,
	}
}

// RunMigrate is `hikyo migrate`: explicit migration application. Loads no
// keyring (DDL only). Signed public evidence authorizes schema application;
// maintenance remains until the exact candidate boot proves hierarchy health.
func RunMigrate(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	result, err := databaseGate(ctx, cfg, nil, upgradegate.Migrate)
	if err != nil {
		return err
	}
	log.Info("verified schema application complete", "phase", result.State.Pending.Phase, "maintenance", result.State.Maintenance)
	return nil
}

// Server is a booted, listening server that has not started serving yet.
type Server struct {
	Maintenance        bool
	Addr               string
	OperationalAddr    string
	db                 *store.DB
	keyring            *crypto.Keyring // held for the process lifetime
	publicLn           net.Listener
	operationalLn      net.Listener
	publicHandler      http.Handler
	operationalHandler http.Handler
	log                *slog.Logger
	selfConfig         *service.SelfConfig
	owner              *ownerRuntime
}

// devRootKeyName sits beside the dev sqlite database (cwd when no sqlite
// path exists). Dev bootstrap only; a production start never generates a
// root key.
const devRootKeyName = "hikyo-dev.rootkey"

func devRootKeyPath(cfg *config.Config) string {
	if cfg.Store.Engine == config.EngineSQLite && cfg.Store.Path != "" {
		return filepath.Join(filepath.Dir(cfg.Store.Path), devRootKeyName)
	}
	if cfg.Store.Engine == config.EnginePostgres && cfg.Upgrade.StateDirectory != "" {
		return filepath.Join(cfg.Upgrade.StateDirectory, devRootKeyName)
	}
	return devRootKeyName
}

// resolveRootKey reads the operator root key, or — in --dev with no source
// configured — generates and persists a development root key beside the dev
// database.
//
// The dev generation is a recorded deviation from the encryption-model ADR's
// refusal 1 ("the server never auto-generates a root key on first run"),
// forced by the system-architecture ADR's zero-config `--dev` evaluation mode: an
// ephemeral key would brick hikyo-dev.db on every restart, and refusing would
// make --dev not zero-config. The rationale behind refusal 1 (a silent key
// nobody backed up, discovered at restore) does not bite an evaluation
// database sitting next to its own key file, and the generation is loud.
func resolveRootKey(cfg *config.Config, log *slog.Logger) ([]byte, error) {
	file := cfg.RootKeyFile
	if file == "" && !cfg.RootKeyFromEnv && cfg.Dev {
		if cfg.Store.Engine == config.EnginePostgres && cfg.Upgrade.StateDirectory == "" {
			return nil, errors.New("development PostgreSQL requires HIKYO_UPGRADE_STATE_DIR before root-key creation")
		}
		devPath := devRootKeyPath(cfg)
		created, err := ensureDevRootKey(devPath)
		if err != nil {
			return nil, err
		}
		if created {
			log.Warn("generated development root key; evaluation only, back it up with the dev database", "path", devPath)
		} else {
			log.Warn("using development root key", "path", devPath)
		}
		file = devPath
	}
	var envValue string
	if cfg.RootKeyFromEnv {
		envValue = os.Getenv("HIKYO_ROOT_KEY")
		log.Warn("root key delivered via HIKYO_ROOT_KEY: the value stays readable in the process environment for the whole lifetime; prefer --root-key-file or a systemd credential")
	}
	return crypto.ReadRootKey(file, envValue)
}

// rootKeySource re-reads the operator root key for the rotation operations that
// need it — master rotation seals the new master under the current root, root
// rotation reads the primary source at verify. It reuses resolveRootKey, so it
// honours the same file/env/systemd sources boot used; on a live instance the
// source already exists, so no dev generation fires on a re-read.
type rootKeySource struct {
	cfg *config.Config
	log *slog.Logger
}

func (s rootKeySource) Current(context.Context) ([]byte, error) {
	return resolveRootKey(s.cfg, s.log)
}

func (s rootKeySource) Next(context.Context) ([]byte, error) {
	if s.cfg.NewRootKeyFile == "" {
		return nil, errors.New("no new root key source configured; set HIKYO_NEW_ROOT_KEY_FILE to the new root before --prepare")
	}
	// Same file-source validation as the primary root — 64 hex chars, and the
	// file must not be group/world readable.
	return crypto.ReadRootKey(s.cfg.NewRootKeyFile, "")
}

// bootGuard tracks the resources Boot acquires so that any error after
// acquisition releases exactly what was acquired, in reverse order. It is
// armed by default — Boot defers cleanup so it runs on every return — and
// disarmed once ownership transfers to the Server, which then becomes the sole
// steady-state owner. Arming by default is the point: a future post-listen
// constructor that forgets its cleanup on an error return is still released by
// the deferred cleanup, so no error path can bypass it.
type bootGuard struct {
	closers []func() error
	log     *slog.Logger
}

// bootResources is the narrow seam for boot dependencies and resources whose
// acquisition can leave boot or a local-admin command with something to
// release. Service construction and wiring stay local to their caller; tests
// replace only these functions to pin ordering, inject failures at ownership
// boundaries, and count releases.
type bootResources struct {
	openDatabase        func(context.Context, store.Config, upgrade.Admission) (*store.DB, error)
	databaseGate        func(context.Context, *config.Config, []byte, upgradegate.Mode) (upgradegate.Result, error)
	closeDatabase       func(*store.DB) error
	warmOpenAPI         func() error
	listen              func(string, string) (net.Listener, error)
	closeListener       func(net.Listener) error
	newDirectoryClient  func(remotefetch.Config) (*remotefetch.Client, error)
	configureDeployment func(context.Context, *config.Config, *store.DB, *crypto.Keyring) (service.BootstrapDeployment, error)
}

func defaultBootResources() bootResources {
	return bootResources{
		openDatabase: func(ctx context.Context, cfg store.Config, admission upgrade.Admission) (*store.DB, error) {
			return store.Open(ctx, cfg, admission)
		},
		databaseGate: databaseGate,
		closeDatabase: func(db *store.DB) error {
			return db.Close()
		},
		warmOpenAPI: api.Warm,
		listen:      net.Listen,
		closeListener: func(ln net.Listener) error {
			return ln.Close()
		},
		newDirectoryClient:  remotefetch.New,
		configureDeployment: configureBootstrapDeployment,
	}
}

// add registers a resource's release in acquisition order. cleanup runs these
// in reverse, so registering the database before the listener yields the
// listener-before-database shutdown order Server.Close also uses.
func (g *bootGuard) add(closer func() error) {
	g.closers = append(g.closers, closer)
}

// cleanup releases every still-registered resource in reverse acquisition
// order. It is idempotent (clears the list) and never returns an error: a
// cleanup failure is logged, never allowed to mask the primary boot error that
// triggered it.
func (g *bootGuard) cleanup() {
	for i := len(g.closers) - 1; i >= 0; i-- {
		if err := g.closers[i](); err != nil {
			g.log.Warn("boot cleanup: releasing an acquired resource failed", "err", err)
		}
	}
	g.closers = nil
}

// disarm transfers ownership to the Server: the deferred cleanup becomes a
// no-op. Called once, immediately before Boot's success return, with nothing
// between it and the return that can fail.
func (g *bootGuard) disarm() {
	g.closers = nil
}

// openKeyed owns the startup prefix shared by server and local-admin commands:
// harden before key material exists, migrate with the pre-migration safety
// record, verify the exact schema, resolve the root, open the datastore, load
// the keyring, and warn about unfinished root rotation. Callers supply the
// resource guard so every error after datastore acquisition closes it exactly
// once.
func openKeyed(ctx context.Context, cfg *config.Config, log *slog.Logger, sc store.Config, resources bootResources, guard *bootGuard) (*store.DB, *crypto.Keyring, error) {
	if err := crypto.HardenProcess(); err != nil {
		return nil, nil, err
	}

	root, err := resolveRootKey(cfg, log)
	if err != nil {
		return nil, nil, err
	}
	admitted, err := resources.databaseGate(ctx, cfg, root, upgradegate.Boot)
	if err != nil {
		crypto.Zero(root)
		return nil, nil, err
	}
	db, err := resources.openDatabase(ctx, sc, admitted.Admission)
	if err != nil {
		crypto.Zero(root)
		return nil, nil, err
	}
	guard.add(func() error { return resources.closeDatabase(db) })
	logDatastorePoolSizes(log, db)

	// LoadKeyring consumes root: it is zeroed before this returns.
	kr, err := crypto.LoadKeyring(ctx, &keyring.Store{DB: db}, root)
	if err != nil {
		return nil, nil, err
	}
	if kr.RootRotationPending() {
		// A root rotation is half-done — the master is dual-wrapped under two
		// roots. Bootable under either, but warned on every server or admin start
		// until `rotate-root-key --finalize` completes it.
		log.Warn("root key rotation is UNFINISHED: the master is dual-wrapped under the old and new roots; run `hikyo rotate-root-key --verify` then `--finalize` to complete it")
	}
	return db, kr, nil
}

func logDatastorePoolSizes(log *slog.Logger, db *store.DB) {
	limits := db.ConnectionPoolLimits()
	switch db.Engine() {
	case store.EngineSQLite:
		log.Info("datastore connection pools configured",
			"engine", db.Engine(),
			"write_max_connections", limits.Primary,
			"read_max_connections", limits.ReadOnly)
	case store.EnginePostgres:
		log.Info("datastore connection pool configured",
			"engine", db.Engine(),
			"max_connections", limits.Primary)
	}
}

// Boot runs the fail-closed startup sequence: process hardening before any
// key material exists, migrations (auto-apply by default; with auto-apply
// disabled a pending migration state refuses to serve), datastore open with
// the boot-enforced pragma policy, keyring load (root key read, master key
// unwrapped or minted, root key zeroed), then the listener. Any error means the
// process must exit without serving.
func Boot(ctx context.Context, cfg *config.Config, log *slog.Logger) (*Server, error) {
	return boot(ctx, cfg, log, defaultBootResources())
}

func boot(ctx context.Context, cfg *config.Config, log *slog.Logger, resources bootResources) (*Server, error) {
	var selectionErr error
	cfg, selectionErr = resolveSelectedUpgrade(ctx, cfg, deploymentSelectionDirectory)
	if selectionErr != nil {
		return nil, fmt.Errorf("boot: upgrade selection: %w", selectionErr)
	}
	if cfg.UpdaterSocket != "" {
		return nil, fmt.Errorf("boot: HIKYO_UPDATER_SOCKET: %w", updater.ErrRemoteApplyDisabled)
	}
	sc := storeConfig(cfg)

	// Every resource acquired below has one temporary owner — this guard —
	// until the Server takes over. Armed by default; disarmed only at the
	// ownership transfer immediately before the success return.
	guard := &bootGuard{log: log}
	defer guard.cleanup()

	db, kr, err := openKeyed(ctx, cfg, log, sc, resources, guard)
	if err != nil {
		if errors.Is(err, upgradegate.ErrRestoreRequired) || errors.Is(err, upgradegate.ErrNextBinary) {
			return bootMaintenance(cfg, log, resources)
		}
		return nil, fmt.Errorf("boot: refusing to serve: %w", err)
	}

	// The coordinator survives graph replacement. Its Auth owns only the
	// datastore-backed exact reauthentication operations and logging; each
	// serving graph gets its own immutable configuration-dependent Auth.
	bootstrapAuth := &service.Auth{DB: db, Keyring: kr, Log: log}
	selfConfig := newSelfConfig(cfg, db, kr, bootstrapAuth)
	configureDeployment := resources.configureDeployment
	if configureDeployment == nil {
		configureDeployment = configureBootstrapDeployment
	}
	selfConfig.Deployment, err = configureDeployment(ctx, cfg, db, kr)
	if err != nil {
		return nil, fmt.Errorf("boot: deployment enrollment unavailable")
	}
	bootstrapAuth.SelfConfig = selfConfig
	bundle, err := selfConfig.ResolveRuntimeBundle(ctx)
	if err != nil {
		return nil, fmt.Errorf("boot: self-configuration: %w", err)
	}
	sourcesPending := bundle.BootstrapSources() != (config.ManagedBootstrapSources{}) &&
		(selfConfig.Deployment == nil || selfConfig.Deployment.VerifyInstalled(ctx, bundle) != nil)
	configurationBundle := bundle
	if sourcesPending {
		configurationBundle, err = selfConfig.ResolveRepairRuntimeBundle(ctx)
		if err != nil {
			return nil, fmt.Errorf("boot: previous configuration required for deployment repair is unavailable")
		}
	}
	bootstrap := cfg
	effective, ownerValues, nodeValues, missingNode, err := bootNodeConfiguration(cfg, selfConfig, configurationBundle)
	if err != nil {
		return nil, fmt.Errorf("boot: managed configuration is invalid: %w", err)
	}
	cfg = effective
	if err := resources.warmOpenAPI(); err != nil {
		return nil, fmt.Errorf("boot: refusing to serve: OpenAPI contract: %w", err)
	}

	srv := &Server{db: db, keyring: kr, log: log, selfConfig: selfConfig}
	owner := &ownerRuntime{server: srv, base: bootstrap, resources: resources,
		selfConfig: selfConfig, budget: serviceBudget(cfg), advisory: service.NewAdvisory(), fakeProvider: newDevFakeProvider(),
		values: ownerValues, nodeValues: nodeValues, seedNodeValues: nodeValues,
		endpointErrors: make(chan error, 4)}
	certificate, err := newManagedCertificate(cfg.TLSCertPEM, cfg.TLSKeyPEM)
	if err != nil {
		return nil, err
	}
	endpoints, err := owner.prepareEndpoints(cfg, certificate)
	if err != nil {
		return nil, fmt.Errorf("boot: listeners: %w", err)
	}
	endpoints.activate(owner)
	guard.add(func() error { return resources.closeListener(srv.publicLn) })
	guard.add(func() error { return resources.closeListener(srv.operationalLn) })
	if cfg.Store.PostgresPoolMax != bootstrap.Store.PostgresPoolMax {
		pool, err := db.PreparePostgresPool(ctx, cfg.Store.PostgresPoolMax)
		if err != nil {
			return nil, err
		}
		err = pool.Activate(ctx)
		closeErr := pool.Close()
		if err != nil || closeErr != nil {
			return nil, errors.Join(err, closeErr)
		}
	}
	selfConfig.Budget = owner.budget
	if cfg.HA {
		owner.haCoord, owner.haTick, owner.haStatus, err = configureHA(ctx, cfg, log, db, sc, kr)
		if err != nil {
			return nil, err
		}
		kr.SetHAFreshness(true)
	}
	graph, err := owner.prepareGeneration(ctx, cfg)
	if err != nil {
		return nil, err
	}
	owner.current = newRunningGeneration(graph)
	guard.add(func() error {
		owner.stop()
		return selfConfig.CloseRuntime()
	})
	srv.owner = owner
	srv.publicHandler = owner.handler(false)
	srv.operationalHandler = owner.handler(true)
	selfConfig.Installer = owner
	if missingNode || sourcesPending {
		if err := selfConfig.RecordRepairOrigin(ctx, cfg.ExternalOrigin); err != nil {
			return nil, err
		}
	}
	if err := selfConfig.LoadRuntime(ctx); err != nil && !(missingNode && errors.Is(err, runtimeconfig.ErrNodeNotConfigured)) {
		if !sourcesPending {
			return nil, fmt.Errorf("boot: self-configuration: %w", err)
		}
		// The committed rollout worker must survive a pending source or mailbox
		// outage. Capture remains fenced; only the validated repair graph runs.
		log.Warn("deployment sources pending; administrative recovery remains available")
	}
	publicAddress, operationalAddress := endpoints.public.listener.Addr().String(), endpoints.operational.listener.Addr().String()
	log.Info("boot complete", "version", Version, "engine", sc.Engine, "external_origin", cfg.ExternalOrigin,
		"addr", publicAddress, "operational_addr", operationalAddress, "dev", cfg.Dev,
		"argon2_memory_kib", cfg.Argon2MemoryKiB, "mcp_enabled", cfg.MCPEnabled)

	// Ownership transfers only after the Server is complete. Nothing remains
	// between disarm and return, so Server.Close is now the sole owner.
	guard.disarm()
	return srv, nil
}

// serviceBudget keeps production policy fail-closed even when a caller builds
// Config directly instead of going through config.Load. The development-only
// off switch exists for the browser flow harness; budget behavior itself is
// covered by service-level conformance and unit tests.
func serviceBudget(cfg *config.Config) *service.Budget {
	budget := service.NewBudget()
	budget.SetDevelopmentDisabled(cfg.Dev && cfg.DevServiceBudgetsDisabled)
	return budget
}

// AuthComponents resolves the two authentication settings and, in doing so,
// runs two boot invariants that must fail fast rather than surface at the
// first login:
//
//   - the Argon2id parameters are checked against the floor the human-auth
//     ADR fixes, and the server refuses to start below it;
//   - the admission budget must hold at least one verification plus the
//     global headroom, so a configuration where one login cannot fit is a
//     config error caught here, never a runtime surprise.
//
// It deliberately does not build the service: the service holds the keyring,
// and the redaction analyzer bans key-bearing types from reaching a log call
// — so the caller assembles it and logs from these values instead.
func AuthComponents(cfg *config.Config) (crypto.PasswordParams, *admission.Limiter, error) {
	kdf := crypto.PasswordParams{
		MemoryKiB:   cfg.Argon2MemoryKiB,
		Time:        cfg.Argon2Time,
		Parallelism: cfg.Argon2Parallelism,
	}
	if err := kdf.CheckFloor(); err != nil {
		return crypto.PasswordParams{}, nil, err
	}
	limiter, err := admission.New(admission.Config{
		BudgetMiB:      cfg.AdmissionBudgetMiB,
		ArgonMemoryKiB: kdf.MemoryKiB,
		PerIPPerMinute: cfg.DevAdmissionPerIPPerMinute,
	})
	if err != nil {
		return crypto.PasswordParams{}, nil, err
	}
	return kdf, limiter, nil
}

func parseCIDRs(raw []string) ([]*net.IPNet, error) {
	out := make([]*net.IPNet, 0, len(raw))
	for _, s := range raw {
		_, network, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy CIDR %q: %w", s, err)
		}
		out = append(out, network)
	}
	return out, nil
}

// newHTTPServer applies the locked slow-client limits. SSE replaces the
// ordinary response deadline with a fresh deadline for each frame.
type managedHTTPServer struct {
	*http.Server
	cancelActive context.CancelFunc
	requests     *requestTracker
}

type requestTracker struct {
	handler http.Handler
	mu      sync.Mutex
	active  sync.WaitGroup
	stopped bool
}

func (t *requestTracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	t.active.Add(1)
	t.mu.Unlock()
	defer t.active.Done()
	t.handler.ServeHTTP(w, r)
}

func (t *requestTracker) stop() {
	t.mu.Lock()
	t.stopped = true
	t.mu.Unlock()
}

func newHTTPServer(h http.Handler) *managedHTTPServer {
	activeContext, cancelActive := context.WithCancel(context.Background())
	requests := &requestTracker{handler: h}
	return &managedHTTPServer{Server: &http.Server{
		Handler:           requests,
		BaseContext:       func(net.Listener) context.Context { return activeContext },
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      server.ResponseWriteTimeout,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}, cancelActive: cancelActive, requests: requests}
}

// shutdownHTTPServers gives every active request the same drain window. If the
// window expires, Close tears down the connections so request contexts are
// cancelled instead of leaving handlers running after Serve returns.
func shutdownHTTPServers(grace time.Duration, servers ...*managedHTTPServer) error {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	for _, httpServer := range servers {
		httpServer.requests.stop()
	}

	errs := make(chan error, len(servers))
	var shutdownWG sync.WaitGroup
	for _, httpServer := range servers {
		shutdownWG.Add(1)
		go func() {
			defer shutdownWG.Done()
			errs <- httpServer.Shutdown(ctx)
		}()
	}
	shutdownWG.Wait()
	close(errs)
	var joined error
	for err := range errs {
		joined = errors.Join(joined, err)
	}
	if ctx.Err() != nil {
		for _, httpServer := range servers {
			httpServer.cancelActive()
			joined = errors.Join(joined, httpServer.Close())
		}
	} else {
		for _, httpServer := range servers {
			httpServer.cancelActive()
		}
	}
	// Shutdown waits for graceful completions. After forced cancellation,
	// Close only tears down connections, so explicitly wait for deferred
	// request cleanup before the caller releases shared stores and services.
	for _, httpServer := range servers {
		httpServer.requests.active.Wait()
	}
	return joined
}

// Serve blocks until ctx is cancelled, then shuts down gracefully.
// ServeWithReady runs both HTTP serving goroutines and calls ready (when
// non-nil) after they have started. The command uses the ready hook to present
// an accurate startup summary without teaching the application package about
// terminal output; tests pass nil.
func (s *Server) ServeWithReady(ctx context.Context, ready func()) error {
	return s.serve(ctx, ready)
}

func (s *Server) serve(ctx context.Context, ready func()) error {
	if s.Maintenance {
		return s.serveMaintenance(ctx, ready)
	}
	return s.owner.serve(ctx, ready)
}

// ReloadTLS preserves the applied managed certificate when SIGHUP is received.
func (s *Server) ReloadTLS() error {
	// Managed TLS is immutable configuration. A signal must never reread an
	// old bootstrap mount and override the administrator's applied revision.
	return errors.New("TLS certificates are managed by instance configuration; publish and Apply the node certificate from Hikyo")
}

// Close releases resources for a booted server that never served.
func (s *Server) Close() error {
	if s.Maintenance {
		return s.operationalLn.Close()
	}
	if s.owner != nil {
		s.owner.stop()
	}
	var runtimeErr error
	if s.selfConfig != nil {
		runtimeErr = s.selfConfig.CloseRuntime()
	}
	return errors.Join(runtimeErr, s.publicLn.Close(), s.operationalLn.Close(), s.db.Close())
}

// approvalMetricsSource adapts the change-approval service to the metrics
// collector's synchronous ApprovalSnapshot contract (#151): a bounded read that
// reports zeros on any error rather than failing the scrape.
type approvalMetricsSource struct {
	svc *service.Approvals
}

func (s approvalMetricsSource) ApprovalSnapshot() server.ApprovalStats {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	active, expired, err := s.svc.OperationalCounts(ctx)
	if err != nil {
		return server.ApprovalStats{}
	}
	return server.ApprovalStats{Open: float64(active), Expired: float64(expired)}
}
