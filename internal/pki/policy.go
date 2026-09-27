package pki

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"
)

// MaxLeafTTL is the hard ceiling on any profile's max_ttl: the PKI is for
// short-lived service identity certificates (ADR D5).
const MaxLeafTTL = 90 * 24 * time.Hour

// MinLeafTTL is the floor on any requested or configured leaf lifetime.
const MinLeafTTL = 5 * time.Minute

// KeyUsage is the closed leaf key-usage enum.
type KeyUsage string

const (
	UsageDigitalSignature KeyUsage = "digital-signature"
	UsageKeyEncipherment  KeyUsage = "key-encipherment"
	UsageKeyAgreement     KeyUsage = "key-agreement"
)

// KeyUsages is the closed set, in canonical order.
var KeyUsages = []KeyUsage{UsageDigitalSignature, UsageKeyEncipherment, UsageKeyAgreement}

// ExtKeyUsage is the closed leaf extended-key-usage enum.
type ExtKeyUsage string

const (
	ExtUsageServerAuth ExtKeyUsage = "server-auth"
	ExtUsageClientAuth ExtKeyUsage = "client-auth"
)

// ExtKeyUsages is the closed set, in canonical order.
var ExtKeyUsages = []ExtKeyUsage{ExtUsageServerAuth, ExtUsageClientAuth}

var (
	// ErrInvalidPolicy is a malformed profile.
	ErrInvalidPolicy = errors.New("pki: invalid certificate profile")
	// ErrRefused is a request the profile does not permit.
	ErrRefused = errors.New("pki: request refused by the certificate profile")
	// ErrWidening is a profile update that is not provably a narrowing. It
	// covers both real widening and anything whose containment cannot be
	// decided: ambiguous changes fail closed (ADR D5).
	ErrWidening = errors.New("pki: profile change is not a narrowing")
)

// Policy is a certificate profile's closed issuance policy (ADR D5). Name
// patterns are deliberately not regular expressions, so Narrows is decidable.
type Policy struct {
	AllowedIssuers     []string
	DNSPatterns        []string
	IPRanges           []string
	URIPatterns        []string
	AllowWildcardNames bool
	KeyAlgorithms      []KeyAlgorithm
	KeyUsages          []KeyUsage
	ExtKeyUsages       []ExtKeyUsage
	MaxTTL             time.Duration
	DefaultTTL         time.Duration
	RenewWindow        time.Duration
	AllowCSR           bool
	AllowGeneratedKey  bool
	MachineIssuance    bool
	Organization       string
}

// Normalize returns the policy with every list lowercased where that is the
// canonical spelling, de-duplicated and sorted, so stored and compared forms
// are stable. It does not validate.
func (p Policy) Normalize() Policy {
	out := p
	out.AllowedIssuers = sortedUnique(p.AllowedIssuers, strings.TrimSpace)
	out.DNSPatterns = sortedUnique(p.DNSPatterns, func(s string) string {
		return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
	})
	out.IPRanges = sortedUnique(p.IPRanges, func(s string) string {
		if _, network, err := net.ParseCIDR(strings.TrimSpace(s)); err == nil {
			return network.String()
		}
		return strings.TrimSpace(s)
	})
	out.URIPatterns = sortedUnique(p.URIPatterns, strings.TrimSpace)
	out.KeyAlgorithms = sortedUnique(p.KeyAlgorithms, func(a KeyAlgorithm) KeyAlgorithm { return a })
	out.KeyUsages = sortedUnique(p.KeyUsages, func(u KeyUsage) KeyUsage { return u })
	out.ExtKeyUsages = sortedUnique(p.ExtKeyUsages, func(u ExtKeyUsage) ExtKeyUsage { return u })
	out.Organization = strings.TrimSpace(p.Organization)
	return out
}

