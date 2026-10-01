package conformance

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func init() {
	corpus = append(corpus, scenario{"delivery_presence_cursor_hides_secret_writes", scenarioDeliveryPresenceCursor})
}

func scenarioDeliveryPresenceCursor(t *testing.T, db *store.DB) {
	who, scope, values, envs, keys := valueFixture(t, db, "presencecursor")
	actor := service.LocalPrincipal(who)
	env := mustEnv(t, envs, actor, scope, "prod")
	mustKey(t, keys, actor, scope, "MODE", string(schema.Config), schema.DefaultPresenceRules())
	mustKey(t, keys, actor, scope, "PASSWORD", string(schema.Secret), schema.DefaultPresenceRules())
	publishValue(t, db, values, actor, env, "MODE", "production")
	publishValue(t, db, values, actor, env, "PASSWORD", "old hidden value")
	reader := newPrincipal(t, db, "usr_presence_cursor_"+string(scope.Project), []grantSpec{{capability: "read", scope: scope}})
	fetch := &service.Delivery{DB: db, Keyring: sharedKeyring(t, db)}
	first, err := fetch.FetchAs(t.Context(), service.LocalPrincipal(reader), env, "", service.FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range first.Keys {
		if key.Name == "PASSWORD" && key.Value != nil {
			t.Fatal("hidden value disclosed")
		}
	}
	for _, value := range []string{"new hidden value", "new hidden value"} {
		publishValue(t, db, values, actor, env, "PASSWORD", value)
		got, err := fetch.FetchAs(t.Context(), service.LocalPrincipal(reader), env, first.Cursor, service.FetchOptions{})
		if err != nil || !got.Current || got.Cursor != first.Cursor || got.ChangeToken != first.ChangeToken || len(got.Keys) != 0 {
			t.Fatalf("hidden plaintext/occurrence moved authorized cursor: current=%t error=%v", got.Current, err)
		}
	}
	publishValue(t, db, values, actor, env, "MODE", "development")
	visible, err := fetch.FetchAs(t.Context(), service.LocalPrincipal(reader), env, first.Cursor, service.FetchOptions{})
	if err != nil || visible.Current || visible.Cursor == first.Cursor {
		t.Fatalf("visible config failed to invalidate cursor: %v", err)
	}
	unpublishValue(t, db, values, actor, env, "PASSWORD")
	absent, err := fetch.FetchAs(t.Context(), service.LocalPrincipal(reader), env, visible.Cursor, service.FetchOptions{})
	if err != nil || absent.Current || absent.Cursor == visible.Cursor {
		t.Fatalf("authorized presence failed to invalidate cursor: %v", err)
	}
}
