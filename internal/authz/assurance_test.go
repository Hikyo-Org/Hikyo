package authz

import "testing"

// AdequateAssurance is the MFA-mandatory rule the chokepoint will enforce once
// factors ship: two distinct factor classes, or a WebAuthn assertion.
func TestAdequateAssurance(t *testing.T) {
	cases := []struct {
		name    string
		factors []string
		want    bool
	}{
		{"password only", []string{"password"}, false},
		{"empty", nil, false},
		{"password and totp", []string{"password", "totp"}, true},
		{"webauthn alone", []string{"webauthn"}, true},
		{"duplicate class is not two factors", []string{"password", "password"}, false},
		{"oidc single factor", []string{"oidc"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := AdequateAssurance(Assurance{Factors: c.factors}); got != c.want {
				t.Errorf("AdequateAssurance(%v) = %v, want %v", c.factors, got, c.want)
			}
		})
	}
}

// The gate is exempt for a session-less caller (local host authority): it
// presents no session assurance to inspect, so it is never MFA-gated.
func TestAssuranceInadequateGating(t *testing.T) {
	var a TxAuthorizer
	if a.assuranceInadequate(Identity{Principal: "p"}, OpOrgCreate) {
		t.Error("session-less local authority must be exempt from the MFA gate")
	}
}