func sortedUnique[T ~string](in []T, canon func(T) T) []T {
	out := make([]T, 0, len(in))
	for _, v := range in {
		out = append(out, canon(v))
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Validate checks that a (normalized) policy is well formed.
func (p Policy) Validate() error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalidPolicy, fmt.Sprintf(format, args...))
	}
	if len(p.AllowedIssuers) == 0 {
		return fail("allowed_issuers must name at least one issuer")
	}
	for _, name := range p.AllowedIssuers {
		if err := ValidateName(name); err != nil {
			return fail("allowed issuer %q: %v", name, err)
		}
	}
	if len(p.DNSPatterns)+len(p.IPRanges)+len(p.URIPatterns) == 0 {
		return fail("at least one of dns_patterns, ip_ranges or uri_patterns is required")
	}
	for _, pattern := range p.DNSPatterns {
		if err := validateDNSPattern(pattern); err != nil {
			return fail("dns pattern %q: %v", pattern, err)
		}
	}
	for _, cidr := range p.IPRanges {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return fail("ip range %q is not a CIDR", cidr)
		}
	}
	for _, pattern := range p.URIPatterns {
		if err := validateURIPattern(pattern); err != nil {
			return fail("uri pattern %q: %v", pattern, err)
		}
	}
	if len(p.KeyAlgorithms) == 0 {
		return fail("key_algorithms must name at least one algorithm")
	}
	for _, algorithm := range p.KeyAlgorithms {
		if !slices.Contains(LeafKeyAlgorithms, algorithm) {
			return fail("unknown key algorithm %q", algorithm)
		}
	}
	if len(p.KeyUsages) == 0 {
		return fail("key_usages must name at least one usage")
	}
	for _, usage := range p.KeyUsages {
		if !slices.Contains(KeyUsages, usage) {
			return fail("unknown key usage %q", usage)
		}
	}
	if len(p.ExtKeyUsages) == 0 {
		return fail("ext_key_usages must name at least one usage")
	}
	for _, usage := range p.ExtKeyUsages {
		if !slices.Contains(ExtKeyUsages, usage) {
			return fail("unknown extended key usage %q", usage)
		}
	}
	switch {
	case p.MaxTTL < MinLeafTTL || p.MaxTTL > MaxLeafTTL:
		return fail("max_ttl must be between %s and %s", MinLeafTTL, MaxLeafTTL)
	case p.DefaultTTL < MinLeafTTL || p.DefaultTTL > p.MaxTTL:
		return fail("default_ttl must be between %s and max_ttl", MinLeafTTL)
	case p.RenewWindow <= 0 || p.RenewWindow >= p.MaxTTL:
		return fail("renew_window must be positive and shorter than max_ttl")
	}
	if !p.AllowCSR && !p.AllowGeneratedKey {
		return fail("at least one of allow_csr or allow_generated_key is required")
	}
	if len(p.Organization) > 64 || strings.ContainsFunc(p.Organization, isControl) {
		return fail("organization must be at most 64 printable characters")
	}
	return nil
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

// ValidateName checks an issuer or profile name: 1-63 of [a-z0-9-], not
// starting or ending with a hyphen.
func ValidateName(name string) error {
	if !validLabel(name) {
		return errors.New("must be 1-63 lowercase letters, digits or hyphens, not starting or ending with a hyphen")
	}
	return nil
}

func validLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
		return false
	}
	for _, r := range label {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// validDNSName checks a concrete (non-pattern) lowercase DNS name.
func validDNSName(name string) bool {
	if len(name) == 0 || len(name) > 253 {
		return false
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if !validLabel(label) {
			return false
		}
	}
	return true
}

// validateDNSPattern accepts an exact name or "*." followed by an exact name.
func validateDNSPattern(pattern string) error {
	if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
		if !validDNSName(suffix) {
			return errors.New("the part after '*.' must be a DNS name with at least two labels")
		}
		return nil
	}
	if !validDNSName(pattern) {
		return errors.New("must be a DNS name with at least two labels, or '*.' followed by one")
	}
	return nil
}

// validateURIPattern accepts an exact absolute URI or one ending in "/*"
// (a path-prefix pattern, for SPIFFE IDs).
func validateURIPattern(pattern string) error {
	base := pattern
	if prefix, ok := strings.CutSuffix(pattern, "/*"); ok {
		base = prefix + "/"
	}
	if strings.Contains(base, "*") {
		return errors.New("'*' is only allowed as a trailing '/*'")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("must be an absolute URI with a host and no query, fragment or userinfo")
	}
	return nil
}

// dnsPatternMatches reports whether a requested name matches a pattern. A
// requested wildcard name ("*.x.y") only matches the identical wildcard
// pattern, and only when the profile allows wildcard names.
func dnsPatternMatches(pattern, name string, allowWildcard bool) bool {
	if strings.HasPrefix(name, "*.") {
		return allowWildcard && pattern == name
	}
	if suffix, ok := strings.CutPrefix(pattern, "*."); ok {
		label, rest, found := strings.Cut(name, ".")
		return found && rest == suffix && validLabel(label)
	}
	return pattern == name
}

