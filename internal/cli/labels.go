package cli

import (
	"context"
	"net/http"
	"strings"
	"unicode"

	"github.com/Hikyo-Org/hikyo/api/apigen"
)

// idWithName renders "id (name)" for a human table. The ID stays in the cell so
// a script can still cut it; a missing or redundant name leaves the bare ID.
// Names are other people's input, so anything a terminal would act on goes:
// a newline or escape in a name must not forge a row in an audit or grant table.
func idWithName(id string, name *string) string {
	if name == nil {
		return id
	}
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			return -1
		}
		return r
	}, *name)
	if clean == "" || clean == id {
		return id
	}
	return id + " (" + clean + ")"
}

// lazyLabeler returns a function that labels an ID as "id (name)" from a name
// map fetched at most once, on the first label asked for. Naming is a courtesy:
// a failed fetch (nil map) leaves the bare ID and never fails the command, and
// JSON output never fetches, since it must stay the API's shape.
func lazyLabeler(format Format, fetch func() map[string]string) func(id string) string {
	if format == FormatJSON {
		return func(id string) string { return id }
	}
	var names map[string]string
	loaded := false
	return func(id string) string {
		if !loaded {
			loaded = true
			names = fetch()
		}
		name := names[id]
		return idWithName(id, &name)
	}
}

// environmentLabeler labels environment IDs with the project's environment names.
func environmentLabeler(ctx context.Context, client *Client, projectBase string, format Format) func(id string) string {
	return lazyLabeler(format, func() map[string]string {
		var list apigen.EnvironmentList
		if err := client.Do(ctx, http.MethodGet, projectBase+"/environments", nil, &list); err != nil {
			return nil
		}
		names := make(map[string]string, len(list.Items))
		for _, env := range list.Items {
			names[env.Id] = env.Name
		}
		return names
	})
}
