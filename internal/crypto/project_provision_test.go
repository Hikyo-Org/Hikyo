package crypto

import (
	"context"
	"testing"
)

func TestPreparedProjectKeyIsUsableOnlyAfterItsTransactionPersistsIt(t *testing.T) {
	ctx := context.Background()
	storage := newMemStore()
	root := newRoot(t)
	kr, err := LoadKeyring(ctx, storage, append([]byte(nil), root...))
	if err != nil {
		t.Fatal(err)
	}
	row, sealer, err := kr.PrepareNewProject("org_new", "prj_new")
	if err != nil {
		t.Fatal(err)
	}
	aad := ValueAAD{OrgID: "org_new", ProjectID: "prj_new", EnvID: "env_new", KeyID: "key_new", RowID: "val_new", FieldTag: "value"}
	sealed, err := sealer.SealValue(aad, []byte("  exact-secret\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := storage.Tier3Versions(ctx, PurposeProject, "org_new", "prj_new")
	if err != nil || len(rows) != 0 {
		t.Fatalf("prepare persisted a project key: %v", err)
	}
	if err := storage.CreateTier3(ctx, row); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadKeyring(ctx, storage, root)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := reloaded.ForProject(ctx, "org_new", "prj_new")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := reader.OpenValue(aad, sealed)
	if err != nil || string(plain) != "  exact-secret\r\n" {
		t.Fatalf("committed project cannot open its seeded value: %v", err)
	}
}
