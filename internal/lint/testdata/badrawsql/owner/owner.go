// Package owner holds one function per RawSQL case. The reviewed pins of the
// fixture are internal/lint/testdata/reviewed_pins.json under this fixture's
// root. A missing owner, a retired caller pin and a dropped helper dependency
// have no source line to carry a want comment: the driver aggregates them, and
// TestVetDriverFixtures states them.
package owner

import (
	"context"
	"database/sql"

	"github.com/Hikyo-Org/hikyo/internal/lint/testdata/badrawsql/helper"
)

func Unpinned(ctx context.Context, db *sql.DB) {
	_, _ = db.ExecContext(ctx, "select 1") // want "executes handwritten SQL outside a named engine protocol"
}

func Clean(ctx context.Context, db *sql.DB) {
	_, _ = db.ExecContext(ctx, "select 2")
}

func StaleBody(ctx context.Context, db *sql.DB) { // want "changed its reviewed engine protocol"
	_, _ = db.ExecContext(ctx, "select 3")
}

func StaleDependency(ctx context.Context, db *sql.DB) { // want `changed reviewed helper dependency helper/helper.go:Build \(hash `
	_, _ = db.ExecContext(ctx, helper.Build("a"))
}

func StaleDeepDependency(ctx context.Context, db *sql.DB) { // want `changed reviewed helper dependency deep/deep.go:Escape \(hash `
	_, _ = db.ExecContext(ctx, helper.Wrap("a"))
}

func MissingDependency(ctx context.Context, db *sql.DB) { // want "is missing reviewed helper dependency helper/helper.go:Extra"
	_, _ = db.ExecContext(ctx, helper.Extra("a"))
}

func DroppedDependencies(ctx context.Context, db *sql.DB) {
	_, _ = db.ExecContext(ctx, "select 4")
}

func Restricted(ctx context.Context, db *sql.DB) {
	_, _ = db.ExecContext(ctx, "select 5")
}

func Caller(ctx context.Context, db *sql.DB) {
	Restricted(ctx, db)
}

func Intruder(ctx context.Context, db *sql.DB) {
	Restricted(ctx, db) // want "owner/owner.go:Intruder references protocol owner/owner.go:Restricted outside its reviewed caller body"
}

// ViaMethod reaches a method of a package it does not import (deep.Q.Lit)
// through helper.NewQ.
func ViaMethod(ctx context.Context, db *sql.DB) { // want `changed reviewed helper dependency deep/deep.go:Q.Lit \(hash `
	_, _ = db.ExecContext(ctx, helper.NewQ().Lit("a"))
}

// ViaUnexported reaches deep.Quote through the unexported helper.quote2.
func ViaUnexported(ctx context.Context, db *sql.DB) { // want `changed reviewed helper dependency helper/helper.go:quote2 \(hash `
	_, _ = db.ExecContext(ctx, helper.Build2("a"))
}
