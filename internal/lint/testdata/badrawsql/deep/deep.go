// Package deep is two imports below the protocol owners: its helpers reach an
// owner only through the facts of helper, which imports it.
package deep

import "strings"

func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func Escape(s string) string { return strings.ReplaceAll(s, "%", "%%") }

// Q is a type whose method another package reaches through helper.NewQ, without
// importing deep itself.
type Q struct{}

func (Q) Lit(s string) string { return "'" + s + "'" }
