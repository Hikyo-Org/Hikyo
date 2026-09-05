package service

import (
	"path/filepath"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/crypto"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/keyring"
)

// proveFixture is a migrated sqlite instance with one project, environment and
// secret key, plus a keyring, so the drill's decrypt proof can be exercised
// against a real sealed value rather than a mock.
func proveFixture(t *testing.T) (*store.DB, *crypto.Keyring) {
	t.Helper()
	cfg := store.Config{Engine: store.EngineSQLite, Path: filepath.Join(t.TempDir(), "prove.db")}
	db, err := openServiceFixture(t, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range []string{
		`INSERT INTO orgs (id,name,active,metadata,created_at) VALUES ('org_p','P',1,'{}','2026-08-17T00:00:00Z')`,
		`INSERT INTO projects (id,org_id,name,created_at) VALUES ('prj_p','org_p','P','2026-08-17T00:00:00Z')`,
		`INSERT INTO environments (id,org_id,project_id,name,note,created_at,display_order) VALUES ('env_p','org_p','prj_p','p','','2026-08-17T00:00:00Z',0)`,
		`INSERT INTO keys (id,org_id,project_id,name,folder_path,classification,description,deprecated,deprecation_note,declaration,required_mode,forbidden_mode,created_at) VALUES ('key_p','org_p','prj_p','DB_PASSWORD','','secret','',0,'','{}','none','none','2026-08-17T00:00:00Z')`,
	} {
		if _, err := db.SQLiteWrite().ExecContext(t.Context(), stmt); err != nil {
			t.Fatal(err)
		}
	}
	root := serviceFixtureRoot(t, db)
	kr, err := crypto.LoadKeyring(t.Context(), &keyring.Store{DB: db}, root)
	if err != nil {
		t.Fatal(err)
	}
	return db, kr
}

// plantSecret seals one value cell under the project sealer and inserts it, so
// SampleSecretEntry returns a genuinely encrypted row bound to its AAD.
func plantSecret(t *testing.T, db *store.DB, kr *crypto.Keyring, plaintext string) {
	t.Helper()
	sealer, err := kr.ForProject(t.Context(), "org_p", "prj_p")
	if err != nil {
		t.Fatal(err)
	}
	aad := crypto.ValueAAD{OrgID: "org_p", ProjectID: "prj_p", EnvID: "env_p", KeyID: "key_p", RowID: "val_p", FieldTag: valueFieldTag}
	record, err := sealer.SealValue(aad, []byte(plaintext))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQLiteWrite().ExecContext(t.Context(),
		`INSERT INTO value_entries (id,org_id,project_id,environment_id,key_id,ciphertext,updated_at,updated_by) VALUES ('val_p','org_p','prj_p','env_p','key_p',?,'2026-08-17T00:00:00Z','usr_p')`,
		record); err != nil {
		t.Fatal(err)
	}
}

// TestProveValuesReadable is the drill's decrypt-proof, exercised for real: it
// must return true only when the stored secret actually opens under the
// keyring, and it must fail (not silently pass) when the ciphertext is
// tampered. This is what stops the drill's ValuesReadable from degrading to a
// hardcoded true.
func TestProveValuesReadable(t *testing.T) {
	db, kr := proveFixture(t)
	svc := &Backup{DB: db}

	// No secret stored yet: the proof is loud, not vacuously true.
	if _, err := svc.ProveValuesReadable(t.Context(), kr); err == nil {
		t.Fatal("ProveValuesReadable returned true with no secret to prove")
	}

	plantSecret(t, db, kr, "correct-horse-battery-staple")
	ok, err := svc.ProveValuesReadable(t.Context(), kr)
	if err != nil || !ok {
		t.Fatalf("a readable secret did not prove readable: ok=%v err=%v", ok, err)
	}

	// Tamper the ciphertext: the AEAD open must fail, and the proof must
	// surface that rather than reporting the value readable.
	if _, err := db.SQLiteWrite().ExecContext(t.Context(),
		`UPDATE value_entries SET ciphertext = X'00010203040506070809' WHERE id = 'val_p'`); err != nil {
		t.Fatal(err)
	}
	if ok, err := svc.ProveValuesReadable(t.Context(), kr); err == nil || ok {
		t.Fatalf("tampered ciphertext still proved readable: ok=%v err=%v", ok, err)
	}
}
