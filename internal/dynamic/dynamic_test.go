package dynamic

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// Pin the public material contracts independently of the production constants
// and membership helpers, so generator and validator cannot drift together.
var passwordContract = regexp.MustCompile(`^[ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789]{32}$`)
var roleNameContract = regexp.MustCompile(`^hikyo_[a-z0-9]{1,57}$`)

// TestProviderInterfaceIsPinned freezes the security-load-bearing property of
// the provider seam: no method returns a string. The whole argument for the
// dynamic-secret feature is that the only credential string crossing the
// boundary is the password Hikyo GENERATES and passes IN via CreateRoleRequest,
// never one read back out. (Method count/name drift is a visible diff to the
// Provider declaration itself, so it is not restated here.)
func TestProviderInterfaceIsPinned(t *testing.T) {
	typ := reflect.TypeOf((*Provider)(nil)).Elem()
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		for j := 0; j < m.Type.NumOut(); j++ {
			if m.Type.Out(j).Kind() == reflect.String {
				t.Errorf("%s returns a string; a provider must never read a secret back out", m.Name)
			}
		}
	}
}

func TestGeneratePasswordShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		pw, err := GeneratePassword()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if !passwordContract.MatchString(pw) {
			t.Fatalf("generated password fails the independent 32-character alphabet contract")
		}
		if seen[pw] {
			t.Fatalf("duplicate password across draws: %q", pw)
		}
		seen[pw] = true
	}
}

func TestValidPasswordRejectsOffCharset(t *testing.T) {
	base := strings.Repeat("A", 32)
	for _, bad := range []string{
		"",                       // empty
		base[:len(base)-1],       // too short
		base + "A",               // too long
		base[:len(base)-1] + "'", // a quote (the DDL-break case)
		base[:len(base)-1] + " ", // whitespace
		base[:len(base)-1] + ";", // statement terminator
	} {
		if ValidPassword(bad) {
			t.Errorf("ValidPassword accepted off-charset value %q", bad)
		}
	}
}

func TestRoleNameShape(t *testing.T) {
	for _, tc := range []struct{ leaseID, want string }{
		{"dlease_0192f3a4-b5c6-7d8e-9fa0-b1c2d3e4f5a6", "hikyo_dlease0192f3a4b5c67d8e9fa0b1c2d3e4f5a6"},
		{"dlease_0192f3a4-b5c6-7d8e-9fa0-b1c2d3e4f5a7", "hikyo_dlease0192f3a4b5c67d8e9fa0b1c2d3e4f5a7"},
		{"dlease_UPPER-Case_ID", "hikyo_dleaseuppercaseid"},
		{"x", "hikyo_x"},
		{"'\";_-", "hikyo_role"},
		{strings.Repeat("a", 57), "hikyo_" + strings.Repeat("a", 56)},
	} {
		name := RoleName(tc.leaseID)
		if name != tc.want {
			t.Errorf("RoleName(%q)=%q, want %q", tc.leaseID, name, tc.want)
		}
		if !roleNameContract.MatchString(name) {
			t.Errorf("RoleName(%q)=%q fails the independent role-name contract", tc.leaseID, name)
		}
	}
}

func TestValidRoleNameRejectsInjection(t *testing.T) {
	for _, bad := range []string{
		"",
		"hikyo_",                    // prefix only
		"admin",                     // missing prefix
		"hikyo_a b",                 // space
		"hikyo_o'hare",              // single quote alone, without other invalid bytes
		"hikyo_a\"; DROP ROLE x;--", // identifier break attempt
		"hikyo_" + string(make([]byte, 70)),
	} {
		if ValidRoleName(bad) {
			t.Errorf("ValidRoleName accepted %q", bad)
		}
	}
}

// FuzzValidators checks both acceptance and rejection against independently
// pinned contracts, including valid inputs an always-false validator would miss.
func FuzzValidators(f *testing.F) {
	for _, seed := range []string{
		"", "hikyo_", "hikyo_abc123", "hikyo_o'hare", "hikyo_UPPER", "other_abc",
		"hikyo_" + strings.Repeat("a", 57), "hikyo_" + strings.Repeat("a", 58),
		strings.Repeat("A", 31), strings.Repeat("A", 32), strings.Repeat("A", 33),
		strings.Repeat("A", 31) + "'", strings.Repeat("A", 31) + "0",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := ValidPassword(s), passwordContract.MatchString(s); got != want {
			t.Fatalf("ValidPassword(%q)=%t, want %t", s, got, want)
		}
		if got, want := ValidRoleName(s), roleNameContract.MatchString(s); got != want {
			t.Fatalf("ValidRoleName(%q)=%t, want %t", s, got, want)
		}
	})
}
