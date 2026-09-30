package isolation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/store/pggen"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPKIStoredDEKVersionRefusesCorruption(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		now := time.Now().UTC().Truncate(time.Second)
		svc := pkiSvc(t, db, &pkiClock{now: now})
		issuer, err := svc.CreateIssuer(t.Context(), service.LocalPrincipal(root), service.PKIIssuerRequest{Mode: "root", Name: "corrupt-dek", CommonName: "Version boundary CA", TTL: 365 * 24 * time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		readKey := func() error {
			return storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
				proof, err := az.Authorize(ctx, authz.Identity{Principal: root}, authz.OpPKIIssuerCreate, domain.Scope{})
				if err != nil {
					return err
				}
				_, _, err = repos.PKI().IssuerKey(ctx, proof, issuer.Issuer.ID)
				return err
			})
		}
		listReencrypt := func() error {
			return storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
				proof, err := az.Authorize(ctx, authz.Identity{Principal: root}, authz.OpReencryptInstance, domain.Scope{})
				if err != nil {
					return err
				}
				rows, err := repos.Reencrypt().ListPkiIssuersForReencrypt(ctx, proof, "", 10)
				if err == nil && len(rows) != 1 {
					return fmt.Errorf("expected one issuer for reencryption, got %d", len(rows))
				}
				return err
			})
		}
		if err := listReencrypt(); err != nil {
			t.Fatalf("valid reencryption row: %v", err)
		}
		originalVersion := queryInt(t, db, "SELECT dek_version FROM pki_issuers WHERE id='"+issuer.Issuer.ID+"'")
		if err := readKey(); err != nil {
			t.Fatalf("valid key: %v", err)
		}
		runtime := store.NewPKIRuntime(db)
		if rows, err := runtime.DueCRLs(t.Context(), now); err != nil || len(rows) != 1 {
			t.Fatalf("valid candidate: %+v %v", rows, err)
		}
		for _, version := range []int64{-1, math.MaxUint32 + 1, math.MaxInt64} {
			execRaw(t, db, fmt.Sprintf("UPDATE pki_issuers SET dek_version=%d WHERE id='%s'", version, issuer.Issuer.ID))
			if err := readKey(); err == nil || !strings.Contains(err.Error(), "dek_version out of range") {
				t.Fatalf("corrupt key version %d: %v", version, err)
			}
			if _, err := runtime.DueCRLs(t.Context(), now); err == nil || !strings.Contains(err.Error(), "dek_version out of range") {
				t.Fatalf("corrupt candidate version %d: %v", version, err)
			}
			if err := listReencrypt(); err == nil || !strings.Contains(err.Error(), "dek_version out of range") {
				t.Fatalf("corrupt reencryption version %d: %v", version, err)
			}
		}
		execRaw(t, db, fmt.Sprintf("UPDATE pki_issuers SET dek_version=%d WHERE id='%s'", originalVersion, issuer.Issuer.ID))
		for _, version := range []int64{math.MaxUint32 + 1, math.MaxInt64} {
			execRaw(t, db, fmt.Sprintf("UPDATE pki_issuers SET row_version=%d WHERE id='%s'", version, issuer.Issuer.ID))
			if err := listReencrypt(); err == nil || !strings.Contains(err.Error(), "row_version out of range") {
				t.Fatalf("corrupt reencryption row version %d: %v", version, err)
			}
		}
	})
}

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

func TestReencryptGeneratedIntegerInputsRefuseNarrowing(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for _, input := range []struct {
			name     string
			limit    int
			dek, row uint32
		}{
			{"valid", 10, 1, 1}, {"limit", math.MaxInt32 + 1, 1, 1}, {"dek", 10, math.MaxUint32, 1}, {"row", 10, 1, math.MaxUint32},
		} {
			t.Run(input.name, func(t *testing.T) {
				err := storetx.Write(t.Context(), db, func(ctx context.Context, repos store.Repos, az *authz.TxAuthorizer) error {
					proof, err := az.Authorize(ctx, authz.Identity{Principal: root}, authz.OpReencryptInstance, domain.Scope{})
					if err != nil {
						return err
					}
					if input.name == "limit" {
						_, err = repos.Reencrypt().ListPasswordCredsForReencrypt(ctx, proof, "", input.limit)
						return err
					}
					_, err = repos.Reencrypt().ReencryptSelfConfigSeedInput(ctx, proof, "absent-input", []byte("fixture"), []byte("old fixture"), input.dek, input.row)
					return err
				})
				if db.Engine() == store.EnginePostgres && input.name != "valid" {
					if err == nil || !strings.Contains(err.Error(), "PostgreSQL integer out of range") {
						t.Fatalf("wide %s input did not fail before query: %v", input.name, err)
					}
				} else if err != nil {
					t.Fatalf("representable %s input failed: %v", input.name, err)
				}
			})
		}
	})
}
