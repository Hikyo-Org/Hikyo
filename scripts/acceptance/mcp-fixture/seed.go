//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/devupgrade"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/scanning"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
	"github.com/jackc/pgx/v5"
)

type credential struct {
	Token     string             `json:"token"`
	Account   string             `json:"account"`
	ID        string             `json:"credential"`
	Principal domain.PrincipalID `json:"principal"`
}

// Seed's empty-datastore refusal precedes migration/admission control writes.
// Other supported writer commands intentionally reopen this already owned
// installation and therefore must not use the fresh-only entry point.
func openFreshFixture(ctx context.Context, cfg settings) (*store.DB, *crypto.Keyring, error) {
	if err := requireEmptyFixture(ctx, cfg); err != nil {
		return nil, nil, err
	}
	db, kr, err := openFixture(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	// Admission may create the catalogue. Recheck before any supported seed
	// writer, retaining the same pre-admission refusal for existing tenants.
	if err := requireEmptyFixture(ctx, cfg); err != nil {
		db.Close()
		return nil, nil, err
	}
	return db, kr, nil
}

func requireEmptyFixture(ctx context.Context, cfg settings) error {
	if err := validateDSN(cfg.DSN, cfg.Development); err != nil {
		return err
	}
	return requireEmptyPostgres(ctx, cfg, false)
}
func requireEmptyPostgres(ctx context.Context, cfg settings, audit bool) error {
	// The shared read-only preflight has two separate exact datastore guards.
	// Audit custody does not broaden the MCP acceptance database guard.
	if audit {
		if _, _, err := auditStoreConfig(auditSeedSettings{settings: cfg, Engine: "postgres", Installation: "audit-pg-1037"}); err != nil {
			return err
		}
	} else if err := validateDSN(cfg.DSN, cfg.Development); err != nil {
		return err
	}
	connection, err := pgx.Connect(ctx, cfg.DSN)
	if err != nil {
		return errors.New("fresh datastore preflight unavailable")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		connection.Close(cleanup)
	}()
	tx, err := connection.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return errors.New("read-only fresh preflight unavailable")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		tx.Rollback(cleanup)
	}()
	var exists bool
	if tx.QueryRow(ctx, "SELECT to_regclass('public.orgs') IS NOT NULL").Scan(&exists) != nil {
		return errors.New("fresh datastore catalogue unavailable")
	}
	if exists {
		var count int
		if tx.QueryRow(ctx, "SELECT COUNT(*) FROM public.orgs").Scan(&count) != nil || count != 0 {
			return errors.New("refusing nonempty datastore before admission")
		}
	}
	return nil
}

type fixture struct {
	Admin        domain.PrincipalID    `json:"admin"`
	Org          domain.OrgID          `json:"org"`
	Project      domain.ProjectID      `json:"project"`
	Environment  domain.EnvID          `json:"environment"`
	OtherOrg     domain.OrgID          `json:"other_org"`
	OtherProject domain.ProjectID      `json:"other_project"`
	Canaries     []string              `json:"canaries"`
	Credentials  map[string]credential `json:"credentials"`
}

func validateDSN(raw string, development bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("invalid fixture datastore")
	}
	q, err := url.ParseQuery(u.RawQuery)
	port, portErr := strconv.Atoi(u.Port())
	if err != nil || portErr != nil || port < 1024 || port > 65535 || u.Scheme != "postgres" || u.User == nil || u.User.Username() != "hikyo" || u.Path != "/hikyo_mcp_651" || u.Fragment != "" || len(q) != 1 || len(q["sslmode"]) != 1 {
		return errors.New("dedicated fixture datastore required")
	}
	if development && u.Hostname() == "127.0.0.1" && q.Get("sslmode") == "disable" {
		return nil
	}
	if !development && u.Hostname() == "db.hikyo-mcp-651.svc.cluster.local" && port == 5432 && q.Get("sslmode") == "verify-full" {
		return nil
	}
	return errors.New("fixture host or TLS policy refused")
}

func openFixture(ctx context.Context, cfg settings) (*store.DB, *crypto.Keyring, error) {
	if err := validateDSN(cfg.DSN, cfg.Development); err != nil {
		return nil, nil, err
	}
	return admitFixture(ctx, cfg, upgrade.Config{Engine: releaseidentity.Postgres, DSN: cfg.DSN}, store.Config{Engine: store.EnginePostgres, DSN: cfg.DSN, PostgresPoolMax: 4})
}

