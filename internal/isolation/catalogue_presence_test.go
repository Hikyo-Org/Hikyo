package isolation

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/schema"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func TestCataloguePresenceListAndDefinitions(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		f := seedDefinitionsProject(t, db, "presence_index", true)
		keys := keySvc(t, db)
		actor := service.LocalPrincipal(alice)
		// Insert out of name order; catalogue and definitions reads must retain
		// the store order while decoding all/none/explicit presence rules.
		for i, forbidden := range []schema.Presence{
			{Mode: schema.PresenceExplicit, Environments: []string{f.env}},
			{Mode: schema.PresenceAll},
			{Mode: schema.PresenceNone},
		} {
			_, err := keys.Create(t.Context(), actor, f.scope(), service.KeySpec{
				Name: fmt.Sprintf("PRESENCE_%d", 2-i), Classification: string(schema.Config),
				Declaration: schema.Declaration{Rule: &schema.Rule{Type: schema.TypeString}},
				Presence:    schema.PresenceRules{Required: schema.Presence{Mode: schema.PresenceNone}, Forbidden: forbidden},
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
		}
		listed, revision, err := keys.List(t.Context(), actor, f.scope())
		if err != nil || len(listed) != 4 {
			t.Fatalf("catalogue: len=%d revision=%d err=%v", len(listed), revision, err)
		}
		for i, key := range listed {
			got, err := keys.Get(t.Context(), actor, f.scope(), key.ID)
			if err != nil || !reflect.DeepEqual(key, got) {
				t.Fatalf("list/get mismatch for %s: %+v / %+v, %v", key.Name, key, got, err)
			}
			if i > 0 && listed[i-1].Name >= key.Name {
				t.Fatalf("catalogue order changed: %s before %s", listed[i-1].Name, key.Name)
			}
		}
		bundle := parseDefinitions(t, exportDefinitions(t, definitionsService(t, db), f))
		if bundle.BaseRevision == nil || *bundle.BaseRevision != revision || len(bundle.Keys) != len(listed) {
			t.Fatalf("definitions revision/cardinality differs: %+v", bundle)
		}
		for i, key := range bundle.Keys {
			want := listed[i]
			if key.ID != want.ID || key.RequiredIn.Mode != string(want.Presence.Required.Mode) || key.ForbiddenIn.Mode != string(want.Presence.Forbidden.Mode) {
				t.Fatalf("definitions key %d differs: %+v / %+v", i, key, want)
			}
			if want.Presence.Forbidden.Mode == schema.PresenceExplicit && !reflect.DeepEqual(key.ForbiddenIn.Environments, []string{"dev"}) {
				t.Fatalf("explicit presence export = %+v", key.ForbiddenIn)
			}
		}
		again, afterRevision, err := keys.List(t.Context(), actor, f.scope())
		if err != nil || afterRevision != revision || !reflect.DeepEqual(again, listed) {
			t.Fatalf("reads changed catalogue or revision: revision=%d err=%v", afterRevision, err)
		}
	})
}
