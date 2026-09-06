package service

import "testing"

func TestValidateAccountProfile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile AccountProfile
		valid   bool
	}{
		{"valid", AccountProfile{Username: "marc", DisplayName: "Marc Went", Email: "marc@example.test"}, true},
		{"clear contact", AccountProfile{Username: "marc", DisplayName: "Marc"}, true},
		{"blank username", AccountProfile{DisplayName: "Marc"}, false},
		{"blank legacy name", AccountProfile{Username: "marc"}, true},
		{"whitespace", AccountProfile{Username: " marc", DisplayName: "Marc"}, false},
		{"control", AccountProfile{Username: "marc", DisplayName: "Marc\n"}, false},
		{"display address", AccountProfile{Username: "marc", DisplayName: "Marc", Email: "Marc <marc@example.test>"}, false},
		{"invalid email", AccountProfile{Username: "marc", DisplayName: "Marc", Email: "invalid"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateAccountProfile(tc.profile); (err == nil) != tc.valid {
				t.Fatalf("validation=%v want valid=%v", err, tc.valid)
			}
		})
	}
}
