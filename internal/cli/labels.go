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
		// Cf covers the direction controls and zero-width characters; Zl and Zp break lines.
		if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
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
// A machine credential gets the bare ID: a publish-only CI principal may not read
// the list, and its courtesy lookup would add a grant.denied to every run.
func environmentLabeler(ctx context.Context, client *Client, projectBase string, format Format) func(id string) string {
	return lazyLabeler(format, func() map[string]string {
		if client.machine {
			return nil
		}
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