// dnsPatternCovers reports whether every name newer admits is admitted by
// older.
func dnsPatternCovers(older, newer string) bool {
	if older == newer {
		return true
	}
	// An exact pattern is covered by the wildcard directly above it.
	if !strings.HasPrefix(newer, "*.") {
		return dnsPatternMatches(older, newer, false)
	}
	return false
}

func uriPatternMatches(pattern, uri string) bool {
	if prefix, ok := strings.CutSuffix(pattern, "*"); ok {
		return strings.HasPrefix(uri, prefix) && len(uri) > len(prefix)
	}
	return pattern == uri
}

func uriPatternCovers(older, newer string) bool {
	if older == newer {
		return true
	}
	olderPrefix, olderIsPrefix := strings.CutSuffix(older, "*")
	if !olderIsPrefix {
		return false
	}
	newerBase := strings.TrimSuffix(newer, "*")
	return strings.HasPrefix(newerBase, olderPrefix) && len(newerBase) > len(olderPrefix)
}

func ipRangeCovers(older, newer string) bool {
	_, olderNet, err1 := net.ParseCIDR(older)
	_, newerNet, err2 := net.ParseCIDR(newer)
	if err1 != nil || err2 != nil {
		return false
	}
	olderOnes, olderBits := olderNet.Mask.Size()
	newerOnes, newerBits := newerNet.Mask.Size()
	return olderBits == newerBits && olderOnes <= newerOnes && olderNet.Contains(newerNet.IP)
}

// Narrows returns nil only when every certificate newer could permit, older
// also permits. It is the fail-closed gate on profile updates: a widening, or
// any change it cannot prove is a narrowing, is refused with ErrWidening and
// the first reason found. Both policies must be valid; this function
// normalizes them but does not validate them.
func Narrows(older, newer Policy) error {
	older, newer = older.Normalize(), newer.Normalize()
	widen := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrWidening, fmt.Sprintf(format, args...))
	}
	for _, issuer := range newer.AllowedIssuers {
		if !slices.Contains(older.AllowedIssuers, issuer) {
			return widen("adds allowed issuer %q", issuer)
		}
	}
	for _, pattern := range newer.DNSPatterns {
		if !slices.ContainsFunc(older.DNSPatterns, func(o string) bool { return dnsPatternCovers(o, pattern) }) {
			return widen("dns pattern %q is not covered by the current profile", pattern)
		}
	}
	for _, cidr := range newer.IPRanges {
		if !slices.ContainsFunc(older.IPRanges, func(o string) bool { return ipRangeCovers(o, cidr) }) {
			return widen("ip range %q is not covered by the current profile", cidr)
		}
	}
	for _, pattern := range newer.URIPatterns {
		if !slices.ContainsFunc(older.URIPatterns, func(o string) bool { return uriPatternCovers(o, pattern) }) {
			return widen("uri pattern %q is not covered by the current profile", pattern)
		}
	}
	for _, algorithm := range newer.KeyAlgorithms {
		if !slices.Contains(older.KeyAlgorithms, algorithm) {
			return widen("adds key algorithm %q", algorithm)
		}
	}
	for _, usage := range newer.KeyUsages {
		if !slices.Contains(older.KeyUsages, usage) {
			return widen("adds key usage %q", usage)
		}
	}
	for _, usage := range newer.ExtKeyUsages {
		if !slices.Contains(older.ExtKeyUsages, usage) {
			return widen("adds extended key usage %q", usage)
		}
	}
	switch {
	case newer.AllowWildcardNames && !older.AllowWildcardNames:
		return widen("enables wildcard names")
	case newer.MaxTTL > older.MaxTTL:
		return widen("raises max_ttl")
	case newer.RenewWindow > older.RenewWindow:
		return widen("lengthens renew_window")
	case newer.AllowCSR && !older.AllowCSR:
		return widen("enables CSR issuance")
	case newer.AllowGeneratedKey && !older.AllowGeneratedKey:
		return widen("enables generated-key issuance")
	case newer.MachineIssuance && !older.MachineIssuance:
		return widen("enables machine issuance")
	case newer.Organization != older.Organization:
		// A different fixed subject is neither a subset nor a superset of the
		// old one: ambiguous, so it fails closed.
		return widen("changes the fixed organization")
	}
	return nil
}

// Request is one leaf request, after the service resolved its public key.
type Request struct {
	CommonName  string
	DNSNames    []string
	IPAddresses []string
	URIs        []string
	TTL         time.Duration // zero means the profile's default_ttl
	PublicKey   any
	Generated   bool // the server generates the key (else a CSR supplied it)
	// Renewal re-signs an existing certificate's public key: no key is
	// delivered, so the CSR and generated-key delivery gates do not apply.
	// Every name, algorithm and TTL rule still does, against the CURRENT
	// profile.
	Renewal bool
}

