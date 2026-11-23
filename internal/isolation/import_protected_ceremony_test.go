package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
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
		// A machine has no ceremony to complete, and import has no field to
		// confirm a protected write, so it is refused rather than let through.
		if _, err := fixture.values.Import(ctx, service.LocalPrincipal(fixture.admin.boot.PrincipalID), scope, req); !errors.Is(err, service.ErrProtectedDestination) {
			t.Fatalf("protected import by a machine caller: %v", err)
		}
		if after := latestRevision(t, db, string(envA1)); after != before {
			t.Fatalf("machine import advanced revision: %d -> %d", before, after)
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
		// A covered destination refuses on the approval gate before a human is
		// asked for the ceremony this write would otherwise need.
		admin := service.LocalPrincipal(fixture.admin.boot.PrincipalID)
		if _, err := (&service.Approvals{DB: db}).CreatePolicy(ctx, admin, domain.Scope{Org: orgA, Project: prjA1}, service.ApprovalPolicyInput{
			EnvironmentID: string(envA1), MinApprovals: 1, RequestTTLSeconds: 3600, Enabled: true,
			Approvers: []service.ApprovalApproverSpec{{Kind: "principal", SubjectID: string(fixture.admin.boot.PrincipalID)}},
		}); err != nil {
			t.Fatalf("policy for %s: %v", envA1, err)
		}
		req.Overwrite = []string{ceremonySecretA}
		if _, err := fixture.values.Import(ctx, service.Bearer(token), scope, req); !errors.Is(err, service.ErrApprovalRequired) {
			t.Fatalf("protected import into a covered destination: %v, want approval required", err)
		}
	})
}

// Declare delivers a supplied value like a publish, so a protected destination
// takes the same exact-key publish ceremony as import and refuses a machine.
func TestProtectedDeclareRequiresExactKeyCeremony(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		fixture := ceremonyFixture(t, db, "protected-declare")
		ctx, auth, token := t.Context(), fixture.admin.auth, fixture.admin.token
		project := domain.Scope{Org: orgA, Project: prjA1}
		execRaw(t, db, "UPDATE environments SET protected = TRUE WHERE id = 'env_a1'")
		before := latestRevision(t, db, string(envA1))
		declare := func(actor service.Actor) error {
			_, _, err := fixture.values.Declare(ctx, actor, project, []string{string(envA1)}, ceremonySecretA, "declared")
			return err
		}
		if err := declare(service.Bearer(token)); !errors.Is(err, service.ErrNoReauthWindow) {
			t.Fatalf("protected declare without ceremony: %v", err)
		}
		if err := declare(service.LocalPrincipal(fixture.admin.boot.PrincipalID)); !errors.Is(err, service.ErrProtectedDestination) {
			t.Fatalf("protected declare by a machine caller: %v", err)
		}
		if after := latestRevision(t, db, string(envA1)); after != before {
			t.Fatalf("refused declare advanced revision: %d -> %d", before, after)
		}
		token = passkeyCeremony(t, auth, ctx, token, service.PurposePublish, string(envA1),
			[]string{"key_" + ceremonySecretB}, fixture.device).SessionToken
		if err := declare(service.Bearer(token)); !errors.Is(err, service.ErrReauthUnitMismatch) {
			t.Fatalf("protected declare with another key's ceremony: %v", err)
		}
		token = passkeyCeremony(t, auth, ctx, token, service.PurposePublish, string(envA1),
			[]string{"key_" + ceremonySecretA}, fixture.device).SessionToken
		if err := declare(service.Bearer(token)); err != nil {
			t.Fatalf("protected declare with its ceremony: %v", err)
		}
		if after := latestRevision(t, db, string(envA1)); after != before+1 {
			t.Fatalf("authorized declare revision: %d -> %d", before, after)
		}
	})
}
