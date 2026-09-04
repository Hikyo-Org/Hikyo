// Package config parses flags and environment strictly and fail-fast
// (system-architecture ADR § Tooling defaults): unknown HIKYO_* keys warn,
// missing prod-critical keys refuse to start, nothing silently conjures a
// database.
package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Engine string

const (
	EngineSQLite   Engine = "sqlite"
	EnginePostgres Engine = "postgres"
)

// Datastore is the parsed, validated datastore selection.
type Datastore struct {
	Engine          Engine
	Path            string // sqlite file path
	DSN             string // postgres DSN
	PostgresPoolMax int32  // HIKYO_PG_POOL_MAX override; zero uses DSN/locked default
}

type Config struct {
	Dev               bool
	Listen            string
	OperationalListen string
	TLSCertFile       string
	TLSKeyFile        string
	TrustedProxyCIDRs []string
	AutoMigrate       bool
	Store             Datastore

	// DirectoryProxy is the OPTIONAL forward proxy the outbound directory
	// client tunnels through, from HIKYO_DIRECTORY_PROXY. Empty is the default
	// and means direct egress.
	//
	// It is EXPLICIT CONFIGURATION and nothing else: http.ProxyFromEnvironment
	// is deliberately not consulted anywhere, because ambient HTTP_PROXY
	// discovery would let a process's environment redirect authenticated fleet
	// traffic. https only — the CONNECT request names the remote host, so a
	// plaintext proxy publishes the fleet topology to the path.
	DirectoryProxy string

	// AdapterEgressPolicy is loaded once at startup from the operator-owned
	// policy file. Entries are keyed by an exact canonical HTTPS origin; a
	// private address is usable only for the origin whose entry contains it.
	AdapterEgressPolicy map[string][]netip.Prefix

	// DynamicEgressPolicy is the dynamic-secret (#147) equivalent, keyed by an
	// exact postgres:// origin. A self-hosted PostgreSQL target is normally on a
	// private address, so without an entry the default-deny public-egress rule
	// refuses it; this is the explicit operator allow-list that permits it.
	DynamicEgressPolicy map[string][]netip.Prefix

	// ExternalOrigin is the instance's public origin (scheme + host), used to
	// build per-provider OIDC redirect URIs (A1). Never derived from a request
	// header. Defaults to http://<Listen> when unset.
	ExternalOrigin string

	// MCPEnabled gates the separately versioned MCP protocol endpoint. It is
	// off unless the operator explicitly enables it. MCPAllowedOrigins is the
	// exact allowlist for requests that carry a browser Origin header; an empty
	// list still permits non-browser clients that omit Origin.
	MCPEnabled        bool
	MCPAllowedOrigins []string

	// Root-key source descriptor — never the key material itself; the crypto
	// package reads and validates it at boot. Only `hikyo server` consults it.
	RootKeyFile    string // --root-key-file (also covers systemd LoadCredential paths)
	RootKeyFromEnv bool   // HIKYO_ROOT_KEY is set (documented weakest tier)
	// NewRootKeyFile is the NEW root source consulted only by
	// `rotate-root-key --prepare` (HIKYO_NEW_ROOT_KEY_FILE). Empty means no
	// rotation is configured, and prepare refuses rather than reading the wire —
	// no root key material ever crosses the API.
	NewRootKeyFile string

	// Auth tuning. The Argon2id parameters may be raised for stronger
	// hardware and never lowered: boot verifies them against the floor the
	// human-auth ADR fixes and refuses to start below it, rather than
	// degrading quietly. AdmissionBudgetMiB derives the pre-authentication
	// concurrency, so raising the KDF memory lowers concurrency automatically
	// instead of silently doubling the memory bill.
	Argon2MemoryKiB    uint32
	Argon2Time         uint32
	Argon2Parallelism  uint8
	AdmissionBudgetMiB int

	// Backup export configuration (#76, ops spec section 11). Recipients are
	// PUBLIC age recipients; the private backup identity never touches this
	// process, this configuration or the datastore — it is a separate custody
	// store from the root key by requirement, not by convention.
	//
	// Dir is where automatic exports are published. It is REQUIRED when
	// recipients are configured: an export policy with no destination is a
	// backup that silently goes nowhere, which is the failure mode the loud
	// skip exists to prevent.
	BackupRecipients []string
	BackupDir        string
	// Disaster-recovery schedule (#145, ops spec section 11). Scheduling is
	// enabled exactly when recipients and a destination are configured
	// (BackupScheduled); the knobs below then bound it. Each has a default
	// and a range, and a value outside the range is a startup error rather
	// than a clamp: a retention of 400 days silently becoming 180 is the
	// kind of quiet correction the ops spec forbids.
	BackupInterval    time.Duration // HIKYO_BACKUP_INTERVAL, default 24h, minimum 1h
	BackupRPO         time.Duration // HIKYO_BACKUP_RPO, default 26h, at least the interval
	BackupRetainCount int           // HIKYO_BACKUP_RETAIN_COUNT, default 7, minimum 1
	BackupRetainDays  int           // HIKYO_BACKUP_RETAIN_DAYS, default 180, maximum 180
	BackupRTOTarget   time.Duration // HIKYO_BACKUP_RTO_TARGET, default 30m; the drill's verdict line

	// DevAdmissionPerIPPerMinute raises the per-source-IP pre-auth allowance.
	// Zero means the locked default.
	//
	// It exists for one caller — the browser flow suite, which drives every
	// login of every flow from one loopback address, a traffic shape the
	// default is deliberately not sized for. It is refused outside development
	// mode (see Load), so no production instance can have its ceiling raised
	// by an environment variable, and the key name says so out loud for anyone
	// who copies it into a compose file.
	DevAdmissionPerIPPerMinute int
	// DevServiceBudgetsDisabled disables the authenticated, in-memory expensive
	// operation budgets for a development server. It exists for the browser flow
	// suite, whose scenarios intentionally reuse one principal while exercising
	// more than the production publish allowance. Load refuses the override
	// outside --dev; false keeps the production budget enabled.
	DevServiceBudgetsDisabled bool
	// DevAdapterFakeProvider replaces the deployment-adapter provider modules
	// with an in-process, in-memory stand-in (#157). It exists for the browser
	// flow suite, which cannot reach a real HTTPS provider from inside the
	// harness; every other part of the adapter path (ceremonies, outbox,
	// ledger, INTENT/OUTCOME journaling, audit) stays real. Load refuses the
	// override outside --dev.
	DevAdapterFakeProvider bool
	// ReauthWindow is the instance-default disclosure reauthentication
	// window (human-auth ADR section Assurance; permission-model ADR's
	// per-environment knob inherits it). Zero - the production default - means
	// every disclosure takes its own ceremony, which only WebAuthn can honour;
	// a non-zero window lets TOTP open a sliding window. --dev sets 15 minutes
	// when the key is absent so an evaluation instance can reveal with an
	// authenticator alone.
	ReauthWindow time.Duration
	// UpdateChannel is this server installation's release notification track.
	UpdateChannel string
	// UpdaterSocket is the optional Unix socket of the separately privileged
	// local updater helper. Empty keeps update checks notification-only.
	UpdaterSocket string

	// HA enables multi-node application-tier high availability (#146): the
	// scheduler runs singleton work under a fenced datastore lease, admission
	// counters are shared across nodes, and this node registers in the live
	// node table. It requires PostgreSQL, a stable unique NodeID, and an
	// explicitly configured shared root-key authority. It is refused on sqlite
	// and refused when no root-key source is configured, at boot rather than
	// as a degraded single-node fallback.
	HA bool
	// NodeID is this node's stable unique identity under HA (HIKYO_NODE_ID; the
	// chart sets it from the pod name). Empty outside HA.
	NodeID string
}

