package api_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Hikyo-Org/hikyo/api"
)

// Negative fixtures for the freeze gate (mvp-boundary S1).
//
// The ADR names three that must fail permanently — deleting a deprecated
// endpoint, removing a response-enum value, and removing a security
// requirement — and the 3.1 amendment adds the profile set: nullable, an
// alternate dialect, and top-level webhooks. Each is a base/revised pair
// differing by exactly one thing, because a fixture that changes two things
// proves neither.
//
// Fixture layout: api/testdata/freeze/<case>/{base.yaml,revised.yaml,verdict}
// where verdict is "pass" or "fail".

func TestFreezeGateFixtures(t *testing.T) {
	root := filepath.Join("testdata", "freeze")
	cases, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no freeze fixtures: the gate would be vacuously green")
	}
	sawFail := false
	for _, c := range cases {
		if !c.IsDir() {
			continue
		}
		t.Run(c.Name(), func(t *testing.T) {
			dir := filepath.Join(root, c.Name())
			base := mustRead(t, filepath.Join(dir, "base.yaml"))
			revised := mustRead(t, filepath.Join(dir, "revised.yaml"))
			verdict := strings.TrimSpace(string(mustRead(t, filepath.Join(dir, "verdict"))))

			violations, err := api.CheckFreeze(base, revised)
			if err != nil {
				t.Fatalf("gate errored: %v", err)
			}
			switch verdict {
			case "pass":
				if len(violations) > 0 {
					t.Fatalf("a permitted addition was refused:\n%s", render(violations))
				}
			case "fail":
				if len(violations) == 0 {
					t.Fatal("a forbidden change passed the gate")
				}
			default:
				t.Fatalf("verdict file says %q, want pass or fail", verdict)
			}
		})
		if strings.Contains(c.Name(), "remove") || strings.Contains(c.Name(), "delete") {
			sawFail = true
		}
	}
	if !sawFail {
		t.Error("no removal fixture present — the gate's whole purpose is unproven")
	}
}

// The gate must be green against the live contract compared with itself: a
// no-op diff producing violations would mean every future change is refused
// for reasons unrelated to the change.
func TestFreezeGateIsGreenAgainstAnIdenticalDocument(t *testing.T) {
	violations, err := api.CheckFreeze(api.SpecYAML, api.SpecYAML)
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("the contract is not equal to itself under the gate:\n%s", render(violations))
	}
}

