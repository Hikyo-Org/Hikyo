package service

import "testing"

func TestValidateAccountProfile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile ProfileUpdate
		valid   bool
	}{
		{"valid", ProfileUpdate{Username: "marc", DisplayName: "Marc Went"}, true},
		{"blank username", ProfileUpdate{DisplayName: "Marc"}, false},
		{"blank legacy name", ProfileUpdate{Username: "marc"}, true},
		{"whitespace", ProfileUpdate{Username: " marc", DisplayName: "Marc"}, false},
		{"control", ProfileUpdate{Username: "marc", DisplayName: "Marc\n"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateAccountProfile(tc.profile); (err == nil) != tc.valid {
				t.Fatalf("validation=%v want valid=%v", err, tc.valid)
			}
		})
	}
}
