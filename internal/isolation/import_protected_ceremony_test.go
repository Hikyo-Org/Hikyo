package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestProtectedImportRequiresExactWrittenKeyCeremony(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "protected-import")
		ctx, auth, token := t.Context(), fixture.admin.auth, fixture.admin.token
		scope := scopeEnv(orgA, prjA1, envA1)
		execRaw(t, db, "UPDATE environments SET protected = TRUE WHERE id = 'env_a1'")
		before := latestRevision(t, db, string(envA1))
		req := service.ImportRequest{
			Entries:   []service.ImportEntry{{Key: ceremonySecretA, Value: "replacement"}, {Key: ceremonySecretB, Value: "must-skip"}},
			Overwrite: []string{ceremonySecretA},
		}
		if _, err := fixture.values.Import(ctx, service.Bearer(token), scope, req); !errors.Is(err, service.ErrNoReauthWindow) {
			t.Fatalf("protected import without ceremony: %v", err)
		}
		if after := latestRevision(t, db, string(envA1)); after != before {
			t.Fatalf("refused import advanced revision: %d -> %d", before, after)
		}
		token = passkeyCeremony(t, auth, ctx, token, service.PurposePublish, string(envA1),
			[]string{"key_" + ceremonySecretB}, fixture.device).SessionToken
		if _, err := fixture.values.Import(ctx, service.Bearer(token), scope, req); !errors.Is(err, service.ErrReauthUnitMismatch) {
			t.Fatalf("protected import with another key's ceremony: %v", err)
		}
		token = passkeyCeremony(t, auth, ctx, token, service.PurposePublish, string(envA1),
			[]string{"key_" + ceremonySecretA}, fixture.device).SessionToken
		result, err := fixture.values.Import(ctx, service.Bearer(token), scope, req)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Imported) != 1 || result.Imported[0] != ceremonySecretA || len(result.Skipped) != 1 || result.Skipped[0] != ceremonySecretB {
			t.Fatalf("exact import result: %+v", result)
		}
		if after := latestRevision(t, db, string(envA1)); after != before+1 {
			t.Fatalf("authorized import revision: %d -> %d", before, after)
		}
		// Skip-only requests open no values and commit no publication, so they
		// need no publish decision after the one-shot window was consumed.
		req.Overwrite = nil
		if result, err := fixture.values.Import(ctx, service.Bearer(token), scope, req); err != nil || len(result.Imported) != 0 {
			t.Fatalf("skip-only import: %+v, %v", result, err)
		}
	})
}
