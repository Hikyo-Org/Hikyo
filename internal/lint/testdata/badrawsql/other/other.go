// Package other holds restricted callers outside the owner's package.
package other

import (
	"context"
	"database/sql"

	"github.com/Hikyo-Org/hikyo/internal/lint/testdata/badrawsql/owner"
)

func Sneak(ctx context.Context, db *sql.DB) {
	owner.Restricted(ctx, db) // want "other/other.go:Sneak references protocol owner/owner.go:Restricted outside its reviewed caller body"
}

func sneakLocal(ctx context.Context, db *sql.DB) {
	owner.Restricted(ctx, db) // want "other/other.go:sneakLocal references protocol owner/owner.go:Restricted outside its reviewed caller body"
}
