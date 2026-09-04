// Package app wires config, store, migrations, service, and the HTTP layer
// into the runnable subcommands.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Hikyo-Org/hikyo/api"
	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/oidcfed"
	"github.com/Hikyo-Org/hikyo/internal/remotefetch"
	"github.com/Hikyo-Org/hikyo/internal/scanning"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/migrate"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/updater"
	"github.com/Hikyo-Org/hikyo/internal/webui"
)

// ClientVerbs are the fixed not-yet-implemented client-side subcommands from
// the system-architecture component set. Implemented verbs move to cli.Verbs;
// both lists are enumerated by the classification-totality invariant.
// `run` moved to cli.Verbs with #63; `render`/`sync` are now `compose`
// sub-verbs but keep their scaffolded top-level stubs until the help surface
// retires them.
var ClientVerbs = []string{"render", "sync", "adopt", "definitions"}

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
// keyring (DDL only).
func RunMigrate(ctx context.Context, cfg *config.Config, log *slog.Logger) error {
	sc := storeConfig(cfg)
	log.Info("applying migrations", "engine", sc.Engine)
	rec, err := beforeMigration(ctx, cfg, log, sc)
	if err != nil {
		return err
	}
	if err := migrate.Run(ctx, sc); err != nil {
		return err
	}
	recordPreMigration(ctx, cfg, log, rec)
	log.Info("migrations current")
	return nil
}

// beforeMigration runs the automatic pre-migration export, but only when this
// binary actually has a migration to apply: an export per ordinary restart is
// a backup policy nobody asked for.
func beforeMigration(ctx context.Context, cfg *config.Config, log *slog.Logger, sc store.Config) (preMigrationRecord, error) {
	pending, err := migrate.HasPending(ctx, sc)
	if err != nil {
		// Fail TOWARD the backup: a check that errors while the migration
		// then succeeds would silently skip the one export standing between
		// a bad migration and a rebuilt instance. Attempting the export costs
		// at worst one unneeded artifact; if the store is truly down, the
		// export preflight and the migration both say so.
		log.Warn("pre-migration pending check failed; attempting the export anyway", "err", err)
		return preMigrationExport(ctx, cfg, log)
	}
	if !pending {
		return preMigrationRecord{}, nil
	}
	return preMigrationExport(ctx, cfg, log)
}

// Server is a booted, listening server that has not started serving yet.
type Server struct {
	Addr               string
	OperationalAddr    string
	db                 *store.DB
	keyring            *crypto.Keyring // held for the process lifetime
	publicLn           net.Listener
	operationalLn      net.Listener
	publicHandler      http.Handler
	operationalHandler http.Handler
	tlsReloader        *certReloader
	log                *slog.Logger
	scheduler          *Scheduler
	adapterWorker      *adapter.Worker
	dynamicWorker      *dynamicWorker
	updateReconciler   *service.Updates
}

// devRootKeyName sits beside the dev sqlite database (cwd when no sqlite
// path exists). Dev bootstrap only; a production start never generates a
// root key.
const devRootKeyName = "hikyo-dev.rootkey"