// knownEnv is the closed set of HIKYO_* keys this build understands.
var knownEnv = map[string]bool{
	"HIKYO_DB":                         true,
	"HIKYO_PG_POOL_MAX":                true,
	"HIKYO_LISTEN":                     true,
	"HIKYO_OPERATIONAL_LISTEN":         true,
	"HIKYO_TLS_CERT_FILE":              true,
	"HIKYO_TLS_KEY_FILE":               true,
	"HIKYO_EXTERNAL_ORIGIN":            true,
	"HIKYO_MCP_ENABLED":                true,
	"HIKYO_MCP_ALLOWED_ORIGINS":        true,
	"HIKYO_TRUSTED_PROXY_CIDRS":        true,
	"HIKYO_ROOT_KEY":                   true,
	"HIKYO_NEW_ROOT_KEY_FILE":          true,
	"HIKYO_DIRECTORY_PROXY":            true,
	"HIKYO_ARGON2_MEMORY_KIB":          true,
	"HIKYO_ARGON2_TIME":                true,
	"HIKYO_ARGON2_PARALLELISM":         true,
	"HIKYO_ADMISSION_BUDGET_MIB":       true,
	"HIKYO_BACKUP_RECIPIENTS":          true,
	"HIKYO_BACKUP_DIR":                 true,
	"HIKYO_BACKUP_INTERVAL":            true,
	"HIKYO_BACKUP_RPO":                 true,
	"HIKYO_BACKUP_RETAIN_COUNT":        true,
	"HIKYO_BACKUP_RETAIN_DAYS":         true,
	"HIKYO_BACKUP_RTO_TARGET":          true,
	"HIKYO_ADAPTER_EGRESS_POLICY_FILE": true,
	"HIKYO_DYNAMIC_EGRESS_POLICY_FILE": true,
	"HIKYO_REAUTH_WINDOW_SECONDS":      true,
	"HIKYO_UPDATE_CHANNEL":             true,
	"HIKYO_UPDATER_SOCKET":             true,
	"HIKYO_HA":                         true,
	"HIKYO_NODE_ID":                    true,

	// Development-only. Named so the deployment it does not belong in is
	// obvious at a glance, and refused at boot outside --dev regardless.
	"HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE": true,
	"HIKYO_DEV_SERVICE_BUDGETS_DISABLED":    true,
	"HIKYO_DEV_ADAPTER_FAKE_PROVIDER":       true,

	// Client-side keys. They configure no server behaviour, but they are
	// listed here because the unknown-key warning is a typo detector: a
	// mistyped HIKYO_PROJEKT that produced no warning would silently target
	// the wrong project, which is the class of mistake the explicit-first
	// context model exists to prevent.
	"HIKYO_STATE_DIR":      true,
	"HIKYO_TRUST_BUNDLE":   true,
	"HIKYO_CONTEXT":        true,
	"HIKYO_INSTANCE":       true,
	"HIKYO_ORG":            true,
	"HIKYO_PROJECT":        true,
	"HIKYO_ENV":            true,
	"HIKYO_TOKEN":          true,
	"HIKYO_COMPOSE_DOCKER": true,
	"XDG_STATE_HOME":       true,
}

