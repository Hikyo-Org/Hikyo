// Package deep is two imports below the protocol owners: its helpers reach an
// owner only through the facts of helper, which imports it.
package deep

import "strings"

func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func Escape(s string) string { return strings.ReplaceAll(s, "%", "%%") }
