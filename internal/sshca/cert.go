package sshca

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/ssh"
)

// Bounds shared by profiles and requests. They are the contract's limits too.
const (
	MinTTL             = time.Minute
	MaxTTL             = 30 * 24 * time.Hour
	MaxPrincipals      = 32
	MaxPrincipalLength = 256
	MaxSourceAddresses = 32
	MaxForceCommand    = 1024
	// ClockSkew backdates valid_after so a host whose clock trails Hikyo's
	// by up to a minute still accepts a freshly issued certificate.
	ClockSkew = time.Minute
)

// Extension is the closed set of standard OpenSSH certificate extensions a
// profile may permit.
type Extension string

const (
	ExtX11Forwarding   Extension = "permit-X11-forwarding"
	ExtAgentForwarding Extension = "permit-agent-forwarding"
	ExtPortForwarding  Extension = "permit-port-forwarding"
	ExtPTY             Extension = "permit-pty"
	ExtUserRC          Extension = "permit-user-rc"
)

// Extensions returns the closed set in canonical order.
func Extensions() []Extension {
	return []Extension{ExtAgentForwarding, ExtPortForwarding, ExtPTY, ExtUserRC, ExtX11Forwarding}
}

// ErrConstraint is a request or profile outside the declared bounds.
var ErrConstraint = errors.New("sshca: constraint violated")

func constraintf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrConstraint, fmt.Sprintf(format, args...))
}

