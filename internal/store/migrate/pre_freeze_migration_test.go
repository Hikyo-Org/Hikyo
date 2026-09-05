package migrate

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

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
	if err := Run(ctx, cfg); err != nil {
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
	source, err := store.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	var archive bytes.Buffer
	if _, err := store.Export(ctx, source, &archive, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	restored := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "restored.db")}
	if cfg.Engine == store.EnginePostgres {
		restored = postgresTestConfig(t, "retirement_restore")
		// A fresh canonical schema catches positional COPY incompatibility
		// with the schema obtained by upgrading a populated database.
		if err := Run(ctx, restored); err != nil {
			t.Fatal(err)
		}
		target, err := store.Open(ctx, restored)
		if err != nil {
			t.Fatal(err)
		}
		defer target.Close()
		if _, err := store.RestorePostgres(ctx, target, &archive, nil); err != nil {
			t.Fatal(err)
		}
	} else if _, err := store.RestoreSQLite(ctx, &archive, restored.Path, nil); err != nil {
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