// The allowlist is the policy, and an empty one would refuse everything —
// which is fail-closed but useless, and would be quietly "fixed" by whoever
// hit it first. Pin that it names the additions the promise actually offers.
func TestAllowlistNamesThePromisedAdditions(t *testing.T) {
	for _, want := range []string{
		"endpoint-added",
		"new-optional-request-property",
		"response-property-added",
	} {
		if !api.PermittedChanges[want] {
			t.Errorf("the allowlist does not permit %q, which the version promise offers", want)
		}
	}
	// And the things it must never permit, however oasdiff classifies them.
	for _, forbidden := range []string{
		"api-removed-without-deprecation",
		"api-removed-before-sunset",
		"request-property-became-required",
		"new-required-request-property",
		"api-security-removed",
		"response-property-enum-value-removed",
		// Growing an enum is not allowlisted: an OPEN enum carries no `enum`
		// keyword, so its growth is not an oasdiff enum change at all, and
		// permitting the id would only licence growth of the CLOSED ones.
		"response-property-enum-value-added",
		"request-property-enum-value-added",
		// A required response field becoming optional is a field every client
		// was entitled to require becoming one the server may omit.
		"response-property-became-optional",
	} {
		if api.PermittedChanges[forbidden] {
			t.Errorf("the allowlist permits %q, which is a break however oasdiff grades it", forbidden)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func render(vs []api.Violation) string {
	var b strings.Builder
	for _, v := range vs {
		b.WriteString("  " + v.String() + "\n")
	}
	return b.String()
}

// The one-time mldsa65 amendment (api-cli-surface, 2026-11-04). The base is
// the current spec with mldsa65 taken out of the enums it was added to, so the
// pair is exactly the amendment and needs no v1.0.0 tag in the checkout.
func mldsa65Pair(t *testing.T) (base, revised string) {
	t.Helper()
	revised = string(mustRead(t, "openapi.yaml"))
	base = strings.ReplaceAll(revised, ", mldsa65]", "]")
	if base == revised || strings.Contains(base, "mldsa65]") {
		t.Fatal("could not derive the pre-amendment spec from the current one")
	}
	return base, revised
}

func TestFreezeAllowsOnlyTheMLDSA65Amendment(t *testing.T) {
	base, revised := mldsa65Pair(t)
	violations, err := api.CheckFreeze([]byte(base), []byte(revised))
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("the exact mldsa65 additions were refused:\n%s", render(violations))
	}
	if n := len(api.MLDSA65EnumGrowth); n != 20 {
		t.Fatalf("the exception lists %d findings, the amendment names 20", n)
	}

	// Each entry covers exactly one finding, so dropping it surfaces that
	// finding alone. A match that ignored the path, operation or id would let
	// a sibling entry absorb it.
	all := api.MLDSA65EnumGrowth
	t.Cleanup(func() { api.MLDSA65EnumGrowth = all })
	for i, want := range all {
		api.MLDSA65EnumGrowth = slices.Delete(slices.Clone(all), i, i+1)
		got, err := api.CheckFreeze([]byte(base), []byte(revised))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != want.ID || got[0].Operation != want.Operation || got[0].Path != want.Path {
			t.Errorf("without %+v the guard reported:\n%s", want, render(got))
		}
	}
	api.MLDSA65EnumGrowth = all

	// Each near miss is the amendment plus one more enum edit, and must fail.
	transit := "enum: [xchacha20-poly1305, ed25519, hmac-sha256, mldsa65]"
	for name, edit := range map[string][3]string{
		"mldsa44 on the same path":       {transit, "enum: [xchacha20-poly1305, ed25519, hmac-sha256, mldsa65, mldsa44]", "mldsa44"},
		"future mldsa87 on the same":     {transit, "enum: [xchacha20-poly1305, ed25519, hmac-sha256, mldsa65, mldsa87]", "mldsa87"},
		"mldsa65 on another enum":        {"enum: [software, external]", "enum: [software, external, mldsa65]", "mldsa65"},
		"mldsa65 on a PKI-adjacent enum": {"enum: [ed25519, ecdsa-p256, rsa-3072]", "enum: [ed25519, ecdsa-p256, rsa-3072, mldsa65]", "mldsa65"},
	} {
		t.Run(name, func(t *testing.T) {
			if strings.Count(revised, edit[0]) == 0 {
				t.Fatalf("fixture anchor %q is gone from the spec", edit[0])
			}
			mutated := strings.Replace(revised, edit[0], edit[1], 1)
			violations, err := api.CheckFreeze([]byte(base), []byte(mutated))
			if err != nil {
				t.Fatal(err)
			}
			// The violation must be about the near miss, not an unrelated drift.
			if len(violations) == 0 || !strings.Contains(render(violations), "`"+edit[2]+"`") {
				t.Fatalf("a near miss of the mldsa65 amendment was not refused for %s:\n%s", edit[2], render(violations))
			}
		})
	}
}

// mintReauthPair derives the spec before the mint amendment (#1516) from the
// current one. It leaves `access` in both documents, so the pair is exactly the
// mint additions; accessReauthPair does the same for #1531.
func mintReauthPair(t *testing.T) (base, revised string) {
	t.Helper()
	revised = string(mustRead(t, "openapi.yaml"))
	base = strings.ReplaceAll(revised, "bypass, mint, access, self-config", "bypass, access, self-config")
	base = strings.ReplaceAll(base, "approval.bypass, identity.credential-mint, access.bypass, self-config", "approval.bypass, access.bypass, self-config")
	if base == revised || strings.Contains(base, "bypass, mint") || strings.Contains(base, "approval.bypass, identity.credential-mint") {
		t.Fatal("could not derive the pre-amendment spec from the current one")
	}
	return base, revised
}

func accessReauthPair(t *testing.T) (base, revised string) {
	t.Helper()
	revised = string(mustRead(t, "openapi.yaml"))
	base = strings.ReplaceAll(revised, "bypass, mint, access, self-config", "bypass, mint, self-config")
	base = strings.ReplaceAll(base, "identity.credential-mint, access.bypass, self-config", "identity.credential-mint, self-config")
	if base == revised || strings.Contains(base, "mint, access") || strings.Contains(base, "access.bypass, self-config") {
		t.Fatal("could not derive the pre-amendment spec from the current one")
	}
	return base, revised
}

// Both amendments together are the real v1.0.0 to current pair.
func TestFreezeAllowsBothReauthAmendmentsTogether(t *testing.T) {
	base, revised := mintReauthPair(t)
	base = strings.ReplaceAll(base, "bypass, access, self-config", "bypass, self-config")
	base = strings.ReplaceAll(base, "approval.bypass, access.bypass, self-config", "approval.bypass, self-config")
	if strings.Contains(base, "access.bypass, self-config") || strings.Contains(base, "bypass, access") {
		t.Fatal("could not derive the v1.0.0 enums from the current spec")
	}
	violations, err := api.CheckFreeze([]byte(base), []byte(revised))
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("mint and access together were refused:\n%s", render(violations))
	}
}

func TestFreezeAllowsOnlyTheMintReauthAmendment(t *testing.T) {
	testReauthAmendment(t, mintReauthPair, &api.MintReauthEnumGrowth, "mint", "identity.credential-mint", "identity.credential-rotate")
}

func TestFreezeAllowsOnlyTheAccessReauthAmendment(t *testing.T) {
	testReauthAmendment(t, accessReauthPair, &api.AccessReauthEnumGrowth, "access", "access.bypass", "access.revoke")
}

func testReauthAmendment(t *testing.T, pair func(*testing.T) (string, string), growth *[]api.EnumGrowth, value, operationValue, otherOperation string) {
	t.Helper()
	base, revised := pair(t)
	violations, err := api.CheckFreeze([]byte(base), []byte(revised))
	if err != nil {
		t.Fatal(err)
	}
	if len(violations) > 0 {
		t.Fatalf("the exact %s additions were refused:\n%s", value, render(violations))
	}
	if n := len(*growth); n != 4 {
		t.Fatalf("the exception lists %d findings, the amendment names 4", n)
	}

	all := *growth
	t.Cleanup(func() { *growth = all })
	for i, want := range all {
		*growth = slices.Delete(slices.Clone(all), i, i+1)
		got, err := api.CheckFreeze([]byte(base), []byte(revised))
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != want.ID || got[0].Operation != want.Operation || got[0].Path != want.Path {
			t.Errorf("without %+v the guard reported:\n%s", want, render(got))
		}
	}
	*growth = all

	// A near miss is the amendment plus one more value, or a value on the
	// wrong enum, and must fail on that value.
	purpose := "bypass, mint, access, self-config, developer-credential]"
	operation := "approval.bypass, identity.credential-mint, access.bypass, self-config.adopt"
	other := map[string]string{"mint": "access", "access": "mint"}[value]
	otherOperationValue := map[string]string{"mint": "access.bypass", "access": "identity.credential-mint"}[value]
	for name, edit := range map[string][3]string{
		"another purpose":                   {purpose, "bypass, mint, access, rotate, self-config, developer-credential]", "rotate"},
		"another operation":                 {operation, "approval.bypass, identity.credential-mint, access.bypass, " + otherOperation + ", self-config.adopt", otherOperation},
		"the operation value on purpose":    {purpose, "bypass, mint, access, " + operationValue + ", self-config, developer-credential]", operationValue},
		"the purpose value on operation":    {operation, "approval.bypass, identity.credential-mint, access.bypass, " + value + ", self-config.adopt", value},
		"the value on another closed enum":  {"enum: [software, external]", "enum: [software, external, " + value + "]", value},
		"mldsa65 on the reauth request":     {purpose, "bypass, mint, access, mldsa65, self-config, developer-credential]", "mldsa65"},
		"the sibling value on another enum": {"enum: [software, external]", "enum: [software, external, " + other + "]", other},
		"the sibling operation on another":  {"enum: [software, external]", "enum: [software, external, " + otherOperationValue + "]", otherOperationValue},
	} {
		t.Run(name, func(t *testing.T) {
			if strings.Count(revised, edit[0]) == 0 {
				t.Fatalf("fixture anchor %q is gone from the spec", edit[0])
			}
			mutated := strings.Replace(revised, edit[0], edit[1], 1)
			violations, err := api.CheckFreeze([]byte(base), []byte(mutated))
			if err != nil {
				t.Fatal(err)
			}
			if len(violations) == 0 || !strings.Contains(render(violations), "`"+edit[2]+"`") {
				t.Fatalf("a near miss of the %s amendment was not refused for %s:\n%s", value, edit[2], render(violations))
			}
		})
	}
}
