package migrate

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/Hikyo-Org/hikyo/internal/backupreceipt"
	"github.com/Hikyo-Org/hikyo/internal/crypto/backup"
	"github.com/Hikyo-Org/hikyo/internal/releaseidentity"
	"github.com/Hikyo-Org/hikyo/internal/store/upgrade"
	bundlefixture "github.com/Hikyo-Org/hikyo/internal/upgradebundle/testfixture"
	"github.com/Hikyo-Org/hikyo/internal/upgradecompat"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/pressly/goose/v3"
)

func TestRetiredProviderPolicyMigrationSQLite(t *testing.T) {
	testRetiredProviderPolicy(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "retirement.db")})
}
func TestRetiredProviderPolicyMigrationPostgres(t *testing.T) {
	testRetiredProviderPolicy(t, postgresTestConfig(t, "retirement"))
}
func testRetiredProviderPolicy(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	if err := RunUpTo(ctx, cfg, 43); err != nil {
		t.Fatal(err)
	}
	if err := withProvider(ctx, cfg, func(_ *goose.Provider, db *sql.DB) error {
		secret := "X'01'"
		if cfg.Engine == store.EnginePostgres {
			secret = "decode('01','hex')"
		}
		_, err := db.ExecContext(ctx, "INSERT INTO oidc_providers (id,slug,display_name,kind,issuer,client_id,client_secret,scopes,redirect_uri,jit_policy,assurance_policy,enabled,dek_version,row_version,created_at,updated_at) VALUES ('provider_retire','retire','Retained provider','oidc','https://idp.test','client',"+secret+",'openid','https://hikyo.test/callback','{\"claim\":\"sub\",\"values\":[\"user\"]}','{\"amr_sets\":[[\"mfa\"]]}',1,1,1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')")
		if err != nil {
			return err
		}
		// Seed linked identity and active session before the destructive column
		// change, including non-default epochs and opaque verifier bytes.
		for _, statement := range []string{
			`INSERT INTO principals (id,kind,created_at,session_generation,reconciled_epoch) VALUES ('principal_retire','human','2026-01-01T00:00:00Z',7,3)`,
			`INSERT INTO accounts (id,principal_id,username,display_name,created_at) VALUES ('account_retire','principal_retire','retained','Retained account','2026-01-01T00:00:00Z')`,
			`INSERT INTO external_identities (id,account_id,kind,issuer,subject,provider_id,credential_epoch,created_at) VALUES ('identity_retire','account_retire','oidc','https://idp.test','CaseSensitiveSubject','provider_retire',3,'2026-01-01T00:00:00Z')`,
			"INSERT INTO sessions (id,principal_id,verifier,artifact,session_generation,credential_epoch,auth_method,factors,authenticated_at,created_at,last_seen_at,idle_expires_at,absolute_expires_at,source_ip,user_agent,provider_id) VALUES ('session_retire','principal_retire'," + secret + ",'browser',7,3,'oidc:https://idp.test','[\"federated\"]','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-01-02T00:00:00Z','2026-01-03T00:00:00Z','192.0.2.1','retained-agent','provider_retire')",
		} {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := retainedIdentityRows(t, cfg)
	// Migration 44 retires the provider policy. Its archive is deliberately
	// verified against the immutable legacy genesis at that same version.
	if err := RunUpTo(ctx, cfg, 44); err != nil {
		t.Fatal(err)
	}
	if after := retainedIdentityRows(t, cfg); !reflect.DeepEqual(before, after) {
		t.Fatalf("migration changed retained identity rows: before=%#v after=%#v", before, after)
	}
	if err := withProvider(ctx, cfg, func(_ *goose.Provider, db *sql.DB) error {
		var name, policy string
		if err := db.QueryRowContext(ctx, "SELECT display_name,assurance_policy FROM oidc_providers WHERE id = 'provider_retire'").Scan(&name, &policy); err != nil {
			return err
		}
		if name != "Retained provider" || policy != `{"amr_sets":[["mfa"]]}` {
			t.Fatalf("provider data changed: %q %q", name, policy)
		}
		rows, err := db.QueryContext(ctx, "SELECT * FROM oidc_providers LIMIT 0")
		if err != nil {
			return err
		}
		defer rows.Close()
		columns, err := rows.Columns()
		if err != nil {
			return err
		}
		for _, column := range columns {
			if column == "jit_policy" {
				t.Fatal("retired provider column survived upgrade")
			}
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	authenticated, plan := historicalPreparedArchive(t, cfg)
	defer authenticated.Close()
	archive, err := authenticated.Open()
	if err != nil {
		t.Fatal(err)
	}
	restored := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "restored.db")}
	if cfg.Engine == store.EnginePostgres {
		restored = postgresTestConfig(t, "retirement_restore")
		// A fresh canonical schema catches positional COPY incompatibility
		// with the schema obtained by upgrading a populated database.
		if err := RunUpTo(ctx, restored, 44); err != nil {
			t.Fatal(err)
		}
		err = upgrade.WithLock(ctx, upgrade.Config{Engine: releaseidentity.Postgres, DSN: restored.DSN}, func(session *upgrade.Session) error {
			authority, err := session.ValidateRestoreDestination(ctx, authenticated, plan)
			if err != nil {
				return err
			}
			target, err := store.OpenRestoreDestination(ctx, restored, authority, authenticated, plan)
			if err != nil {
				return err
			}
			defer target.Close()
			_, err = target.RestorePostgres(ctx, nil)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	} else if _, err := store.RestoreUpgradeSQLite(ctx, archive, restored.Path, plan, func(context.Context, *sql.Tx) error { return nil }); err != nil {
		t.Fatal(err)
	}

	if after := retainedIdentityRows(t, restored); !reflect.DeepEqual(before, after) {
		t.Fatalf("backup restore changed retained identity rows: before=%#v after=%#v", before, after)
	}

}

// Capture every column and row of the affected identity graph. The retired
// policy is the only allowed difference between schema 43 and its successor.
func retainedIdentityRows(t *testing.T, cfg store.Config) map[string][][]any {
	t.Helper()
	snapshot := make(map[string][][]any)
	if err := withProvider(t.Context(), cfg, func(_ *goose.Provider, db *sql.DB) error {
		for _, table := range []string{"oidc_providers", "principals", "accounts", "external_identities", "sessions"} {
			rows, err := db.QueryContext(t.Context(), "SELECT * FROM "+table+" ORDER BY id")
			if err != nil {
				return err
			}
			columns, err := rows.Columns()
			if err != nil {
				rows.Close()
				return err
			}
			var header []any
			for _, column := range columns {
				if column != "jit_policy" {
					header = append(header, column)
				}
			}
			snapshot[table] = [][]any{header}
			for rows.Next() {
				values := make([]any, len(columns))
				dest := make([]any, len(columns))
				for i := range values {
					dest[i] = &values[i]
				}
				if err := rows.Scan(dest...); err != nil {
					rows.Close()
					return err
				}
				var kept []any
				for i, value := range values {
					if columns[i] == "jit_policy" {
						continue
					}
					if data, ok := value.([]byte); ok {
						value = string(data)
					}
					kept = append(kept, value)
				}
				snapshot[table] = append(snapshot[table], kept)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

// Historical migration compatibility uses the genuine signed legacy
// preparation/export protocol, never an unrestricted runtime store.
func historicalPreparedArchive(t *testing.T, cfg store.Config) (*backupreceipt.AuthenticatedArchive, upgradecompat.Plan) {
	t.Helper()
	engine := releaseidentity.Engine(cfg.Engine)
	sourceConfig := upgrade.Config{Engine: engine, Path: cfg.Path, DSN: cfg.DSN}
	manifest, err := upgrade.PinnedLegacyManifest(engine)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := upgrade.InspectInstalled(t.Context(), sourceConfig, manifest)
	if err != nil {
		t.Fatal(err)
	}
	bundle := bundlefixture.Write(t, upgradecompat.InstalledSource{Identity: installed.Source, Migrations: manifest, SchemaSHA256: installed.SchemaDigest}, []bundlefixture.Target{{Version: "1.0.0", Sequence: 1, Commit: strings.Repeat("a", 40), Migrations: manifest, SchemaSHA256: installed.SchemaDigest}})
	proposal, err := backupreceipt.NewLegacyProposal()
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := backupreceipt.NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	identity, recipient, err := backup.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	options := backup.Options{Recipients: []string{recipient}}
	fingerprints, err := options.UpgradeRecipientFingerprints()
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	var archived store.Manifest
	err = upgrade.WithLock(t.Context(), sourceConfig, func(session *upgrade.Session) error {
		admission, err := session.PrepareExport(t.Context(), bundle.Plan)
		if err != nil {
			return err
		}
		source, err := store.OpenPreparation(t.Context(), cfg, admission)
		if err != nil {
			return err
		}
		defer source.Close()
		archived, err = source.ExportUpgrade(t.Context(), &plain, t.TempDir(), store.UpgradeExportRequest{Plan: bundle.Plan, Recipients: fingerprints, LegacyProposal: &proposal, BackupID: nonce, CreatedAt: time.Now().UTC().Truncate(time.Second)})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var ciphertext bytes.Buffer
	encrypt, err := backup.Encrypt(&ciphertext, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := encrypt.Write(plain.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := encrypt.Close(); err != nil {
		t.Fatal(err)
	}
	digest, err := store.ManifestDigest(archived)
	if err != nil {
		t.Fatal(err)
	}
	receipt := backupreceipt.Receipt{Format: backupreceipt.ReceiptFormat, CiphertextSHA256: releaseidentity.Hash(ciphertext.Bytes()), CiphertextBytes: int64(ciphertext.Len()), ManifestSHA256: digest, Snapshot: archived.Upgrade.Clone()}
	raw, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "backup.age")
	if err := os.WriteFile(path, ciphertext.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	pinned, err := backupreceipt.PinCiphertext(t.Context(), path, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer pinned.Close()
	authenticated, err := backupreceipt.AuthenticateArchive(t.Context(), pinned, raw, bundle.Plan, backup.Unlock{Identity: identity}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return authenticated, bundle.Plan
}