const devSQLitePath = "hikyo-dev.db"

// Load parses configuration for a subcommand. getenv supplies single keys;
// environ (os.Environ() shape) is scanned for unknown HIKYO_* keys and may be
// nil. Returned warnings are for the caller to log — Load itself never logs.
func Load(subcommand string, args []string, getenv func(string) string, environ []string) (*Config, []string, error) {
	fs := flag.NewFlagSet(subcommand, flag.ContinueOnError)
	dev := fs.Bool("dev", false, "development mode: zero-config sqlite, text logs")
	listen, operationalListen, tlsCertFile, tlsKeyFile, autoMigrate, rootKeyFile := new(string), new(string), new(string), new(string), new(bool), new(string)
	*autoMigrate = true
	if subcommand == "server" {
		listen = fs.String("listen", "", "listen address (default 127.0.0.1:8080, env HIKYO_LISTEN)")
		operationalListen = fs.String("operational-listen", "", "operational listen address (default 127.0.0.1:8081, env HIKYO_OPERATIONAL_LISTEN)")
		tlsCertFile = fs.String("tls-cert-file", "", "TLS certificate chain file (env HIKYO_TLS_CERT_FILE)")
		tlsKeyFile = fs.String("tls-key-file", "", "TLS private key file (env HIKYO_TLS_KEY_FILE)")
		autoMigrate = fs.Bool("auto-migrate", true, "apply pending migrations at boot")
		rootKeyFile = fs.String("root-key-file", "", "path to the 64-hex-char root key file (mode 0600)")
	}
	if err := fs.Parse(args); err != nil {
		return nil, nil, err
	}
	if rest := fs.Args(); len(rest) > 0 {
		return nil, nil, fmt.Errorf("unexpected argument %q", rest[0])
	}

	var warnings []string
	for _, kv := range environ {
		k, _, ok := strings.Cut(kv, "=")
		if ok && strings.HasPrefix(k, "HIKYO_") && !knownEnv[k] {
			warnings = append(warnings, fmt.Sprintf("unknown environment key %s ignored", k))
		}
	}

	cfg := &Config{
		Dev:               *dev,
		AutoMigrate:       *autoMigrate,
		Listen:            *listen,
		OperationalListen: *operationalListen,
		TLSCertFile:       *tlsCertFile,
		TLSKeyFile:        *tlsKeyFile,
		RootKeyFile:       *rootKeyFile,
		RootKeyFromEnv:    getenv("HIKYO_ROOT_KEY") != "",
		NewRootKeyFile:    getenv("HIKYO_NEW_ROOT_KEY_FILE"),
	}
	if subcommand == "server" {
		cfg.UpdaterSocket = strings.TrimSpace(getenv("HIKYO_UPDATER_SOCKET"))
		if cfg.UpdaterSocket != "" && !filepath.IsAbs(cfg.UpdaterSocket) {
			return nil, nil, fmt.Errorf("HIKYO_UPDATER_SOCKET: path must be absolute")
		}
	}
	updateChannel := getenv("HIKYO_UPDATE_CHANNEL")
	if updateChannel == "" {
		updateChannel = "stable"
	}
	updateChannel = strings.ToLower(strings.TrimSpace(updateChannel))
	switch updateChannel {
	case "stable", "nightly", "off":
		cfg.UpdateChannel = updateChannel
	default:
		return nil, nil, fmt.Errorf("HIKYO_UPDATE_CHANNEL: channel must be stable, nightly, or off, got %q", updateChannel)
	}
	if cfg.RootKeyFile != "" && cfg.RootKeyFromEnv {
		return nil, nil, fmt.Errorf("both --root-key-file and HIKYO_ROOT_KEY are set: configure exactly one root-key source")
	}
	if cfg.Listen == "" {
		cfg.Listen = getenv("HIKYO_LISTEN")
	}
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:8080"
	}
	if subcommand == "server" || subcommand == "admin" {
		var err error
		if cfg.Argon2MemoryKiB, err = uintEnv(getenv, "HIKYO_ARGON2_MEMORY_KIB", 64*1024); err != nil {
			return nil, nil, err
		}
		if cfg.Argon2Time, err = uintEnv(getenv, "HIKYO_ARGON2_TIME", 3); err != nil {
			return nil, nil, err
		}
		parallelism, err := uintEnv(getenv, "HIKYO_ARGON2_PARALLELISM", 2)
		if err != nil {
			return nil, nil, err
		}
		if parallelism > 255 {
			return nil, nil, fmt.Errorf("HIKYO_ARGON2_PARALLELISM: %d exceeds the 255 Argon2id allows", parallelism)
		}
		cfg.Argon2Parallelism = uint8(parallelism)
		reauthDefault := uint64(0)
		if cfg.Dev && getenv("HIKYO_REAUTH_WINDOW_SECONDS") == "" {
			reauthDefault = 900
		}
		reauthSeconds, err := uintEnv(getenv, "HIKYO_REAUTH_WINDOW_SECONDS", reauthDefault)
		if err != nil {
			return nil, nil, err
		}
		if reauthSeconds > 86400 {
			return nil, nil, fmt.Errorf("HIKYO_REAUTH_WINDOW_SECONDS: %d exceeds the 24h ceiling", reauthSeconds)
		}
		cfg.ReauthWindow = time.Duration(reauthSeconds) * time.Second
		budget, err := uintEnv(getenv, "HIKYO_ADMISSION_BUDGET_MIB", 272)
		if err != nil {
			return nil, nil, err
		}
		// Admission consumes an int in the limiter. Keep the serialized config
		// portable across architectures instead of allowing uint32-to-int wrap on
		// a 32-bit build.
		if budget > math.MaxInt32 {
			return nil, nil, fmt.Errorf(
				"HIKYO_ADMISSION_BUDGET_MIB: %d exceeds the largest portable integer", budget)
		}
		cfg.AdmissionBudgetMiB = int(budget)

		// The dev-only override. Fail-closed twice over: a non-dev process
		// refuses to start when it is set at all, and a malformed or
		// non-positive value is an error rather than a silent fallback to the
		// default — a typo in a security ceiling must not mean "use the
		// default", which is the same rule every other tunable here follows.
		if raw := strings.TrimSpace(getenv("HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE")); raw != "" {
			if !cfg.Dev {
				return nil, nil, fmt.Errorf(
					"HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE is a development-mode override and this is not a development server: " +
						"remove it, or pass --dev if this is an evaluation instance")
			}
			perIP, err := strconv.Atoi(raw)
			if err != nil || perIP < 1 {
				return nil, nil, fmt.Errorf("HIKYO_DEV_ADMISSION_PER_IP_PER_MINUTE: %q is not a positive integer", raw)
			}
			cfg.DevAdmissionPerIPPerMinute = perIP
		}
		if raw := strings.TrimSpace(getenv("HIKYO_DEV_SERVICE_BUDGETS_DISABLED")); raw != "" {
			if !cfg.Dev {
				return nil, nil, fmt.Errorf(
					"HIKYO_DEV_SERVICE_BUDGETS_DISABLED is a development-mode override and this is not a development server: " +
						"remove it, or pass --dev if this is an evaluation instance")
			}
			disabled, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, nil, fmt.Errorf("HIKYO_DEV_SERVICE_BUDGETS_DISABLED: %q is not a boolean", raw)
			}
			cfg.DevServiceBudgetsDisabled = disabled
		}
		if raw := strings.TrimSpace(getenv("HIKYO_DEV_ADAPTER_FAKE_PROVIDER")); raw != "" {
			if !cfg.Dev {
				return nil, nil, fmt.Errorf(
					"HIKYO_DEV_ADAPTER_FAKE_PROVIDER is a development-mode override and this is not a development server: " +
						"remove it, or pass --dev if this is an evaluation instance")
			}
			fake, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, nil, fmt.Errorf("HIKYO_DEV_ADAPTER_FAKE_PROVIDER: %q is not a boolean", raw)
			}
			cfg.DevAdapterFakeProvider = fake
		}
	}
	if subcommand == "server" {
		if cfg.OperationalListen == "" {
			cfg.OperationalListen = getenv("HIKYO_OPERATIONAL_LISTEN")
		}
		if cfg.OperationalListen == "" {
			cfg.OperationalListen = "127.0.0.1:8081"
		}
		if cfg.TLSCertFile == "" {
			cfg.TLSCertFile = getenv("HIKYO_TLS_CERT_FILE")
		}
		if cfg.TLSKeyFile == "" {
			cfg.TLSKeyFile = getenv("HIKYO_TLS_KEY_FILE")
		}
		if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
			return nil, nil, fmt.Errorf("HIKYO_TLS_CERT_FILE and HIKYO_TLS_KEY_FILE must be configured together")
		}
		if cfg.OperationalListen == cfg.Listen {
			return nil, nil, fmt.Errorf("operational listen %q must differ from public listen", cfg.OperationalListen)
		}
		trustedProxyCIDRs, err := parseTrustedProxyCIDRs(getenv("HIKYO_TRUSTED_PROXY_CIDRS"))
		if err != nil {
			return nil, nil, err
		}
		cfg.TrustedProxyCIDRs = trustedProxyCIDRs
		if !IsLoopbackListen(cfg.Listen) && cfg.TLSCertFile == "" && len(cfg.TrustedProxyCIDRs) == 0 {
			return nil, nil, fmt.Errorf("non-loopback plaintext listen %q requires HIKYO_TRUSTED_PROXY_CIDRS or a TLS certificate pair", cfg.Listen)
		}
		if raw := getenv("HIKYO_DIRECTORY_PROXY"); raw != "" {
			u, err := url.Parse(raw)
			if err != nil || u.Host == "" {
				return nil, nil, fmt.Errorf("HIKYO_DIRECTORY_PROXY: %q is not a URL naming a host", raw)
			}
			if u.Scheme != "https" {
				return nil, nil, fmt.Errorf("HIKYO_DIRECTORY_PROXY: %q must be https — a plaintext "+
					"forward proxy publishes which installations this one talks to", raw)
			}
			cfg.DirectoryProxy = raw
		}
		policy, err := loadAdapterEgressPolicy(getenv("HIKYO_ADAPTER_EGRESS_POLICY_FILE"))
		if err != nil {
			return nil, nil, err
		}
		cfg.AdapterEgressPolicy = policy
		dynamicPolicy, err := loadDynamicEgressPolicy(getenv("HIKYO_DYNAMIC_EGRESS_POLICY_FILE"))
		if err != nil {
			return nil, nil, err
		}
		cfg.DynamicEgressPolicy = dynamicPolicy
	}
	if cfg.ExternalOrigin == "" {
		cfg.ExternalOrigin = getenv("HIKYO_EXTERNAL_ORIGIN")
	}
	if cfg.ExternalOrigin == "" {
		scheme := "http://"
		if cfg.TLSCertFile != "" {
			scheme = "https://"
		}
		cfg.ExternalOrigin = scheme + cfg.Listen
	}
	origin, err := url.Parse(cfg.ExternalOrigin)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") ||
		origin.Host == "" || origin.User != nil || origin.Path != "" ||
		origin.RawQuery != "" || origin.Fragment != "" ||
		cfg.ExternalOrigin != origin.Scheme+"://"+origin.Host {
		return nil, nil, fmt.Errorf("HIKYO_EXTERNAL_ORIGIN must be an exact canonical HTTP(S) origin without credentials, path, query, or fragment")
	}
	if subcommand == "server" {
		rawEnabled := strings.TrimSpace(getenv("HIKYO_MCP_ENABLED"))
		if rawEnabled != "" {
			enabled, err := strconv.ParseBool(rawEnabled)
			if err != nil {
				return nil, nil, fmt.Errorf("HIKYO_MCP_ENABLED: %q is not a boolean", rawEnabled)
			}
			cfg.MCPEnabled = enabled
		}
		rawOrigins := strings.TrimSpace(getenv("HIKYO_MCP_ALLOWED_ORIGINS"))
		if rawOrigins != "" && !cfg.MCPEnabled {
			return nil, nil, errors.New("HIKYO_MCP_ALLOWED_ORIGINS requires HIKYO_MCP_ENABLED=true")
		}
		if cfg.MCPEnabled {
			if origin.Scheme != "https" && (!cfg.Dev || !isLoopbackHost(origin.Hostname())) {
				return nil, nil, errors.New("MCP requires an https HIKYO_EXTERNAL_ORIGIN; plaintext is allowed only for a loopback origin in development mode")
			}
			cfg.MCPAllowedOrigins, err = parseMCPAllowedOrigins(rawOrigins)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	if err := loadBackupPolicy(cfg, getenv); err != nil {
		return nil, nil, err
	}

	dbURL := getenv("HIKYO_DB")
	switch {
	case dbURL != "":
		ds, err := parseDatastore(dbURL)
		if err != nil {
			return nil, nil, err
		}
		cfg.Store = ds
	case cfg.Dev:
		cfg.Store = Datastore{Engine: EngineSQLite, Path: devSQLitePath}
	default:
		return nil, nil, fmt.Errorf("no datastore configured: set HIKYO_DB (sqlite:PATH or postgres://...) or pass --dev for zero-config sqlite evaluation")
	}
	if raw := strings.TrimSpace(getenv("HIKYO_PG_POOL_MAX")); raw != "" {
		if cfg.Store.Engine != EnginePostgres {
			return nil, nil, errors.New("HIKYO_PG_POOL_MAX requires a PostgreSQL datastore")
		}
		poolMax, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || poolMax <= 0 {
			return nil, nil, fmt.Errorf("HIKYO_PG_POOL_MAX: %q is not a positive 32-bit integer", raw)
		}
		cfg.Store.PostgresPoolMax = int32(poolMax)
	}
	if subcommand == "server" {
		if err := loadHAConfig(cfg, getenv); err != nil {
			return nil, nil, err
		}
	}
	return cfg, warnings, nil
}

func parseMCPAllowedOrigins(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	seen := make(map[string]bool)
	var origins []string
	for _, part := range strings.Split(raw, ",") {
		candidate := strings.TrimSpace(part)
		parsed, err := url.Parse(candidate)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
			parsed.Host == "" || parsed.User != nil || parsed.Path != "" ||
			parsed.RawQuery != "" || parsed.Fragment != "" ||
			candidate != parsed.Scheme+"://"+parsed.Host {
			return nil, fmt.Errorf("HIKYO_MCP_ALLOWED_ORIGINS: %q is not an exact HTTP(S) origin", candidate)
		}
		if candidate == "null" || candidate == "*" || seen[candidate] {
			return nil, fmt.Errorf("HIKYO_MCP_ALLOWED_ORIGINS: %q is not a unique exact origin", candidate)
		}
		seen[candidate] = true
		origins = append(origins, candidate)
	}
	return origins, nil
}

// loadHAConfig parses and validates the multi-node HA switch. Every
// misconfiguration is a boot refusal, never a silent single-node fallback:
// HA requires PostgreSQL, a stable unique node identity, and an explicitly
// configured shared root-key authority.
func loadHAConfig(cfg *Config, getenv func(string) string) error {
	raw := strings.TrimSpace(getenv("HIKYO_HA"))
	nodeID := strings.TrimSpace(getenv("HIKYO_NODE_ID"))
	if raw == "" {
		if nodeID != "" {
			return fmt.Errorf("HIKYO_NODE_ID is set but HIKYO_HA is not: node identity only applies to multi-node HA: set HIKYO_HA=true or remove HIKYO_NODE_ID")
		}
		return nil
	}
	ha, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("HIKYO_HA: %q is not a boolean", raw)
	}
	if !ha {
		if nodeID != "" {
			return fmt.Errorf("HIKYO_NODE_ID is set but HIKYO_HA is false: set HIKYO_HA=true or remove HIKYO_NODE_ID")
		}
		return nil
	}
	if cfg.Store.Engine != EnginePostgres {
		return fmt.Errorf("HIKYO_HA requires a PostgreSQL datastore: sqlite is single-writer and cannot back multi-node HA; set HIKYO_DB to a postgres:// DSN or disable HIKYO_HA")
	}
	if nodeID == "" {
		return fmt.Errorf("HIKYO_HA requires HIKYO_NODE_ID: each node needs a stable unique identity (the chart sets it from the pod name)")
	}
	if cfg.RootKeyFile == "" && !cfg.RootKeyFromEnv {
		return fmt.Errorf("HIKYO_HA requires an explicit shared root-key authority: set --root-key-file or HIKYO_ROOT_KEY; a development auto-generated root key is per-node and would split the installation")
	}
	cfg.HA = true
	cfg.NodeID = nodeID
	return nil
}