func admitFixture(ctx context.Context, cfg settings, upgradeConfig upgrade.Config, storeConfig store.Config) (*store.DB, *crypto.Keyring, error) {
	if err := privatePath(cfg.State, true); err != nil {
		return nil, nil, err
	}
	if err := privatePathModes(cfg.Root, false, 0400, 0600); err != nil {
		return nil, nil, err
	}
	root, err := crypto.ReadRootKey(cfg.Root, "")
	if err != nil {
		return nil, nil, err
	}
	defer crypto.Zero(root)
	req := upgradegate.Request{Store: upgradeConfig, Migrations: store.MigrationsFS, MigrationDirectory: "migrations/" + string(upgradeConfig.Engine), Mode: upgradegate.Boot, AllowMigrations: true, RootKey: root}
	var admitted upgradegate.Result
	if cfg.Development {
		material, err := devupgrade.Open(ctx, cfg.State)
		if err != nil {
			return nil, nil, err
		}
		req.BundleDirectory, req.Pinned = material.Directory, material.Pinned
		admitted, err = upgradegate.RunDevelopment(ctx, req)
		if err != nil {
			return nil, nil, err
		}
	} else {
		req.Pinned, err = buildcompat.ProductionTrust()
		if err != nil {
			return nil, nil, err
		}
		req.BundleDirectory, req.StateDirectory = cfg.Bundle, cfg.State
		req.InitialOperatorPublicKey, err = backupreceipt.ReadPublicArtifact(cfg.Operator, 1<<20)
		if err != nil {
			return nil, nil, err
		}
		admitted, err = upgradegate.Run(ctx, req)
		if err != nil {
			return nil, nil, err
		}
	}
	db, err := store.Open(ctx, storeConfig, admitted.Admission)
	if err != nil {
		return nil, nil, err
	}
	kr, err := crypto.LoadKeyring(ctx, &keyring.Store{DB: db}, root)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return db, kr, nil
}

