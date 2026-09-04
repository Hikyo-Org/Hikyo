package migrate

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/Hikyo-Org/hikyo/internal/store"
)

func runSAMLIdentityBackfill(t *testing.T, cfg store.Config) {
	t.Helper()
	ctx := t.Context()
	err := withProvider(ctx, cfg, func(provider *goose.Provider, db *sql.DB) error {
		if _, err := provider.UpTo(ctx, 9); err != nil {
			return err
		}
		const created = "2026-08-09T12:00:00Z"
		for _, statement := range []string{
			`INSERT INTO principals (id, kind, created_at) VALUES ('usr_saml_migration', 'human', '` + created + `')`,
			`INSERT INTO accounts (id, principal_id, username, display_name, created_at) VALUES ('acc_saml_migration', 'usr_saml_migration', 'migration-user', 'Migration User', '` + created + `')`,
			`INSERT INTO external_identities (id, account_id, kind, issuer, subject, provider_id, credential_epoch, created_at) VALUES ('eid_oidc_before_saml', 'acc_saml_migration', 'oidc', 'https://idp.example/realm', 'byte-exact-subject', 'oidcp_old', 7, '` + created + `')`,
		} {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
		if _, err := provider.UpTo(ctx, 10); err != nil {
			return err
		}
		var kind, issuer, subject string
		var epoch int64
		if err := db.QueryRowContext(ctx, `SELECT kind, issuer, subject, credential_epoch FROM external_identities WHERE id = 'eid_oidc_before_saml'`).Scan(&kind, &issuer, &subject, &epoch); err != nil {
			return err
		}
		if kind != "oidc" || issuer != "https://idp.example/realm" || subject != "byte-exact-subject" || epoch != 7 {
			return fmt.Errorf("backfilled identity = (%q, %q, %q, %d)", kind, issuer, subject, epoch)
		}
		_, err := db.ExecContext(ctx, `INSERT INTO external_identities (id, account_id, kind, issuer, subject, provider_id, credential_epoch, created_at) VALUES ('eid_saml_after_migration', 'acc_saml_migration', 'saml', 'https://idp.example/realm', 'byte-exact-subject', 'samlp_new', 7, '`+created+`')`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSAMLIdentityBackfillSQLite(t *testing.T) {
	runSAMLIdentityBackfill(t, store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "saml-migration.db")})
}

func TestSAMLIdentityBackfillPostgres(t *testing.T) {
	runSAMLIdentityBackfill(t, postgresTestConfig(t, "saml_migration"))
}