func loadAdapterEgressPolicy(path string) (map[string][]netip.Prefix, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("HIKYO_ADAPTER_EGRESS_POLICY_FILE: read policy: %w", err)
	}
	var encoded map[string][]string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("HIKYO_ADAPTER_EGRESS_POLICY_FILE: invalid JSON: %w", err)
	}
	out := make(map[string][]netip.Prefix, len(encoded))
	for origin, cidrs := range encoded {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || origin != "https://"+u.Host {
			return nil, fmt.Errorf("HIKYO_ADAPTER_EGRESS_POLICY_FILE: origin %q is not an exact canonical HTTPS origin", origin)
		}
		for _, rawCIDR := range cidrs {
			prefix, err := netip.ParsePrefix(rawCIDR)
			if err != nil {
				return nil, fmt.Errorf("HIKYO_ADAPTER_EGRESS_POLICY_FILE: origin %q has invalid CIDR %q", origin, rawCIDR)
			}
			out[origin] = append(out[origin], prefix.Masked())
		}
	}
	return out, nil
}

// loadDynamicEgressPolicy resolves the dynamic-secret egress allow-list. Keys
// are exact postgres:// origins (scheme + user + host[:port] + database, no
// password, no query/fragment); values are the private CIDRs a lease mint may
// dial for that origin. It fails loud on a malformed entry rather than silently
// dropping it.
func loadDynamicEgressPolicy(path string) (map[string][]netip.Prefix, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("HIKYO_DYNAMIC_EGRESS_POLICY_FILE: read policy: %w", err)
	}
	var encoded map[string][]string
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return nil, fmt.Errorf("HIKYO_DYNAMIC_EGRESS_POLICY_FILE: invalid JSON: %w", err)
	}
	out := make(map[string][]netip.Prefix, len(encoded))
	for origin, cidrs := range encoded {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" ||
			u.User == nil || u.User.Username() == "" || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") == "" {
			return nil, fmt.Errorf("HIKYO_DYNAMIC_EGRESS_POLICY_FILE: origin %q is not an exact postgres:// origin (user@host[:port]/db, no password/query)", origin)
		}
		if _, hasPassword := u.User.Password(); hasPassword {
			return nil, fmt.Errorf("HIKYO_DYNAMIC_EGRESS_POLICY_FILE: origin %q must not embed a password", origin)
		}
		for _, rawCIDR := range cidrs {
			prefix, err := netip.ParsePrefix(rawCIDR)
			if err != nil {
				return nil, fmt.Errorf("HIKYO_DYNAMIC_EGRESS_POLICY_FILE: origin %q has invalid CIDR %q", origin, rawCIDR)
			}
			out[origin] = append(out[origin], prefix.Masked())
		}
	}
	return out, nil
}

