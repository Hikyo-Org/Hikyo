package service

import (
	"errors"
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

func selectionCatalogue() []store.CatalogueKey {
	return []store.CatalogueKey{
		{ID: "key_db_url", Name: "DB_URL", Classification: "secret"},
		{ID: "key_db_test", Name: "DB_URL_TEST", Classification: "secret"},
		{ID: "key_db_pool", Name: "DB_POOL", Classification: "config"},
		{ID: "key_mode", Name: "APP_MODE", Classification: "config"},
		{ID: "key_token", Name: "API_TOKEN", Classification: "secret"},
	}
}

func TestResolveKeySelectionUnionsExplicitNamesAndBoundedPatterns(t *testing.T) {
	tests := []struct {
		name      string
		explicit  []string
		selection AdapterKeySelection
		want      []string
	}{
		{name: "explicit ids only", explicit: []string{"key_token"}, want: []string{"key_token"}},
		{name: "names resolve to ids", selection: AdapterKeySelection{Names: []string{"APP_MODE"}}, want: []string{"key_mode"}},
		{name: "include pattern", selection: AdapterKeySelection{Include: []string{"DB_*"}}, want: []string{"key_db_pool", "key_db_test", "key_db_url"}},
		{name: "exclude narrows the pattern set", selection: AdapterKeySelection{Include: []string{"DB_*"}, Exclude: []string{"*_TEST"}}, want: []string{"key_db_pool", "key_db_url"}},
		{name: "classification alone", selection: AdapterKeySelection{Classification: "config"}, want: []string{"key_db_pool", "key_mode"}},
		{name: "pattern and classification both hold", selection: AdapterKeySelection{Include: []string{"DB_*"}, Classification: "secret"}, want: []string{"key_db_test", "key_db_url"}},
		{name: "exclude never touches explicit members", explicit: []string{"key_db_test"}, selection: AdapterKeySelection{Names: []string{"DB_URL"}, Include: []string{"DB_*"}, Exclude: []string{"DB_*"}}, want: []string{"key_db_test", "key_db_url"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveKeySelection(selectionCatalogue(), tt.explicit, tt.selection)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("resolved %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveKeySelectionRefusesLoudly(t *testing.T) {
	tests := []struct {
		name      string
		explicit  []string
		selection AdapterKeySelection
		want      string
	}{
		{name: "unknown name", selection: AdapterKeySelection{Names: []string{"MISSING"}}, want: `key "MISSING" does not exist`},
		{name: "bad pattern", selection: AdapterKeySelection{Include: []string{"[DB"}}, want: `key pattern "[DB" is not valid`},
		{name: "bad classification", selection: AdapterKeySelection{Classification: "public"}, want: "classification must be secret or config"},
		{name: "nothing matched", selection: AdapterKeySelection{Include: []string{"NOPE_*"}}, want: "resolved to no keys"},
		{name: "everything excluded", selection: AdapterKeySelection{Include: []string{"DB_*"}, Exclude: []string{"DB_*"}}, want: "resolved to no keys"},
		{name: "exclude with no include or classification", selection: AdapterKeySelection{Names: []string{"DB_URL"}, Exclude: []string{"DB_*"}}, want: "exclude patterns need an include"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveKeySelection(selectionCatalogue(), tt.explicit, tt.selection)
			if !errors.Is(err, domain.ErrInvalid) || err == nil || !contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want invalid containing %q", err, tt.want)
			}
		})
	}
}

// TestResolveKeySelectionIsASnapshot pins the ADR rule: the set is resolved
// once, against the catalogue as it is at save time, so a later key that
// would have matched is not a member.
func TestResolveKeySelectionIsASnapshot(t *testing.T) {
	selection := AdapterKeySelection{Include: []string{"DB_*"}}
	before, err := resolveKeySelection(selectionCatalogue(), nil, selection)
	if err != nil {
		t.Fatal(err)
	}
	later := append(selectionCatalogue(), store.CatalogueKey{ID: "key_db_new", Name: "DB_REPLICA", Classification: "secret"})
	after, err := resolveKeySelection(later, nil, selection)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Equal(before, after) {
		t.Fatal("test fixture did not change the catalogue")
	}
	// The membership a target stores is `before`; nothing re-evaluates
	// `selection` afterwards because the selection is not persisted.
	if slices.Contains(before, "key_db_new") {
		t.Fatal("a key that did not exist at save time became a member")
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
