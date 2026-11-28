package clientauth

import (
	"maps"
	"net/url"
	"slices"
)

// exactCallbackQuery accepts only ?code=<one nonempty value>&state=<state>.
func exactCallbackQuery(values url.Values, state string) bool {
	keys := slices.Sorted(maps.Keys(values))
	return slices.Equal(keys, []string{"code", "state"}) && len(values["code"]) == 1 && values.Get("code") != "" && len(values["state"]) == 1 && values.Get("state") == state
}
