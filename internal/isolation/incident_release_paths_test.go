package isolation

import (
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/service"
	"github.com/Hikyo-Org/hikyo/internal/store"
)

// Recovery-code consumption is not a way back for a contained account: the
// lookup it uses is containment-gated, so it spends no code and mints no
// authority. Release therefore never sees a recovery establishment.
func TestIncidentContainedAccountRefusesRecoveryCodeEstablishment(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		s := containmentFixture(t, db)
		ctx := t.Context()
		codes, _, err := s.auth.GenerateRecoveryCodes(ctx, s.token, containedPassword)
		if err != nil || len(codes) < 2 {
			t.Fatalf("recovery codes: %d, %v", len(codes), err)
		}
		// Positive control: the same path works before containment.
		if _, err := s.auth.ConsumeRecoveryCode(ctx, s.username, codes[1]); err != nil {
			t.Fatalf("recovery before containment: %v", err)
		}
		s.contain(t)
		minted := queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities WHERE account_id = '`+s.account+`' AND issued_by = 'recovery'`)
		if _, err := s.auth.ConsumeRecoveryCode(ctx, s.username, codes[0]); err == nil {
			t.Fatal("a contained account consumed a recovery code")
		}
		if n := queryInt(t, db, `SELECT COUNT(*) FROM credential_authorities WHERE account_id = '`+s.account+`' AND issued_by = 'recovery'`); n != minted {
			t.Fatalf("recovery minted %d authorities for a contained account", n-minted)
		}
	})
}

// A federated claim is not a way back either: even with an authority minted
// after containment, the claim finds no account to establish.
func TestIncidentContainedAccountRefusesFederatedClaim(t *testing.T) {
	forEngines(t, func(t *testing.T, db *store.DB) {
		for _, kind := range []string{service.OIDCKind, service.OAuth2Kind} {
			t.Run(kind, func(t *testing.T) {
				d := newClaimDriver(t, db, kind)
				// Positive control: an uncontained invited account claims through the same path.
				control := d.invite("control")
				if _, err := d.claim(control.Authority, d.subject()); err != nil {
					t.Fatalf("claim of an uncontained account: %v", err)
				}
				inv := d.invite("contained")
				containPrincipalForTest(t, db, inv.PrincipalID)
				reset := d.reset(inv)
				if _, err := d.claim(reset.Authority, d.subject()); err == nil {
					t.Fatal("a contained account was claimed through a federated identity")
				}
				if n := d.identities(inv.AccountID); n != 0 {
					t.Fatalf("%d identities linked to a contained account", n)
				}
				if d.consumed(reset.AuthorityID) {
					t.Fatal("the refused claim consumed the authority")
				}
			})
		}
	})
}
