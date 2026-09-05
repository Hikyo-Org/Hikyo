package isolation

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/buildcompat"
	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	"github.com/Hikyo-Org/hikyo/internal/upgradegate"
)

// TestMCPDeploymentFixture prepares only an explicitly owned disposable
// database for external real-process probes. Ordinary suites do not run it.
func TestMCPDeploymentFixture(t *testing.T) {
	dir := os.Getenv("HIKYO_MCP_PROOF_DIR")
	if dir == "" {
		t.Skip("explicit disposable deployment fixture only")
	}
	info, err := os.Stat(dir)
	if err != nil || !filepath.IsAbs(dir) || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatal("fixture requires an absolute owner-only runtime directory")
	}
	var settings struct {
		DSN string `json:"dsn"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &settings); err != nil {
		t.Fatal(err)
	}
	if err := validateMCPFixtureDSN(settings.DSN); err != nil {
		t.Fatal(err)
	}
	cfg := store.Config{Engine: store.EnginePostgres, DSN: settings.DSN}
	// The fixture and deployed binary must use the same authenticated production
	// build. A development seed cannot stand in for production deployment proof.
	pinned, err := buildcompat.ProductionTrust()
	if err != nil {
		t.Fatal("MCP fixture requires the deployed binary's verified release linker stamps")
	}
	bundle := os.Getenv("HIKYO_MCP_PROOF_BUNDLE")
	publicPath := os.Getenv("HIKYO_MCP_PROOF_OPERATOR_KEY")
	if bundle == "" || publicPath == "" {
		t.Fatal("MCP fixture requires public release bundle and installation operator key")
	}
	public, err := backupreceipt.ReadPublicArtifact(publicPath, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(dir, "root.key")
	if _, err := os.Lstat(rootPath); os.IsNotExist(err) && os.Getenv("HIKYO_MCP_PROOF_REVOKE") == "" {
		root, err := crypto.GenerateRootKey()
		if err != nil {
			t.Fatal(err)
		}
		defer crypto.Zero(root)
		file, err := os.OpenFile(rootPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, writeErr := file.WriteString(hex.EncodeToString(root))
		syncErr := file.Sync()
		closeErr := file.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			t.Fatal("could not durably create fixture root custody")
		}
	}
	root, err := crypto.ReadRootKey(rootPath, "")
	if err != nil {
		t.Fatal(err)
	}
	defer crypto.Zero(root)
	// Docker Desktop can present a bind mount's root as UID 0 even when its
	// host owner differs. Mount only this traversable wrapper into the server;
	// the private child retains the fixture UID and strict custody permissions.
	installationDir := filepath.Join(dir, "installation")
	if err := os.Mkdir(installationDir, 0755); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	stateDir := filepath.Join(installationDir, "operator-state")
	if err := os.Mkdir(stateDir, 0700); err != nil && !os.IsExist(err) {
		t.Fatal(err)
	}
	admitted, err := upgradegate.Run(t.Context(), upgradegate.Request{
		Store:           upgrade.Config{Engine: releaseidentity.Postgres, DSN: settings.DSN},
		BundleDirectory: bundle, Pinned: pinned, StateDirectory: stateDir, InitialOperatorPublicKey: public,
		Migrations: store.MigrationsFS, MigrationDirectory: "migrations/postgres",
		Mode: upgradegate.Boot, AllowMigrations: true, RootKey: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), cfg, admitted.Admission)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	type credential struct {
		Token        string
		AccountID    string
		CredentialID string
	}
	credentials := map[string]credential{}
	if os.Getenv("HIKYO_MCP_PROOF_REVOKE") != "" {
		b, err := os.ReadFile(filepath.Join(dir, "credentials.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &credentials); err != nil {
			t.Fatal(err)
		}
		root, err := crypto.ReadRootKey(filepath.Join(dir, "root.key"), "")
		if err != nil {
			t.Fatal(err)
		}
		kr := loadAndRegisterKeyring(t, db, root)
		auth := authServiceForKeyring(t, db, kr)
		name := os.Getenv("HIKYO_MCP_PROOF_REVOKE")
		if name == "grant-remove" || name == "grant-restore" {
			var principal string
			if err := db.PG().QueryRow(t.Context(), "SELECT principal_id FROM service_accounts WHERE id=$1", credentials["live"].AccountID).Scan(&principal); err != nil {
				t.Fatal(err)
			}
			grants := &service.Grants{DB: db, Auth: auth}
			actor := service.LocalPrincipal(domain.PrincipalID("usr_orgadmin"))
			spec := service.GrantSpec{Target: domain.PrincipalID(principal), Capability: domain.CapRead, Scope: scopeProject(orgA, prjA1)}
			if name == "grant-remove" {
				if err := grants.Revoke(t.Context(), actor, spec); err != nil {
					t.Fatal(err)
				}
			} else if _, err := grants.Create(t.Context(), actor, spec); err != nil {
				t.Fatal(err)
			}
			return
		}
		refreshRotating := name == "refresh-rotating"
		if refreshRotating {
			name = "rotating"
		}
		if name != "live" && name != "rotating" {
			t.Fatal("unknown fixture revocation target")
		}
		c := credentials[name]
		identities := &service.Identities{DB: db, Auth: auth}
		if !refreshRotating {
			if err := identities.RevokeCredential(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), c.AccountID, c.CredentialID); err != nil {
				t.Fatal(err)
			}
		}
		if name == "live" || refreshRotating {
			minted, err := identities.MintCredential(t.Context(), service.LocalPrincipal(custodian), scopeProject(orgA, prjA1), c.AccountID, service.MintRequest{})
			if err != nil {
				t.Fatal(err)
			}
			credentials[name] = credential{Token: minted.Value, AccountID: c.AccountID, CredentialID: minted.Credential.ID}
			b, err := json.Marshal(credentials)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "credentials.json"), b, 0600); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	var exists bool
	if err := db.PG().QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM orgs)").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("refusing nonempty fixture database")
	}
	kr := loadAndRegisterKeyring(t, db, root)
	seededDB(t, func(*testing.T) *store.DB { return db })
	auth := authServiceForKeyring(t, db, kr)
	keys := &service.Keys{DB: db, Keyring: kr}
	values := &service.Values{DB: db, Keyring: kr, Auth: auth}
	revisions := &service.Revisions{DB: db, Keyring: kr, Auth: auth}
	projectScope, envScope := scopeProject(orgA, prjA1), scopeEnv(orgA, prjA1, envA1)
	execRaw(t, db, `INSERT INTO grants (id,principal_id,capability,org_id,project_id,env_id,created_at) VALUES ('g_mcp_proof_manage','usr_custodian','manage-identities','org_a',NULL,NULL,`+ts+`)`)
	versionIDs := []string{}
	for _, entry := range []struct {
		name, value string
		class       schema.Classification
	}{{"CANARY_SECRET", mcpCanaryPlaintext, schema.Secret}, {"PUBLIC_CFG", "synthetic-config-value", schema.Config}, {"SECOND_CFG", "second-synthetic-value", schema.Config}} {
		mustCreateKey(t, keys, projectScope, entry.name, entry.class)
		v, err := values.Set(t.Context(), service.LocalPrincipal(custodian), envScope, entry.name, entry.value, nil)
		if err != nil {
			t.Fatal(err)
		}
		versionIDs = append(versionIDs, v.VersionID)
	}
	if _, err := revisions.PublishPlanned(t.Context(), service.LocalPrincipal(custodian), envScope, service.PublishRequest{VersionIDs: versionIDs}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"live", "rotating", "ungranted"} {
		c := mintMCPAutomation(t, db, auth, projectScope, "proof-"+name, name != "ungranted")
		credentials[name] = credential{Token: c.token, AccountID: c.AccountID, CredentialID: c.CredentialID}
	}
	b, err = json.Marshal(credentials)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

// Reject every datastore outside this rehearsal's dedicated loopback database
// before migration can write its version table. The caller creates that owned
// container; ordinary application DSNs are deliberately not supported.
func validateMCPFixtureDSN(dsn string) error {
	u, err := url.Parse(dsn)
	if err != nil {
		return fmt.Errorf("invalid fixture DSN")
	}
	port, err := strconv.Atoi(u.Port())
	query, queryErr := url.ParseQuery(u.RawQuery)
	if err != nil || port < 1024 || port > 65535 || u.Scheme != "postgres" ||
		u.Hostname() != "127.0.0.1" || u.Path != "/hikyo_mcp_651" || u.Fragment != "" ||
		u.User == nil || u.User.Username() != "hikyo" || queryErr != nil || len(query) != 1 ||
		len(query["sslmode"]) != 1 || query.Get("sslmode") != "disable" {
		return fmt.Errorf("fixture requires the dedicated hikyo_mcp_651 database on a loopback container port")
	}
	return nil
}

func TestMCPFixtureRefusesNonScratchDatastores(t *testing.T) {
	if err := validateMCPFixtureDSN("postgres://hikyo:synthetic@127.0.0.1:15432/hikyo_mcp_651?sslmode=disable"); err != nil {
		t.Fatal(err)
	}
	for _, dsn := range []string{
		"postgres://hikyo:synthetic@db.example:15432/hikyo_mcp_651?sslmode=disable",
		"postgres://hikyo:synthetic@127.0.0.1:15432/hikyo?sslmode=disable",
		"postgres://hikyo:synthetic@127.0.0.1:543/hikyo_mcp_651?sslmode=disable",
		"postgres://hikyo:synthetic@127.0.0.1:15432/hikyo_mcp_651?sslmode=require",
		"postgres://hikyo:synthetic@127.0.0.1:15432/hikyo_mcp_651?sslmode=disable&host=db.example",
		"postgres://hikyo:synthetic@127.0.0.1:15432/hikyo_mcp_651?sslmode=disable&dbname=hikyo",
		"host=127.0.0.1 dbname=hikyo_mcp_651",
	} {
		if validateMCPFixtureDSN(dsn) == nil {
			t.Fatal("accepted a datastore outside the disposable fixture contract")
		}
	}
}
