//go:build darwin || linux

package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/admission"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
)

type auditSeedSettings struct {
	settings
	Engine       string `json:"engine"`
	Installation string `json:"installation"`
	SQLitePath   string `json:"sqlite_path"`
}

type auditServerIdentity struct {
	SourceSHA          string `json:"source_sha"`
	ImageDigest        string `json:"immutable_image_digest"`
	PlatformDigest     string `json:"platform_artifact_digest"`
	ServerBinarySHA256 string `json:"server_binary_sha256"`
	TrustMode          string `json:"trust_mode"`
}
type auditVerifiedOrigin struct {
	Origin      string `json:"https_origin"`
	CAPath      string `json:"local_ca_path,omitempty"`
	ProofSHA256 string `json:"verified_receipt_sha256"`
}
type auditResources struct {
	Namespace     string   `json:"namespace"`
	Workload      string   `json:"workload"`
	PVCs          []string `json:"pvcs"`
	Database      string   `json:"database"`
	TeardownOwner string   `json:"teardown_owner"`
}
type auditSeedReceipt struct {
	Schema              string               `json:"schema"`
	Status              string               `json:"status"`
	Engine              string               `json:"engine"`
	Installation        string               `json:"installation"`
	SourceID            string               `json:"source_id"`
	TrustMode           string               `json:"trust_mode"`
	Org                 domain.OrgID         `json:"org_id"`
	Project             domain.ProjectID     `json:"project_id"`
	Principal           domain.PrincipalID   `json:"principal_id"`
	Credential          string               `json:"credential_id"`
	Expires             time.Time            `json:"credential_expires_at_utc"`
	BearerConfig        string               `json:"bearer_curl_config"`
	ProjectReceiptPath  string               `json:"project_receipt_path"`
	TenantEventID       string               `json:"synthetic_tenant_event_id"`
	TenantEventType     string               `json:"synthetic_tenant_event_type"`
	FailedLoginAt       time.Time            `json:"failed_local_login_writer_return_at_utc"`
	InstanceObservation string               `json:"supervised_instance_event_observation"`
	ServerIdentity      *auditServerIdentity `json:"actual_server_source_and_immutable_artifact"`
	VerifiedOrigin      *auditVerifiedOrigin `json:"verified_https_origin_and_trust"`
	Resources           *auditResources      `json:"actual_owned_resources_and_teardown_owner"`
}

func auditStoreConfig(cfg auditSeedSettings) (upgrade.Config, store.Config, error) {
	switch cfg.Engine {
	case "postgres":
		if cfg.Installation != "audit-pg-1037" || cfg.SQLitePath != "" {
			break
		}
		u, err := url.Parse(cfg.DSN)
		if err != nil {
			break
		}
		q, err := url.ParseQuery(u.RawQuery)
		port, portErr := strconv.Atoi(u.Port())
		if err != nil || portErr != nil || port < 1024 || port > 65535 || u.Scheme != "postgres" || u.User == nil || u.User.Username() != "hikyo_audit_1037" || u.Path != "/hikyo_audit_1037" || u.Fragment != "" || len(q) != 1 || len(q["sslmode"]) != 1 {
			break
		}
		if password, ok := u.User.Password(); !ok || password == "" {
			break
		}
		if !(cfg.Development && u.Hostname() == "127.0.0.1" && q.Get("sslmode") == "disable") && !(!cfg.Development && u.Hostname() == "audit-db-1037.hikyo-mcp-651.svc.cluster.local" && port == 5432 && q.Get("sslmode") == "verify-full") {
			break
		}
		return upgrade.Config{Engine: releaseidentity.Postgres, DSN: cfg.DSN}, store.Config{Engine: store.EnginePostgres, DSN: cfg.DSN, PostgresPoolMax: 4}, nil
	case "sqlite":
		if cfg.Installation != "audit-sqlite-1037" || cfg.DSN != "" || !filepath.IsAbs(cfg.SQLitePath) || filepath.Base(cfg.SQLitePath) != "audit-sqlite-1037.db" || privatePath(filepath.Dir(cfg.SQLitePath), true) != nil {
			break
		}
		if filepath.Clean(cfg.SQLitePath) != cfg.SQLitePath {
			break
		}
		return upgrade.Config{Engine: releaseidentity.SQLite, Path: cfg.SQLitePath}, store.Config{Engine: store.EngineSQLite, Path: cfg.SQLitePath}, nil
	}
	return upgrade.Config{}, store.Config{}, errors.New("separate dedicated audit source guard refused")
}

func auditSeedConfigured(ctx context.Context, dir string) error {
	var cfg auditSeedSettings
	if err := readPrivate(filepath.Join(dir, "audit-settings.json"), &cfg); err != nil {
		return err
	}
	upgradeConfig, storeConfig, err := auditStoreConfig(cfg)
	if err != nil {
		return err
	}
	if cfg.Engine == "postgres" {
		if err := requireEmptyPostgres(ctx, cfg.DSN); err != nil {
			return err
		}
	} else if _, err := os.Lstat(cfg.SQLitePath); !os.IsNotExist(err) {
		return errors.New("audit SQLite seed requires a new file; preserve partial custody")
	}
	for _, name := range []string{"audit-seed-receipt.json", "audit-project-bearer.curl", "audit-bootstrap-authority.json"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			return errors.New("audit seed custody already exists")
		}
	}
	db, kr, err := admitFixture(ctx, cfg.settings, upgradeConfig, storeConfig)
	if err != nil {
		return errors.New("audit source admission refused; preserve custody")
	}
	defer db.Close()
	return seedAuditSource(ctx, dir, cfg, upgradeConfig, db, kr)
}

