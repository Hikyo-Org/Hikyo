package domain

import (
	"fmt"
	"net/mail"
	"strings"
)

// ErrEmailMalformed refuses an address that is not a bare canonical addr-spec.
var ErrEmailMalformed = fmt.Errorf("%w: malformed email address", ErrInvalid)

// ErrEmailIDNA refuses an internationalized domain: v1 admits ASCII domains
// only (social-signin spec 2.5, a named refusal).
var ErrEmailIDNA = fmt.Errorf("%w: internationalized email domains are not supported", ErrInvalid)

// maxEmailBytes bounds the canonical form (RFC 5321 path limit less brackets).
const maxEmailBytes = 254

// CanonicalEmail returns the stored form of an email address for
// accounts.email and registration_signups.email (social-signin spec 2.5):
// exactly one bare addr-spec as net/mail parses it, with no display name,
// comment, group, angle brackets or surrounding space; an ASCII domain,
// lowercased, with no trailing dot and no address literal; the local part
// byte-preserved; at most 254 bytes. Uniqueness and domain-allowlist matching
// are over this string.
func CanonicalEmail(raw string) (string, error) {
	if raw == "" || len(raw) > maxEmailBytes {
		return "", ErrEmailMalformed
	}
	addr, err := mail.ParseAddress(raw)
	// Anything ParseAddress normalized away (display name, comment, brackets,
	// folding space) makes Address differ from the input: refuse it rather
	// than silently keep a different address than the one presented.
	if err != nil || addr.Name != "" || addr.Address != raw {
		return "", ErrEmailMalformed
	}
	at := strings.LastIndexByte(raw, '@')
	local, host := raw[:at], raw[at+1:]
	for i := range len(host) {
		if host[i] >= 0x80 {
			return "", ErrEmailIDNA
		}
	}
	if host == "" || strings.HasSuffix(host, ".") || strings.HasPrefix(host, "[") {
		return "", ErrEmailMalformed
	}
	return local + "@" + strings.ToLower(host), nil
}

// ErrAmbiguousProviderSlug refuses a bare slug that names providers of more
// than one kind.
var ErrAmbiguousProviderSlug = fmt.Errorf("%w: ambiguous provider slug", ErrInvalid)

// ProviderRef names a federated provider on every request, response and flag
// (social-signin spec 2.6). Slugs are unique per provider table only, so the
// kind is part of the name. An empty Kind is a bare slug awaiting resolution.
type ProviderRef struct {
	Kind ProviderKind
	Slug string
}

// String renders the `<kind>:<slug>` form.
func (r ProviderRef) String() string { return string(r.Kind) + ":" + r.Slug }

// ParseProviderRef reads `<kind>:<slug>` or a bare `<slug>`.
func ParseProviderRef(s string) (ProviderRef, error) {
	kind, slug, qualified := strings.Cut(s, ":")
	if !qualified {
		kind, slug = "", s
	}
	if slug == "" || strings.Contains(slug, ":") {
		return ProviderRef{}, fmt.Errorf("%w: provider reference %q", ErrInvalid, s)
	}
	switch ProviderKind(kind) {
	case "", ProviderOIDC, ProviderSAML, ProviderOAuth2:
	default:
		return ProviderRef{}, fmt.Errorf("%w: unknown provider kind %q", ErrInvalid, kind)
	}
	if qualified && kind == "" {
		return ProviderRef{}, fmt.Errorf("%w: provider reference %q", ErrInvalid, s)
	}
	return ProviderRef{Kind: ProviderKind(kind), Slug: slug}, nil
}

// ResolveProviderRef finds ref among the enabled providers. A bare slug
// resolves only when exactly one kind carries it.
func ResolveProviderRef(ref ProviderRef, enabled []ProviderRef) (ProviderRef, error) {
	var found []ProviderRef
	for _, p := range enabled {
		if p.Slug == ref.Slug && (ref.Kind == "" || p.Kind == ref.Kind) {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 0:
		return ProviderRef{}, ErrNotFound
	case 1:
		return found[0], nil
	default:
		return ProviderRef{}, ErrAmbiguousProviderSlug
	}
}