// loadBackupPolicy resolves the export recipient set and destination. Both
// halves fail loudly rather than degrading: an unparseable recipient list is
// an error, and recipients without a destination are an error, because the
// only quiet alternative is an instance that believes it is taking backups.
func loadBackupPolicy(cfg *Config, getenv func(string) string) error {
	for _, part := range strings.Split(getenv("HIKYO_BACKUP_RECIPIENTS"), ",") {
		if r := strings.TrimSpace(part); r != "" {
			cfg.BackupRecipients = append(cfg.BackupRecipients, r)
		}
	}
	cfg.BackupDir = strings.TrimSpace(getenv("HIKYO_BACKUP_DIR"))
	if len(cfg.BackupRecipients) > 0 && cfg.BackupDir == "" {
		return errors.New("HIKYO_BACKUP_RECIPIENTS is set but HIKYO_BACKUP_DIR is not: an export policy with no destination writes nothing")
	}
	return loadBackupSchedule(cfg, getenv)
}

// Backup schedule bounds (#145). The retention ceiling is ops-spec section 2:
// 180 days, no unlimited option, because the key hierarchy travels in every
// archive and an immortal backup is an immortal ciphertext archive.
const (
	DefaultBackupInterval    = 24 * time.Hour
	MinBackupInterval        = time.Hour
	DefaultBackupRPO         = 26 * time.Hour
	DefaultBackupRetainCount = 7
	// MaxBackupRetainCount is a loud sanity ceiling: a retain count is a small
	// operational number, and bounding it keeps the uint-to-int conversion
	// below safe on every platform.
	MaxBackupRetainCount    = 100_000
	DefaultBackupRetainDays = 180
	MaxBackupRetainDays     = 180
	DefaultBackupRTOTarget  = 30 * time.Minute
)