func seed(ctx context.Context, dir string, db *store.DB, kr *crypto.Keyring) error {
	path := filepath.Join(dir, "fixture.json")
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return errors.New("fixture custody already exists")
	}
	// openFreshFixture refuses nonempty storage before this supported writer.
	// No SQL recreates grants or principals.
	auth := &service.Auth{DB: db, Keyring: kr, KDF: crypto.PasswordFloor}
	admin, err := auth.BootstrapAdmin(ctx, "mcp-651-custodian", "Synthetic fixture custodian", "file")
	if err != nil {
		return errors.New("supported administrator bootstrap refused")
	}
	if err := writePrivate(filepath.Join(dir, "bootstrap-authority.json"), map[string]string{"authority": admin.Authority}); err != nil {
		return err
	}
	actor := service.LocalPrincipal(admin.PrincipalID)
	orgs, projects := &service.Orgs{DB: db}, &service.Projects{DB: db}
	org, err := orgs.Create(ctx, actor, "MCP 651 acceptance", true, []byte(`{}`))
	if err != nil {
		return errors.New("supported organization creation refused")
	}
	other, err := orgs.Create(ctx, actor, "MCP 651 disjoint canary", true, []byte(`{}`))
	if err != nil {
		return errors.New("supported disjoint organization creation refused")
	}
	project, err := projects.Create(ctx, actor, domain.OrgID(org.ID), "Synthetic production")
	if err != nil {
		return errors.New("supported project creation refused")
	}
	otherProject, err := projects.Create(ctx, actor, domain.OrgID(other.ID), "Disjoint")
	if err != nil {
		return errors.New("supported disjoint project creation refused")
	}
	f := fixture{Admin: admin.PrincipalID, Org: domain.OrgID(org.ID), Project: domain.ProjectID(project.ID), OtherOrg: domain.OrgID(other.ID), OtherProject: domain.ProjectID(otherProject.ID), Credentials: map[string]credential{}}
	scan, err := scanning.Load()
	if err != nil {
		return errors.New("scanning rules unavailable")
	}
	environments := &service.Environments{DB: db, Keyring: kr, Scan: scan}
	env, err := environments.Create(ctx, actor, f.projectScope(), "production", nil)
	if err != nil {
		return errors.New("supported environment creation refused")
	}
	f.Environment = domain.EnvID(env.ID)
	keys := &service.Keys{DB: db, Keyring: kr, Scan: scan}
	values := &service.Values{DB: db, Keyring: kr, Auth: auth, Scan: scan}
	revisions := &service.Revisions{DB: db, Keyring: kr, Auth: auth}
	f.Canaries = []string{"CANARY-PLAINTEXT-9Z-do-not-disclose", "DISJOINT-651-material-never-export"}
	var versions []string
	for _, entry := range []struct {
		name, value string
		class       schema.Classification
	}{
		{"CANARY_SECRET", f.Canaries[0], schema.Secret},
		{"PUBLIC_CFG", "synthetic-config-value", schema.Config},
		{"SECOND_CFG", "second-synthetic-value", schema.Config},
	} {
		_, err = keys.Create(ctx, actor, f.projectScope(), service.KeySpec{Name: entry.name, Classification: string(entry.class), Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}, nil)
		if err != nil {
			return errors.New("supported definition creation refused")
		}
		v, err := values.Set(ctx, actor, f.envScope(), entry.name, entry.value, nil)
		if err != nil {
			return errors.New("supported value staging refused")
		}
		versions = append(versions, v.VersionID)
	}
	if _, err := revisions.PublishPlanned(ctx, actor, f.envScope(), service.PublishRequest{VersionIDs: versions}); err != nil {
		return errors.New("supported synthetic publication refused")
	}
	// Material confined to the disjoint org tests both scope and accidental
	// aggregation, independently from the in-scope secret-value canary.
	otherScope := domain.Scope{Org: f.OtherOrg, Project: f.OtherProject}
	if _, err := keys.Create(ctx, actor, otherScope, service.KeySpec{Name: "DISJOINT_CFG", Description: f.Canaries[1], Classification: string(schema.Config), Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}}, Presence: schema.DefaultPresenceRules()}, nil); err != nil {
		return errors.New("disjoint canary seed refused")
	}
	identities, grants := &service.Identities{DB: db, Auth: auth}, &service.Grants{DB: db, Auth: auth}
	for _, name := range []string{"live", "rotating", "smoke", "ungranted", "clients", "lifecycle"} {
		account, err := identities.CreateServiceAccount(ctx, actor, f.projectScope(), "mcp-651-"+name, domain.ClassAutomation)
		if err != nil {
			return errors.New("supported automation identity creation refused")
		}
		if name != "ungranted" {
			if _, err := grants.Create(ctx, actor, service.GrantSpec{Target: account.Principal, Capability: domain.CapRead, Scope: f.projectScope()}); err != nil {
				return errors.New("supported automation read grant refused")
			}
		}
		minted, err := identities.MintCredential(ctx, actor, f.projectScope(), account.ID, service.MintRequest{})
		if err != nil {
			return errors.New("supported credential mint refused")
		}
		f.Credentials[name] = credential{Token: minted.Value, Account: account.ID, ID: minted.Credential.ID, Principal: account.Principal}
	}
	return writePrivate(path, f)
}

func (f fixture) projectScope() domain.Scope { return domain.Scope{Org: f.Org, Project: f.Project} }
func (f fixture) envScope() domain.Scope {
	return domain.Scope{Org: f.Org, Project: f.Project, Env: f.Environment}
}

func mutate(ctx context.Context, dir string, db *store.DB, kr *crypto.Keyring, action string) error {
	var f fixture
	if err := readPrivate(filepath.Join(dir, "fixture.json"), &f); err != nil {
		return err
	}
	auth := &service.Auth{DB: db, Keyring: kr}
	actor := service.LocalPrincipal(f.Admin)
	if action == "revoke" || action == "revoke-smoke" {
		name := "rotating"
		if action == "revoke-smoke" {
			name = "smoke"
		}
		c := f.Credentials[name]
		return (&service.Identities{DB: db, Auth: auth}).RevokeCredential(ctx, actor, f.projectScope(), c.Account, c.ID)
	}
	spec := service.GrantSpec{Target: f.Credentials["live"].Principal, Capability: domain.CapRead, Scope: f.projectScope()}
	grants := &service.Grants{DB: db, Auth: auth}
	if action == "grant-remove" {
		return grants.Revoke(ctx, actor, spec)
	}
	_, err := grants.Create(ctx, actor, spec)
	return err
}
