package domain

import (
	"errors"
	"strings"
	"testing"
)

// Canonical email form (social-signin spec 2.5): the fixtures #605 names.
func TestCanonicalEmail(t *testing.T) {
	for _, c := range []struct {
		name, in, want string
		err            error
	}{
		{"plain", "user@example.com", "user@example.com", nil},
		{"domain lowercased, local preserved", "User.Name+Tag@Example.COM", "User.Name+Tag@example.com", nil},
		// net/mail unquotes a quoted local part, so it is not the bare form
		// presented; refused, as the profile validator already does.
		{"quoted local part refused", `"John Doe"@example.com`, "", ErrEmailMalformed},
		{"punycode domain is ASCII", "user@xn--bcher-kva.example", "user@xn--bcher-kva.example", nil},
		{"display name refused", "User <user@example.com>", "", ErrEmailMalformed},
		{"angle brackets refused", "<user@example.com>", "", ErrEmailMalformed},
		{"comment refused", "user@example.com (work)", "", ErrEmailMalformed},
		{"group refused", "team: user@example.com;", "", ErrEmailMalformed},
		{"surrounding space refused", " user@example.com", "", ErrEmailMalformed},
		{"no at sign", "user.example.com", "", ErrEmailMalformed},
		{"empty", "", "", ErrEmailMalformed},
		{"trailing dot refused", "user@example.com.", "", ErrEmailMalformed},
		{"domain literal refused", "user@[192.0.2.1]", "", ErrEmailMalformed},
		{"IDNA refused by name", "user@bücher.example", "", ErrEmailIDNA},
		{"IDNA refused by name, mixed case", "User@Bücher.Example", "", ErrEmailIDNA},
		{"254 bytes admitted", strings.Repeat("a", 64) + "@" + longDomain(254-65), strings.Repeat("a", 64) + "@" + longDomain(254-65), nil},
		{"255 bytes refused", strings.Repeat("a", 64) + "@" + longDomain(255-65), "", ErrEmailMalformed},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := CanonicalEmail(c.in)
			if c.err != nil {
				if !errors.Is(err, c.err) || got != "" {
					t.Fatalf("CanonicalEmail(%q) = %q, %v; want %v", c.in, got, err, c.err)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("CanonicalEmail(%q) = %q, %v; want %q", c.in, got, err, c.want)
			}
			// Canonical form is a fixpoint: uniqueness is over this string.
			if again, err := CanonicalEmail(got); err != nil || again != got {
				t.Fatalf("canonical form is not stable: %q -> %q, %v", got, again, err)
			}
		})
	}
}

// longDomain builds an ASCII domain of exactly n bytes from labels of at most
// 63 bytes.
func longDomain(n int) string {
	var labels []string
	for n > 0 {
		size := min(n, 63)
		if n-size == 1 { // a lone trailing byte cannot follow a dot
			size--
		}
		labels = append(labels, strings.Repeat("b", size))
		n -= size
		if n > 0 {
			n-- // the dot
		}
	}
	return strings.Join(labels, ".")
}

// Provider references (spec 2.6): slugs are unique per provider table only, so
// every surface names {kind, slug}; a bare slug resolves only when unambiguous.
func TestProviderRef(t *testing.T) {
	for _, c := range []struct {
		in   string
		want ProviderRef
		ok   bool
	}{
		{"oidc:corp", ProviderRef{Kind: ProviderKindOIDC, Slug: "corp"}, true},
		{"oauth2:github", ProviderRef{Kind: ProviderKindOAuth2, Slug: "github"}, true},
		{"saml:okta", ProviderRef{Kind: ProviderKindSAML, Slug: "okta"}, true},
		{"github", ProviderRef{Slug: "github"}, true},
		{"ldap:corp", ProviderRef{}, false},
		{"oidc:", ProviderRef{}, false},
		{":corp", ProviderRef{}, false},
		{"", ProviderRef{}, false},
		{"oidc:a:b", ProviderRef{}, false},
	} {
		got, err := ParseProviderRef(c.in)
		if c.ok != (err == nil) || got != c.want {
			t.Errorf("ParseProviderRef(%q) = %+v, %v; want %+v ok=%t", c.in, got, err, c.want, c.ok)
		}
		if c.ok && got.Kind != "" && got.String() != c.in {
			t.Errorf("%+v.String() = %q, want %q", got, got.String(), c.in)
		}
	}

	enabled := []ProviderRef{
		{Kind: ProviderKindOIDC, Slug: "corp"},
		{Kind: ProviderKindOIDC, Slug: "shared"},
		{Kind: ProviderKindOAuth2, Slug: "shared"},
		{Kind: ProviderKindOAuth2, Slug: "github"},
	}
	for _, c := range []struct {
		ref  ProviderRef
		want ProviderRef
		err  error
	}{
		{ProviderRef{Slug: "github"}, ProviderRef{Kind: ProviderKindOAuth2, Slug: "github"}, nil},
		{ProviderRef{Kind: ProviderKindOIDC, Slug: "shared"}, ProviderRef{Kind: ProviderKindOIDC, Slug: "shared"}, nil},
		{ProviderRef{Slug: "shared"}, ProviderRef{}, ErrAmbiguousProviderSlug},
		{ProviderRef{Slug: "missing"}, ProviderRef{}, ErrNotFound},
		{ProviderRef{Kind: ProviderKindSAML, Slug: "corp"}, ProviderRef{}, ErrNotFound},
	} {
		got, err := ResolveProviderRef(c.ref, enabled)
		if !errors.Is(err, c.err) || got != c.want {
			t.Errorf("ResolveProviderRef(%+v) = %+v, %v; want %+v, %v", c.ref, got, err, c.want, c.err)
		}
	}
}