// BackupScheduled reports whether the in-process export schedule runs: it
// does exactly when an export policy is complete. There is no separate
// enable switch, so an instance with a policy cannot have its schedule
// quietly turned off by one more variable.
func (c *Config) BackupScheduled() bool {
	return len(c.BackupRecipients) > 0 && c.BackupDir != ""
}

// loadBackupSchedule resolves the DR schedule knobs. A schedule knob set on
// an instance with no export policy is refused: it would describe a
// schedule that never runs, and the operator who set it believes otherwise.
func loadBackupSchedule(cfg *Config, getenv func(string) string) error {
	var err error
	if cfg.BackupInterval, err = durationEnv(getenv, "HIKYO_BACKUP_INTERVAL", DefaultBackupInterval); err != nil {
		return err
	}
	if cfg.BackupInterval < MinBackupInterval {
		return fmt.Errorf("HIKYO_BACKUP_INTERVAL: %s is below the %s minimum", cfg.BackupInterval, MinBackupInterval)
	}
	if cfg.BackupRPO, err = durationEnv(getenv, "HIKYO_BACKUP_RPO", DefaultBackupRPO); err != nil {
		return err
	}
	if cfg.BackupRPO < cfg.BackupInterval {
		return fmt.Errorf("HIKYO_BACKUP_RPO: %s is shorter than the %s export interval, so every export would already be late", cfg.BackupRPO, cfg.BackupInterval)
	}
	retainCount, err := uintEnv(getenv, "HIKYO_BACKUP_RETAIN_COUNT", DefaultBackupRetainCount)
	if err != nil {
		return err
	}
	if retainCount < 1 || retainCount > MaxBackupRetainCount {
		return fmt.Errorf("HIKYO_BACKUP_RETAIN_COUNT: %d is outside 1..%d", retainCount, MaxBackupRetainCount)
	}
	cfg.BackupRetainCount = int(retainCount)
	retainDays, err := uintEnv(getenv, "HIKYO_BACKUP_RETAIN_DAYS", DefaultBackupRetainDays)
	if err != nil {
		return err
	}
	if retainDays < 1 || retainDays > MaxBackupRetainDays {
		return fmt.Errorf("HIKYO_BACKUP_RETAIN_DAYS: %d is outside 1..%d (ops spec: backup retention is bounded, no unlimited option exists)", retainDays, MaxBackupRetainDays)
	}
	cfg.BackupRetainDays = int(retainDays)
	if cfg.BackupRTOTarget, err = durationEnv(getenv, "HIKYO_BACKUP_RTO_TARGET", DefaultBackupRTOTarget); err != nil {
		return err
	}
	if cfg.BackupRTOTarget <= 0 {
		return errors.New("HIKYO_BACKUP_RTO_TARGET: must be a positive duration")
	}
	if !cfg.BackupScheduled() {
		for _, key := range []string{"HIKYO_BACKUP_INTERVAL", "HIKYO_BACKUP_RPO", "HIKYO_BACKUP_RETAIN_COUNT", "HIKYO_BACKUP_RETAIN_DAYS"} {
			if strings.TrimSpace(getenv(key)) != "" {
				return fmt.Errorf("%s is set but no export policy is configured: set HIKYO_BACKUP_RECIPIENTS and HIKYO_BACKUP_DIR, or remove it", key)
			}
		}
	}
	return nil
}

