package isolation

import (
	"errors"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestTransitAdmissionLockAllowsForeignKeyChecksAndSerializesAdmissions(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		if db.Engine() != store.EnginePostgres {
			t.Skip("PostgreSQL lock-mode compatibility; SQLite admission is tested by the concurrent create limit")
		}
		ctx := t.Context()
		reference, err := db.PG().Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer reference.Rollback(ctx)
		if _, err := reference.Exec(ctx, "SELECT id FROM environments WHERE id='env_a1' FOR KEY SHARE"); err != nil {
			t.Fatal(err)
		}
		admission, err := db.PG().Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer admission.Rollback(ctx)
		if _, err := admission.Exec(ctx, "SET LOCAL lock_timeout='100ms'"); err != nil {
			t.Fatal(err)
		}
		args := pggen.TransitAdmissionLockParams{ChainOrg: string(orgA), ChainProject: string(prjA1), ChainEnv: string(envA1)}
		if _, err := pggen.New(admission).TransitAdmissionLock(ctx, args); err != nil {
			t.Fatalf("admission blocked a foreign-key reader: %v", err)
		}
		rival, err := db.PG().Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rival.Rollback(ctx)
		if _, err := rival.Exec(ctx, "SET LOCAL lock_timeout='100ms'"); err != nil {
			t.Fatal(err)
		}
		_, err = pggen.New(rival).TransitAdmissionLock(ctx, args)
		var locked *pgconn.PgError
		if !errors.As(err, &locked) || locked.Code != "55P03" {
			t.Fatalf("concurrent admission was not serialized: %v", err)
		}
		if err := admission.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if err := rival.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		next, err := db.PG().Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer next.Rollback(ctx)
		if _, err := next.Exec(ctx, "SET LOCAL lock_timeout='100ms'"); err != nil {
			t.Fatal(err)
		}
		if _, err := pggen.New(next).TransitAdmissionLock(ctx, args); err != nil {
			t.Fatalf("released admission stayed blocked: %v", err)
		}
	})
}
