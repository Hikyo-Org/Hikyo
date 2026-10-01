package isolation

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Hikyo-Org/hikyo/internal/authz"
	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	storetx "github.com/Hikyo-Org/hikyo/internal/store/tx"
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