// durationEnv parses a Go duration tunable ("24h", "90m"), failing loudly on
// a malformed value for the same reason uintEnv does.
func durationEnv(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a duration (for example 24h or 90m)", key, raw)
	}
	return d, nil
}

// uintEnv parses an unsigned tunable, failing loudly rather than falling back
// to the default on a malformed value: a typo in a security parameter must
// not silently mean "use the default".
func uintEnv(getenv func(string) string, key string, fallback uint64) (uint32, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return uint32(fallback), nil
	}
	v, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%s: %q is not a non-negative integer", key, raw)
	}
	return uint32(v), nil
}

func parseTrustedProxyCIDRs(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	cidrs := make([]string, 0, len(parts))
	for _, part := range parts {
		cidr := strings.TrimSpace(part)
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return nil, fmt.Errorf("HIKYO_TRUSTED_PROXY_CIDRS: invalid CIDR %q", cidr)
		}
		cidrs = append(cidrs, cidr)
	}
	return cidrs, nil
}

// EmitHSTS reports whether responses should carry Strict-Transport-Security.
//
// The gate is the CONFIGURED PUBLIC ORIGIN'S SCHEME, not the presence of a
// native certificate (#517). HSTS is a statement about how the browser reached
// this instance, and a reverse proxy terminating TLS in front of a plaintext
// listener is https to the browser in exactly the way native TLS is. The old
// certificate-presence gate silently withheld the header from every
// proxy deployment, which is the majority shape. The origin is operator
// configuration validated at load time, never a request header, so it cannot
// be talked into the header by a client.
//
// Loopback still never promises: `Strict-Transport-Security` is stored per
// browser-visible HOST, so pinning `localhost` to https would break every other
// plaintext development server on the machine for one year. The bind address
// cannot answer that question: a supported same-host proxy uses a loopback bind
// for a non-loopback public origin.
func EmitHSTS(externalOrigin string) bool {
	origin, err := url.Parse(externalOrigin)
	return err == nil && origin.Scheme == "https" && origin.Host != "" && !isLoopbackHost(origin.Hostname())
}

