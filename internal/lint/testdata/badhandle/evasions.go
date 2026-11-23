package badhandle

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pool is the alias escape: `*pool` is spelled without ever writing
// pgxpool.Pool at the use site, so a String()-match on the written type
// and the module-scope guard both miss it unless aliases are unwrapped.
type pool = pgxpool.Pool // want "names driver type github\\.com/jackc/pgx/v5/pgxpool\\.Pool"

type aliasHolder interface{ PG() *pool } // want "names driver type github\\.com/jackc/pgx/v5/pgxpool\\.Pool"

func viaAlias(ctx context.Context, h aliasHolder, q string) error { // want "names driver type github\\.com/jackc/pgx/v5/pgxpool\\.Pool"
	_, err := h.PG().Exec(ctx, q) // want "names driver type github\\.com/jackc/pgx/v5/pgxpool\\.Pool"
	return err
}

// holder is the generic escape: the declaration carries only the type
// parameter T, so the concrete handle type exists solely in the
// instantiation expression below.
type holder[T any] interface{ Handle() T }

func viaGenericInstantiation(ctx context.Context, db any, q string) error {
	if h, ok := db.(holder[*pgxpool.Pool]); ok { // want "names driver type github\\.com/jackc/pgx/v5/pgxpool\\.Pool"
		_, err := h.Handle().Exec(ctx, q) // want "names driver type github\\.com/jackc/pgx/v5/pgxpool\\.Pool"
		return err
	}
	return nil
}

var _ = viaAlias // want "names driver type github\\.com/jackc/pgx/v5/pgxpool\\.Pool"
var _ = viaGenericInstantiation