// ValidPrincipal reports whether s is an acceptable certificate principal: a
// non-empty printable string with no whitespace, commas (the OpenSSH list
// separator in authorized principals files), or control characters.
func ValidPrincipal(s string) bool {
	if s == "" || len(s) > MaxPrincipalLength {
		return false
	}
	for _, r := range s {
		if r == ',' || unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// ParseExtension accepts only the closed extension set.
func ParseExtension(s string) (Extension, error) {
	for _, e := range Extensions() {
		if string(e) == s {
			return e, nil
		}
	}
	return "", constraintf("unknown extension %q", s)
}

// ParseSourceAddresses parses and canonicalizes a CIDR list. A bare address
// is read as a single-host prefix.
func ParseSourceAddresses(in []string) ([]netip.Prefix, error) {
	if len(in) > MaxSourceAddresses {
		return nil, constraintf("at most %d source addresses", MaxSourceAddresses)
	}
	out := make([]netip.Prefix, 0, len(in))
	for _, s := range in {
		var p netip.Prefix
		var err error
		if strings.Contains(s, "/") {
			p, err = netip.ParsePrefix(s)
		} else {
			var a netip.Addr
			a, err = netip.ParseAddr(s)
			if err == nil {
				p = netip.PrefixFrom(a, a.BitLen())
			}
		}
		if err != nil || p.Addr().Zone() != "" {
			return nil, constraintf("invalid source address %q", s)
		}
		p = p.Masked()
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// prefixWithin reports whether inner is entirely contained in outer.
func prefixWithin(inner, outer netip.Prefix) bool {
	return inner.Addr().Is4() == outer.Addr().Is4() && inner.Bits() >= outer.Bits() && outer.Contains(inner.Addr())
}

// Profile is the signing policy a certificate is constrained by. The service
// loads it from the profile row; every field is already validated there.
type Profile struct {
	Principals      []string
	ForceCommand    string
	SourceAddresses []netip.Prefix
	Extensions      []Extension
	KeyAlgorithms   []Algorithm
	DefaultTTL      time.Duration
	MaxTTL          time.Duration
}

// ValidateProfile checks a profile's own shape.
func ValidateProfile(p Profile) error {
	if len(p.Principals) == 0 || len(p.Principals) > MaxPrincipals {
		return constraintf("a profile needs 1 to %d principals", MaxPrincipals)
	}
	seen := map[string]bool{}
	for _, pr := range p.Principals {
		if !ValidPrincipal(pr) {
			return constraintf("invalid principal %q", pr)
		}
		if seen[pr] {
			return constraintf("duplicate principal %q", pr)
		}
		seen[pr] = true
	}
	if len(p.ForceCommand) > MaxForceCommand || strings.ContainsAny(p.ForceCommand, "\x00\n\r") {
		return constraintf("force-command must be one line of at most %d bytes", MaxForceCommand)
	}
	if len(p.SourceAddresses) > MaxSourceAddresses {
		return constraintf("at most %d source addresses", MaxSourceAddresses)
	}
	if len(p.KeyAlgorithms) == 0 {
		return constraintf("a profile needs at least one key algorithm")
	}
	for _, a := range p.KeyAlgorithms {
		if _, err := ParseAlgorithm(string(a)); err != nil {
			return err
		}
	}
	for _, e := range p.Extensions {
		if _, err := ParseExtension(string(e)); err != nil {
			return err
		}
	}
	if p.MaxTTL < MinTTL || p.MaxTTL > MaxTTL {
		return constraintf("max_ttl must be between %s and %s", MinTTL, MaxTTL)
	}
	if p.DefaultTTL < MinTTL || p.DefaultTTL > p.MaxTTL {
		return constraintf("default_ttl must be between %s and max_ttl", MinTTL)
	}
	return nil
}

// Request is what the requester asks for. Empty fields take the profile's
// defaults; every non-empty field may only narrow the profile.
type Request struct {
	Principals      []string
	SourceAddresses []netip.Prefix
	Extensions      []Extension
	// ExtensionsSet distinguishes "no extensions" from "profile default".
	ExtensionsSet bool
	TTL           time.Duration
}

// Resolved is the exact content of a certificate after applying a request to
// a profile.
type Resolved struct {
	Principals      []string
	ForceCommand    string
	SourceAddresses []netip.Prefix
	Extensions      []Extension
	TTL             time.Duration
}

// Resolve applies a request to a profile, refusing anything that would widen
// it. The key algorithm is checked separately (CheckKeyAlgorithm).
func Resolve(p Profile, req Request) (Resolved, error) {
	var out Resolved
	switch {
	case len(req.Principals) > 0:
		seen := map[string]bool{}
		for _, pr := range req.Principals {
			if !slices.Contains(p.Principals, pr) {
				return Resolved{}, constraintf("principal %q is not allowed by the profile", pr)
			}
			if !seen[pr] {
				seen[pr] = true
				out.Principals = append(out.Principals, pr)
			}
		}
	case len(p.Principals) == 1:
		out.Principals = []string{p.Principals[0]}
	default:
		return Resolved{}, constraintf("the profile allows several principals; name the ones you need")
	}
	out.ForceCommand = p.ForceCommand

	if len(req.SourceAddresses) > 0 {
		if len(p.SourceAddresses) > 0 {
			for _, want := range req.SourceAddresses {
				ok := false
				for _, allowed := range p.SourceAddresses {
					if prefixWithin(want, allowed) {
						ok = true
						break
					}
				}
				if !ok {
					return Resolved{}, constraintf("source address %s is outside the profile's", want)
				}
			}
		}
		out.SourceAddresses = slices.Clone(req.SourceAddresses)
	} else {
		out.SourceAddresses = slices.Clone(p.SourceAddresses)
	}

	if req.ExtensionsSet {
		for _, e := range req.Extensions {
			if !slices.Contains(p.Extensions, e) {
				return Resolved{}, constraintf("extension %q is not allowed by the profile", e)
			}
			if !slices.Contains(out.Extensions, e) {
				out.Extensions = append(out.Extensions, e)
			}
		}
	} else {
		out.Extensions = slices.Clone(p.Extensions)
	}
	slices.Sort(out.Extensions)

	switch {
	case req.TTL == 0:
		out.TTL = p.DefaultTTL
	case req.TTL < MinTTL || req.TTL > p.MaxTTL:
		return Resolved{}, constraintf("ttl must be between %s and the profile's max_ttl %s", MinTTL, p.MaxTTL)
	default:
		out.TTL = req.TTL
	}
	return out, nil
}

// CheckKeyAlgorithm refuses a user key whose algorithm the profile does not
// allow.
func CheckKeyAlgorithm(p Profile, alg Algorithm) error {
	if !slices.Contains(p.KeyAlgorithms, alg) {
		return constraintf("key algorithm %s is not allowed by the profile", alg)
	}
	return nil
}

// NewSerial returns a uniformly random serial in [1, 2^63-1]. It fits a
// signed 64-bit column and is never 0, which OpenSSH KRLs cannot revoke.
func NewSerial() (uint64, error) {
	var b [8]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			return 0, fmt.Errorf("sshca: serial: %w", err)
		}
		s := binary.BigEndian.Uint64(b[:]) &^ (1 << 63)
		if s != 0 {
			return s, nil
		}
	}
}

// Template is everything a certificate states besides the key being certified.
type Template struct {
	Serial      uint64
	KeyID       string
	Resolved    Resolved
	ValidAfter  time.Time
	ValidBefore time.Time
}

// Sign builds and signs a user certificate for pub. The caller has already
// resolved the request against the profile.
func Sign(ca ssh.Signer, pub ssh.PublicKey, t Template) (*ssh.Certificate, error) {
	if t.Serial == 0 {
		return nil, constraintf("serial must be non-zero")
	}
	if !t.ValidBefore.After(t.ValidAfter) {
		return nil, constraintf("validity window is empty")
	}
	critical := map[string]string{}
	if t.Resolved.ForceCommand != "" {
		critical["force-command"] = t.Resolved.ForceCommand
	}
	if len(t.Resolved.SourceAddresses) > 0 {
		parts := make([]string, 0, len(t.Resolved.SourceAddresses))
		for _, p := range t.Resolved.SourceAddresses {
			parts = append(parts, p.String())
		}
		critical["source-address"] = strings.Join(parts, ",")
	}
	extensions := map[string]string{}
	for _, e := range t.Resolved.Extensions {
		extensions[string(e)] = ""
	}
	cert := &ssh.Certificate{
		Key:             pub,
		Serial:          t.Serial,
		CertType:        ssh.UserCert,
		KeyId:           t.KeyID,
		ValidPrincipals: slices.Clone(t.Resolved.Principals),
		ValidAfter:      uint64(t.ValidAfter.Unix()),
		ValidBefore:     uint64(t.ValidBefore.Unix()),
		Permissions: ssh.Permissions{
			CriticalOptions: critical,
			Extensions:      extensions,
		},
	}
	if err := cert.SignCert(rand.Reader, ca); err != nil {
		return nil, fmt.Errorf("sshca: sign certificate: %w", err)
	}
	return cert, nil
}
