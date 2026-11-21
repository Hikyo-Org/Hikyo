// Package helper holds the helpers the protocol owners build SQL with.
package helper

import "github.com/Hikyo-Org/hikyo/internal/lint/testdata/badrawsql/deep"

func Build(s string) string { return "select " + deep.Quote(s) }

func Wrap(s string) string { return "select " + deep.Escape(s) }

func Extra(s string) string { return "select " + s }

func Spare(s string) string { return s }

func NewQ() deep.Q { return deep.Q{} }

// Build2 reaches deep.Quote through the unexported quote2.
func Build2(s string) string { return "select " + quote2(s) }

func quote2(s string) string { return deep.Quote(s) }