// IsLoopbackListen reports whether a configured TCP listen address is local-only.
func IsLoopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	return isLoopbackHost(host)
}

func isLoopbackHost(host string) bool {
	host = strings.TrimRight(strings.ToLower(host), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil {
		return ip.IsLoopback()
	}
	ipv4, ok := browserIPv4(host)
	return ok && byte(ipv4>>24) == 127
}

// browserIPv4 parses the legacy one-to-four-part IPv4 spellings accepted by
// the URL Standard. Browsers canonicalize values such as 127.1, 2130706433,
// and 0x7f000001 to 127.0.0.1 before applying HSTS; net.ParseIP deliberately
// accepts only canonical literals, so relying on it alone would pin loopback.
func browserIPv4(host string) (uint32, bool) {
	parts := strings.Split(host, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return 0, false
	}

	numbers := make([]uint64, len(parts))
	for i, part := range parts {
		base := 10
		digits := part
		switch {
		case strings.HasPrefix(part, "0x"):
			base, digits = 16, part[2:]
		case len(part) > 1 && part[0] == '0':
			base, digits = 8, part[1:]
		}
		if digits == "" {
			return 0, false
		}
		n, err := strconv.ParseUint(digits, base, 32)
		if err != nil {
			return 0, false
		}
		numbers[i] = n
	}

	for _, n := range numbers[:len(numbers)-1] {
		if n > 255 {
			return 0, false
		}
	}
	lastLimit := uint64(1) << (8 * (5 - len(numbers)))
	if numbers[len(numbers)-1] >= lastLimit {
		return 0, false
	}

	value := numbers[len(numbers)-1]
	for i, n := range numbers[:len(numbers)-1] {
		value += n << (8 * (3 - i))
	}
	return uint32(value), true
}

func parseDatastore(raw string) (Datastore, error) {
	switch {
	case strings.HasPrefix(raw, "sqlite:"):
		path := strings.TrimPrefix(raw, "sqlite:")
		if path == "" {
			return Datastore{}, fmt.Errorf("HIKYO_DB sqlite: requires a file path")
		}
		return Datastore{Engine: EngineSQLite, Path: path}, nil
	case strings.HasPrefix(raw, "postgres://"), strings.HasPrefix(raw, "postgresql://"):
		if err := validatePostgresTLS(raw); err != nil {
			return Datastore{}, err
		}
		return Datastore{Engine: EnginePostgres, DSN: raw}, nil
	default:
		// Never echo the raw value: an unrecognized DSN can still carry
		// credentials, and these errors reach stderr and logs.
		scheme, _, hasScheme := strings.Cut(raw, ":")
		if !hasScheme {
			scheme = "<none>"
		}
		return Datastore{}, fmt.Errorf("HIKYO_DB: unsupported datastore scheme %q (want sqlite:PATH or postgres://...)", scheme)
	}
}

// validatePostgresTLS enforces the threat-model boundary restated in the
// system-architecture ADR: remote postgres requires TLS with certificate
// verification or a same-host socket; no plaintext to a non-loopback host.
// The effective host may arrive as the URL authority or as a libpq-style
// ?host= parameter; both are validated, and a DSN naming no host at all is
// refused rather than left to driver/environment defaults (fail-fast, no
// silent resolution through PGHOST).
func validatePostgresTLS(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		// url.Error embeds the raw URL (credentials included) — report only
		// the underlying cause.
		var uerr *url.Error
		if errors.As(err, &uerr) {
			return fmt.Errorf("HIKYO_DB: invalid postgres DSN: %w", uerr.Err)
		}
		return fmt.Errorf("HIKYO_DB: invalid postgres DSN")
	}
	host := u.Hostname()
	if hostParam := u.Query().Get("host"); hostParam != "" {
		if host != "" && host != hostParam {
			return fmt.Errorf("HIKYO_DB: conflicting hosts %q and ?host=%q", host, hostParam)
		}
		host = hostParam
	}
	if host == "" {
		return fmt.Errorf("HIKYO_DB: postgres DSN must name its host explicitly (no implicit PGHOST/default resolution)")
	}
	if strings.Contains(host, ",") {
		return fmt.Errorf("HIKYO_DB: multi-host DSNs are not supported")
	}
	if strings.HasPrefix(host, "/") {
		return nil // same-host unix socket
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	switch u.Query().Get("sslmode") {
	case "verify-full", "verify-ca":
		return nil
	}
	return fmt.Errorf("HIKYO_DB: remote postgres host %q requires sslmode=verify-full or verify-ca (no plaintext on a non-loopback boundary)", host)
}
