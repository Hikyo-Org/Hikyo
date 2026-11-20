// Package helper holds the helpers the protocol owners build SQL with.
package helper

import "github.com/Hikyo-Org/hikyo/internal/lint/testdata/badrawsql/deep"

func Build(s string) string { return "select " + deep.Quote(s) }

func Wrap(s string) string { return "select " + deep.Escape(s) }

func Extra(s string) string { return "select " + s }

func Spare(s string) string { return s }
