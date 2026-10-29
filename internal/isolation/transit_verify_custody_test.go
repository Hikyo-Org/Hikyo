package isolation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/domain"
	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
	"github.com/Hikyo-Org/hikyo/internal/transit"
)

// Verification may check custody availability, but must not ask any provider
// to use private material. The real provider creates and signs before this
// guard is installed; every later material-bearing call fails the test.
type verificationCustody struct {
	transit.Custody
	unavailable  bool
	checks       int
	privateCalls int
}

func (p *verificationCustody) Available(ctx context.Context) error {
	p.checks++
	if p.unavailable {
		return transit.ErrUnavailable
	}
	return p.Custody.Available(ctx)
}

func (p *verificationCustody) privateOperation() error {
	p.privateCalls++
	return errors.New("verification attempted a private custody operation")
}

func (p *verificationCustody) Create(context.Context, transit.Target) (transit.Version, error) {
	return transit.Version{}, p.privateOperation()
}

func (p *verificationCustody) Encrypt(context.Context, transit.Target, transit.Version, []byte, []byte) ([]byte, error) {
	return nil, p.privateOperation()
}

func (p *verificationCustody) Decrypt(context.Context, transit.Target, transit.Version, []byte, []byte) ([]byte, error) {
	return nil, p.privateOperation()
}

func (p *verificationCustody) Sign(context.Context, transit.Target, transit.Version, []byte) ([]byte, error) {
	return nil, p.privateOperation()
}

func (p *verificationCustody) MAC(context.Context, transit.Target, transit.Version, []byte) ([]byte, error) {
	return nil, p.privateOperation()
}

func (p *verificationCustody) Destroy(context.Context, transit.Target, transit.Version) error {
	return p.privateOperation()
}