func devRootKeyPath(cfg *config.Config) string {
	if cfg.Store.Engine == config.EngineSQLite && cfg.Store.Path != "" {
		return filepath.Join(filepath.Dir(cfg.Store.Path), devRootKeyName)
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
		devPath := devRootKeyPath(cfg)
		if _, err := os.Stat(devPath); errors.Is(err, os.ErrNotExist) {
			key, err := crypto.GenerateRootKey()
			if err != nil {
				return nil, err
			}
			defer crypto.Zero(key)
			if err := os.WriteFile(devPath, []byte(crypto.EncodeRootKey(key)+"\n"), 0o600); err != nil {
				return nil, fmt.Errorf("write dev root key: %w", err)
			}
			log.Warn("generated development root key — evaluation only, back it up with the dev database or lose the data",
				"path", devPath)
		} else if err != nil {
			return nil, fmt.Errorf("dev root key: %w", err)
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
	openDatabase       func(context.Context, store.Config) (*store.DB, error)
	closeDatabase      func(*store.DB) error
	warmOpenAPI        func() error
	listen             func(string, string) (net.Listener, error)
	closeListener      func(net.Listener) error
	newDirectoryClient func(remotefetch.Config) (*remotefetch.Client, error)
}

func defaultBootResources() bootResources {
	return bootResources{
		openDatabase: store.Open,
		closeDatabase: func(db *store.DB) error {
			return db.Close()
		},
		warmOpenAPI: api.Warm,
		listen:      net.Listen,
		closeListener: func(ln net.Listener) error {
			return ln.Close()
		},
		newDirectoryClient: remotefetch.New,
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

	var migrationRecord preMigrationRecord
	if cfg.AutoMigrate {
		var err error
		if migrationRecord, err = beforeMigration(ctx, cfg, log, sc); err != nil {
			return nil, nil, err
		}
		if err := migrate.Run(ctx, sc); err != nil {
			return nil, nil, err
		}
	}
	// Always verify exact schema match — with auto-migrate off this catches
	// pending migrations; in both modes it catches a database migrated by a
	// newer binary (Run applies nothing there and the schema stays ahead).
	if err := migrate.Check(ctx, sc); err != nil {
		return nil, nil, err
	}
	recordPreMigration(ctx, cfg, log, migrationRecord)

	root, err := resolveRootKey(cfg, log)
	if err != nil {
		return nil, nil, err
	}
	db, err := resources.openDatabase(ctx, sc)
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
	sc := storeConfig(cfg)

	// Every resource acquired below has one temporary owner — this guard —
	// until the Server takes over. Armed by default; disarmed only at the
	// ownership transfer immediately before the success return.
	guard := &bootGuard{log: log}
	defer guard.cleanup()

	db, kr, err := openKeyed(ctx, cfg, log, sc, resources, guard)
	if err != nil {
		return nil, fmt.Errorf("boot: refusing to serve: %w", err)
	}

	// The secret-scanning ruleset compiles once at boot; a Load error refuses to
	// serve (#74, ADR §7 fail-fast — a binary that ships a half-compiled ruleset
	// is a scanner that silently is not one).
	ruleset, err := scanning.Load()
	if err != nil {
		return nil, fmt.Errorf("boot: refusing to serve: secret-scanning ruleset: %w", err)
	}

	kdf, limiter, err := AuthComponents(cfg)
	if err != nil {
		return nil, fmt.Errorf("boot: refusing to serve: %w", err)
	}
	// The advisory channel is in-process fan-out: one per server, wired into
	// every surface that announces a change.
	advisory := service.NewAdvisory()
	// The expensive-path budget (ops-spec § 179 / § 20 / § 151): one per server,
	// in-memory like admission, wired into every surface that owns a named
	// expensive category — export, publish, adapter sync, machine fetch, and
	// schema revision.
	budget := serviceBudget(cfg)
	authSvc := &service.Auth{DB: db, Keyring: kr, KDF: kdf, Admission: limiter, Log: log, ExternalOrigin: cfg.ExternalOrigin, ReauthWindow: cfg.ReauthWindow}
	samlProviders := service.NewSAMLProviders(db, kr, cfg.ExternalOrigin)
	// RP ID + expected origins are immutable instance config derived from the
	// configured external origin, never a request header (human-auth ADR §5). An
	// origin that cannot yield a valid relying party is a boot refusal, not a
	// first-ceremony surprise.
	if err := authSvc.ConfigureWebAuthnRP(); err != nil {
		return nil, fmt.Errorf("boot: refusing to serve: webauthn relying party: %w", err)
	}
	workspaceSvc := &service.Workspace{DB: db, Version: Version, Reauth: authSvc}
	if err := workspaceSvc.PrimeOriginAllowlist(ctx); err != nil {
		return nil, fmt.Errorf("boot: refusing to serve: workspace origin allowlist: %w", err)
	}

	proxies, err := parseCIDRs(cfg.TrustedProxyCIDRs)
	if err != nil {
		return nil, fmt.Errorf("boot: refusing to serve: %w", err)
	}
	if err := resources.warmOpenAPI(); err != nil {
		return nil, fmt.Errorf("boot: refusing to serve: OpenAPI contract: %w", err)
	}

	ln, err := resources.listen("tcp", cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("boot: listen %s: %w", cfg.Listen, err)
	}
	// Registered immediately after Listen so every later error path releases
	// it, and — being registered after the database — before the database.
	guard.add(func() error { return resources.closeListener(ln) })
	operationalLn, err := resources.listen("tcp", cfg.OperationalListen)
	if err != nil {
		return nil, fmt.Errorf("boot: operational listen %s: %w", cfg.OperationalListen, err)
	}
	guard.add(func() error { return resources.closeListener(operationalLn) })

	var tlsReloader *certReloader
	publicLn := ln
	if cfg.TLSCertFile != "" {
		tlsReloader, err = newCertReloader(cfg.TLSCertFile, cfg.TLSKeyFile, log, 10*time.Second)
		if err != nil {
			return nil, fmt.Errorf("boot: TLS certificate: %w", err)
		}
		publicLn = tls.NewListener(ln, tlsReloader.tlsConfig())
	}

	federation := &service.Federation{
		DB: db, Auth: authSvc, Admission: limiter,
		Cache: &oidcfed.Cache{Limiter: limiter},
	}
	scimSvc := &service.SCIM{DB: db, Auth: authSvc}
	fetchCfg := remotefetch.DefaultConfig()
	if cfg.DirectoryProxy != "" {
		// Explicit configuration is the ONLY way egress traverses a forward
		// proxy. config.Load has already refused a non-https or hostless value,
		// so a parse failure here would be an internal inconsistency rather
		// than operator input — it still fails the boot loudly rather than
		// silently reverting to direct egress, because "the proxy I configured
		// is being bypassed" is exactly the surprise this control exists to
		// prevent.
		proxy, err := url.Parse(cfg.DirectoryProxy)
		if err != nil {
			return nil, fmt.Errorf("boot: directory proxy: %w", err)
		}
		fetchCfg.Proxy = proxy
	}
	fetcher, err := resources.newDirectoryClient(fetchCfg)
	if err != nil {
		return nil, fmt.Errorf("boot: outbound directory client: %w", err)
	}
	retentionSvc := &service.Retention{DB: db, Backup: backupPolicy(cfg)}
	backupSvc := &service.Backup{DB: db, Options: backup.Options{Recipients: cfg.BackupRecipients}}
	approvalsSvc := &service.Approvals{DB: db, Auth: authSvc, Keyring: kr}
	updateHTTP, err := updatecheck.NewHTTPClient(3 * time.Second)
	if err != nil {
		return nil, fmt.Errorf("boot: update release client: %w", err)
	}
	updateSource, err := updatecheck.NewCachedSource(updatecheck.NewGitHubSource(updateHTTP), 6*time.Hour, nil)
	if err != nil {
		return nil, fmt.Errorf("boot: update release cache: %w", err)
	}
	var updateControl updater.Control
	if cfg.UpdaterSocket != "" {
		updateControl = updater.NewClient(cfg.UpdaterSocket)
	}
	reencryptSvc := &service.Reencrypt{DB: db, Keyring: kr, Budget: budget}
	adapterRuntime := store.NewAdapterRuntime(db, func(ctx context.Context, job adapter.Job, _ adapter.Effect) error {
		return tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			_, err := az.Authorize(ctx, authz.Identity{Principal: domain.PrincipalID(job.AuthorityPrincipal), Class: domain.ClassHuman}, authz.OpAdapterPush, domain.Scope{
				Org: domain.OrgID(job.OrgID), Project: domain.ProjectID(job.ProjectID), Env: domain.EnvID(job.EnvironmentID),
			})
			return err
		})
	})
	moduleWiring := newAdapterModuleWiring(cfg.AdapterEgressPolicy)
	if cfg.Dev && cfg.DevAdapterFakeProvider {
		// The browser flow suite's stand-in provider (#157): config.Load has
		// already refused this switch on anything but a --dev server.
		fake := newDevFakeProvider()
		moduleWiring = adapterModuleWiring{worker: fake.factory, service: fake.factory}
		log.Warn("deployment adapters use the in-process development fake provider; no provider is contacted")
	}
	adapterWorker := &adapter.Worker{
		Store: adapterRuntime, Loader: &adapterLoader{runtime: adapterRuntime, keyring: kr, moduleFactory: moduleWiring.worker},
		ID: "adapter-worker-" + uuid.Must(uuid.NewV7()).String(), Poll: time.Second, Log: log,
	}
	adapterService := &service.Adapters{DB: db, Auth: authSvc, Keyring: kr, Budget: budget, ModuleFactory: moduleWiring.service}
	definitionsService := &service.Definitions{DB: db, Keyring: kr, Advisory: advisory, Budget: budget, Scan: ruleset}
	dynamicRuntime := store.NewDynamicRuntime(db)
	dynamicService := &service.Dynamic{
		DB: db, Auth: authSvc, Keyring: kr, Budget: budget, Runtime: dynamicRuntime,
		ProviderFactory: newDynamicFactory(cfg.DynamicEgressPolicy), LeaseDeadline: dynamicProviderDeadline,
	}

	updatesService := &service.Updates{DB: db, Source: updateSource, Version: Version, Channel: updatecheck.Channel(cfg.UpdateChannel), Control: updateControl, Log: log}
	// One RED collector shared by the API middleware (writer) and the
	// operational /metrics handler (reader) (#513). The limiter supplies its
	// admission-pressure gauges at scrape time.
	metrics := server.NewMetrics(limiter)
	// Secret-change approvals (#151): the two label-free approval gauges read
	// their counts at scrape time under scheduler authority (#151, mirroring the
	// storage high-water gauge's shared-door read).
	metrics.SetApprovalSource(approvalMetricsSource{svc: approvalsSvc})
	metrics.SetDynamicSource(dynamicGaugeSource{runtime: dynamicRuntime, log: log})
	api := &server.API{
		Auth:     authSvc,
		SAMLAuth: authSvc,
		Orgs:     &service.Orgs{DB: db},
		Projects: &service.Projects{DB: db},
		// The keyring reaches the value surface (#50): clone-at-creation and
		// every value write re-seal under the project data key, in the
		// transaction that writes the row. The ruleset (#74) reaches every
		// surface that writes a config value or a declaration leaf.
		Environments: &service.Environments{DB: db, Keyring: kr, Auth: authSvc, Advisory: advisory, Budget: budget, Scan: ruleset},
		Folders:      &service.Folders{DB: db, Keyring: kr, Scan: ruleset},
		Keys:         &service.Keys{DB: db, Keyring: kr, Advisory: advisory, Budget: budget, Scan: ruleset},
		Definitions:  definitionsService,
		// The reveal ceremony (#58): the value surface's disclosure routes
		// consume the SAME reauthentication window machinery the passkey and
		// TOTP reauth endpoints open, so both sides take the one Auth. A
		// Values without it refuses every disclosure rather than disclosing
		// without a ceremony.
		// One Advisory across the value and revision surfaces: staging and
		// publishing both announce on the same channel, and two channels would
		// mean a subscriber saw half the events.
		Values:    &service.Values{DB: db, Keyring: kr, Auth: authSvc, Advisory: advisory, Scan: ruleset, Budget: budget},
		Revisions: &service.Revisions{DB: db, Keyring: kr, Auth: authSvc, Advisory: advisory, Budget: budget},
		Rotation:  &service.Rotation{DB: db, Keyring: kr, RootKey: rootKeySource{cfg: cfg, log: log}, Budget: budget},
		Reencrypt: reencryptSvc,
		Pins:      &service.Pins{DB: db, Keyring: kr, Auth: authSvc},
		Reveal:    &service.Reveal{DB: db, Auth: authSvc},
		KeyGroups: &service.KeyGroups{DB: db, Keyring: kr, Advisory: advisory, Budget: budget, Scan: ruleset},
		// One Auth across the grant surface, the settings knob and the machine
		// identity surface: the reauthentication conjunct a machine widening
		// carries is the SAME window machinery human disclosure consumes, so
		// they cannot come from two configurations.
		Grants:     &service.Grants{DB: db, Auth: authSvc},
		Identities: &service.Identities{DB: db, Auth: authSvc},
		// One Federation across the issuer surface and the delivery surface, and
		// one JWKS cache inside it: the cache's staleness bound is an instance
		// property, so two caches would mean two answers to "are this issuer's
		// keys fresh". Its unknown-`kid` refresh rides the SAME admission limiter
		// every other pre-authentication path rides, which is what the ADR means
		// by putting the trigger under the instance-wide budget.
		Federation: federation,
		Delivery: &service.Delivery{
			DB: db, Keyring: kr, Federation: federation, Budget: budget,
		},
		// The settings knob calls LowerEffectiveWindow, which is the Auth
		// service's library — one Auth, so the window the knob writes and the
		// window the reveal guard reads cannot come from two configurations.
		Settings:        &service.ProjectSettings{DB: db, Auth: authSvc},
		Retention:       retentionSvc,
		RetentionHealth: retentionSvc,
		Updates:         updatesService,
		Providers:       &service.Providers{DB: db, Keyring: kr, ExternalOrigin: cfg.ExternalOrigin, Log: log},
		SAMLProviders:   samlProviders,
		Adapters:        adapterService,
		Dynamic:         dynamicService,
		Audits:          &service.Audits{DB: db, Budget: budget},
		Approvals:       approvalsSvc,
		// ONE SCIM service behind both surfaces: the administration verbs and
		// the identity provider's wire read the same bindings, the same mapping
		// table and the same bounds. Two instances would let the wire clamp a
		// page against a different number than the one the discovery document
		// advertises.
		SCIM:     scimSvc,
		SCIMWire: scimSvc,
		// Multi-instance (#71). The outbound client is built from the
		// owner-ratified bounds table (2026-08-13) and is the ONLY door to a
		// foreign instance: with zero configured remotes it originates zero
		// connections, which is what leaves the air-gap posture unchanged.
		Remotes:        &service.Remotes{DB: db, Keyring: kr, Fetch: fetcher},
		Workspace:      workspaceSvc,
		Admission:      limiter,
		Metrics:        metrics,
		Version:        Version,
		Log:            log,
		TrustedProxies: proxies,
	}

	log.Info("boot complete", "version", Version, "engine", sc.Engine, "external_origin", cfg.ExternalOrigin,
		"addr", ln.Addr().String(), "operational_addr", operationalLn.Addr().String(), "dev", cfg.Dev, "update_channel", cfg.UpdateChannel,
		"argon2_memory_kib", cfg.Argon2MemoryKiB, "auth_concurrency", limiter.Concurrency())
	// The operational readiness check is the datastore-and-schema probe, plus
	// an optional HA lease-datastore probe attached below when HA is enabled so
	// /readyz fails closed if the lease table becomes unreachable.
	readyChk := &readyChecker{base: &service.System{DB: db, Store: sc}}
	// Construct the complete owner before disarming: future fallible work added
	// to construction stays inside the guard's protection.
	srv := &Server{
		Addr:            ln.Addr().String(),
		OperationalAddr: operationalLn.Addr().String(),
		db:              db,
		keyring:         kr,
		publicLn:        publicLn,
		operationalLn:   operationalLn,
		publicHandler: server.NewPublic(&service.System{DB: db, Store: sc}, api, webui.Assets(), server.PublicOptions{
			HSTS:           config.EmitHSTS(cfg.ExternalOrigin),
			ExternalOrigin: cfg.ExternalOrigin,
		}),
		operationalHandler: server.NewOperational(readyChk, operationalHealth{retention: retentionSvc, tls: tlsReloader}, metrics),
		tlsReloader:        tlsReloader,
		log:                log,
		scheduler: &Scheduler{Log: log, Jobs: []ScheduledJob{{
			Name: "payload_gc",
			Run: func(ctx context.Context) error {
				_, err := retentionSvc.Sweep(ctx)
				return err
			},
			LastSuccess: retentionSvc.LastPruneSuccess,
		}, {
			// Secret-change approvals (#151): resolve requests past their expiry
			// across all tenants, fail closed, and emit a per-request expiry
			// event. Idempotent and cross-tenant, like payload_gc beside it.
			Name: "approval_expiry_sweep",
			Run:  approvalsSvc.ExpireDue,
		}, {
			// Read-only operator nudge (#75/#187, scheduler option A): warn when a
			// scope still carries a retiring DEK version so an operator runs
			// `reencrypt`. It writes nothing and holds no write grant on any
			// ciphertext table — reencrypt itself stays an operator act.
			Name: "reencrypt_retiring_sweep",
			Run: func(ctx context.Context) error {
				scopes, err := reencryptSvc.SweepRetiring(ctx)
				if err != nil {
					return err
				}
				for _, sc := range scopes {
					log.Warn("DEK scope has a retiring version awaiting reencrypt",
						"purpose", sc.Purpose, "org", sc.OrgID, "project", sc.ProjectID,
						"openable_versions", sc.OpenableVersions)
				}
				return nil
			},
		}}},
		adapterWorker:    adapterWorker,
		dynamicWorker:    &dynamicWorker{svc: dynamicService, id: "dynamic-worker-" + uuid.Must(uuid.NewV7()).String(), log: log},
		updateReconciler: updatesService,
	}
	if cfg.BackupScheduled() {
		srv.scheduler.Jobs = append(srv.scheduler.Jobs, backupJobs(cfg, log, backupSvc)...)
	}
	if cfg.HA {
		coord, onTick, status, err := configureHA(ctx, cfg, log, db, sc, kr)
		if err != nil {
			return nil, fmt.Errorf("boot: refusing to serve: %w", err)
		}
		srv.scheduler.Lease = coord
		srv.scheduler.NodeID = cfg.NodeID
		srv.scheduler.OnTick = onTick
		status.leader = srv.scheduler.IsLeader
		metrics.SetHASource(status)
		readyChk.setHAProbe(haReadinessProbe(coord))
		// Revalidate cached project DEKs per fetch so a rotate-dek on another
		// node cannot leave this node fencing every write or missing records at
		// the new version.
		kr.SetHAFreshness(true)
		// Share the pre-authentication counters installation-wide so node
		// hopping cannot bypass a per-IP, per-account, or per-issuer limit. The
		// concurrency semaphore stays per node. Wired before the listener
		// accepts, so the limiter is not yet serving concurrently.
		limiter.UseShared(coord, log)
	}
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
	if cfg.Dev && cfg.DevServiceBudgetsDisabled {
		return nil
	}
	return service.NewBudget()
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

// newHTTPServer applies the baseline slow-client hardening: bounded header
// read, request read, idle keep-alive, and header size. WriteTimeout stays
// deliberately unset — long-lived streamed responses (SSE) arrive later.
// Tuned values belong to the ops spec.
func newHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
}

// Serve blocks until ctx is cancelled, then shuts down gracefully.
func (s *Server) Serve(ctx context.Context) error {
	return s.serve(ctx, nil)
}

// ServeWithReady behaves like Serve and calls ready after both HTTP serving
// goroutines have started. The command uses it to present an accurate startup
// summary without teaching the application package about terminal output.
func (s *Server) ServeWithReady(ctx context.Context, ready func()) error {
	return s.serve(ctx, ready)
}

func (s *Server) serve(ctx context.Context, ready func()) error {
	defer s.db.Close()
	schedulerCtx, stopScheduler := context.WithCancel(ctx)
	var schedulerDone chan struct{}
	if s.scheduler != nil {
		schedulerDone = make(chan struct{})
		go func() {
			defer close(schedulerDone)
			s.scheduler.Run(schedulerCtx)
		}()
	}
	defer func() {
		stopScheduler()
		if schedulerDone != nil {
			<-schedulerDone
		}
	}()
	updateCtx, stopUpdates := context.WithCancel(ctx)
	var updateDone chan struct{}
	if s.updateReconciler != nil {
		updateDone = make(chan struct{})
		go func() {
			defer close(updateDone)
			s.updateReconciler.Run(updateCtx)
		}()
	}
	defer func() {
		stopUpdates()
		if updateDone != nil {
			<-updateDone
		}
	}()
	workerCtx, stopWorker := context.WithCancel(ctx)
	var workerDone chan struct{}
	if s.adapterWorker != nil {
		workerDone = make(chan struct{})
		go func() {
			defer close(workerDone)
			s.adapterWorker.Run(workerCtx)
		}()
	}
	var dynamicWorkerDone chan struct{}
	if s.dynamicWorker != nil {
		dynamicWorkerDone = make(chan struct{})
		go func() {
			defer close(dynamicWorkerDone)
			s.dynamicWorker.Run(workerCtx)
		}()
	}
	defer func() {
		stopWorker()
		if workerDone != nil {
			<-workerDone
		}
		if dynamicWorkerDone != nil {
			<-dynamicWorkerDone
		}
	}()
	reloaderCtx, stopReloader := context.WithCancel(ctx)
	var reloaderDone chan struct{}
	if s.tlsReloader != nil {
		reloaderDone = make(chan struct{})
		go func() {
			defer close(reloaderDone)
			s.tlsReloader.run(reloaderCtx)
		}()
	}
	defer func() {
		stopReloader()
		if reloaderDone != nil {
			<-reloaderDone
		}
	}()

	publicServer := newHTTPServer(s.publicHandler)
	operationalServer := newHTTPServer(s.operationalHandler)
	errCh := make(chan error, 2)
	var serveWG sync.WaitGroup
	serveWG.Add(2)
	go func() {
		defer serveWG.Done()
		errCh <- publicServer.Serve(s.publicLn)
	}()
	go func() {
		defer serveWG.Done()
		errCh <- operationalServer.Serve(s.operationalLn)
	}()
	addr, operationalAddr := s.Addr, s.OperationalAddr
	s.log.Info("server ready", "version", Version, "addr", addr, "operational_addr", operationalAddr)
	if ready != nil {
		ready()
	}

	var serveErr error
	select {
	case serveErr = <-errCh:
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	shutdownErr := errors.Join(publicServer.Shutdown(shutdownCtx), operationalServer.Shutdown(shutdownCtx))
	cancel()
	serveWG.Wait()
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return shutdownErr
}

// ReloadTLS reloads the configured pair immediately, as used by SIGHUP.
func (s *Server) ReloadTLS() error {
	if s.tlsReloader == nil {
		return nil
	}
	return s.tlsReloader.reload()
}

// Close releases resources for a booted server that never served.
func (s *Server) Close() error {
	return errors.Join(s.publicLn.Close(), s.operationalLn.Close(), s.db.Close())
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
