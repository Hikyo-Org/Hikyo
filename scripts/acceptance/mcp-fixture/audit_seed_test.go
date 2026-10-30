//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestAuditSeedCannotReuseMCPDatastoreOrOtherInstallation(t *testing.T) {
	cfg := auditSeedSettings{settings: settings{DSN: "postgres://hikyo_audit_1037:synthetic@127.0.0.1:15432/hikyo_audit_1037?sslmode=disable", Development: true}, Engine: "postgres", Installation: "audit-pg-1037"}
	if _, _, err := auditStoreConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if validateDSN(cfg.DSN, true) == nil {
		t.Fatal("MCP guard broadened for audit sibling")
	}
	for _, dsn := range []string{
		"postgres://hikyo:synthetic@127.0.0.1:15432/hikyo_mcp_651?sslmode=disable",
		"postgres://hikyo_audit_1037:synthetic@production.example:15432/hikyo_audit_1037?sslmode=disable",
		"postgres://hikyo_audit_1037:synthetic@127.0.0.1:15432/hikyo_audit_1037?sslmode=disable&host=production.example",
	} {
		cfg.DSN = dsn
		if _, _, err := auditStoreConfig(cfg); err == nil {
			t.Fatal("audit guard accepted different datastore")
		}
	}
}

// Opt-in real supported provisioning. It owns one bounded PG18 container and
// network, seeds each engine once, emits only redacted review metadata, and
// removes credential/root/database scratch and all owned runtime on return.
func TestAuditSeedSupportedBothEngines(t *testing.T) {
	output := os.Getenv("MCP651_AUDIT_REHEARSAL_DIR")
	if output == "" {
		t.Skip("opt-in disposable both-engine audit source seeding")
	}
	if err := privatePath(output, true); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(output); err != nil || len(entries) != 0 {
		t.Fatal("new empty private audit receipt directory required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	name := "hikyo-mcp-651-audit-" + randomHex()[:12]
	var container, network string
	t.Cleanup(func() {
		if !cleanupDocker(name, container, network, boundedDocker) {
			t.Error("owned audit runtime cleanup failed")
		}
	})
	network, err = command(ctx, "docker", "network", "create", "--label", "hikyo.acceptance=651", name)
	if err != nil {
		t.Fatal(err)
	}
	password := randomHex()
	env := filepath.Join(root, "postgres.env")
	if err := os.WriteFile(env, []byte("POSTGRES_USER=hikyo_audit_1037\nPOSTGRES_DB=hikyo_audit_1037\nPOSTGRES_PASSWORD="+password+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	container, err = command(ctx, "docker", "run", "-d", "--name", name, "--label", "hikyo.acceptance=651", "--network", network, "--memory", "512m", "--cpus", "1", "--pids-limit", "128", "--mount", "type=tmpfs,destination=/var/lib/postgresql,tmpfs-size=536870912", "--env-file", env, "--publish", "127.0.0.1::5432", postgresImage, "-c", "max_connections=40", "-c", "shared_buffers=32MB")
	if err != nil {
		t.Fatal(err)
	}
	address, err := command(ctx, "docker", "port", container, "5432/tcp")
	if err != nil || !strings.HasPrefix(address, "127.0.0.1:") {
		t.Fatal("owned audit PostgreSQL not loopback")
	}
	dsn := "postgres://hikyo_audit_1037:" + password + "@" + address + "/hikyo_audit_1037?sslmode=disable"
	if err := wait(ctx, 20*time.Second, func(ctx context.Context) bool {
		c, err := pgx.Connect(ctx, dsn)
		if err != nil {
			return false
		}
		defer c.Close(ctx)
		return c.Ping(ctx) == nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, engine := range []string{"sqlite", "postgres"} {
		dir := filepath.Join(root, engine)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		state := filepath.Join(dir, "authority")
		if err := os.Mkdir(state, 0700); err != nil {
			t.Fatal(err)
		}
		key := filepath.Join(dir, "root-key")
		if err := os.WriteFile(key, []byte(randomHex()), 0600); err != nil {
			t.Fatal(err)
		}
		cfg := auditSeedSettings{settings: settings{Root: key, State: state, Development: true}, Engine: engine, Installation: "audit-sqlite-1037", SQLitePath: filepath.Join(dir, "audit-sqlite-1037.db")}
		if engine == "postgres" {
			cfg.Installation = "audit-pg-1037"
			cfg.SQLitePath = ""
			cfg.DSN = dsn
		}
		if err := writePrivate(filepath.Join(dir, "audit-settings.json"), cfg); err != nil {
			t.Fatal(err)
		}
		if err := auditSeedConfigured(ctx, dir); err != nil {
			t.Fatal(engine, err)
		}
		if err := auditSeedConfigured(ctx, dir); err == nil {
			t.Fatal("repeated source seed accepted")
		}
		var receipt auditSeedReceipt
		if err := readPrivate(filepath.Join(dir, "audit-seed-receipt.json"), &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.SourceID == "" || receipt.TenantEventID == "" || receipt.TrustMode != "local-development" || receipt.ServerIdentity != nil || receipt.VerifiedOrigin != nil || receipt.Resources != nil || !receipt.Expires.After(time.Now()) {
			t.Fatal("seed identity/gate receipt incomplete or invented live binding")
		}
		if err := privatePath(receipt.BearerConfig, false); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "audit-seed-receipt.json"))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(password)) || bytes.Contains(raw, []byte("Authorization:")) || bytes.Contains(raw, []byte("postgres://")) || bytes.Contains(raw, []byte("hik_")) {
			t.Fatal("seed receipt disclosed a credential")
		}
		// Retain only nonsecret outcome IDs and measured trust mode. Curl custody
		// remains in test-owned scratch and is deleted by testing's cleanup.
		summary := map[string]any{"engine": engine, "outcome": "PASS", "evidence_class": "local_supported_service_seeding_only", "source_id": receipt.SourceID, "tenant_event_id": receipt.TenantEventID, "trust_mode": receipt.TrustMode, "public_deployment": "NOTRUN", "supervised_instance_export": "HOLD"}
		data, _ := json.Marshal(summary)
		if err := os.WriteFile(filepath.Join(output, engine+"-seed-receipt.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