func TestTransitVerifyCustodyAvailability(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		manager, _ := transitEnv(t, db, "_verify_custody")
		ctx := tctx(t)
		me := service.LocalPrincipal(alice)
		message := []byte("payload")
		for _, kind := range []transit.CustodyKind{transit.CustodySoftware, transit.CustodyExternal} {
			t.Run(string(kind), func(t *testing.T) {
				key := mustTransitKey(t, manager, service.CreateTransitKeyRequest{
					Name: "verify-" + string(kind), Algorithm: "ed25519", Custody: string(kind),
				})
				signed, err := manager.Sign(ctx, me, transitScope, key.Name, message, 0)
				if err != nil {
					t.Fatal(err)
				}
				provider, err := manager.Custody.Resolve(kind)
				if err != nil {
					t.Fatal(err)
				}
				other := transit.CustodySoftware
				if kind == other {
					other = transit.CustodyExternal
				}
				fallback, err := manager.Custody.Resolve(other)
				if err != nil {
					t.Fatal(err)
				}
				guard := &verificationCustody{Custody: provider}
				unused := &verificationCustody{Custody: fallback}
				registry := transit.NewRegistry(guard, unused)
				// No service keyring is supplied: public verification needs none.
				verify := &service.Transit{DB: db, Custody: registry}
				t.Cleanup(func() {
					if guard.privateCalls != 0 || unused.privateCalls != 0 || unused.checks != 0 {
						t.Errorf("private/fallback work: selected=%d, fallback=%d checks=%d", guard.privateCalls, unused.privateCalls, unused.checks)
					}
				})
				failures := func() int64 {
					return queryInt(t, db, `SELECT COUNT(*) FROM audit_tenant_events WHERE type='transit.operation' AND object_id='`+key.ID+`' AND outcome='failure' AND payload LIKE '%"operation":"verify"%' AND payload LIKE '%"refusal":"custody-unavailable"%'`)
				}
				wantUnavailable := func(t *testing.T) {
					t.Helper()
					before := failures()
					out, err := verify.Verify(ctx, me, transitScope, key.Name, message, signed.Value)
					if !errors.Is(err, service.ErrTransitCustodyUnavailable) || !errors.Is(err, transit.ErrUnavailable) {
						t.Fatalf("Verify = %+v, %v; want custody unavailable", out, err)
					}
					if out.KeyVersion != 0 || out.Valid || out.Value != "" || len(out.Plaintext) != 0 {
						t.Fatalf("refusal returned output: %+v", out)
					}
					if got := failures(); got != before+1 {
						t.Fatalf("durable custody failure audit = %d, want %d", got, before+1)
					}
				}
				for _, state := range []string{"available", "down", "missing", "recovered"} {
					t.Run(state, func(t *testing.T) {
						verify.Custody = registry
						guard.unavailable = state == "down"
						if state == "missing" {
							verify.Custody = transit.NewRegistry(unused)
						}
						if state == "down" || state == "missing" {
							wantUnavailable(t)
							return
						}
						before := guard.checks
						out, err := verify.Verify(ctx, me, transitScope, key.Name, message, signed.Value)
						if err != nil || !out.Valid || out.KeyVersion != 1 {
							t.Fatalf("available Verify = %+v, %v", out, err)
						}
						out, err = verify.Verify(ctx, me, transitScope, key.Name, []byte("different message"), signed.Value)
						if err != nil || out.Valid || out.KeyVersion != 1 {
							t.Fatalf("mismatched signature = %+v, %v", out, err)
						}
						if guard.checks != before+2 {
							t.Fatalf("availability checks = %d, want %d", guard.checks, before+2)
						}
					})
				}
				t.Run("policy_before_custody", func(t *testing.T) {
					verify.Custody = registry
					guard.unavailable = true
					before := guard.checks
					unknown := strings.Replace(signed.Value, "hikyo:v1:", "hikyo:v2:", 1)
					_, err := verify.Verify(ctx, me, transitScope, key.Name, message, unknown)
					wantConflictCause(t, err, "version")
					if _, err := manager.ChangeKeyState(ctx, me, transitScope, key.Name, "disable", 0); err != nil {
						t.Fatal(err)
					}
					_, err = verify.Verify(ctx, me, transitScope, key.Name, message, signed.Value)
					wantConflictCause(t, err, "state")
					if _, err := manager.ChangeKeyState(ctx, me, transitScope, key.Name, "enable", 0); err != nil {
						t.Fatal(err)
					}
					if _, err := manager.RotateKey(ctx, me, transitScope, key.Name); err != nil {
						t.Fatal(err)
					}
					sig2, err := manager.Sign(ctx, me, transitScope, key.Name, message, 0)
					if err != nil {
						t.Fatal(err)
					}
					two := uint32(2)
					if _, err := manager.ConfigureKey(ctx, me, transitScope, key.Name, service.ConfigureTransitKeyRequest{MinEncryptVersion: &two, MinDecryptVersion: &two}); err != nil {
						t.Fatal(err)
					}
					_, err = verify.Verify(ctx, me, transitScope, key.Name, message, signed.Value)
					wantConflictCause(t, err, "version")
					if _, err := manager.ChangeKeyState(ctx, me, transitScope, key.Name, "compromise", 0); err != nil {
						t.Fatal(err)
					}
					_, err = verify.Verify(ctx, me, transitScope, key.Name, message, sig2.Value)
					wantConflictCause(t, err, "compromised")
					for _, scope := range []domain.Scope{transitScope, {Org: orgA, Project: prjA1, Env: "env_prod"}} {
						actor := me
						if scope == transitScope {
							actor = service.LocalPrincipal(bob)
						}
						if _, err := verify.Verify(ctx, actor, scope, key.Name, message, sig2.Value); !errors.Is(err, domain.ErrNotFound) {
							t.Fatalf("unauthorized/sibling Verify = %v, want concealed not-found", err)
						}
					}
					if guard.checks != before {
						t.Fatalf("policy refusal contacted custody: %d -> %d", before, guard.checks)
					}
				})
			})
		}
		assertTransitAuditClean(t, db)
	})
}
