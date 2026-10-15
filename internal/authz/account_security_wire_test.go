package authz

import (
	"slices"
	"testing"

	"github.com/Hikyo-Org/hikyo/internal/audit"
)

func TestAccountSecurityWireDeclaresDeveloperRevocations(t *testing.T) {
	wireEvents := (RegistryFacts{}).WireEvents()
	for _, tc := range []struct {
		route string
		event audit.EventType
	}{
		{"http:PUT /api/v1/auth/password", audit.EventAuthPasswordChanged},
		{"http:DELETE /api/v1/auth/identities/{id}", audit.EventIdentityUnlinked},
	} {
		t.Run(tc.route, func(t *testing.T) {
			// These security mutations revoke delegated developer credentials in
			// the same transaction. Their audit declarations must include that
			// conditional outcome alongside the mutation and replacement session.
			for _, event := range []audit.EventType{tc.event, audit.EventDeveloperCredentialRevoked, audit.EventAuthSessionCreated, audit.EventAuthThrottleCrossed} {
				if !slices.Contains(wireEvents[tc.route], event) {
					t.Errorf("%s omits emitted event %s", tc.route, event)
				}
			}
		})
	}
}
