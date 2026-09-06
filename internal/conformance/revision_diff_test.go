package conformance

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func init() {
	corpus = append(corpus, scenario{"revision_diff_is_per_key_and_secret_safe", scenarioRevisionDiff})
}

func scenarioRevisionDiff(t *testing.T, db *store.DB) {
	who, scope, values, envs, keys := valueFixture(t, db, "revisiondiff")
	actor := service.LocalPrincipal(who)
	dev := mustEnv(t, envs, actor, scope, "dev")
	secret := mustKey(t, keys, actor, scope, "SECRET", string(schema.Secret), schema.DefaultPresenceRules())
	mustKey(t, keys, actor, scope, "OTHER_SECRET", string(schema.Secret), schema.DefaultPresenceRules())
	mustKey(t, keys, actor, scope, "CONFIG", string(schema.Config), schema.DefaultPresenceRules())
	publishValue(t, db, values, actor, dev, "SECRET", "same-secret")
	publishValue(t, db, values, actor, dev, "OTHER_SECRET", "other-secret")
	publishValue(t, db, values, actor, dev, "CONFIG", "before")
	left := latestRevisionOf(t, db, string(dev.Env))
	publishValue(t, db, values, actor, dev, "SECRET", "same-secret")
	publishValue(t, db, values, actor, dev, "CONFIG", "after")
	right := latestRevisionOf(t, db, string(dev.Env))
	reader := newPrincipal(t, db, "usr_diff_reader_"+string(scope.Project), []grantSpec{{capability: "read", scope: scope}})
	readActor := service.LocalPrincipal(reader)
	revisions := revisionSvc(t, db)
	diff, err := revisions.Diff(t.Context(), readActor, dev, left, right, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Items) != 3 {
		t.Fatalf("diff rows = %+v", diff)
	}
	for _, row := range diff.Items {
		if row.Classification == string(schema.Secret) {
			if row.Revealed || row.Before != nil || row.After != nil || row.Status == "unchanged" || row.Status == "changed" {
				t.Fatalf("secret comparison oracle: %+v", row)
			}
			if row.Name == "SECRET" && row.Status != "edited" {
				t.Fatalf("same-byte rewrite lost write-presence: %+v", row)
			}
		} else if row.Before == nil || *row.Before != "before" || row.After == nil || *row.After != "after" || row.Status != "changed" {
			t.Fatalf("config diff = %+v", row)
		}
	}
	if _, err := revisions.Diff(t.Context(), readActor, dev, left, right, secret.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("reader reveal = %v", err)
	}
	grantOrg(t, db, reader, scope.Org, "diffcurrent", "reveal")
	if _, err := revisions.Diff(t.Context(), readActor, dev, left, right, secret.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("current-only historical reveal = %v", err)
	}
	grantOrg(t, db, reader, scope.Org, "diffhistory", "reveal-history")
	disclosed, err := revisions.Diff(t.Context(), readActor, dev, left, right, secret.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(disclosed.Items) != 1 || disclosed.Items[0].KeyID != secret.ID || !disclosed.Items[0].Revealed || disclosed.Items[0].Before == nil || *disclosed.Items[0].Before != "same-secret" || disclosed.Items[0].After == nil || *disclosed.Items[0].After != "same-secret" || disclosed.Items[0].Status != "unchanged" {
		t.Fatalf("per-key reveal = %+v", disclosed)
	}
	if got := auditEventCount(t, db, string(dev.Env), "disclosure.value_revealed"); got != 2 {
		t.Fatalf("disclosure audit events = %d, want both revision reads", got)
	}
	if _, _, err := keys.Reclassify(t.Context(), actor, scope, secret.ID, string(schema.Config)); err != nil {
		t.Fatal(err)
	}
	reclassified := latestRevisionOf(t, db, string(dev.Env))
	sticky, err := revisions.Diff(t.Context(), readActor, dev, left, reclassified, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range sticky.Items {
		if row.KeyID == secret.ID && (row.Classification != string(schema.Secret) || row.Revealed || row.Before != nil || row.After != nil) {
			t.Fatalf("historical declassification: %+v", row)
		}
	}
	seed(t, db, []string{fmt.Sprintf("UPDATE snapshots SET payload_present=FALSE, collected_at='2026-08-17T00:00:00Z', collected_policy='diff-test' WHERE environment_id='%s' AND revision=%d", dev.Env, left)})
	_, err = revisions.Diff(t.Context(), readActor, dev, left, right, "")
	var collected *domain.CollectedRevisionError
	if !errors.As(err, &collected) || collected.Revision != left {
		t.Fatalf("collected diff = %v", err)
	}
}