func seedAuditSource(ctx context.Context, dir string, cfg auditSeedSettings, upgradeConfig upgrade.Config, db *store.DB, kr *crypto.Keyring) error {
	limiter, err := admission.New(admission.Config{BudgetMiB: 128, ArgonMemoryKiB: crypto.PasswordFloor.MemoryKiB})
	if err != nil {
		return err
	}
	auth := &service.Auth{DB: db, Keyring: kr, KDF: crypto.PasswordFloor, Admission: limiter}
	human, err := auth.BootstrapAdmin(ctx, "audit-source-custodian-1037", "Synthetic audit source custodian", "file")
	if err != nil {
		return errors.New("audit supported bootstrap refused")
	}
	if err := writePrivate(filepath.Join(dir, "audit-bootstrap-authority.json"), map[string]string{"authority": human.Authority}); err != nil {
		return err
	}
	actor := service.LocalPrincipal(human.PrincipalID)
	org, err := (&service.Orgs{DB: db}).Create(ctx, actor, cfg.Installation+"-source", true, []byte(`{}`))
	if err != nil {
		return errors.New("audit supported org creation refused")
	}
	projects := &service.Projects{DB: db}
	project, err := projects.Create(ctx, actor, domain.OrgID(org.ID), "audit-collector-1037-initial")
	if err != nil {
		return errors.New("audit supported project creation refused")
	}
	scope := domain.Scope{Org: domain.OrgID(org.ID), Project: domain.ProjectID(project.ID)}
	identities := &service.Identities{DB: db, Auth: auth}
	account, err := identities.CreateServiceAccount(ctx, actor, scope, "audit-collector-1037", domain.ClassAutomation)
	if err != nil {
		return errors.New("audit automation creation refused")
	}
	if _, err := (&service.Grants{DB: db, Auth: auth}).Create(ctx, actor, service.GrantSpec{Target: account.Principal, Capability: domain.CapAuditRead, Scope: scope}); err != nil {
		return errors.New("supported project audit-read grant refused")
	}
	minted, err := identities.MintCredential(ctx, actor, scope, account.ID, service.MintRequest{Lifetime: 4 * time.Hour})
	if err != nil {
		return errors.New("audit credential mint refused")
	}
	bearerPath := filepath.Join(dir, "audit-project-bearer.curl")
	if err := writeBearerConfig(bearerPath, minted.Value); err != nil {
		return err
	}
	if _, err := projects.Rename(ctx, actor, scope, "audit-collector-1037"); err != nil {
		return errors.New("supported audit synthetic rename refused")
	}
	if _, err := auth.LocalLogin(ctx, "audit-source-custodian-1037", "deliberately-wrong-synthetic-password", service.ArtifactCLI); !errors.Is(err, domain.ErrUnauthenticated) {
		return errors.New("synthetic failed local login not observed")
	}
	loginReturned := time.Now().UTC()
	audits := &service.Audits{DB: db}
	page, err := audits.QueryActor(ctx, service.Bearer(minted.Value), scope, store.AuditFilter{Type: "settings.project_renamed", Limit: 10})
	if err != nil || len(page.Events) != 1 {
		return errors.New("supported project audit read did not recover synthetic event")
	}
	if _, err := audits.InstanceQueryActor(ctx, service.Bearer(minted.Value), store.AuditFilter{Limit: 1}); !errors.Is(err, domain.ErrUnauthorized) {
		return errors.New("project audit credential reached instance trail")
	}
	if _, err := audits.QueryActor(ctx, service.Bearer(minted.Value), domain.Scope{Org: scope.Org}, store.AuditFilter{Limit: 1}); !errors.Is(err, domain.ErrNotFound) {
		return errors.New("project audit credential org denial unconfirmed")
	}
	control, err := upgrade.InspectControl(ctx, upgradeConfig)
	if err != nil || control.InstanceID == "" {
		return errors.New("native installation identity unavailable")
	}
	receipt := auditSeedReceipt{Schema: "hikyo.dev/audit-source-seed/v1", Status: "SEED_ONLY; HOLD live artifact/origin/resource binding and supervised human MFA", Engine: cfg.Engine, Installation: cfg.Installation, SourceID: control.InstanceID, TrustMode: string(control.TrustDomain), Org: scope.Org, Project: scope.Project, Principal: account.Principal, Credential: minted.Credential.ID, Expires: minted.Credential.ExpiresAt, BearerConfig: bearerPath, ProjectReceiptPath: fmt.Sprintf("/api/v1/orgs/%s/projects/%s/audit/export?receipt=v1", scope.Org, scope.Project), TenantEventID: page.Events[0].ID, TenantEventType: string(page.Events[0].Type), FailedLoginAt: loginReturned, InstanceObservation: "HOLD: supervised human plus current MFA observes the instance event; no unattended instance/org authority minted"}
	return writePrivate(filepath.Join(dir, "audit-seed-receipt.json"), receipt)
}

func writeBearerConfig(path, token string) error {
	if !regexp.MustCompile(`^[A-Za-z0-9_.-]+$`).MatchString(token) {
		return errors.New("credential cannot be encoded safely in curl config")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("protected bearer config already exists")
	}
	_, writeErr := fmt.Fprintf(file, "header = \"Authorization: Bearer %s\"\nproto = \"=https\"\nmax-redirs = 0\n", token)
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}