// Resolved is a request the profile admits, in canonical form.
type Resolved struct {
	CommonName   string
	DNSNames     []string
	IPAddresses  []net.IP
	URIs         []*url.URL
	TTL          time.Duration
	KeyAlgorithm KeyAlgorithm
}

// Check admits or refuses a request against the (normalized, valid) policy.
// Every refusal wraps ErrRefused and names the first offending field.
func (p Policy) Check(req Request) (Resolved, error) {
	refuse := func(format string, args ...any) (Resolved, error) {
		return Resolved{}, fmt.Errorf("%w: %s", ErrRefused, fmt.Sprintf(format, args...))
	}
	if !req.Renewal && req.Generated && !p.AllowGeneratedKey {
		return refuse("the profile does not allow server-generated keys")
	}
	if !req.Renewal && !req.Generated && !p.AllowCSR {
		return refuse("the profile does not allow CSR issuance")
	}
	algorithm, err := AlgorithmOf(req.PublicKey)
	if err != nil {
		return refuse("the public key algorithm is not supported")
	}
	if !slices.Contains(p.KeyAlgorithms, algorithm) {
		return refuse("key algorithm %s is not allowed", algorithm)
	}
	if applicableKeyUsage(req.PublicKey, p.KeyUsages) == 0 {
		return refuse("no configured key usage applies to key algorithm %s", algorithm)
	}
	out := Resolved{KeyAlgorithm: algorithm}
	if len(req.DNSNames)+len(req.IPAddresses)+len(req.URIs) == 0 {
		return refuse("at least one subject alternative name is required")
	}
	if len(req.DNSNames)+len(req.IPAddresses)+len(req.URIs) > 100 {
		return refuse("at most 100 subject alternative names are allowed")
	}
	for _, raw := range req.DNSNames {
		name := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
		wildcardSuffix, isWildcard := strings.CutPrefix(name, "*.")
		if !(validDNSName(name) || isWildcard && validDNSName(wildcardSuffix)) {
			return refuse("dns name %q is not a valid DNS name", raw)
		}
		if !slices.ContainsFunc(p.DNSPatterns, func(pattern string) bool {
			return dnsPatternMatches(pattern, name, p.AllowWildcardNames)
		}) {
			return refuse("dns name %q is not allowed", raw)
		}
		out.DNSNames = append(out.DNSNames, name)
	}
	for _, raw := range req.IPAddresses {
		ip := net.ParseIP(strings.TrimSpace(raw))
		if ip == nil {
			return refuse("ip address %q is not valid", raw)
		}
		if !slices.ContainsFunc(p.IPRanges, func(cidr string) bool {
			_, network, err := net.ParseCIDR(cidr)
			return err == nil && network.Contains(ip)
		}) {
			return refuse("ip address %q is not allowed", raw)
		}
		out.IPAddresses = append(out.IPAddresses, ip)
	}
	for _, raw := range req.URIs {
		uri := strings.TrimSpace(raw)
		parsed, err := url.Parse(uri)
		// Only canonical URIs reach the prefix match: a query, fragment, opaque
		// form, dot segment or alternate spelling could be normalized by a
		// relying party into a path outside the profile's pattern.
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || strings.Contains(uri, "*") ||
			parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" || parsed.String() != uri ||
			slices.ContainsFunc(strings.Split(parsed.Path, "/"), func(s string) bool { return s == "." || s == ".." }) {
			return refuse("uri %q is not a valid absolute URI", raw)
		}
		if !slices.ContainsFunc(p.URIPatterns, func(pattern string) bool { return uriPatternMatches(pattern, uri) }) {
			return refuse("uri %q is not allowed", raw)
		}
		out.URIs = append(out.URIs, parsed)
	}
	if cn := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(req.CommonName)), "."); cn != "" {
		if !slices.Contains(out.DNSNames, cn) {
			return refuse("common name must equal one of the requested dns names")
		}
		out.CommonName = cn
	}
	out.TTL = req.TTL
	if out.TTL == 0 {
		out.TTL = p.DefaultTTL
	}
	if out.TTL < MinLeafTTL || out.TTL > p.MaxTTL {
		return refuse("ttl must be between %s and the profile's max_ttl %s", MinLeafTTL, p.MaxTTL)
	}
	return out, nil
}
