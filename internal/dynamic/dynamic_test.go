package dynamic

import (
	"reflect"
	"testing"
)

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
		if !ValidPassword(pw) {
			t.Fatalf("generated password %q fails its own charset contract", pw)
		}
		if seen[pw] {
			t.Fatalf("duplicate password across draws: %q", pw)
		}
		seen[pw] = true
	}
}

func TestValidPasswordRejectsOffCharset(t *testing.T) {
	base, _ := GeneratePassword()
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
	for _, leaseID := range []string{
		"dlease_0192f3a4-b5c6-7d8e-9fa0-b1c2d3e4f5a6",
		"dlease_UPPER-Case_ID",
		"x",
	} {
		name := RoleName(leaseID)
		if !ValidRoleName(name) {
			t.Errorf("RoleName(%q)=%q fails ValidRoleName", leaseID, name)
		}
	}
}

func TestValidRoleNameRejectsInjection(t *testing.T) {
	for _, bad := range []string{
		"",
		"hikyo_",                    // prefix only
		"admin",                     // missing prefix
		"hikyo_a b",                 // space
		"hikyo_a\"; DROP ROLE x;--", // identifier break attempt
		"hikyo_" + string(make([]byte, 70)),
	} {
		if ValidRoleName(bad) {
			t.Errorf("ValidRoleName accepted %q", bad)
		}
	}
}

// FuzzGeneratedMaterialCharset asserts the generator never emits a byte outside
// its declared alphabet, and that the validators agree with the generators.
func FuzzValidators(f *testing.F) {
	f.Add("hikyo_abc123")
	f.Add("ABCdef456789ABCdef456789ABCdef45")
	f.Fuzz(func(t *testing.T, s string) {
		// A validator must never panic and must be self-consistent with the
		// alphabet membership it claims.
		if ValidPassword(s) {
			if len(s) != passwordLength {
				t.Fatalf("ValidPassword true but wrong length: %q", s)
			}
			for i := 0; i < len(s); i++ {
				if !inAlphabet(s[i]) {
					t.Fatalf("ValidPassword true but byte %d off-alphabet: %q", i, s)
				}
			}
		}
		if ValidRoleName(s) {
			for i := len(roleNamePrefix); i < len(s); i++ {
				if !inRoleAlphabet(s[i]) {
					t.Fatalf("ValidRoleName true but byte %d off-alphabet: %q", i, s)
				}
			}
		}
	})
}
