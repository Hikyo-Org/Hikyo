package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/config"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/federationhttp"
	"github.com/Hikyo-Org/hikyo/internal/mcpserver"
	"github.com/Hikyo-Org/hikyo/internal/oidcfed"
	"github.com/Hikyo-Org/hikyo/internal/remotefetch"
	"github.com/Hikyo-Org/hikyo/internal/scanning"
	"github.com/Hikyo-Org/hikyo/internal/server"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/storagehealth"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/Hikyo-Org/hikyo/internal/updatecheck"
	"github.com/Hikyo-Org/hikyo/internal/webui"
	"github.com/google/uuid"
)

// applicationGeneration owns only replaceable services. Database, keyring,
// listeners and the configuration worker belong to the containing server.
type applicationGeneration struct {
	cfg                               *config.Config
	auth                              *service.Auth
	limiter                           *admission.Limiter
	retention                         *service.Retention
	certificate                       *managedCertificate
	closeIdleConnections              func()
	publicHandler, operationalHandler http.Handler
	scheduler                         *Scheduler
	adapterWorker                     *adapter.Worker
	dynamicWorker                     *dynamicWorker
	updateReconciler                  *service.Updates
}

// prepareGeneration constructs the graph without serving, starting workers,
// binding listeners or registering HA membership. Every fallible constructor
// completes before the active generation can be retired.
func (owner *ownerRuntime) prepareGeneration(ctx context.Context, cfg *config.Config) (*applicationGeneration, error) {
	certificate, err := newManagedCertificate(cfg.TLSCertPEM, cfg.TLSKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("managed TLS certificate: %w", err)
	}
	db, kr, log, resources := owner.server.db, owner.server.keyring, owner.server.log, owner.resources
	sc := storeConfig(cfg)
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
	advisory := owner.advisory
	// The expensive-path budget (ops-spec § 179 / § 20 / § 151): one per server,
	// in-memory like admission, wired into every surface that owns a named
	// expensive category — export, publish, adapter sync, machine fetch, and
	// schema revision.
	budget := owner.budget
	federationPolicy := federationhttp.Policy{AllowedCIDRs: cfg.OIDCEgressPolicy, Development: cfg.Dev}
	authSvc := &service.Auth{
		DB: db, Keyring: kr, KDF: kdf, Admission: limiter, Log: log,
		ExternalOrigin: cfg.ExternalOrigin, ReauthWindow: cfg.ReauthWindow,
		FederationPolicy: federationPolicy,
	}
	selfConfig := owner.selfConfig
	authSvc.SelfConfig = selfConfig
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
	diagnostics := &service.Diagnostics{Passwords: &kdf}
	if cfg.Store.Engine == config.EngineSQLite {
		diagnostics.Volume = func() (storagehealth.Capacity, error) { return storagehealth.Read(filepath.Dir(cfg.Store.Path)) }
	}
	retentionSvc := &service.Retention{DB: db, AuditPolicy: store.AuditRetentionPolicy{AccessDays: cfg.AuditAccessRetainDays, SecurityDays: cfg.AuditSecurityRetainDays}, Backup: backupPolicy(cfg), Diagnostics: diagnostics}
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
	reencryptSvc := &service.Reencrypt{DB: db, Keyring: kr, Budget: budget}
	adapterRuntime := store.NewAdapterRuntime(db, func(ctx context.Context, job adapter.Job, _ adapter.Effect) error {
		return tx.Read(ctx, db, func(ctx context.Context, _ store.ReadRepos, az *authz.TxAuthorizer) error {
			_, err := az.Authorize(ctx, authz.Identity{Principal: domain.PrincipalID(job.AuthorityPrincipal), Class: domain.ClassHuman}, authz.OpAdapterPush, domain.Scope{
				Org: domain.OrgID(job.OrgID), Project: domain.ProjectID(job.ProjectID), Env: domain.EnvID(job.EnvironmentID),
			})
			return err
		})
	})
	var moduleFactory adapter.ModuleFactory = newAdapterModuleFactory(cfg.AdapterEgressPolicy).Build
	if cfg.Dev && cfg.DevAdapterFakeProvider {
		// The browser flow suite's stand-in provider (#157): config.Load has
		// already refused this switch on anything but a --dev server.
		fake := newDevFakeProvider()
		moduleFactory = fake.factory
		log.Warn("deployment adapters use the in-process development fake provider; no provider is contacted")
	}
	adapterWorker := &adapter.Worker{
		Store: &configurationAdapterStore{AdapterRuntime: adapterRuntime, selfConfig: selfConfig}, Loader: &adapterLoader{runtime: adapterRuntime, keyring: kr, moduleFactory: moduleFactory},
		ID: "adapter-worker-" + uuid.Must(uuid.NewV7()).String(), Poll: time.Second, Log: log,
	}
	adapterService := &service.Adapters{DB: db, Auth: authSvc, Keyring: kr, Budget: budget, ModuleFactory: moduleFactory}
	definitionsService := &service.Definitions{DB: db, Keyring: kr, Advisory: advisory, Budget: budget, Scan: ruleset}
	dynamicRuntime := store.NewDynamicRuntime(db)
	dynamicService := &service.Dynamic{
		DB: db, Auth: authSvc, Keyring: kr, Budget: budget, Runtime: dynamicRuntime,
		ProviderFactory: newDynamicFactory(cfg.DynamicEgressPolicy), LeaseDeadline: dynamicProviderDeadline,
	}

	updatesService := &service.Updates{DB: db, Source: updateSource, Version: Version, Channel: updatecheck.Channel(cfg.UpdateChannel), Log: log, SelfConfig: selfConfig}
	// One RED collector shared by the API middleware (writer) and the
	// operational /metrics handler (reader) (#513). The limiter supplies its
	// admission-pressure gauges at scrape time.
	metrics := server.NewMetrics(limiter)
	// Secret-change approvals (#151): the two label-free approval gauges read
	// their counts at scrape time under scheduler authority (#151, mirroring the
	// storage high-water gauge's shared-door read).
	metrics.SetApprovalSource(approvalMetricsSource{svc: approvalsSvc})
	metrics.SetDynamicSource(dynamicGaugeSource{runtime: dynamicRuntime, log: log})
	// The hierarchy, value, and revision services are named here so the read-only
	// MCP tools (#629) map onto the SAME instances the REST surface uses: one
	// keyring, one budget, one authorization path.
	environmentsSvc := &service.Environments{DB: db, Keyring: kr, Auth: authSvc, Advisory: advisory, Budget: budget, Scan: ruleset}
	keysSvc := &service.Keys{DB: db, Keyring: kr, Advisory: advisory, Budget: budget, Scan: ruleset}
	valuesSvc := &service.Values{DB: db, Keyring: kr, Auth: authSvc, Advisory: advisory, Scan: ruleset, Budget: budget}
	revisionsSvc := &service.Revisions{DB: db, Keyring: kr, Auth: authSvc, Advisory: advisory, Budget: budget}
	api := &server.API{
		Auth:     authSvc,
		SAMLAuth: authSvc,
		Orgs:     &service.Orgs{DB: db},
		Projects: &service.Projects{DB: db},
		// The keyring reaches the value surface (#50): clone-at-creation and
		// every value write re-seal under the project data key, in the
		// transaction that writes the row. The ruleset (#74) reaches every
		// surface that writes a config value or a declaration leaf.
		Environments: environmentsSvc,
		Folders:      &service.Folders{DB: db, Keyring: kr, Scan: ruleset},
		Keys:         keysSvc,
		Definitions:  definitionsService,
		// The reveal ceremony (#58): the value surface's disclosure routes
		// consume the SAME reauthentication window machinery the passkey and
		// TOTP reauth endpoints open, so both sides take the one Auth. A
		// Values without it refuses every disclosure rather than disclosing
		// without a ceremony.
		// One Advisory across the value and revision surfaces: staging and
		// publishing both announce on the same channel, and two channels would
		// mean a subscriber saw half the events.
		Values:    valuesSvc,
		Revisions: revisionsSvc,
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
		Discovery:       &service.Discovery{DB: db},
		Settings:        &service.ProjectSettings{DB: db, Auth: authSvc},
		Retention:       retentionSvc,
		RetentionHealth: retentionSvc,
		Updates:         updatesService,
		SelfConfig:      selfConfig,
		Providers: &service.Providers{
			DB: db, Keyring: kr, ExternalOrigin: cfg.ExternalOrigin, Log: log,
			FederationPolicy: federationPolicy,
		},
		SAMLProviders: samlProviders,
		Adapters:      adapterService,
		Dynamic:       dynamicService,
		Audits:        &service.Audits{DB: db, Budget: budget},
		Approvals:     approvalsSvc,
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

	var mcpHandler http.Handler
	if cfg.MCPEnabled {
		registry := mcpserver.NewRegistry()
		if err := mcpserver.RegisterProductionTools(registry, mcpserver.ProductionServices{
			Admission:     &service.MCPAdmission{DB: db},
			Definitions:   keysSvc,
			Environments:  environmentsSvc,
			Configuration: valuesSvc,
			Pending:       revisionsSvc,
			Revisions:     revisionsSvc,
		}); err != nil {
			return nil, fmt.Errorf("boot: refusing to serve: MCP tools: %w", err)
		}
		cursorSealer, err := kr.MCPCursorSealer()
		if err != nil {
			return nil, fmt.Errorf("boot: refusing to serve: MCP cursor sealer: %w", err)
		}
		mcpHandler, err = mcpserver.New(mcpserver.Options{
			Registry:       registry,
			ExternalOrigin: cfg.ExternalOrigin,
			AllowedOrigins: cfg.MCPAllowedOrigins,
			TrustedProxies: proxies,
			Admission:      limiter,
			Version:        Version,
			CursorSealer:   cursorSealer,
		})
		if err != nil {
			return nil, fmt.Errorf("boot: refusing to serve: MCP transport: %w", err)
		}
		mcpHandler = metrics.ObserveMCP(mcpHandler, log, mcpserver.ProductionToolNames())
		mcpHandler = requireMCPRuntime(selfConfig, mcpHandler)
	}

	// The operational readiness check is the datastore-and-schema probe, plus
	// an optional HA lease-datastore probe attached below when HA is enabled so
	// /readyz fails closed if the lease table becomes unreachable.
	readyChk := &readyChecker{base: &service.System{DB: db, Store: sc}, selfConfig: selfConfig}
	// Construct the complete owner before disarming: future fallible work added
	// to construction stays inside the guard's protection.
	srv := &applicationGeneration{
		cfg:                  cfg,
		auth:                 authSvc,
		limiter:              limiter,
		retention:            retentionSvc,
		certificate:          certificate,
		closeIdleConnections: updateHTTP.CloseIdleConnections,
		publicHandler: server.NewPublic(&service.System{DB: db, Store: sc}, api, webui.Assets(), server.PublicOptions{
			HSTS:           config.EmitHSTS(cfg.ExternalOrigin),
			ExternalOrigin: cfg.ExternalOrigin,
			MCP:            mcpHandler,
		}),
		operationalHandler: server.NewOperational(readyChk, operationalHealth{retention: retentionSvc, tls: certificate}, metrics),
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
		dynamicWorker:    &dynamicWorker{svc: dynamicService, id: "dynamic-worker-" + uuid.Must(uuid.NewV7()).String(), log: log, selfConfig: selfConfig},
		updateReconciler: updatesService,
	}
	if cfg.BackupScheduled() {
		srv.scheduler.Jobs = append(srv.scheduler.Jobs, backupJobs(cfg, log, backupSvc)...)
	}
	for i := range srv.scheduler.Jobs {
		run := srv.scheduler.Jobs[i].Run
		srv.scheduler.Jobs[i].Run = func(ctx context.Context) error {
			if _, err := selfConfig.Capture(ctx); err != nil {
				return err
			}
			return run(ctx)
		}
	}
	if owner.haCoord != nil {
		srv.scheduler.Lease = owner.haCoord
		srv.scheduler.NodeID = cfg.NodeID
		srv.scheduler.OnTick = owner.haTick
		metrics.SetHASource(&generationHAStatus{status: owner.haStatus, scheduler: srv.scheduler})
		readyChk.setHAProbe(haReadinessProbe(owner.haCoord))
		limiter.UseShared(owner.haCoord, log)
	}
	return srv, nil
}

func requireMCPRuntime(selfConfig *service.SelfConfig, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := selfConfig.Capture(r.Context()); err != nil {
			w.Header().Set("Retry-After", "2")
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Existing claims may complete under their captured graph. A stale graph must
// not admit another job after the durable configuration generation changes.
type configurationAdapterStore struct {
	*store.AdapterRuntime
	selfConfig *service.SelfConfig
}

func (s *configurationAdapterStore) ClaimDue(ctx context.Context, worker string, now, deadline time.Time) (adapter.Job, bool, error) {
	if _, err := s.selfConfig.Capture(ctx); err != nil {
		return adapter.Job{}, false, err
	}
	return s.AdapterRuntime.ClaimDue(ctx, worker, now, deadline)
}
